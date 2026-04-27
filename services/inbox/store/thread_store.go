/*
 * Copyright (c) 2026 nyklabs.com. All rights reserved.
 *
 * Licensed under the nGX Commercial Source License v1.0.
 * See LICENSE file in the project root for full license information.
 */

package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"agentmail/pkg/models"
	"agentmail/pkg/pagination"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ThreadListQuery contains filter parameters for listing threads.
type ThreadListQuery struct {
	InboxID   uuid.UUID
	Status    *string
	LabelID   *uuid.UUID
	IsRead    *bool
	IsStarred *bool
	Cursor    string
	Limit     int
}

// ThreadPatch contains optional fields to update on a thread.
type ThreadPatch struct {
	Status    *string
	IsRead    *bool
	IsStarred *bool
}

// ThreadStore defines data access for threads.
type ThreadStore interface {
	Create(ctx context.Context, tx pgx.Tx, thread *models.Thread) error
	GetByID(ctx context.Context, tx pgx.Tx, orgID, threadID uuid.UUID) (*models.Thread, error)
	FindByMessageIDHeaders(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, inboxID uuid.UUID, msgIDs []string) (*models.Thread, error)
	List(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, q ThreadListQuery) ([]*models.Thread, string, error)
	Update(ctx context.Context, tx pgx.Tx, orgID, threadID uuid.UUID, patch ThreadPatch) (*models.Thread, error)
	IncrMessageCount(ctx context.Context, tx pgx.Tx, threadID uuid.UUID, lastMsgAt time.Time, snippet string) error
	ApplyLabel(ctx context.Context, tx pgx.Tx, threadID, labelID uuid.UUID) error
	RemoveLabel(ctx context.Context, tx pgx.Tx, threadID, labelID uuid.UUID) error
	GetLabels(ctx context.Context, tx pgx.Tx, orgID, threadID uuid.UUID) ([]*models.Label, error)
}

// PostgresThreadStore implements ThreadStore using PostgreSQL.
type PostgresThreadStore struct {
	pool *pgxpool.Pool
}

// NewPostgresThreadStore creates a new PostgresThreadStore.
func NewPostgresThreadStore(pool *pgxpool.Pool) *PostgresThreadStore {
	return &PostgresThreadStore{pool: pool}
}

// Create inserts a new thread.
func (s *PostgresThreadStore) Create(ctx context.Context, tx pgx.Tx, thread *models.Thread) error {
	participantsJSON, err := json.Marshal(thread.Participants)
	if err != nil {
		return fmt.Errorf("marshal participants: %w", err)
	}
	q := `
		INSERT INTO threads (id, org_id, inbox_id, subject, snippet, status, is_read, is_starred, message_count, participants, last_message_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
	`
	_, err = tx.Exec(ctx, q,
		thread.ID,
		thread.OrgID,
		thread.InboxID,
		thread.Subject,
		thread.Snippet,
		string(thread.Status),
		thread.IsRead,
		thread.IsStarred,
		thread.MessageCount,
		participantsJSON,
		thread.LastMessageAt,
		thread.CreatedAt,
		thread.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert thread: %w", err)
	}
	return nil
}

// GetByID retrieves a thread by org and thread ID.
func (s *PostgresThreadStore) GetByID(ctx context.Context, tx pgx.Tx, orgID, threadID uuid.UUID) (*models.Thread, error) {
	q := `
		SELECT id, org_id, inbox_id, subject, snippet, status, is_read, is_starred, message_count, participants, last_message_at, created_at, updated_at
		FROM threads
		WHERE org_id = $1 AND id = $2
	`
	row := tx.QueryRow(ctx, q, orgID, threadID)
	return scanThread(row)
}

// FindByMessageIDHeaders finds a thread that references any of the given Message-ID headers.
func (s *PostgresThreadStore) FindByMessageIDHeaders(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, inboxID uuid.UUID, msgIDs []string) (*models.Thread, error) {
	q := `
		SELECT DISTINCT t.id, t.org_id, t.inbox_id, t.subject, t.snippet, t.status, t.is_read, t.is_starred, t.message_count, t.participants, t.last_message_at, t.created_at, t.updated_at
		FROM threads t
		JOIN messages m ON m.thread_id = t.id
		WHERE t.org_id = $1 AND t.inbox_id = $2
		AND m.message_id_header = ANY($3)
		LIMIT 1
	`
	row := tx.QueryRow(ctx, q, orgID, inboxID, msgIDs)
	return scanThread(row)
}

