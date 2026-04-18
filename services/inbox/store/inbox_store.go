/*
 * Copyright (c) 2026 nyklabs.com. All rights reserved.
 *
 * Licensed under the nGX Commercial Source License v1.0.
 * See LICENSE file in the project root for full license information.
 */

package store

import (
	"context"
	"fmt"
	"time"

	"agentmail/pkg/pagination"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"agentmail/pkg/models"
)

// InboxPatch contains optional fields to update on an inbox.
type InboxPatch struct {
	DisplayName *string
	Status      *string
	Settings    map[string]any
}

// InboxStore defines data access for inboxes.
type InboxStore interface {
	Create(ctx context.Context, tx pgx.Tx, inbox *models.Inbox) error
	GetByID(ctx context.Context, tx pgx.Tx, orgID, inboxID uuid.UUID) (*models.Inbox, error)
	GetByAddress(ctx context.Context, address string) (*models.Inbox, error)
	List(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, podID *uuid.UUID, limit int, cursor string) ([]*models.Inbox, string, error)
	Update(ctx context.Context, tx pgx.Tx, orgID, inboxID uuid.UUID, patch InboxPatch) (*models.Inbox, error)
	Delete(ctx context.Context, tx pgx.Tx, orgID, inboxID uuid.UUID) error
}

// PostgresInboxStore implements InboxStore using PostgreSQL.
type PostgresInboxStore struct {
	pool *pgxpool.Pool
}

// NewPostgresInboxStore creates a new PostgresInboxStore.
func NewPostgresInboxStore(pool *pgxpool.Pool) *PostgresInboxStore {
	return &PostgresInboxStore{pool: pool}
}

// Create inserts a new inbox record.
func (s *PostgresInboxStore) Create(ctx context.Context, tx pgx.Tx, inbox *models.Inbox) error {
	q := `
		INSERT INTO inboxes (id, org_id, pod_id, address, display_name, status, settings, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`
	_, err := tx.Exec(ctx, q,
		inbox.ID,
		inbox.OrgID,
		inbox.PodID,
		inbox.Email,
		inbox.DisplayName,
		string(inbox.Status),
		inbox.Settings,
		inbox.CreatedAt,
		inbox.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert inbox: %w", err)
	}
	return nil
}

// GetByID retrieves an inbox by org and inbox ID using the provided transaction.
func (s *PostgresInboxStore) GetByID(ctx context.Context, tx pgx.Tx, orgID, inboxID uuid.UUID) (*models.Inbox, error) {
	q := `
		SELECT id, org_id, pod_id, address, display_name, status, settings, created_at, updated_at
		FROM inboxes
		WHERE org_id = $1 AND id = $2
	`
	row := tx.QueryRow(ctx, q, orgID, inboxID)
	return scanInbox(row)
}

// GetByAddress retrieves an inbox by email address without RLS (for the email pipeline).
func (s *PostgresInboxStore) GetByAddress(ctx context.Context, address string) (*models.Inbox, error) {
	q := `
		SELECT id, org_id, pod_id, address, display_name, status, settings, created_at, updated_at
		FROM inboxes
		WHERE address = $1
	`
	row := s.pool.QueryRow(ctx, q, address)
	return scanInbox(row)
}

