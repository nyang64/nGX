package models

import (
	"time"

	"github.com/google/uuid"
)

// Pod represents a logical grouping of inboxes within an organization.
// Pods allow for multi-tenant isolation within a single organization,
// e.g., separating inboxes by team, customer, or environment.
type Pod struct {
	ID          uuid.UUID      `json:"id"          db:"id"`
	OrgID       uuid.UUID      `json:"org_id"      db:"org_id"`
	Name        string         `json:"name"        db:"name"`
	Slug        string         `json:"slug"        db:"slug"`
	Description string         `json:"description" db:"description"`
	Settings    map[string]any `json:"settings"    db:"settings"`
	CreatedAt   time.Time      `json:"created_at"  db:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"  db:"updated_at"`
}
