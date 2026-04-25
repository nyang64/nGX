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
	"errors"
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

// ErrInvalidReviewStatus is returned when an action requires a specific review state.
var ErrInvalidReviewStatus = errors.New("draft is not in the required review status")

// CreateDraftRequest is the input for creating a draft.
type CreateDraftRequest struct {
	InboxID     uuid.UUID             `json:"-"`
	ThreadID    *uuid.UUID            `json:"thread_id,omitempty"`
	Subject     string                `json:"subject"`
	To          []models.EmailAddress `json:"to"`
	Cc          []models.EmailAddress `json:"cc"`
	Bcc         []models.EmailAddress `json:"bcc"`
	TextBody    string                `json:"body_text"`
	HtmlBody    string                `json:"body_html"`
	InReplyTo   string                `json:"in_reply_to,omitempty"`
	ScheduledAt *time.Time            `json:"scheduled_at,omitempty"`
	Metadata    map[string]any        `json:"metadata,omitempty"`
	Attachments []AttachmentRequest   `json:"attachments,omitempty"`
}

// UpdateDraftRequest is the input for updating a draft.
type UpdateDraftRequest struct {
	Subject     *string               `json:"subject,omitempty"`
	To          []models.EmailAddress `json:"to"`
	Cc          []models.EmailAddress `json:"cc"`
	Bcc         []models.EmailAddress `json:"bcc"`
	TextBody    *string               `json:"body_text,omitempty"`
	HtmlBody    *string               `json:"body_html,omitempty"`
	ScheduledAt *time.Time            `json:"scheduled_at,omitempty"`
	Metadata    map[string]any        `json:"metadata,omitempty"`
	// Attachments replaces the draft's existing attachments when non-nil.
	Attachments []AttachmentRequest `json:"attachments,omitempty"`
}

// DraftService handles draft business logic.
type DraftService struct {
	pool             *pgxpool.Pool
	draftStore       store.DraftStore
	messageStore     store.MessageStore
	threadStore      store.ThreadStore
	inboxStore       store.InboxStore
	eventProducer    events.EventPublisher
	outboundProducer events.OutboundPublisher
	attachmentsS3    *s3pkg.Client
}

// NewDraftService creates a new DraftService.
func NewDraftService(
	pool *pgxpool.Pool,
	draftStore store.DraftStore,
	messageStore store.MessageStore,
	threadStore store.ThreadStore,
	inboxStore store.InboxStore,
	eventProducer events.EventPublisher,
	outboundProducer events.OutboundPublisher,
	attachmentsS3 *s3pkg.Client,
) *DraftService {
	return &DraftService{
		pool:             pool,
		draftStore:       draftStore,
		messageStore:     messageStore,
		threadStore:      threadStore,
		inboxStore:       inboxStore,
		eventProducer:    eventProducer,
		outboundProducer: outboundProducer,
		attachmentsS3:    attachmentsS3,
	}
}

// uploadAttachments decodes and uploads inline attachment requests to S3.
// Returns (s3Key, filename, contentType, sizeBytes) tuples.
func (s *DraftService) uploadAttachments(ctx context.Context, orgID uuid.UUID, parentID uuid.UUID, prefix string, atts []AttachmentRequest) ([]struct {
	s3Key, filename, contentType string
	sizeBytes                    int64
}, error) {
	type upload struct {
		s3Key, filename, contentType string
		sizeBytes                    int64
	}
	var uploads []upload
	for _, a := range atts {
		data, err := base64.StdEncoding.DecodeString(a.Content)
		if err != nil {
			return nil, fmt.Errorf("decode attachment %q: %w", a.Filename, err)
		}
		ct := a.ContentType
		if ct == "" {
			ct = "application/octet-stream"
		}
		key := fmt.Sprintf("%s/%s/%s/%s", orgID, prefix, parentID, a.Filename)
		if s.attachmentsS3 != nil {
			if err := s.attachmentsS3.Upload(ctx, key, data, ct); err != nil {
				return nil, fmt.Errorf("upload attachment %q: %w", a.Filename, err)
			}
		}
		uploads = append(uploads, upload{
			s3Key:       key,
			filename:    a.Filename,
			contentType: ct,
			sizeBytes:   int64(len(data)),
		})
	}
	result := make([]struct {
		s3Key, filename, contentType string
		sizeBytes                    int64
	}, len(uploads))
	for i, u := range uploads {
		result[i].s3Key = u.s3Key
		result[i].filename = u.filename
		result[i].contentType = u.contentType
		result[i].sizeBytes = u.sizeBytes
	}
	return result, nil
}