// List returns a cursor-paginated list of threads matching the query.
func (s *PostgresThreadStore) List(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, q ThreadListQuery) ([]*models.Thread, string, error) {
	limit := pagination.ClampLimit(q.Limit)

	args := []any{orgID, q.InboxID}
	argIdx := 3
	where := "t.org_id = $1 AND t.inbox_id = $2"

	if q.Status != nil {
		where += fmt.Sprintf(" AND t.status = $%d", argIdx)
		args = append(args, *q.Status)
		argIdx++
	}
	if q.IsRead != nil {
		where += fmt.Sprintf(" AND t.is_read = $%d", argIdx)
		args = append(args, *q.IsRead)
		argIdx++
	}
	if q.IsStarred != nil {
		where += fmt.Sprintf(" AND t.is_starred = $%d", argIdx)
		args = append(args, *q.IsStarred)
		argIdx++
	}
	if q.LabelID != nil {
		where += fmt.Sprintf(" AND EXISTS (SELECT 1 FROM thread_labels tl WHERE tl.thread_id = t.id AND tl.label_id = $%d)", argIdx)
		args = append(args, *q.LabelID)
		argIdx++
	}

	if q.Cursor != "" {
		parts, err := pagination.DecodeCursor(q.Cursor)
		if err != nil || len(parts) < 2 {
			return nil, "", fmt.Errorf("invalid cursor")
		}
		where += fmt.Sprintf(" AND (t.last_message_at, t.id) < ($%d::timestamptz, $%d::uuid)", argIdx, argIdx+1)
		args = append(args, parts[0], parts[1])
		argIdx += 2
	}

	args = append(args, limit+1)
	sql := fmt.Sprintf(`
		SELECT t.id, t.org_id, t.inbox_id, t.subject, t.snippet, t.status, t.is_read, t.is_starred, t.message_count, t.participants, t.last_message_at, t.created_at, t.updated_at
		FROM threads t
		WHERE %s
		ORDER BY t.last_message_at DESC NULLS LAST, t.id DESC
		LIMIT $%d
	`, where, argIdx)

	rows, err := tx.Query(ctx, sql, args...)
	if err != nil {
		return nil, "", fmt.Errorf("list threads: %w", err)
	}
	defer rows.Close()

	var threads []*models.Thread
	for rows.Next() {
		t, err := scanThreadRows(rows)
		if err != nil {
			return nil, "", err
		}
		threads = append(threads, t)
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("iterate threads: %w", err)
	}

	var nextCursor string
	if len(threads) > limit {
		threads = threads[:limit]
		last := threads[len(threads)-1]
		ts := ""
		if last.LastMessageAt != nil {
			ts = last.LastMessageAt.Format(time.RFC3339Nano)
		}
		nextCursor = pagination.EncodeCursor(ts, last.ID.String())
	}

	return threads, nextCursor, nil
}

// Update applies patch fields to a thread.
func (s *PostgresThreadStore) Update(ctx context.Context, tx pgx.Tx, orgID, threadID uuid.UUID, patch ThreadPatch) (*models.Thread, error) {
	setClauses := []string{}
	args := []any{}
	argIdx := 1

	if patch.Status != nil {
		setClauses = append(setClauses, fmt.Sprintf("status = $%d", argIdx))
		args = append(args, *patch.Status)
		argIdx++
	}
	if patch.IsRead != nil {
		setClauses = append(setClauses, fmt.Sprintf("is_read = $%d", argIdx))
		args = append(args, *patch.IsRead)
		argIdx++
	}
	if patch.IsStarred != nil {
		setClauses = append(setClauses, fmt.Sprintf("is_starred = $%d", argIdx))
		args = append(args, *patch.IsStarred)
		argIdx++
	}

	if len(setClauses) == 0 {
		return s.GetByID(ctx, tx, orgID, threadID)
	}

	setClauses = append(setClauses, fmt.Sprintf("updated_at = $%d", argIdx))
	args = append(args, time.Now())
	argIdx++

	args = append(args, orgID, threadID)
	q := fmt.Sprintf(`
		UPDATE threads
		SET %s
		WHERE org_id = $%d AND id = $%d
		RETURNING id, org_id, inbox_id, subject, snippet, status, is_read, is_starred, message_count, participants, last_message_at, created_at, updated_at
	`, joinClauses(setClauses), argIdx, argIdx+1)

	row := tx.QueryRow(ctx, q, args...)
	return scanThread(row)
}

