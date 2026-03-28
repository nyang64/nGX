package db

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SetOrgContext sets the RLS context for the current transaction.
// Must be called at the start of any transaction that accesses tenant-isolated tables.
func SetOrgContext(ctx context.Context, tx pgx.Tx, orgID uuid.UUID) error {
	_, err := tx.Exec(ctx, "SELECT set_config('app.current_org_id', $1, TRUE)", orgID.String())
	return err
}

// SetPodContext sets the optional pod-level RLS context for the current transaction.
// Pass nil to clear the restriction (org-wide access). The policy reads an empty string
// as "no pod restriction" via: current_setting('app.current_pod_id', TRUE) = ''.
func SetPodContext(ctx context.Context, tx pgx.Tx, podID *uuid.UUID) error {
	val := ""
	if podID != nil {
		val = podID.String()
	}
	_, err := tx.Exec(ctx, "SELECT set_config('app.current_pod_id', $1, TRUE)", val)
	return err
}

// WithOrgTx starts a transaction, sets RLS org context, runs fn, and commits.
func WithOrgTx(ctx context.Context, pool *pgxpool.Pool, orgID uuid.UUID, fn func(pgx.Tx) error) error {
	return WithTx(ctx, pool, func(tx pgx.Tx) error {
		if err := SetOrgContext(ctx, tx, orgID); err != nil {
			return err
		}
		return fn(tx)
	})
}

// WithOrgPodTx starts a transaction, sets RLS org + optional pod context, runs fn, and commits.
// When podID is non-nil the RLS policy additionally restricts rows to that pod.
func WithOrgPodTx(ctx context.Context, pool *pgxpool.Pool, orgID uuid.UUID, podID *uuid.UUID, fn func(pgx.Tx) error) error {
	return WithTx(ctx, pool, func(tx pgx.Tx) error {
		if err := SetOrgContext(ctx, tx, orgID); err != nil {
			return err
		}
		if err := SetPodContext(ctx, tx, podID); err != nil {
			return err
		}
		return fn(tx)
	})
}
