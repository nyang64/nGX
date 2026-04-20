/*
 * Copyright (c) 2026 nyklabs.com. All rights reserved.
 *
 * Licensed under the nGX Commercial Source License v1.0.
 * See LICENSE file in the project root for full license information.
 */

package store

import (
	"context"
	"time"

	"agentmail/pkg/models"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DomainStore defines data access for domain_configs.
type DomainStore interface {
	Create(ctx context.Context, tx pgx.Tx, d *models.DomainConfig) error
	GetByID(ctx context.Context, tx pgx.Tx, orgID, domainID uuid.UUID) (*models.DomainConfig, error)
	GetByDomain(ctx context.Context, domain string) (*models.DomainConfig, error)
	List(ctx context.Context, tx pgx.Tx, orgID uuid.UUID) ([]*models.DomainConfig, error)
	UpdateStatus(ctx context.Context, tx pgx.Tx, orgID, domainID uuid.UUID, status string, verifiedAt *time.Time) error
	Delete(ctx context.Context, tx pgx.Tx, orgID, domainID uuid.UUID) error
}

// PostgresDomainStore implements DomainStore backed by PostgreSQL.
type PostgresDomainStore struct {
	pool *pgxpool.Pool
}

func NewPostgresDomainStore(pool *pgxpool.Pool) *PostgresDomainStore {
	return &PostgresDomainStore{pool: pool}
}

func (s *PostgresDomainStore) Create(ctx context.Context, tx pgx.Tx, d *models.DomainConfig) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO domain_configs
			(id, org_id, pod_id, domain, status, dkim_selector, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		d.ID, d.OrgID, d.PodID, d.Domain, d.Status, d.DKIMSelector, d.CreatedAt, d.UpdatedAt,
	)
	return err
}

func (s *PostgresDomainStore) GetByID(ctx context.Context, tx pgx.Tx, orgID, domainID uuid.UUID) (*models.DomainConfig, error) {
	row := tx.QueryRow(ctx, `
		SELECT id, org_id, pod_id, domain, status, dkim_selector, verified_at, created_at, updated_at
		FROM domain_configs
		WHERE org_id = $1 AND id = $2`, orgID, domainID)
	return scanDomain(row)
}

func (s *PostgresDomainStore) GetByDomain(ctx context.Context, domain string) (*models.DomainConfig, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, org_id, pod_id, domain, status, dkim_selector, verified_at, created_at, updated_at
		FROM domain_configs
		WHERE domain = $1`, domain)
	return scanDomain(row)
}

func (s *PostgresDomainStore) List(ctx context.Context, tx pgx.Tx, orgID uuid.UUID) ([]*models.DomainConfig, error) {
	rows, err := tx.Query(ctx, `
		SELECT id, org_id, pod_id, domain, status, dkim_selector, verified_at, created_at, updated_at
		FROM domain_configs
		WHERE org_id = $1
		ORDER BY created_at DESC`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*models.DomainConfig
	for rows.Next() {
		d, err := scanDomain(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *PostgresDomainStore) UpdateStatus(ctx context.Context, tx pgx.Tx, orgID, domainID uuid.UUID, status string, verifiedAt *time.Time) error {
	_, err := tx.Exec(ctx, `
		UPDATE domain_configs
		SET status = $3, verified_at = $4, updated_at = NOW()
		WHERE org_id = $1 AND id = $2`,
		orgID, domainID, status, verifiedAt,
	)
	return err
}

func (s *PostgresDomainStore) Delete(ctx context.Context, tx pgx.Tx, orgID, domainID uuid.UUID) error {
	_, err := tx.Exec(ctx, `
		DELETE FROM domain_configs WHERE org_id = $1 AND id = $2`,
		orgID, domainID,
	)
	return err
}

type scanner interface {
	Scan(dest ...any) error
}

func scanDomain(row scanner) (*models.DomainConfig, error) {
	var d models.DomainConfig
	err := row.Scan(
		&d.ID, &d.OrgID, &d.PodID, &d.Domain, &d.Status,
		&d.DKIMSelector, &d.VerifiedAt, &d.CreatedAt, &d.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &d, nil
}
