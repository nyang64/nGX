package models

import (
	"time"

	"github.com/google/uuid"
)

// Thread represents a conversation made up of one or more related messages.
type Thread struct {
	ID            uuid.UUID      `json:"id"              db:"id"`
	OrgID         uuid.UUID      `json:"org_id"          db:"org_id"`
	InboxID       uuid.UUID      `json:"inbox_id"        db:"inbox_id"`
	Subject       string         `json:"subject"         db:"subject"`
	Snippet       string         `json:"snippet"         db:"snippet"`
	Status        ThreadStatus   `json:"status"          db:"status"`
	IsRead        bool           `json:"is_read"         db:"is_read"`
	IsStarred     bool           `json:"is_starred"      db:"is_starred"`
	MessageCount  int            `json:"message_count"   db:"message_count"`
	Participants  []EmailAddress `json:"participants"    db:"participants"`
	LastMessageAt *time.Time     `json:"last_message_at" db:"last_message_at"`
	// Labels is populated via JOIN and is not stored directly on the thread row.
	Labels    []Label   `json:"labels,omitempty" db:"-"`
	CreatedAt time.Time `json:"created_at"       db:"created_at"`
	UpdatedAt time.Time `json:"updated_at"       db:"updated_at"`
}