// Create stores a new draft.
func (s *DraftService) Create(ctx context.Context, claims *auth.Claims, req CreateDraftRequest) (*models.Draft, error) {
	now := time.Now().UTC()
	draftID := uuid.New()

	// Upload attachments before opening the DB transaction.
	attUploads, err := s.uploadAttachments(ctx, claims.OrgID, draftID, "drafts", req.Attachments)
	if err != nil {
		return nil, err
	}

	draft := &models.Draft{
		ID:           draftID,
		OrgID:        claims.OrgID,
		InboxID:      req.InboxID,
		ThreadID:     req.ThreadID,
		Subject:      req.Subject,
		To:           req.To,
		Cc:           req.Cc,
		Bcc:          req.Bcc,
		TextBody:     req.TextBody,
		HtmlBody:     req.HtmlBody,
		InReplyTo:    req.InReplyTo,
		ScheduledAt:  req.ScheduledAt,
		ReviewStatus: models.DraftReviewStatusPending,
		Metadata:     req.Metadata,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if draft.Metadata == nil {
		draft.Metadata = map[string]any{}
	}

	err = dbpkg.WithOrgTx(ctx, s.pool, claims.OrgID, func(tx pgx.Tx) error {
		if err := s.draftStore.Create(ctx, tx, draft); err != nil {
			return err
		}
		for _, a := range attUploads {
			att := &models.Attachment{
				ID:          uuid.New(),
				OrgID:       claims.OrgID,
				DraftID:     &draftID,
				Filename:    a.filename,
				ContentType: a.contentType,
				SizeBytes:   a.sizeBytes,
				S3Key:       a.s3Key,
				CreatedAt:   now,
			}
			if err := s.messageStore.CreateDraftAttachment(ctx, tx, att); err != nil {
				return fmt.Errorf("create draft attachment: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("create draft: %w", err)
	}

	threadID := uuid.Nil
	if draft.ThreadID != nil {
		threadID = *draft.ThreadID
	}
	evt := &events.DraftCreatedEvent{
		BaseEvent: events.NewBase(events.EventDraftCreated, claims.OrgID),
		Data: events.DraftCreatedData{
			DraftID:  draft.ID,
			ThreadID: threadID,
			InboxID:  draft.InboxID,
		},
	}
	_ = s.eventProducer.PublishEvent(ctx, evt)

	return draft, nil
}

// Get retrieves a draft by ID.
func (s *DraftService) Get(ctx context.Context, claims *auth.Claims, draftID uuid.UUID) (*models.Draft, error) {
	var draft *models.Draft
	err := dbpkg.WithOrgTx(ctx, s.pool, claims.OrgID, func(tx pgx.Tx) error {
		var err error
		draft, err = s.draftStore.GetByID(ctx, tx, claims.OrgID, draftID)
		if err != nil {
			return err
		}
		atts, err := s.messageStore.ListDraftAttachments(ctx, tx, claims.OrgID, draftID)
		if err != nil {
			return err
		}
		for _, a := range atts {
			draft.Attachments = append(draft.Attachments, *a)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("get draft: %w", err)
	}
	return draft, nil
}

// List returns a paginated list of drafts for an inbox.
func (s *DraftService) List(ctx context.Context, claims *auth.Claims, inboxID uuid.UUID, limit int, cursor string) ([]*models.Draft, string, error) {
	var drafts []*models.Draft
	var nextCursor string
	err := dbpkg.WithOrgTx(ctx, s.pool, claims.OrgID, func(tx pgx.Tx) error {
		var err error
		drafts, nextCursor, err = s.draftStore.List(ctx, tx, claims.OrgID, inboxID, limit, cursor)
		return err
	})
	if err != nil {
		return nil, "", fmt.Errorf("list drafts: %w", err)
	}
	return drafts, nextCursor, nil
}

// Update modifies draft content.
func (s *DraftService) Update(ctx context.Context, claims *auth.Claims, draftID uuid.UUID, req UpdateDraftRequest) (*models.Draft, error) {
	now := time.Now().UTC()

	// If attachments are being replaced, upload new ones before the transaction.
	var attUploads []struct {
		s3Key, filename, contentType string
		sizeBytes                    int64
	}
	if req.Attachments != nil {
		var err error
		attUploads, err = s.uploadAttachments(ctx, claims.OrgID, draftID, "drafts", req.Attachments)
		if err != nil {
			return nil, err
		}
	}

	var draft *models.Draft
	err := dbpkg.WithOrgTx(ctx, s.pool, claims.OrgID, func(tx pgx.Tx) error {
		existing, err := s.draftStore.GetByID(ctx, tx, claims.OrgID, draftID)
		if err != nil {
			return err
		}
		if existing.ReviewStatus != models.DraftReviewStatusPending {
			return ErrInvalidReviewStatus
		}
		draft, err = s.draftStore.Update(ctx, tx, claims.OrgID, draftID, store.DraftPatch{
			Subject:     req.Subject,
			To:          req.To,
			Cc:          req.Cc,
			Bcc:         req.Bcc,
			TextBody:    req.TextBody,
			HtmlBody:    req.HtmlBody,
			ScheduledAt: req.ScheduledAt,
			Metadata:    req.Metadata,
		})
		if err != nil {
			return err
		}
		// Replace attachments if provided.
		if req.Attachments != nil {
			if err := s.messageStore.DeleteDraftAttachments(ctx, tx, claims.OrgID, draftID); err != nil {
				return err
			}
			for _, a := range attUploads {
				att := &models.Attachment{
					ID:          uuid.New(),
					OrgID:       claims.OrgID,
					DraftID:     &draftID,
					Filename:    a.filename,
					ContentType: a.contentType,
					SizeBytes:   a.sizeBytes,
					S3Key:       a.s3Key,
					CreatedAt:   now,
				}
				if err := s.messageStore.CreateDraftAttachment(ctx, tx, att); err != nil {
					return fmt.Errorf("create draft attachment: %w", err)
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("update draft: %w", err)
	}
	return draft, nil
}

// Delete removes a draft.
func (s *DraftService) Delete(ctx context.Context, claims *auth.Claims, draftID uuid.UUID) error {
	err := dbpkg.WithOrgTx(ctx, s.pool, claims.OrgID, func(tx pgx.Tx) error {
		return s.draftStore.Delete(ctx, tx, claims.OrgID, draftID)
	})
	if err != nil {
		return fmt.Errorf("delete draft: %w", err)
	}
	return nil
}

// Approve approves a pending draft, sends it, and transitions to approved/sent.
func (s *DraftService) Approve(ctx context.Context, claims *auth.Claims, draftID uuid.UUID, note string) (*models.Draft, error) {
	var draft *models.Draft
	now := time.Now().UTC()

	err := dbpkg.WithOrgTx(ctx, s.pool, claims.OrgID, func(tx pgx.Tx) error {
		existing, err := s.draftStore.GetByID(ctx, tx, claims.OrgID, draftID)
		if err != nil {
			return err
		}
		if existing.ReviewStatus != models.DraftReviewStatusPending {
			return ErrInvalidReviewStatus
		}

		// Load inbox for From address.
		inbox, err := s.inboxStore.GetByID(ctx, tx, claims.OrgID, existing.InboxID)
		if err != nil {
			return fmt.Errorf("get inbox: %w", err)
		}

		// Determine or create thread.
		var thread *models.Thread
		if existing.ThreadID != nil {
			thread, err = s.threadStore.GetByID(ctx, tx, claims.OrgID, *existing.ThreadID)
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
			for _, a := range existing.To {
				addParticipant(a)
			}
			for _, a := range existing.Cc {
				addParticipant(a)
			}
			thread = &models.Thread{
				ID:           uuid.New(),
				OrgID:        claims.OrgID,
				InboxID:      existing.InboxID,
				Subject:      existing.Subject,
				Snippet:      snippetFrom(existing.TextBody),
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

		// Build References if this is a reply.
		var references []string
		if existing.InReplyTo != "" {
			// We have InReplyTo but no parent message reference chain in the draft.
			// Best-effort: put InReplyTo itself as the sole reference.
			references = []string{existing.InReplyTo}
		}

		metadata := existing.Metadata
		if metadata == nil {
			metadata = map[string]any{}
		}

		// Count draft attachments so we can set HasAttachments on the message.
		draftAtts, err := s.messageStore.ListDraftAttachments(ctx, tx, claims.OrgID, draftID)
		if err != nil {
			return fmt.Errorf("list draft attachments: %w", err)
		}

		// Create outbound message.
		msgID := uuid.New()
		msg := &models.Message{
			ID:             msgID,
			OrgID:          claims.OrgID,
			InboxID:        existing.InboxID,
			ThreadID:       thread.ID,
			MessageID:      generateMessageID(),
			Direction:      models.DirectionOutbound,
			Status:         models.MessageStatusSending,
			From: models.EmailAddress{
				Email: inbox.Email,
				Name:  inbox.DisplayName,
			},
			To:             existing.To,
			Cc:             existing.Cc,
			Bcc:            existing.Bcc,
			Subject:        existing.Subject,
			InReplyTo:      existing.InReplyTo,
			References:     references,
			Metadata:       metadata,
			Snippet:        snippetFrom(existing.TextBody),
			HasAttachments: len(draftAtts) > 0,
			SentAt:         &now,
			CreatedAt:      now,
			UpdatedAt:      now,
		}
		if err := s.messageStore.Create(ctx, tx, msg); err != nil {
			return fmt.Errorf("create message: %w", err)
		}

		// Transfer draft attachments to the new message.
		if len(draftAtts) > 0 {
			if err := s.messageStore.LinkDraftAttachments(ctx, tx, draftID, msgID); err != nil {
				return fmt.Errorf("link draft attachments: %w", err)
			}
		}
		if err := s.threadStore.IncrMessageCount(ctx, tx, thread.ID, now, msg.Snippet); err != nil {
			return fmt.Errorf("incr message count: %w", err)
		}

		// Transition draft to approved.
		approvedStatus := string(models.DraftReviewStatusApproved)
		reviewerID := claims.KeyID
		draft, err = s.draftStore.Update(ctx, tx, claims.OrgID, draftID, store.DraftPatch{
			ReviewStatus: &approvedStatus,
			ReviewNote:   &note,
			ReviewedAt:   &now,
			ReviewedBy:   &reviewerID,
		})
		if err != nil {
			return err
		}

		// Publish outbound job. BCC is intentionally omitted from the Kafka payload.
		job := OutboundJob{
			MessageID:  msg.ID.String(),
			OrgID:      claims.OrgID.String(),
			InboxID:    existing.InboxID.String(),
			ThreadID:   thread.ID.String(),
			From:       msg.From,
			To:         existing.To,
			CC:         existing.Cc,
			Subject:    existing.Subject,
			BodyText:   existing.TextBody,
			BodyHTML:   existing.HtmlBody,
			InReplyTo:  existing.InReplyTo,
			References: references,
		}
		jobBytes, _ := json.Marshal(job)
		_ = s.outboundProducer.Publish(ctx, msg.ID.String(), jobBytes)

		// Publish event.
		threadID := uuid.Nil
		if draft.ThreadID != nil {
			threadID = *draft.ThreadID
		}
		evt := &events.DraftApprovedEvent{
			BaseEvent: events.NewBase(events.EventDraftApproved, claims.OrgID),
			Data: events.DraftApprovedData{
				DraftID:  draftID,
				ThreadID: threadID,
				InboxID:  existing.InboxID,
			},
		}
		_ = s.eventProducer.PublishEvent(ctx, evt)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("approve draft: %w", err)
	}
	return draft, nil
}

// Reject rejects a pending draft.
func (s *DraftService) Reject(ctx context.Context, claims *auth.Claims, draftID uuid.UUID, reason string) (*models.Draft, error) {
	var draft *models.Draft
	now := time.Now().UTC()

	err := dbpkg.WithOrgTx(ctx, s.pool, claims.OrgID, func(tx pgx.Tx) error {
		existing, err := s.draftStore.GetByID(ctx, tx, claims.OrgID, draftID)
		if err != nil {
			return err
		}
		if existing.ReviewStatus != models.DraftReviewStatusPending {
			return ErrInvalidReviewStatus
		}

		rejectedStatus := string(models.DraftReviewStatusRejected)
		reviewerID := claims.KeyID
		draft, err = s.draftStore.Update(ctx, tx, claims.OrgID, draftID, store.DraftPatch{
			ReviewStatus: &rejectedStatus,
			ReviewNote:   &reason,
			ReviewedAt:   &now,
			ReviewedBy:   &reviewerID,
		})
		if err != nil {
			return err
		}

		threadID := uuid.Nil
		if draft.ThreadID != nil {
			threadID = *draft.ThreadID
		}
		evt := &events.DraftRejectedEvent{
			BaseEvent: events.NewBase(events.EventDraftRejected, claims.OrgID),
			Data: events.DraftRejectedData{
				DraftID:  draftID,
				ThreadID: threadID,
				InboxID:  existing.InboxID,
				Reason:   reason,
			},
		}
		_ = s.eventProducer.PublishEvent(ctx, evt)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("reject draft: %w", err)
	}
	return draft, nil
}
