package models

// EmailAddress represents an email address with optional display name.
type EmailAddress struct {
	Email string `json:"email"`
	Name  string `json:"name,omitempty"`
}

// Direction indicates whether a message is inbound or outbound.
type Direction string

const (
	DirectionInbound  Direction = "inbound"
	DirectionOutbound Direction = "outbound"
)

// MessageStatus represents the delivery status of a message.
type MessageStatus string

const (
	MessageStatusReceived MessageStatus = "received"
	MessageStatusSending  MessageStatus = "sending"
	MessageStatusSent     MessageStatus = "sent"
	MessageStatusFailed   MessageStatus = "failed"
	MessageStatusBounced  MessageStatus = "bounced"
)

// ThreadStatus represents the state of a conversation thread.
type ThreadStatus string

const (
	ThreadStatusOpen   ThreadStatus = "open"
	ThreadStatusClosed ThreadStatus = "closed"
	ThreadStatusSpam   ThreadStatus = "spam"
	ThreadStatusTrash  ThreadStatus = "trash"
)

// DraftReviewStatus represents the review state of a draft message.
type DraftReviewStatus string

const (
	DraftReviewStatusPending  DraftReviewStatus = "pending"
	DraftReviewStatusApproved DraftReviewStatus = "approved"
	DraftReviewStatusRejected DraftReviewStatus = "rejected"
	DraftReviewStatusSent     DraftReviewStatus = "sent"
)

// InboxStatus represents the operational state of an inbox.
type InboxStatus string

const (
	InboxStatusActive    InboxStatus = "active"
	InboxStatusSuspended InboxStatus = "suspended"
	InboxStatusDeleted   InboxStatus = "deleted"
)
