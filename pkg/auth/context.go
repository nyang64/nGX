/*
 * Copyright (c) 2026 nyklabs.com. All rights reserved.
 *
 * Licensed under the nGX Commercial Source License v1.0.
 * See LICENSE file in the project root for full license information.
 */

package auth

import (
	"context"

	"github.com/google/uuid"
)

type contextKey string

const claimsKey contextKey = "claims"

// WithClaims returns a new context carrying c.
func WithClaims(ctx context.Context, c *Claims) context.Context {
	return context.WithValue(ctx, claimsKey, c)
}

// ClaimsFromCtx retrieves Claims from ctx. Returns nil if not set.
func ClaimsFromCtx(ctx context.Context) *Claims {
	c, _ := ctx.Value(claimsKey).(*Claims)
	return c
}

// OrgIDFromCtx returns the org ID stored in ctx, or uuid.Nil if absent.
func OrgIDFromCtx(ctx context.Context) uuid.UUID {
	if c := ClaimsFromCtx(ctx); c != nil {
		return c.OrgID
	}
	return uuid.Nil
}
