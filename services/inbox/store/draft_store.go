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

// DraftPatch contains optional fields to update on a draft.
type DraftPatch struct {
	Subject      *string
	To           []models.EmailAddress
	Cc           []models.EmailAddress
	Bcc          []models.EmailAddress
	TextBody     *string
	HtmlBody     *string
	ScheduledAt  *time.Time
	ReviewStatus *string
	ReviewNote   *string
	ReviewedAt   *time.Time
	ReviewedBy   *uuid.UUID
	Metadata     map[string]any
}

// DraftStore defines data access for drafts.
type DraftStore interface {
	Create(ctx context.Context, tx pgx.Tx, draft *models.Draft) error
	GetByID(ctx context.Context, tx pgx.Tx, orgID, draftID uuid.UUID) (*models.Draft, error)
	List(ctx context.Context, tx pgx.Tx, orgID, inboxID uuid.UUID, limit int, cursor string) ([]*models.Draft, string, error)
	Update(ctx context.Context, tx pgx.Tx, orgID, draftID uuid.UUID, patch DraftPatch) (*models.Draft, error)
	Delete(ctx context.Context, tx pgx.Tx, orgID, draftID uuid.UUID) error
}

// PostgresDraftStore implements DraftStore using PostgreSQL.
type PostgresDraftStore struct {
	pool *pgxpool.Pool
}

// NewPostgresDraftStore creates a new PostgresDraftStore.
func NewPostgresDraftStore(pool *pgxpool.Pool) *PostgresDraftStore {
	return &PostgresDraftStore{pool: pool}
}

// Create inserts a new draft.
func (s *PostgresDraftStore) Create(ctx context.Context, tx pgx.Tx, draft *models.Draft) error {
	toJSON, _ := json.Marshal(draft.To)
	ccJSON, _ := json.Marshal(draft.Cc)
	bccJSON, _ := json.Marshal(draft.Bcc)
	metaJSON, _ := json.Marshal(draft.Metadata)

	q := `
		INSERT INTO drafts (
			id, org_id, inbox_id, thread_id,
			to_addresses, cc_addresses, bcc_addresses,
			subject, body_text, body_html,
			metadata, review_status, review_note,
			reviewed_by, reviewed_at,
			scheduled_at,
			created_at, updated_at
		) VALUES (
			$1, $2, $3, $4,
			$5, $6, $7,
			$8, $9, $10,
			$11, $12, $13,
			$14, $15,
			$16,
			$17, $18
		)
	`
	var reviewedBy *string
	if draft.ReviewedBy != nil {
		s := draft.ReviewedBy.String()
		reviewedBy = &s
	}
	_, err := tx.Exec(ctx, q,
		draft.ID, draft.OrgID, draft.InboxID, draft.ThreadID,
		toJSON, ccJSON, bccJSON,
		draft.Subject, draft.TextBody, draft.HtmlBody,
		metaJSON, string(draft.ReviewStatus), draft.ReviewNote,
		reviewedBy, draft.ReviewedAt,
		draft.ScheduledAt,
		draft.CreatedAt, draft.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert draft: %w", err)
	}
	return nil
}

// GetByID retrieves a draft by org and draft ID.
func (s *PostgresDraftStore) GetByID(ctx context.Context, tx pgx.Tx, orgID, draftID uuid.UUID) (*models.Draft, error) {
	q := `
		SELECT id, org_id, inbox_id, thread_id,
		       to_addresses, cc_addresses, bcc_addresses,
		       subject, body_text, body_html,
		       metadata, review_status, review_note,
		       reviewed_by, reviewed_at,
		       scheduled_at,
		       created_at, updated_at
		FROM drafts
		WHERE org_id = $1 AND id = $2
	`
	row := tx.QueryRow(ctx, q, orgID, draftID)
	return scanDraft(row)
}

