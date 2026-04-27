/*
 * Copyright (c) 2026 nyklabs.com. All rights reserved.
 *
 * Licensed under the nGX Commercial Source License v1.0.
 * See LICENSE file in the project root for full license information.
 */

package models

import (
	"time"

	"github.com/google/uuid"
)

// Message represents a single email message within a thread.
type Message struct {
	ID        uuid.UUID `json:"id"         db:"id"`
	OrgID     uuid.UUID `json:"org_id"     db:"org_id"`
	InboxID   uuid.UUID `json:"inbox_id"   db:"inbox_id"`
	ThreadID  uuid.UUID `json:"thread_id"  db:"thread_id"`

	// MessageID is the RFC 5322 Message-ID header value.
	// DB column: message_id_header
	MessageID string `json:"message_id" db:"message_id_header"`
	// InReplyTo is the Message-ID of the message being replied to.
	// DB column: in_reply_to
	InReplyTo string `json:"in_reply_to" db:"in_reply_to"`
	// References holds the chain of Message-IDs from the References header.
	// DB column: references_header
	References []string `json:"references" db:"references_header"`

	Direction Direction     `json:"direction" db:"direction"`
	Status    MessageStatus `json:"status"    db:"status"`

	Subject string         `json:"subject"  db:"subject"`
	// From is stored as two columns (from_address, from_name); db:"-" because
	// it cannot be directly scanned from a single column.
	From    EmailAddress   `json:"from"     db:"-"`
	// To/Cc/Bcc are stored as JSONB arrays; db tags reflect actual column names.
	To      []EmailAddress `json:"to"       db:"to_addresses"`
	Cc      []EmailAddress `json:"cc"       db:"cc_addresses"`
	Bcc     []EmailAddress `json:"bcc"      db:"bcc_addresses"`
	// ReplyTo is the RFC 5322 Reply-To header value (single address string).
	// DB column: reply_to TEXT
	ReplyTo string         `json:"reply_to,omitempty" db:"reply_to"`

	// S3 keys for raw and processed content — not returned to clients directly.
	// DB columns: raw_key, body_html_key, body_text_key
	RawS3Key  string `json:"raw_s3_key,omitempty"  db:"raw_key"`
	HtmlS3Key string `json:"html_s3_key,omitempty" db:"body_html_key"`
	TextS3Key string `json:"text_s3_key,omitempty" db:"body_text_key"`

	// SizeBytes is the size of the raw message in bytes.
	SizeBytes int64 `json:"size_bytes" db:"size_bytes"`

	// Headers stores all RFC 5322 headers as a key→[]values map.
	Headers map[string][]string `json:"headers,omitempty" db:"headers"`

	// Metadata is a caller-supplied key→value store. DB column: metadata JSONB.
	Metadata map[string]any `json:"metadata,omitempty" db:"metadata"`

	// IsRead indicates whether the message has been read.
	IsRead    bool `json:"is_read"    db:"is_read"`
	// IsStarred indicates whether the message has been starred.
	IsStarred bool `json:"is_starred" db:"is_starred"`

	// HasAttachments is true when the message has at least one attachment.
	HasAttachments bool `json:"has_attachments" db:"has_attachments"`

	// Snippet does not exist on the messages table (it lives on threads).
	Snippet string `json:"-" db:"-"`

	SentAt     *time.Time `json:"sent_at"     db:"sent_at"`
	ReceivedAt *time.Time `json:"received_at" db:"received_at"`

	// Attachments is populated via JOIN and is not stored on the message row.
	Attachments []Attachment `json:"attachments,omitempty" db:"-"`

	CreatedAt time.Time `json:"created_at" db:"created_at"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
}
