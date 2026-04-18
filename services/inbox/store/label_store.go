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

	"agentmail/pkg/models"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// LabelPatch contains optional fields to update on a label.
type LabelPatch struct {
	Name        *string
	Color       *string
	Description *string
}

// LabelStore defines data access for labels.
type LabelStore interface {
	Create(ctx context.Context, tx pgx.Tx, label *models.Label) error
	GetByID(ctx context.Context, tx pgx.Tx, orgID, labelID uuid.UUID) (*models.Label, error)
	List(ctx context.Context, tx pgx.Tx, orgID uuid.UUID) ([]*models.Label, error)
	Update(ctx context.Context, tx pgx.Tx, orgID, labelID uuid.UUID, patch LabelPatch) (*models.Label, error)
	Delete(ctx context.Context, tx pgx.Tx, orgID, labelID uuid.UUID) error
}

// PostgresLabelStore implements LabelStore using PostgreSQL.
type PostgresLabelStore struct {
	pool *pgxpool.Pool
}

// NewPostgresLabelStore creates a new PostgresLabelStore.
func NewPostgresLabelStore(pool *pgxpool.Pool) *PostgresLabelStore {
	return &PostgresLabelStore{pool: pool}
}

// Create inserts a new label.
func (s *PostgresLabelStore) Create(ctx context.Context, tx pgx.Tx, label *models.Label) error {
	q := `
		INSERT INTO labels (id, org_id, name, color, description, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`
	_, err := tx.Exec(ctx, q,
		label.ID, label.OrgID,
		label.Name, label.Color, label.Description,
		label.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert label: %w", err)
	}
	return nil
}

// GetByID retrieves a label by org and label ID.
func (s *PostgresLabelStore) GetByID(ctx context.Context, tx pgx.Tx, orgID, labelID uuid.UUID) (*models.Label, error) {
	q := `
		SELECT id, org_id, name, color, description, created_at
		FROM labels
		WHERE org_id = $1 AND id = $2
	`
	row := tx.QueryRow(ctx, q, orgID, labelID)
	var l models.Label
	if err := row.Scan(&l.ID, &l.OrgID, &l.Name, &l.Color, &l.Description, &l.CreatedAt); err != nil {
		return nil, fmt.Errorf("scan label: %w", err)
	}
	return &l, nil
}

// List returns all labels for an org.
func (s *PostgresLabelStore) List(ctx context.Context, tx pgx.Tx, orgID uuid.UUID) ([]*models.Label, error) {
	q := `
		SELECT id, org_id, name, color, description, created_at
		FROM labels
		WHERE org_id = $1
		ORDER BY name ASC
	`
	rows, err := tx.Query(ctx, q, orgID)
	if err != nil {
		return nil, fmt.Errorf("list labels: %w", err)
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

// Update applies patch fields to a label.
func (s *PostgresLabelStore) Update(ctx context.Context, tx pgx.Tx, orgID, labelID uuid.UUID, patch LabelPatch) (*models.Label, error) {
	setClauses := []string{}
	args := []any{}
	argIdx := 1

	if patch.Name != nil {
		setClauses = append(setClauses, fmt.Sprintf("name = $%d", argIdx))
		args = append(args, *patch.Name)
		argIdx++
	}
	if patch.Color != nil {
		setClauses = append(setClauses, fmt.Sprintf("color = $%d", argIdx))
		args = append(args, *patch.Color)
		argIdx++
	}
	if patch.Description != nil {
		setClauses = append(setClauses, fmt.Sprintf("description = $%d", argIdx))
		args = append(args, *patch.Description)
		argIdx++
	}

	if len(setClauses) == 0 {
		return s.GetByID(ctx, tx, orgID, labelID)
	}

	args = append(args, orgID, labelID)
	q := fmt.Sprintf(`
		UPDATE labels
		SET %s
		WHERE org_id = $%d AND id = $%d
		RETURNING id, org_id, name, color, description, created_at
	`, joinClauses(setClauses), argIdx, argIdx+1)

	row := tx.QueryRow(ctx, q, args...)
	var l models.Label
	if err := row.Scan(&l.ID, &l.OrgID, &l.Name, &l.Color, &l.Description, &l.CreatedAt); err != nil {
		return nil, fmt.Errorf("scan updated label: %w", err)
	}
	return &l, nil
}

// Delete removes a label.
func (s *PostgresLabelStore) Delete(ctx context.Context, tx pgx.Tx, orgID, labelID uuid.UUID) error {
	q := `DELETE FROM labels WHERE org_id = $1 AND id = $2`
	tag, err := tx.Exec(ctx, q, orgID, labelID)
	if err != nil {
		return fmt.Errorf("delete label: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}