// List returns a cursor-paginated list of drafts in an inbox.
func (s *PostgresDraftStore) List(ctx context.Context, tx pgx.Tx, orgID, inboxID uuid.UUID, limit int, cursor string) ([]*models.Draft, string, error) {
	limit = pagination.ClampLimit(limit)

	args := []any{orgID, inboxID}
	argIdx := 3
	where := "org_id = $1 AND inbox_id = $2 AND review_status = 'pending'"

	if cursor != "" {
		parts, err := pagination.DecodeCursor(cursor)
		if err != nil || len(parts) < 2 {
			return nil, "", fmt.Errorf("invalid cursor")
		}
		where += fmt.Sprintf(" AND (created_at, id) < ($%d::timestamptz, $%d::uuid)", argIdx, argIdx+1)
		args = append(args, parts[0], parts[1])
		argIdx += 2
	}

	args = append(args, limit+1)
	q := fmt.Sprintf(`
		SELECT id, org_id, inbox_id, thread_id,
		       to_addresses, cc_addresses, bcc_addresses,
		       subject, body_text, body_html,
		       metadata, review_status, review_note,
		       reviewed_by, reviewed_at,
		       scheduled_at,
		       created_at, updated_at
		FROM drafts
		WHERE %s
		ORDER BY created_at DESC, id DESC
		LIMIT $%d
	`, where, argIdx)

	rows, err := tx.Query(ctx, q, args...)
	if err != nil {
		return nil, "", fmt.Errorf("list drafts: %w", err)
	}
	defer rows.Close()

	var drafts []*models.Draft
	for rows.Next() {
		d, err := scanDraftRows(rows)
		if err != nil {
			return nil, "", err
		}
		drafts = append(drafts, d)
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("iterate drafts: %w", err)
	}

	var nextCursor string
	if len(drafts) > limit {
		drafts = drafts[:limit]
		last := drafts[len(drafts)-1]
		nextCursor = pagination.EncodeCursor(last.CreatedAt.Format(time.RFC3339Nano), last.ID.String())
	}

	return drafts, nextCursor, nil
}

// Update applies patch fields to a draft.
func (s *PostgresDraftStore) Update(ctx context.Context, tx pgx.Tx, orgID, draftID uuid.UUID, patch DraftPatch) (*models.Draft, error) {
	setClauses := []string{}
	args := []any{}
	argIdx := 1

	if patch.Subject != nil {
		setClauses = append(setClauses, fmt.Sprintf("subject = $%d", argIdx))
		args = append(args, *patch.Subject)
		argIdx++
	}
	if patch.To != nil {
		b, _ := json.Marshal(patch.To)
		setClauses = append(setClauses, fmt.Sprintf("to_addresses = $%d", argIdx))
		args = append(args, b)
		argIdx++
	}
	if patch.Cc != nil {
		b, _ := json.Marshal(patch.Cc)
		setClauses = append(setClauses, fmt.Sprintf("cc_addresses = $%d", argIdx))
		args = append(args, b)
		argIdx++
	}
	if patch.Bcc != nil {
		b, _ := json.Marshal(patch.Bcc)
		setClauses = append(setClauses, fmt.Sprintf("bcc_addresses = $%d", argIdx))
		args = append(args, b)
		argIdx++
	}
	if patch.TextBody != nil {
		setClauses = append(setClauses, fmt.Sprintf("body_text = $%d", argIdx))
		args = append(args, *patch.TextBody)
		argIdx++
	}
	if patch.HtmlBody != nil {
		setClauses = append(setClauses, fmt.Sprintf("body_html = $%d", argIdx))
		args = append(args, *patch.HtmlBody)
		argIdx++
	}
	if patch.ScheduledAt != nil {
		setClauses = append(setClauses, fmt.Sprintf("scheduled_at = $%d", argIdx))
		args = append(args, *patch.ScheduledAt)
		argIdx++
	}
	if patch.ReviewStatus != nil {
		setClauses = append(setClauses, fmt.Sprintf("review_status = $%d", argIdx))
		args = append(args, *patch.ReviewStatus)
		argIdx++
	}
	if patch.ReviewNote != nil {
		setClauses = append(setClauses, fmt.Sprintf("review_note = $%d", argIdx))
		args = append(args, *patch.ReviewNote)
		argIdx++
	}
	if patch.ReviewedAt != nil {
		setClauses = append(setClauses, fmt.Sprintf("reviewed_at = $%d", argIdx))
		args = append(args, *patch.ReviewedAt)
		argIdx++
	}
	if patch.ReviewedBy != nil {
		setClauses = append(setClauses, fmt.Sprintf("reviewed_by = $%d", argIdx))
		args = append(args, patch.ReviewedBy.String())
		argIdx++
	}
	if patch.Metadata != nil {
		b, _ := json.Marshal(patch.Metadata)
		setClauses = append(setClauses, fmt.Sprintf("metadata = $%d", argIdx))
		args = append(args, b)
		argIdx++
	}

	if len(setClauses) == 0 {
		return s.GetByID(ctx, tx, orgID, draftID)
	}

	setClauses = append(setClauses, fmt.Sprintf("updated_at = $%d", argIdx))
	args = append(args, time.Now())
	argIdx++

	args = append(args, orgID, draftID)
	q := fmt.Sprintf(`
		UPDATE drafts
		SET %s
		WHERE org_id = $%d AND id = $%d
		RETURNING id, org_id, inbox_id, thread_id,
		          to_addresses, cc_addresses, bcc_addresses,
		          subject, body_text, body_html,
		          metadata, review_status, review_note,
		          reviewed_by, reviewed_at,
		          scheduled_at,
		          created_at, updated_at
	`, joinClauses(setClauses), argIdx, argIdx+1)

	row := tx.QueryRow(ctx, q, args...)
	return scanDraft(row)
}

