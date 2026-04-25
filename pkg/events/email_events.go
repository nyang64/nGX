/*
 * Copyright (c) 2026 nyklabs.com. All rights reserved.
 *
 * Licensed under the nGX Commercial Source License v1.0.
 * See LICENSE file in the project root for full license information.
 */

package events

import (
	"strings"
	"time"

	"github.com/google/uuid"

	"agentmail/pkg/models"
)

// ── Shared payload types ──────────────────────────────────────────────────────

// EmailAddress is an email address with an optional display name.
// Mirrors models.EmailAddress; defined here so event consumers do not need
// to import the models package.
type EmailAddress struct {
	Email string `json:"email"`
	Name  string `json:"name,omitempty"`
}

// AttachmentInfo carries attachment metadata in event payloads.
// Binary content (S3Key) is intentionally excluded.
type AttachmentInfo struct {
	ID          string `json:"id"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	SizeBytes   int64  `json:"size_bytes"`
	ContentID   string `json:"content_id,omitempty"`
	Inline      bool   `json:"inline,omitempty"`
}

// MessagePayload is the full message snapshot embedded in every message event.
// Consumers receive everything in one payload — no follow-up API call required.
type MessagePayload struct {
	ID        string    `json:"id"`
	MessageID string    `json:"message_id"` // RFC 5322 Message-ID header value
	InboxID   uuid.UUID `json:"inbox_id"`
	ThreadID  uuid.UUID `json:"thread_id"`
	Direction string    `json:"direction"`
	Status    string    `json:"status"`

	Subject string         `json:"subject"`
	From    EmailAddress   `json:"from"`
	To      []EmailAddress `json:"to"`
	Cc      []EmailAddress `json:"cc"`
	Bcc     []EmailAddress `json:"bcc"`
	ReplyTo string         `json:"reply_to,omitempty"`

	BodyText string `json:"body_text,omitempty"`
	BodyHTML string `json:"body_html,omitempty"`
	Preview  string `json:"preview,omitempty"`

	Headers     map[string][]string `json:"headers,omitempty"`
	Attachments []AttachmentInfo    `json:"attachments"`

	SentAt     *time.Time `json:"sent_at,omitempty"`
	ReceivedAt *time.Time `json:"received_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

// MessagePayloadFromModel builds a MessagePayload from a loaded message model
// plus the resolved body content and attachments.
// bodyText / bodyHTML are the decoded strings (already fetched from S3 or inline).
// preview is a short plain-text snippet (use BuildPreview if not already computed).
func MessagePayloadFromModel(
	msg *models.Message,
	bodyText, bodyHTML, preview string,
	atts []models.Attachment,
) MessagePayload {
	p := MessagePayload{
		ID:          msg.ID.String(),
		MessageID:   msg.MessageID,
		InboxID:     msg.InboxID,
		ThreadID:    msg.ThreadID,
		Direction:   string(msg.Direction),
		Status:      string(msg.Status),
		Subject:     msg.Subject,
		From:        EmailAddress{Email: msg.From.Email, Name: msg.From.Name},
		To:          convertAddrs(msg.To),
		Cc:          convertAddrs(msg.Cc),
		Bcc:         convertAddrs(msg.Bcc),
		ReplyTo:     msg.ReplyTo,
		BodyText:    bodyText,
		BodyHTML:    bodyHTML,
		Preview:     preview,
		Headers:     msg.Headers,
		Attachments: make([]AttachmentInfo, 0, len(atts)),
		SentAt:      msg.SentAt,
		ReceivedAt:  msg.ReceivedAt,
		CreatedAt:   msg.CreatedAt,
		UpdatedAt:   msg.UpdatedAt,
	}
	for _, a := range atts {
		p.Attachments = append(p.Attachments, AttachmentInfo{
			ID:          a.ID.String(),
			Filename:    a.Filename,
			ContentType: a.ContentType,
			SizeBytes:   a.SizeBytes,
			ContentID:   a.ContentID,
			Inline:      a.Inline,
		})
	}
	return p
}

// BuildPreview produces a short plain-text preview from a message body.
func BuildPreview(text string, maxLen int) string {
	s := strings.TrimSpace(text)
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", "")
	if len(s) > maxLen {
		return s[:maxLen]
	}
	return s
}

func convertAddrs(in []models.EmailAddress) []EmailAddress {
	out := make([]EmailAddress, len(in))
	for i, a := range in {
		out[i] = EmailAddress{Email: a.Email, Name: a.Name}
	}
	return out
}

// ── Event types ───────────────────────────────────────────────────────────────

// MessageReceivedEvent is published when an inbound email is stored.
type MessageReceivedEvent struct {
	BaseEvent
	Data MessageReceivedData `json:"data"`
}

// MessageReceivedData carries the full inbound message snapshot.
// RawS3Key is the S3 object key for the original RFC 5322 .eml file.
type MessageReceivedData struct {
	MessagePayload
	RawS3Key string `json:"raw_s3_key,omitempty"`
}

// MessageSentEvent is published when an outbound message is accepted by SES.
type MessageSentEvent struct {
	BaseEvent
	Data MessageSentData `json:"data"`
}

// MessageSentData carries the full outbound message snapshot.
type MessageSentData struct {
	MessagePayload
}

// MessageBouncedEvent is published when a delivery attempt results in a bounce
// or when SES reports a complaint, rejection, or rendering failure.
type MessageBouncedEvent struct {
	BaseEvent
	Data MessageBouncedData `json:"data"`
}

// MessageBouncedData carries the full message snapshot plus bounce details.
type MessageBouncedData struct {
	MessagePayload
	BounceCode   string `json:"bounce_code,omitempty"`
	BounceReason string `json:"bounce_reason,omitempty"`
}

// ThreadCreatedEvent is published when a new thread is opened.
type ThreadCreatedEvent struct {
	BaseEvent
	Data ThreadCreatedData `json:"data"`
}

type ThreadCreatedData struct {
	ThreadID  uuid.UUID `json:"thread_id"`
	InboxID   uuid.UUID `json:"inbox_id"`
	Subject   string    `json:"subject"`
	MessageID string    `json:"first_message_id"`
}

// ThreadStatusChangedEvent is published when a thread's status transitions.
type ThreadStatusChangedEvent struct {
	BaseEvent
	Data ThreadStatusChangedData `json:"data"`
}

type ThreadStatusChangedData struct {
	ThreadID  uuid.UUID `json:"thread_id"`
	InboxID   uuid.UUID `json:"inbox_id"`
	OldStatus string    `json:"old_status"`
	NewStatus string    `json:"new_status"`
}

// DraftCreatedEvent is published when an agent queues a draft for review.
type DraftCreatedEvent struct {
	BaseEvent
	Data DraftCreatedData `json:"data"`
}

type DraftCreatedData struct {
	DraftID  uuid.UUID `json:"draft_id"`
	ThreadID uuid.UUID `json:"thread_id"`
	InboxID  uuid.UUID `json:"inbox_id"`
}

// DraftApprovedEvent is published when a draft is approved for sending.
type DraftApprovedEvent struct {
	BaseEvent
	Data DraftApprovedData `json:"data"`
}

type DraftApprovedData struct {
	DraftID  uuid.UUID `json:"draft_id"`
	ThreadID uuid.UUID `json:"thread_id"`
	InboxID  uuid.UUID `json:"inbox_id"`
}

// DraftRejectedEvent is published when a draft is rejected.
type DraftRejectedEvent struct {
	BaseEvent
	Data DraftRejectedData `json:"data"`
}

type DraftRejectedData struct {
	DraftID  uuid.UUID `json:"draft_id"`
	ThreadID uuid.UUID `json:"thread_id"`
	InboxID  uuid.UUID `json:"inbox_id"`
	Reason   string    `json:"reason"`
}

// InboxCreatedEvent is published when a new inbox is provisioned.
type InboxCreatedEvent struct {
	BaseEvent
	Data InboxCreatedData `json:"data"`
}

type InboxCreatedData struct {
	InboxID      uuid.UUID `json:"inbox_id"`
	EmailAddress string    `json:"email_address"`
	PodID        uuid.UUID `json:"pod_id"`
}

// LabelAppliedEvent is published when a label is attached to a thread.
type LabelAppliedEvent struct {
	BaseEvent
	Data LabelAppliedData `json:"data"`
}

type LabelAppliedData struct {
	ThreadID  uuid.UUID `json:"thread_id"`
	LabelID   uuid.UUID `json:"label_id"`
	LabelName string    `json:"label_name"`
}
