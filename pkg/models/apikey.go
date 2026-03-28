package models

import (
	"time"

	"github.com/google/uuid"
)

type APIKey struct {
	ID         uuid.UUID  `json:"id"           db:"id"`
	OrgID      uuid.UUID  `json:"org_id"       db:"org_id"`
	Name       string     `json:"name"         db:"name"`
	KeyPrefix  string     `json:"key_prefix"   db:"key_prefix"`
	KeyHash    string     `json:"-"            db:"key_hash"`
	Scopes     []string   `json:"scopes"       db:"scopes"`
	PodID      *uuid.UUID `json:"pod_id"       db:"pod_id"`
	LastUsedAt *time.Time `json:"last_used_at" db:"last_used_at"`
	ExpiresAt  *time.Time `json:"expires_at"   db:"expires_at"`
	RevokedAt  *time.Time `json:"revoked_at"   db:"revoked_at"`
	CreatedAt  time.Time  `json:"created_at"   db:"created_at"`
}

func (k *APIKey) IsRevoked() bool { return k.RevokedAt != nil }
func (k *APIKey) IsExpired() bool { return k.ExpiresAt != nil && k.ExpiresAt.Before(time.Now()) }
func (k *APIKey) IsValid() bool   { return !k.IsRevoked() && !k.IsExpired() }

// Masked returns a display-safe version: "am_live_XXXXXXXX..."
func (k *APIKey) Masked() string { return k.KeyPrefix + "..." }