// Delete removes a draft record.
func (s *PostgresDraftStore) Delete(ctx context.Context, tx pgx.Tx, orgID, draftID uuid.UUID) error {
	q := `DELETE FROM drafts WHERE org_id = $1 AND id = $2`
	tag, err := tx.Exec(ctx, q, orgID, draftID)
	if err != nil {
		return fmt.Errorf("delete draft: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func scanDraft(row pgx.Row) (*models.Draft, error) {
	var d models.Draft
	var toJSON, ccJSON, bccJSON, metaJSON []byte
	var reviewedBy *string
	err := row.Scan(
		&d.ID, &d.OrgID, &d.InboxID, &d.ThreadID,
		&toJSON, &ccJSON, &bccJSON,
		&d.Subject, &d.TextBody, &d.HtmlBody,
		&metaJSON, &d.ReviewStatus, &d.ReviewNote,
		&reviewedBy, &d.ReviewedAt,
		&d.ScheduledAt,
		&d.CreatedAt, &d.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scan draft: %w", err)
	}
	_ = json.Unmarshal(toJSON, &d.To)
	_ = json.Unmarshal(ccJSON, &d.Cc)
	_ = json.Unmarshal(bccJSON, &d.Bcc)
	_ = json.Unmarshal(metaJSON, &d.Metadata)
	if reviewedBy != nil {
		id, err := uuid.Parse(*reviewedBy)
		if err == nil {
			d.ReviewedBy = &id
		}
	}
	return &d, nil
}

func scanDraftRows(rows pgx.Rows) (*models.Draft, error) {
	var d models.Draft
	var toJSON, ccJSON, bccJSON, metaJSON []byte
	var reviewedBy *string
	err := rows.Scan(
		&d.ID, &d.OrgID, &d.InboxID, &d.ThreadID,
		&toJSON, &ccJSON, &bccJSON,
		&d.Subject, &d.TextBody, &d.HtmlBody,
		&metaJSON, &d.ReviewStatus, &d.ReviewNote,
		&reviewedBy, &d.ReviewedAt,
		&d.ScheduledAt,
		&d.CreatedAt, &d.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scan draft rows: %w", err)
	}
	_ = json.Unmarshal(toJSON, &d.To)
	_ = json.Unmarshal(ccJSON, &d.Cc)
	_ = json.Unmarshal(bccJSON, &d.Bcc)
	_ = json.Unmarshal(metaJSON, &d.Metadata)
	if reviewedBy != nil {
		id, err := uuid.Parse(*reviewedBy)
		if err == nil {
			d.ReviewedBy = &id
		}
	}
	return &d, nil
}
