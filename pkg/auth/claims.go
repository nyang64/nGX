/*
 * Copyright (c) 2026 nyklabs.com. All rights reserved.
 *
 * Licensed under the nGX Commercial Source License v1.0.
 * See LICENSE file in the project root for full license information.
 */

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

	// License fields — populated from the license token via the authorizer context.
	Plan      string
	Features  []string
	SeatLimit int // -1 = unlimited
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

// HasFeature reports whether the license grants the named feature.
func (c *Claims) HasFeature(feature string) bool {
	for _, f := range c.Features {
		if f == feature {
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
