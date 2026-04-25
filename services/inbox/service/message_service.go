/*
 * Copyright (c) 2026 nyklabs.com. All rights reserved.
 *
 * Licensed under the nGX Commercial Source License v1.0.
 * See LICENSE file in the project root for full license information.
 */

package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"agentmail/pkg/auth"
	dbpkg "agentmail/pkg/db"
	"agentmail/pkg/events"
	"agentmail/pkg/models"
	s3pkg "agentmail/pkg/s3"
	"agentmail/services/inbox/store"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// generateMessageID produces a Message-ID value stored in the DB (no angle
// brackets). The MIME builder wraps it with <> when writing the email header,
// consistent with how the inbound MIME parser strips <> before storing.
func generateMessageID() string {
	return fmt.Sprintf("%s@nGX", uuid.New().String())
}

// AttachmentRequest is an inline base64-encoded attachment for send/draft requests.
type AttachmentRequest struct {
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Content     string `json:"content"` // base64-encoded data
}

// SendMessageRequest is the input for sending a new message.
type SendMessageRequest struct {
	To          []models.EmailAddress `json:"to"`
	CC          []models.EmailAddress `json:"cc"`
	BCC         []models.EmailAddress `json:"bcc"`
	Subject     string                `json:"subject"`
	BodyText    string                `json:"body_text"`
	BodyHTML    string                `json:"body_html"`
	ReplyToID   *uuid.UUID            `json:"reply_to_id,omitempty"`
	Metadata    map[string]any        `json:"metadata,omitempty"`
	Attachments []AttachmentRequest   `json:"attachments,omitempty"`
}

// OutboundJob is the payload published to the email outbound queue.
// BCC is intentionally omitted — the pipeline reads it from the DB message record
// to avoid exposing BCC recipients in a durable Kafka log.
type OutboundJob struct {
	MessageID  string               `json:"message_id"`
	OrgID      string               `json:"org_id"`
	InboxID    string               `json:"inbox_id"`
	ThreadID   string               `json:"thread_id"`
	From       models.EmailAddress  `json:"from"`
	To         []models.EmailAddress `json:"to"`
	CC         []models.EmailAddress `json:"cc"`
	Subject    string               `json:"subject"`
	BodyText   string               `json:"body_text"`
	BodyHTML   string               `json:"body_html"`
	InReplyTo  string               `json:"in_reply_to,omitempty"`
	References []string             `json:"references,omitempty"`
}

// MessageService handles message business logic.
type MessageService struct {
	pool             *pgxpool.Pool
	messageStore     store.MessageStore
	threadStore      store.ThreadStore
	inboxStore       store.InboxStore
	outboundProducer events.OutboundPublisher
	eventPublisher   events.EventPublisher
	attachmentsS3    *s3pkg.Client
}

// NewMessageService creates a new MessageService.
func NewMessageService(
	pool *pgxpool.Pool,
	messageStore store.MessageStore,
	threadStore store.ThreadStore,
	inboxStore store.InboxStore,
	outboundProducer events.OutboundPublisher,
	eventPublisher events.EventPublisher,
	attachmentsS3 *s3pkg.Client,
) *MessageService {
	return &MessageService{
		pool:             pool,
		messageStore:     messageStore,
		threadStore:      threadStore,
		inboxStore:       inboxStore,
		outboundProducer: outboundProducer,
		eventPublisher:   eventPublisher,
		attachmentsS3:    attachmentsS3,
	}
}

// List returns a paginated list of messages in a thread.
func (s *MessageService) List(ctx context.Context, claims *auth.Claims, threadID uuid.UUID, limit int, cursor string) ([]*models.Message, string, error) {
	var msgs []*models.Message
	var nextCursor string
	err := dbpkg.WithOrgTx(ctx, s.pool, claims.OrgID, func(tx pgx.Tx) error {
		var err error
		msgs, nextCursor, err = s.messageStore.List(ctx, tx, claims.OrgID, threadID, limit, cursor)
		return err
	})
	if err != nil {
		return nil, "", fmt.Errorf("list messages: %w", err)
	}
	return msgs, nextCursor, nil
}

