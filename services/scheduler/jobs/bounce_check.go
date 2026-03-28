package jobs

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
)

// BounceCheck marks messages that have been in the 'sending' state for more
// than 24 hours as 'failed', assuming the delivery has silently failed.
type BounceCheck struct {
	pool *pgxpool.Pool
}

// NewBounceCheck creates a BounceCheck job.
func NewBounceCheck(pool *pgxpool.Pool) *BounceCheck {
	return &BounceCheck{pool: pool}
}

// Run executes the bounce check query.
func (j *BounceCheck) Run(ctx context.Context) {
	slog.Debug("bounce check running")

	q := `
		UPDATE messages
		SET status     = 'failed',
		    updated_at = NOW()
		WHERE status     = 'sending'
		  AND created_at < NOW() - INTERVAL '24 hours'
	`
	tag, err := j.pool.Exec(ctx, q)
	if err != nil {
		slog.Error("bounce check failed", "error", fmt.Errorf("update stale sending messages: %w", err))
		return
	}

	if rows := tag.RowsAffected(); rows > 0 {
		slog.Info("bounce check: marked stale messages as failed", "count", rows)
	}
}
