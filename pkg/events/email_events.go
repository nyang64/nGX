/*
 * Copyright (c) 2026 nyklabs.com. All rights reserved.
 *
 * Licensed under the nGX Commercial Source License v1.0.
 * See LICENSE file in the project root for full license information.
 */

package events

import (
	"github.com/google/uuid"
)

// MessageReceivedEvent is published when an inbound email is stored.
type MessageReceivedEvent struct {
	BaseEvent
	Data MessageReceivedData `json:"data"`
}

type MessageReceivedData struct {
	MessageID string    `json:"message_id"`
	InboxID   uuid.UUID `json:"inbox_id"`
	ThreadID  uuid.UUID `json:"thread_id"`
	From      string    `json:"from"`
	Subject   string    `json:"subject"`
	// RawS3Key is the S3 object key for the raw RFC 5322 message.
	RawS3Key string `json:"raw_s3_key"`
}

// MessageSentEvent is published when an outbound message is delivered.
type MessageSentEvent struct {
	BaseEvent
	Data MessageSentData `json:"data"`
}

type MessageSentData struct {
	MessageID string    `json:"message_id"`
	InboxID   uuid.UUID `json:"inbox_id"`
	ThreadID  uuid.UUID `json:"thread_id"`
	To        []string  `json:"to"`
	Subject   string    `json:"subject"`
	// BodyText is the plain-text body of the message, included when the event
	// is published at send time (before SES delivery) so the embedder can
	// index outbound messages without requiring a separate S3 object.
	BodyText string `json:"body_text,omitempty"`
}

// MessageBouncedEvent is published when a delivery attempt results in a bounce.
type MessageBouncedEvent struct {
	BaseEvent
	Data MessageBouncedData `json:"data"`
}

type MessageBouncedData struct {
	MessageID    string    `json:"message_id"`
	InboxID      uuid.UUID `json:"inbox_id"`
	ThreadID     uuid.UUID `json:"thread_id"`
	BounceCode   string    `json:"bounce_code"`
	BounceReason string    `json:"bounce_reason"`
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
	ThreadID   uuid.UUID `json:"thread_id"`
	InboxID    uuid.UUID `json:"inbox_id"`
	OldStatus  string    `json:"old_status"`
	NewStatus  string    `json:"new_status"`
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
	ThreadID uuid.UUID `json:"thread_id"`
	LabelID  uuid.UUID `json:"label_id"`
	LabelName string   `json:"label_name"`
}
