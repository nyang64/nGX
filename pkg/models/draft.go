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

// Draft represents a message that has been composed but not yet sent.
// Drafts can optionally require human review before sending.
type Draft struct {
	ID      uuid.UUID `json:"id"       db:"id"`
	OrgID   uuid.UUID `json:"org_id"   db:"org_id"`
	InboxID uuid.UUID `json:"inbox_id" db:"inbox_id"`
	// ThreadID is set when the draft is a reply to an existing thread.
	ThreadID *uuid.UUID `json:"thread_id,omitempty" db:"thread_id"`

	Subject string         `json:"subject"  db:"subject"`
	From    EmailAddress   `json:"from"     db:"from"`
	To      []EmailAddress `json:"to"       db:"to"`
	Cc      []EmailAddress `json:"cc"       db:"cc"`
	Bcc     []EmailAddress `json:"bcc"      db:"bcc"`
	ReplyTo []EmailAddress `json:"reply_to" db:"reply_to"`

	// InReplyTo is the Message-ID of the message being replied to.
	InReplyTo  string   `json:"in_reply_to,omitempty" db:"in_reply_to"`
	References []string `json:"references,omitempty"  db:"references"`

	TextBody string `json:"text_body,omitempty" db:"text_body"`
	HtmlBody string `json:"html_body,omitempty" db:"html_body"`

	ReviewStatus DraftReviewStatus `json:"review_status" db:"review_status"`
	// ReviewNote is an optional note added by the reviewer.
	ReviewNote string     `json:"review_note,omitempty" db:"review_note"`
	ReviewedAt *time.Time `json:"reviewed_at,omitempty" db:"reviewed_at"`
	ReviewedBy *uuid.UUID `json:"reviewed_by,omitempty" db:"reviewed_by"`

	// ScheduledAt, if set, is when the draft should be automatically sent.
	ScheduledAt *time.Time `json:"scheduled_at,omitempty" db:"scheduled_at"`
	SentAt      *time.Time `json:"sent_at,omitempty"      db:"sent_at"`
	// MessageID is populated once the draft has been sent.
	MessageID *uuid.UUID `json:"message_id,omitempty" db:"message_id"`

	// Attachments is populated when fetching a single draft (not in list views).
	Attachments []Attachment `json:"attachments,omitempty" db:"-"`

	Metadata  map[string]any `json:"metadata,omitempty"  db:"metadata"`
	CreatedAt time.Time      `json:"created_at"          db:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"          db:"updated_at"`
}
