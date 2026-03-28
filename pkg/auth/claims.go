package auth

import (
	"github.com/google/uuid"
)

// Claims holds the authenticated identity extracted from a validated API key.
type Claims struct {
	OrgID  uuid.UUID
	KeyID  uuid.UUID
	Scopes []Scope
	// PodID is non-nil when the key is scoped to a single pod.
	PodID *uuid.UUID
}

// HasScope reports whether the claims include s. org:admin implies all scopes.
func (c *Claims) HasScope(s Scope) bool {
	for _, scope := range c.Scopes {
		if scope == ScopeOrgAdmin || scope == s {
			return true
		}
	}
	return false
}

// CanAccessPod reports whether the claims allow access to podID.
// A nil PodID means the key is org-wide and can access any pod.
func (c *Claims) CanAccessPod(podID uuid.UUID) bool {
	if c.PodID == nil {
		return true
	}
	return *c.PodID == podID
}