// IncrMessageCount increments the message count on a thread and updates last_message_at and snippet.
func (s *PostgresThreadStore) IncrMessageCount(ctx context.Context, tx pgx.Tx, threadID uuid.UUID, lastMsgAt time.Time, snippet string) error {
	q := `
		UPDATE threads
		SET message_count = message_count + 1,
		    last_message_at = $2,
		    snippet = $3,
		    updated_at = NOW()
		WHERE id = $1
	`
	_, err := tx.Exec(ctx, q, threadID, lastMsgAt, snippet)
	if err != nil {
		return fmt.Errorf("incr message count: %w", err)
	}
	return nil
}

// ApplyLabel adds a label to a thread.
func (s *PostgresThreadStore) ApplyLabel(ctx context.Context, tx pgx.Tx, threadID, labelID uuid.UUID) error {
	q := `
		INSERT INTO thread_labels (thread_id, label_id, applied_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (thread_id, label_id) DO NOTHING
	`
	_, err := tx.Exec(ctx, q, threadID, labelID)
	if err != nil {
		return fmt.Errorf("apply label: %w", err)
	}
	return nil
}

// RemoveLabel removes a label from a thread.
func (s *PostgresThreadStore) RemoveLabel(ctx context.Context, tx pgx.Tx, threadID, labelID uuid.UUID) error {
	q := `DELETE FROM thread_labels WHERE thread_id = $1 AND label_id = $2`
	_, err := tx.Exec(ctx, q, threadID, labelID)
	if err != nil {
		return fmt.Errorf("remove label: %w", err)
	}
	return nil
}

// GetLabels retrieves all labels applied to a thread.
func (s *PostgresThreadStore) GetLabels(ctx context.Context, tx pgx.Tx, orgID, threadID uuid.UUID) ([]*models.Label, error) {
	q := `
		SELECT l.id, l.org_id, l.name, l.color, l.description, l.created_at
		FROM labels l
		JOIN thread_labels tl ON tl.label_id = l.id
		WHERE l.org_id = $1 AND tl.thread_id = $2
		ORDER BY l.name
	`
	rows, err := tx.Query(ctx, q, orgID, threadID)
	if err != nil {
		return nil, fmt.Errorf("get labels: %w", err)
	}
	defer rows.Close()

	var labels []*models.Label
	for rows.Next() {
		var l models.Label
		if err := rows.Scan(&l.ID, &l.OrgID, &l.Name, &l.Color, &l.Description, &l.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan label: %w", err)
		}
		labels = append(labels, &l)
	}
	return labels, rows.Err()
}

func scanThread(row pgx.Row) (*models.Thread, error) {
	var t models.Thread
	var participantsJSON []byte
	err := row.Scan(
		&t.ID,
		&t.OrgID,
		&t.InboxID,
		&t.Subject,
		&t.Snippet,
		&t.Status,
		&t.IsRead,
		&t.IsStarred,
		&t.MessageCount,
		&participantsJSON,
		&t.LastMessageAt,
		&t.CreatedAt,
		&t.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scan thread: %w", err)
	}
	if err := json.Unmarshal(participantsJSON, &t.Participants); err != nil {
		return nil, fmt.Errorf("unmarshal participants: %w", err)
	}
	return &t, nil
}

func scanThreadRows(rows pgx.Rows) (*models.Thread, error) {
	var t models.Thread
	var participantsJSON []byte
	err := rows.Scan(
		&t.ID,
		&t.OrgID,
		&t.InboxID,
		&t.Subject,
		&t.Snippet,
		&t.Status,
		&t.IsRead,
		&t.IsStarred,
		&t.MessageCount,
		&participantsJSON,
		&t.LastMessageAt,
		&t.CreatedAt,
		&t.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scan thread rows: %w", err)
	}
	if err := json.Unmarshal(participantsJSON, &t.Participants); err != nil {
		return nil, fmt.Errorf("unmarshal participants: %w", err)
	}
	return &t, nil
}