// Get retrieves a message by ID.
func (s *MessageService) Get(ctx context.Context, claims *auth.Claims, messageID uuid.UUID) (*models.Message, error) {
	var msg *models.Message
	err := dbpkg.WithOrgTx(ctx, s.pool, claims.OrgID, func(tx pgx.Tx) error {
		var err error
		msg, err = s.messageStore.GetByID(ctx, tx, claims.OrgID, messageID)
		if err != nil {
			return err
		}
		atts, err := s.messageStore.ListAttachments(ctx, tx, claims.OrgID, messageID)
		if err != nil {
			return err
		}
		for _, a := range atts {
			msg.Attachments = append(msg.Attachments, *a)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("get message: %w", err)
	}
	return msg, nil
}

// Send creates an outbound message and publishes it to the email outbound queue.
func (s *MessageService) Send(ctx context.Context, claims *auth.Claims, inboxID uuid.UUID, req SendMessageRequest) (*models.Message, error) {
	var msg *models.Message
	now := time.Now().UTC()

	// Pre-generate the message ID so we can use it as the S3 key prefix before
	// the DB transaction opens (S3 uploads must not run inside a transaction).
	msgID := uuid.New()

	// Upload inline attachments to S3 before the transaction.
	type attUpload struct {
		id          uuid.UUID
		s3Key       string
		filename    string
		contentType string
		sizeBytes   int64
	}
	var attUploads []attUpload
	for _, a := range req.Attachments {
		data, err := base64.StdEncoding.DecodeString(a.Content)
		if err != nil {
			return nil, fmt.Errorf("decode attachment %q: %w", a.Filename, err)
		}
		ct := a.ContentType
		if ct == "" {
			ct = "application/octet-stream"
		}
		key := fmt.Sprintf("%s/%s/%s", claims.OrgID, msgID, a.Filename)
		if s.attachmentsS3 != nil {
			if err := s.attachmentsS3.Upload(ctx, key, data, ct); err != nil {
				return nil, fmt.Errorf("upload attachment %q: %w", a.Filename, err)
			}
		}
		attUploads = append(attUploads, attUpload{
			id:          uuid.New(),
			s3Key:       key,
			filename:    a.Filename,
			contentType: ct,
			sizeBytes:   int64(len(data)),
		})
	}

	err := dbpkg.WithOrgTx(ctx, s.pool, claims.OrgID, func(tx pgx.Tx) error {
		// Load the inbox to get the From address.
		inbox, err := s.inboxStore.GetByID(ctx, tx, claims.OrgID, inboxID)
		if err != nil {
			return fmt.Errorf("get inbox: %w", err)
		}

		var thread *models.Thread
		var inReplyTo string
		var references []string

		if req.ReplyToID != nil {
			// Load the parent message and its thread.
			parent, err := s.messageStore.GetByID(ctx, tx, claims.OrgID, *req.ReplyToID)
			if err != nil {
				return fmt.Errorf("get reply-to message: %w", err)
			}
			inReplyTo = parent.MessageID
			// RFC 5322: References = parent.References + parent.MessageID
			references = append(parent.References, parent.MessageID)
			thread, err = s.threadStore.GetByID(ctx, tx, claims.OrgID, parent.ThreadID)
			if err != nil {
				return fmt.Errorf("get thread: %w", err)
			}
		} else {
			// Build participants from sender + recipients.
			var participants []models.EmailAddress
			seen := make(map[string]bool)
			addParticipant := func(addr models.EmailAddress) {
				if addr.Email != "" && !seen[addr.Email] {
					seen[addr.Email] = true
					participants = append(participants, addr)
				}
			}
			addParticipant(models.EmailAddress{Email: inbox.Email, Name: inbox.DisplayName})
			for _, a := range req.To {
				addParticipant(a)
			}
			for _, a := range req.CC {
				addParticipant(a)
			}

			// Create a new thread.
			thread = &models.Thread{
				ID:           uuid.New(),
				OrgID:        claims.OrgID,
				InboxID:      inboxID,
				Subject:      req.Subject,
				Snippet:      snippetFrom(req.BodyText),
				Status:       models.ThreadStatusOpen,
				IsRead:       true,
				MessageCount: 0,
				Participants: participants,
				CreatedAt:    now,
				UpdatedAt:    now,
			}
			if err := s.threadStore.Create(ctx, tx, thread); err != nil {
				return fmt.Errorf("create thread: %w", err)
			}
		}

		metadata := req.Metadata
		if metadata == nil {
			metadata = map[string]any{}
		}

		msg = &models.Message{
			ID:             msgID,
			OrgID:          claims.OrgID,
			InboxID:        inboxID,
			ThreadID:       thread.ID,
			MessageID:      generateMessageID(),
			Direction:      models.DirectionOutbound,
			Status:         models.MessageStatusSending,
			From: models.EmailAddress{
				Email: inbox.Email,
				Name:  inbox.DisplayName,
			},
			To:             req.To,
			Cc:             req.CC,
			Bcc:            req.BCC,
			Subject:        req.Subject,
			InReplyTo:      inReplyTo,
			References:     references,
			Metadata:       metadata,
			Snippet:        snippetFrom(req.BodyText),
			HasAttachments: len(attUploads) > 0,
			SentAt:         &now,
			CreatedAt:      now,
			UpdatedAt:      now,
		}

		if err := s.messageStore.Create(ctx, tx, msg); err != nil {
			return fmt.Errorf("create message: %w", err)
		}

		for _, a := range attUploads {
			att := &models.Attachment{
				ID:          a.id,
				OrgID:       claims.OrgID,
				MessageID:   &msgID,
				Filename:    a.filename,
				ContentType: a.contentType,
				SizeBytes:   a.sizeBytes,
				S3Key:       a.s3Key,
				CreatedAt:   now,
			}
			if err := s.messageStore.CreateAttachment(ctx, tx, att); err != nil {
				return fmt.Errorf("create attachment record: %w", err)
			}
		}

		// Update thread message count and snippet.
		if err := s.threadStore.IncrMessageCount(ctx, tx, thread.ID, now, msg.Snippet); err != nil {
			return fmt.Errorf("incr message count: %w", err)
		}

		// Publish outbound job. BCC is omitted from the Kafka payload — the pipeline
		// reads BCC from the DB message record to avoid exposing it in a durable log.
		job := OutboundJob{
			MessageID:  msg.ID.String(),
			OrgID:      claims.OrgID.String(),
			InboxID:    inboxID.String(),
			ThreadID:   thread.ID.String(),
			From:       msg.From,
			To:         req.To,
			CC:         req.CC,
			Subject:    req.Subject,
			BodyText:   req.BodyText,
			BodyHTML:   req.BodyHTML,
			InReplyTo:  inReplyTo,
			References: references,
		}
		if err := s.outboundProducer.Publish(ctx, msg.ID.String(), mustMarshal(job)); err != nil {
			// Log but don't fail - the message is recorded; the pipeline can retry.
			_ = err
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("send message: %w", err)
	}

	// Publish MessageSentEvent immediately after DB insert so downstream consumers
	// (embedder, webhooks) receive the full message snapshot without a follow-up call.
	msgAtts := make([]models.Attachment, len(attUploads))
	for i, a := range attUploads {
		msgAtts[i] = models.Attachment{
			ID:          a.id,
			OrgID:       msg.OrgID,
			MessageID:   &msg.ID,
			Filename:    a.filename,
			ContentType: a.contentType,
			SizeBytes:   a.sizeBytes,
			S3Key:       a.s3Key,
			CreatedAt:   msg.CreatedAt,
		}
	}
	_ = s.eventPublisher.PublishEvent(ctx, &events.MessageSentEvent{
		BaseEvent: events.NewBase(events.EventMessageSent, msg.OrgID),
		Data: events.MessageSentData{
			MessagePayload: events.MessagePayloadFromModel(
				msg, req.BodyText, req.BodyHTML, snippetFrom(req.BodyText), msgAtts,
			),
		},
	})

	return msg, nil
}

// snippetFrom returns a short preview from the body text.
func snippetFrom(body string) string {
	const maxLen = 200
	if len(body) <= maxLen {
		return body
	}
	return body[:maxLen]
}

// mustMarshal marshals v to JSON, ignoring errors (safe for well-defined structs).
func mustMarshal(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}
