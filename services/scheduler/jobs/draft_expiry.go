/*
 * Copyright (c) 2026 nyklabs.com. All rights reserved.
 *
 * Licensed under the nGX Commercial Source License v1.0.
 * See LICENSE file in the project root for full license information.
 */

package jobs

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DraftExpiry expires drafts whose expires_at has passed while
// still in 'pending' review status.
type DraftExpiry struct {
	pool *pgxpool.Pool
}

// NewDraftExpiry creates a DraftExpiry job.
func NewDraftExpiry(pool *pgxpool.Pool) *DraftExpiry {
	return &DraftExpiry{pool: pool}
}

// Run expires pending drafts that are past their expires_at time.
func (j *DraftExpiry) Run(ctx context.Context) {
	slog.Debug("draft expiry running")

	// Drafts with an expires_at in the past and still pending review are
	// considered expired: they can no longer be approved in time.
	q := `
		UPDATE drafts
		SET review_status = 'rejected',
		    review_note   = 'expired: scheduled send time has passed',
		    updated_at    = NOW()
		WHERE review_status = 'pending'
		  AND expires_at IS NOT NULL
		  AND expires_at < NOW()
	`
	tag, err := j.pool.Exec(ctx, q)
	if err != nil {
		slog.Error("draft expiry failed", "error", fmt.Errorf("expire pending drafts: %w", err))
		return
	}

	if rows := tag.RowsAffected(); rows > 0 {
		slog.Info("draft expiry: expired pending drafts", "count", rows)
	}
}
