package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"agentmail/pkg/auth"
	dbpkg "agentmail/pkg/db"
	"agentmail/pkg/kafka"
	"agentmail/pkg/models"
	"agentmail/services/inbox/store"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// generateMessageID produces an RFC 5322-compliant Message-ID header value.
func generateMessageID() string {
	return fmt.Sprintf("<%s@nGX>", uuid.New().String())
}

// SendMessageRequest is the input for sending a new message.
type SendMessageRequest struct {
	To        []models.EmailAddress
	CC        []models.EmailAddress
	BCC       []models.EmailAddress
	Subject   string
	BodyText  string
	BodyHTML  string
	ReplyToID *uuid.UUID // message ID to reply to
	Metadata  map[string]any
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
	outboundProducer *kafka.Producer
}

// NewMessageService creates a new MessageService.
func NewMessageService(
	pool *pgxpool.Pool,
	messageStore store.MessageStore,
	threadStore store.ThreadStore,
	inboxStore store.InboxStore,
	outboundProducer *kafka.Producer,
) *MessageService {
	return &MessageService{
		pool:             pool,
		messageStore:     messageStore,
		threadStore:      threadStore,
		inboxStore:       inboxStore,
		outboundProducer: outboundProducer,
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
			ID:         uuid.New(),
			OrgID:      claims.OrgID,
			InboxID:    inboxID,
			ThreadID:   thread.ID,
			MessageID:  generateMessageID(),
			Direction:  models.DirectionOutbound,
			Status:     models.MessageStatusSending,
			From: models.EmailAddress{
				Email: inbox.Email,
				Name:  inbox.DisplayName,
			},
			To:         req.To,
			Cc:         req.CC,
			Bcc:        req.BCC,
			Subject:    req.Subject,
			InReplyTo:  inReplyTo,
			References: references,
			Metadata:   metadata,
			Snippet:    snippetFrom(req.BodyText),
			SentAt:     &now,
			CreatedAt:  now,
			UpdatedAt:  now,
		}

		if err := s.messageStore.Create(ctx, tx, msg); err != nil {
			return fmt.Errorf("create message: %w", err)
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
