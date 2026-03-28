package events

import (
	"time"

	"github.com/google/uuid"
)

// EventType identifies the kind of domain event.
type EventType string

const (
	// Message events
	EventMessageReceived EventType = "message.received"
	EventMessageSent     EventType = "message.sent"
	EventMessageBounced  EventType = "message.bounced"

	// Thread events
	EventThreadCreated       EventType = "thread.created"
	EventThreadStatusChanged EventType = "thread.status_changed"

	// Draft events
	EventDraftCreated  EventType = "draft.created"
	EventDraftApproved EventType = "draft.approved"
	EventDraftRejected EventType = "draft.rejected"

	// Inbox events
	EventInboxCreated EventType = "inbox.created"

	// Label events
	EventLabelApplied EventType = "label.applied"
)

// BaseEvent contains fields present on every event envelope.
type BaseEvent struct {
	// ID is a unique identifier for this event (UUID or ULID string).
	ID string `json:"id"`
	// Type identifies the event kind.
	Type EventType `json:"type"`
	// OrgID is the tenant that owns this event (used as Kafka partition key).
	OrgID string `json:"org_id"`
	// OccurredAt is when the event occurred.
	OccurredAt time.Time `json:"occurred_at"`
	// CorrelationID links related events across service boundaries.
	CorrelationID string `json:"correlation_id,omitempty"`
}

// GetBase satisfies the Event interface.
func (b BaseEvent) GetBase() BaseEvent { return b }

// NewBase constructs a BaseEvent with a fresh UUID and the current time.
func NewBase(t EventType, orgID uuid.UUID) BaseEvent {
	return BaseEvent{
		ID:         uuid.New().String(),
		Type:       t,
		OrgID:      orgID.String(),
		OccurredAt: time.Now().UTC(),
	}
}