// List returns a cursor-paginated list of inboxes for an org.
func (s *PostgresInboxStore) List(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, podID *uuid.UUID, limit int, cursor string) ([]*models.Inbox, string, error) {
	limit = pagination.ClampLimit(limit)

	var args []any
	args = append(args, orgID)

	baseWhere := "org_id = $1"
	argIdx := 2

	if podID != nil {
		baseWhere += fmt.Sprintf(" AND pod_id = $%d", argIdx)
		args = append(args, *podID)
		argIdx++
	}

	if cursor != "" {
		parts, err := pagination.DecodeCursor(cursor)
		if err != nil || len(parts) < 2 {
			return nil, "", fmt.Errorf("invalid cursor")
		}
		baseWhere += fmt.Sprintf(" AND (created_at, id) < ($%d::timestamptz, $%d::uuid)", argIdx, argIdx+1)
		args = append(args, parts[0], parts[1])
		argIdx += 2
	}

	args = append(args, limit+1)
	q := fmt.Sprintf(`
		SELECT id, org_id, pod_id, address, display_name, status, settings, created_at, updated_at
		FROM inboxes
		WHERE %s
		ORDER BY created_at DESC, id DESC
		LIMIT $%d
	`, baseWhere, argIdx)

	rows, err := tx.Query(ctx, q, args...)
	if err != nil {
		return nil, "", fmt.Errorf("list inboxes: %w", err)
	}
	defer rows.Close()

	var inboxes []*models.Inbox
	for rows.Next() {
		inbox, err := scanInboxRows(rows)
		if err != nil {
			return nil, "", err
		}
		inboxes = append(inboxes, inbox)
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("iterate inboxes: %w", err)
	}

	var nextCursor string
	if len(inboxes) > limit {
		inboxes = inboxes[:limit]
		last := inboxes[len(inboxes)-1]
		nextCursor = pagination.EncodeCursor(last.CreatedAt.Format(time.RFC3339Nano), last.ID.String())
	}

	return inboxes, nextCursor, nil
}

// Update applies patch fields to an inbox and returns the updated record.
func (s *PostgresInboxStore) Update(ctx context.Context, tx pgx.Tx, orgID, inboxID uuid.UUID, patch InboxPatch) (*models.Inbox, error) {
	setClauses := []string{}
	args := []any{}
	argIdx := 1

	if patch.DisplayName != nil {
		setClauses = append(setClauses, fmt.Sprintf("display_name = $%d", argIdx))
		args = append(args, *patch.DisplayName)
		argIdx++
	}
	if patch.Status != nil {
		setClauses = append(setClauses, fmt.Sprintf("status = $%d", argIdx))
		args = append(args, *patch.Status)
		argIdx++
	}
	if patch.Settings != nil {
		setClauses = append(setClauses, fmt.Sprintf("settings = $%d", argIdx))
		args = append(args, patch.Settings)
		argIdx++
	}

	if len(setClauses) == 0 {
		return s.GetByID(ctx, tx, orgID, inboxID)
	}

	setClauses = append(setClauses, fmt.Sprintf("updated_at = $%d", argIdx))
	args = append(args, time.Now())
	argIdx++

	args = append(args, orgID, inboxID)
	q := fmt.Sprintf(`
		UPDATE inboxes
		SET %s
		WHERE org_id = $%d AND id = $%d
		RETURNING id, org_id, pod_id, address, display_name, status, settings, created_at, updated_at
	`, joinClauses(setClauses), argIdx, argIdx+1)

	row := tx.QueryRow(ctx, q, args...)
	return scanInbox(row)
}

// Delete removes an inbox record.
func (s *PostgresInboxStore) Delete(ctx context.Context, tx pgx.Tx, orgID, inboxID uuid.UUID) error {
	q := `DELETE FROM inboxes WHERE org_id = $1 AND id = $2`
	tag, err := tx.Exec(ctx, q, orgID, inboxID)
	if err != nil {
		return fmt.Errorf("delete inbox: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

// scanInbox scans a single inbox from a pgx.Row.
func scanInbox(row pgx.Row) (*models.Inbox, error) {
	var inbox models.Inbox
	err := row.Scan(
		&inbox.ID,
		&inbox.OrgID,
		&inbox.PodID,
		&inbox.Email,
		&inbox.DisplayName,
		&inbox.Status,
		&inbox.Settings,
		&inbox.CreatedAt,
		&inbox.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scan inbox: %w", err)
	}
	return &inbox, nil
}

// scanInboxRows scans a single inbox from pgx.Rows.
func scanInboxRows(rows pgx.Rows) (*models.Inbox, error) {
	var inbox models.Inbox
	err := rows.Scan(
		&inbox.ID,
		&inbox.OrgID,
		&inbox.PodID,
		&inbox.Email,
		&inbox.DisplayName,
		&inbox.Status,
		&inbox.Settings,
		&inbox.CreatedAt,
		&inbox.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scan inbox row: %w", err)
	}
	return &inbox, nil
}

// joinClauses joins SET clause strings with commas.
func joinClauses(parts []string) string {
	result := ""
	for i, p := range parts {
		if i > 0 {
			result += ", "
		}
		result += p
	}
	return result
}
