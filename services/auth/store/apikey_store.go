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

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"agentmail/pkg/auth"
	"agentmail/pkg/db"
	"agentmail/pkg/models"
)

// APIKeyStore defines the persistence operations for API keys.
type APIKeyStore interface {
	Create(ctx context.Context, orgID uuid.UUID, name string, scopes []string, podID *uuid.UUID) (*models.APIKey, string, error)
	Validate(ctx context.Context, plaintextKey string) (*models.APIKey, error)
	List(ctx context.Context, orgID uuid.UUID) ([]*models.APIKey, error)
	Revoke(ctx context.Context, orgID, keyID uuid.UUID) error
	GetByID(ctx context.Context, orgID, keyID uuid.UUID) (*models.APIKey, error)
}

// PostgresAPIKeyStore is a PostgreSQL-backed implementation of APIKeyStore.
type PostgresAPIKeyStore struct {
	pool *pgxpool.Pool
}

// NewPostgresAPIKeyStore creates a new PostgresAPIKeyStore.
func NewPostgresAPIKeyStore(pool *pgxpool.Pool) *PostgresAPIKeyStore {
	return &PostgresAPIKeyStore{pool: pool}
}

// Create generates a new API key, inserts it into the database, and returns the
// model and the one-time-visible plaintext key.
func (s *PostgresAPIKeyStore) Create(ctx context.Context, orgID uuid.UUID, name string, scopes []string, podID *uuid.UUID) (*models.APIKey, string, error) {
	plaintext, keyHash, displayPrefix, err := auth.GenerateAPIKey()
	if err != nil {
		return nil, "", fmt.Errorf("generate api key: %w", err)
	}

	id := uuid.New()
	now := time.Now().UTC()

	const query = `
		INSERT INTO api_keys (id, org_id, name, key_prefix, key_hash, scopes, pod_id, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, org_id, name, key_prefix, key_hash, scopes, pod_id,
		          last_used_at, expires_at, revoked_at, created_at`

	key := &models.APIKey{}
	row := s.pool.QueryRow(ctx, query,
		id, orgID, name, displayPrefix, keyHash, scopes, podID, now,
	)
	if err := row.Scan(
		&key.ID, &key.OrgID, &key.Name, &key.KeyPrefix, &key.KeyHash,
		&key.Scopes, &key.PodID, &key.LastUsedAt, &key.ExpiresAt, &key.RevokedAt, &key.CreatedAt,
	); err != nil {
		return nil, "", fmt.Errorf("insert api key: %w", err)
	}

	return key, plaintext, nil
}

// Validate looks up an API key by its hash, confirms it is active, updates
// last_used_at, and returns the key model. Uses a direct pool connection so
// that no RLS context is required.
func (s *PostgresAPIKeyStore) Validate(ctx context.Context, plaintextKey string) (*models.APIKey, error) {
	hash := auth.HashAPIKey(plaintextKey)

	const selectQuery = `
		SELECT id, org_id, name, key_prefix, key_hash, scopes, pod_id,
		       last_used_at, expires_at, revoked_at, created_at
		FROM api_keys
		WHERE key_hash = $1
		  AND revoked_at IS NULL
		  AND (expires_at IS NULL OR expires_at > NOW())`

	key := &models.APIKey{}
	row := s.pool.QueryRow(ctx, selectQuery, hash)
	if err := row.Scan(
		&key.ID, &key.OrgID, &key.Name, &key.KeyPrefix, &key.KeyHash,
		&key.Scopes, &key.PodID, &key.LastUsedAt, &key.ExpiresAt, &key.RevokedAt, &key.CreatedAt,
	); err != nil {
		if db.IsNotFound(err) {
			return nil, fmt.Errorf("api key not found or inactive")
		}
		return nil, fmt.Errorf("validate api key: %w", err)
	}

	// Update last_used_at in the background; failure is non-fatal.
	const updateQuery = `UPDATE api_keys SET last_used_at = NOW() WHERE id = $1`
	if _, err := s.pool.Exec(ctx, updateQuery, key.ID); err != nil {
		// Log-worthy but not blocking.
		_ = err
	}

	return key, nil
}

// List returns all non-revoked API keys for the given org, enforcing RLS.
func (s *PostgresAPIKeyStore) List(ctx context.Context, orgID uuid.UUID) ([]*models.APIKey, error) {
	var keys []*models.APIKey

	err := db.WithOrgTx(ctx, s.pool, orgID, func(tx pgx.Tx) error {
		const query = `
			SELECT id, org_id, name, key_prefix, key_hash, scopes, pod_id,
			       last_used_at, expires_at, revoked_at, created_at
			FROM api_keys
			WHERE org_id = $1
			  AND revoked_at IS NULL
			ORDER BY created_at DESC`

		rows, err := tx.Query(ctx, query, orgID)
		if err != nil {
			return fmt.Errorf("list api keys: %w", err)
		}
		defer rows.Close()

		for rows.Next() {
			key := &models.APIKey{}
			if err := rows.Scan(
				&key.ID, &key.OrgID, &key.Name, &key.KeyPrefix, &key.KeyHash,
				&key.Scopes, &key.PodID, &key.LastUsedAt, &key.ExpiresAt, &key.RevokedAt, &key.CreatedAt,
			); err != nil {
				return fmt.Errorf("scan api key: %w", err)
			}
			keys = append(keys, key)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}

	if keys == nil {
		keys = []*models.APIKey{}
	}
	return keys, nil
}

// Revoke marks an API key as revoked. The org check ensures a key can only be
// revoked by its owner organization.
func (s *PostgresAPIKeyStore) Revoke(ctx context.Context, orgID, keyID uuid.UUID) error {
	return db.WithOrgTx(ctx, s.pool, orgID, func(tx pgx.Tx) error {
		const query = `
			UPDATE api_keys
			SET revoked_at = NOW()
			WHERE id = $1
			  AND org_id = $2
			  AND revoked_at IS NULL`

		tag, err := tx.Exec(ctx, query, keyID, orgID)
		if err != nil {
			return fmt.Errorf("revoke api key: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return fmt.Errorf("api key not found or already revoked")
		}
		return nil
	})
}

// GetByID fetches a single API key by ID, scoped to the given org.
func (s *PostgresAPIKeyStore) GetByID(ctx context.Context, orgID, keyID uuid.UUID) (*models.APIKey, error) {
	var key *models.APIKey

	err := db.WithOrgTx(ctx, s.pool, orgID, func(tx pgx.Tx) error {
		const query = `
			SELECT id, org_id, name, key_prefix, key_hash, scopes, pod_id,
			       last_used_at, expires_at, revoked_at, created_at
			FROM api_keys
			WHERE id = $1
			  AND org_id = $2`

		k := &models.APIKey{}
		row := tx.QueryRow(ctx, query, keyID, orgID)
		if err := row.Scan(
			&k.ID, &k.OrgID, &k.Name, &k.KeyPrefix, &k.KeyHash,
			&k.Scopes, &k.PodID, &k.LastUsedAt, &k.ExpiresAt, &k.RevokedAt, &k.CreatedAt,
		); err != nil {
			if db.IsNotFound(err) {
				return fmt.Errorf("api key not found")
			}
			return fmt.Errorf("get api key: %w", err)
		}
		key = k
		return nil
	})
	if err != nil {
		return nil, err
	}

	return key, nil
}
