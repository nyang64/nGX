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

	"agentmail/pkg/models"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// OrgStore manages organizations and pods directly in PostgreSQL.
type OrgStore interface {
	CreateOrg(ctx context.Context, name, slug string) (*models.Organization, error)
	GetOrg(ctx context.Context, orgID uuid.UUID) (*models.Organization, error)
	CreatePod(ctx context.Context, tx pgx.Tx, pod *models.Pod) error
	GetPod(ctx context.Context, tx pgx.Tx, orgID, podID uuid.UUID) (*models.Pod, error)
	ListPods(ctx context.Context, tx pgx.Tx, orgID uuid.UUID) ([]*models.Pod, error)
	UpdatePod(ctx context.Context, tx pgx.Tx, orgID, podID uuid.UUID, name, desc string) (*models.Pod, error)
	DeletePod(ctx context.Context, tx pgx.Tx, orgID, podID uuid.UUID) error
}

type pgOrgStore struct {
	pool *pgxpool.Pool
}

// NewOrgStore creates a PostgreSQL-backed OrgStore.
func NewOrgStore(pool *pgxpool.Pool) OrgStore {
	return &pgOrgStore{pool: pool}
}

func (s *pgOrgStore) CreateOrg(ctx context.Context, name, slug string) (*models.Organization, error) {
	org := &models.Organization{
		ID:        uuid.New(),
		Name:      name,
		Slug:      slug,
		Plan:      "free",
		Settings:  map[string]any{},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}

	_, err := s.pool.Exec(ctx,
		`INSERT INTO organizations (id, name, slug, plan, settings, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		org.ID, org.Name, org.Slug, org.Plan, org.Settings, org.CreatedAt, org.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("insert organization: %w", err)
	}
	return org, nil
}

func (s *pgOrgStore) GetOrg(ctx context.Context, orgID uuid.UUID) (*models.Organization, error) {
	var org models.Organization
	err := s.pool.QueryRow(ctx,
		`SELECT id, name, slug, plan, settings, created_at, updated_at
		 FROM organizations WHERE id = $1`,
		orgID,
	).Scan(&org.ID, &org.Name, &org.Slug, &org.Plan, &org.Settings, &org.CreatedAt, &org.UpdatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("organization not found")
		}
		return nil, fmt.Errorf("get organization: %w", err)
	}
	return &org, nil
}

func (s *pgOrgStore) CreatePod(ctx context.Context, tx pgx.Tx, pod *models.Pod) error {
	pod.ID = uuid.New()
	pod.CreatedAt = time.Now().UTC()
	pod.UpdatedAt = time.Now().UTC()
	if pod.Settings == nil {
		pod.Settings = map[string]any{}
	}

	querier := querier(s.pool, tx)
	_, err := querier.Exec(ctx,
		`INSERT INTO pods (id, org_id, name, slug, description, settings, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		pod.ID, pod.OrgID, pod.Name, pod.Slug, pod.Description, pod.Settings, pod.CreatedAt, pod.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert pod: %w", err)
	}
	return nil
}

func (s *pgOrgStore) GetPod(ctx context.Context, tx pgx.Tx, orgID, podID uuid.UUID) (*models.Pod, error) {
	var pod models.Pod
	q := querier(s.pool, tx)
	err := q.QueryRow(ctx,
		`SELECT id, org_id, name, slug, description, settings, created_at, updated_at
		 FROM pods WHERE org_id = $1 AND id = $2`,
		orgID, podID,
	).Scan(&pod.ID, &pod.OrgID, &pod.Name, &pod.Slug, &pod.Description, &pod.Settings, &pod.CreatedAt, &pod.UpdatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("pod not found")
		}
		return nil, fmt.Errorf("get pod: %w", err)
	}
	return &pod, nil
}

func (s *pgOrgStore) ListPods(ctx context.Context, tx pgx.Tx, orgID uuid.UUID) ([]*models.Pod, error) {
	q := querier(s.pool, tx)
	rows, err := q.Query(ctx,
		`SELECT id, org_id, name, slug, description, settings, created_at, updated_at
		 FROM pods WHERE org_id = $1 ORDER BY created_at ASC`,
		orgID,
	)
	if err != nil {
		return nil, fmt.Errorf("list pods: %w", err)
	}
	defer rows.Close()

	var pods []*models.Pod
	for rows.Next() {
		var pod models.Pod
		if err := rows.Scan(&pod.ID, &pod.OrgID, &pod.Name, &pod.Slug, &pod.Description, &pod.Settings, &pod.CreatedAt, &pod.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan pod: %w", err)
		}
		pods = append(pods, &pod)
	}
	return pods, rows.Err()
}

func (s *pgOrgStore) UpdatePod(ctx context.Context, tx pgx.Tx, orgID, podID uuid.UUID, name, desc string) (*models.Pod, error) {
	q := querier(s.pool, tx)
	now := time.Now().UTC()
	_, err := q.Exec(ctx,
		`UPDATE pods SET name = $1, description = $2, updated_at = $3
		 WHERE org_id = $4 AND id = $5`,
		name, desc, now, orgID, podID,
	)
	if err != nil {
		return nil, fmt.Errorf("update pod: %w", err)
	}
	return s.GetPod(ctx, tx, orgID, podID)
}

func (s *pgOrgStore) DeletePod(ctx context.Context, tx pgx.Tx, orgID, podID uuid.UUID) error {
	q := querier(s.pool, tx)
	_, err := q.Exec(ctx,
		`DELETE FROM pods WHERE org_id = $1 AND id = $2`,
		orgID, podID,
	)
	if err != nil {
		return fmt.Errorf("delete pod: %w", err)
	}
	return nil
}

// executor is the common interface between *pgxpool.Pool and pgx.Tx.
type executor interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// querier returns the transaction if non-nil, otherwise the pool.
func querier(pool *pgxpool.Pool, tx pgx.Tx) executor {
	if tx != nil {
		return tx
	}
	return pool
}
