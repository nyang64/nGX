package models

import (
	"time"

	"github.com/google/uuid"
)

// Organization represents a customer account in the AgentMail platform.
type Organization struct {
	ID        uuid.UUID      `json:"id"         db:"id"`
	Name      string         `json:"name"       db:"name"`
	Slug      string         `json:"slug"       db:"slug"`
	Plan      string         `json:"plan"       db:"plan"`
	Settings  map[string]any `json:"settings"   db:"settings"`
	CreatedAt time.Time      `json:"created_at" db:"created_at"`
	UpdatedAt time.Time      `json:"updated_at" db:"updated_at"`
}
