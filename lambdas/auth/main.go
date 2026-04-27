/*
 * Copyright (c) 2026 nyklabs.com. All rights reserved.
 *
 * Licensed under the nGX Commercial Source License v1.0.
 * See LICENSE file in the project root for full license information.
 */

package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	authpkg "agentmail/pkg/auth"
	dbpkg "agentmail/pkg/db"
	"agentmail/pkg/models"
	"agentmail/pkg/pagination"
	"agentmail/lambdas/shared"
)

var pool *pgxpool.Pool

func init() {
	pool = shared.InitDB()
}

func handler(ctx context.Context, event events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	claims, err := shared.ExtractClaims(event)
	if err != nil {
		return shared.Error(401, "unauthorized"), nil
	}

	method := event.HTTPMethod
	resource := event.Resource

	if !claims.HasScope(authpkg.ScopeOrgAdmin) {
		return shared.Error(403, "insufficient scope"), nil
	}

	switch {
	case method == "GET" && resource == "/v1/keys":
		return listKeys(ctx, event, claims)
	case method == "POST" && resource == "/v1/keys":
		return createKey(ctx, event, claims)
	case method == "GET" && resource == "/v1/keys/{keyId}":
		return getKey(ctx, event, claims)
	case method == "DELETE" && resource == "/v1/keys/{keyId}":
		return deleteKey(ctx, event, claims)
	default:
		return shared.Error(404, "not found"), nil
	}
}

func listKeys(ctx context.Context, event events.APIGatewayProxyRequest, claims *authpkg.Claims) (events.APIGatewayProxyResponse, error) {
	limit := 0
	if l := event.QueryStringParameters["limit"]; l != "" {
		fmt.Sscanf(l, "%d", &limit)
	}
	cursor := event.QueryStringParameters["cursor"]
	keys, nextCursor, err := fetchKeys(ctx, claims.OrgID, limit, cursor)
	if err != nil {
		if strings.Contains(err.Error(), "invalid cursor") {
			return shared.Error(400, "invalid cursor"), nil
		}
		return shared.Error(500, "failed to list keys"), nil
	}
	resp := map[string]any{"keys": keys}
	if nextCursor != "" {
		resp["next_cursor"] = nextCursor
	}
	return shared.JSON(200, resp), nil
}

func createKey(ctx context.Context, event events.APIGatewayProxyRequest, claims *authpkg.Claims) (events.APIGatewayProxyResponse, error) {
	var req struct {
		Name      string     `json:"name"`
		Scopes    []string   `json:"scopes"`
		PodID     *uuid.UUID `json:"pod_id"`
		ExpiresAt *time.Time `json:"expires_at"`
	}
	if err := shared.Decode(event, &req); err != nil || req.Name == "" {
		return shared.Error(400, "name is required"), nil
	}

	key, plaintext, err := insertKey(ctx, claims.OrgID, req.Name, req.Scopes, req.PodID, req.ExpiresAt)
	if err != nil {
		return shared.Error(500, "failed to create key"), nil
	}

	// Return the key with the one-time-visible plaintext.
	resp := struct {
		*models.APIKey
		Key string `json:"key"`
	}{
		APIKey: key,
		Key:    plaintext,
	}
	return shared.JSON(201, resp), nil
}

func getKey(ctx context.Context, event events.APIGatewayProxyRequest, claims *authpkg.Claims) (events.APIGatewayProxyResponse, error) {
	keyID, err := shared.ParseUUID(shared.PathParam(event, "keyId"))
	if err != nil {
		return shared.Error(400, "invalid key ID"), nil
	}
	key, err := fetchKey(ctx, claims.OrgID, keyID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return shared.Error(404, "key not found"), nil
		}
		return shared.Error(500, "failed to get key"), nil
	}
	return shared.JSON(200, key), nil
}

func deleteKey(ctx context.Context, event events.APIGatewayProxyRequest, claims *authpkg.Claims) (events.APIGatewayProxyResponse, error) {
	keyID, err := shared.ParseUUID(shared.PathParam(event, "keyId"))
	if err != nil {
		return shared.Error(400, "invalid key ID"), nil
	}
	if err := revokeKey(ctx, claims.OrgID, keyID); err != nil {
		if strings.Contains(err.Error(), "not found") {
			return shared.Error(404, "key not found or already revoked"), nil
		}
		return shared.Error(500, "failed to revoke key"), nil
	}
	return events.APIGatewayProxyResponse{StatusCode: 204}, nil
}

// --- store functions ---

func fetchKeys(ctx context.Context, orgID uuid.UUID, limit int, cursor string) ([]*models.APIKey, string, error) {
	limit = pagination.ClampLimit(limit)

	where := "org_id = $1 AND revoked_at IS NULL"
	args := []any{orgID}
	argIdx := 2

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
		SELECT id, org_id, name, key_prefix, key_hash, scopes, pod_id,
		       last_used_at, expires_at, revoked_at, created_at
		FROM api_keys
		WHERE %s
		ORDER BY created_at DESC, id DESC
		LIMIT $%d`, where, argIdx)

	var keys []*models.APIKey
	err := dbpkg.WithOrgTx(ctx, pool, orgID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, q, args...)
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
		return nil, "", err
	}
	if keys == nil {
		keys = []*models.APIKey{}
	}

	var nextCursor string
	if len(keys) > limit {
		keys = keys[:limit]
		last := keys[len(keys)-1]
		nextCursor = pagination.EncodeCursor(last.CreatedAt.Format(time.RFC3339Nano), last.ID.String())
	}
	return keys, nextCursor, nil
}

func insertKey(ctx context.Context, orgID uuid.UUID, name string, scopes []string, podID *uuid.UUID, expiresAt *time.Time) (*models.APIKey, string, error) {
	plaintext, keyHash, displayPrefix, err := authpkg.GenerateAPIKey()
	if err != nil {
		return nil, "", fmt.Errorf("generate api key: %w", err)
	}

	id := uuid.New()
	now := time.Now().UTC()

	const q = `
		INSERT INTO api_keys (id, org_id, name, key_prefix, key_hash, scopes, pod_id, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id, org_id, name, key_prefix, key_hash, scopes, pod_id,
		          last_used_at, expires_at, revoked_at, created_at`

	key := &models.APIKey{}
	row := pool.QueryRow(ctx, q, id, orgID, name, displayPrefix, keyHash, scopes, podID, expiresAt, now)
	if err := row.Scan(
		&key.ID, &key.OrgID, &key.Name, &key.KeyPrefix, &key.KeyHash,
		&key.Scopes, &key.PodID, &key.LastUsedAt, &key.ExpiresAt, &key.RevokedAt, &key.CreatedAt,
	); err != nil {
		return nil, "", fmt.Errorf("insert api key: %w", err)
	}
	return key, plaintext, nil
}

func fetchKey(ctx context.Context, orgID, keyID uuid.UUID) (*models.APIKey, error) {
	var key *models.APIKey
	err := dbpkg.WithOrgTx(ctx, pool, orgID, func(tx pgx.Tx) error {
		const q = `
			SELECT id, org_id, name, key_prefix, key_hash, scopes, pod_id,
			       last_used_at, expires_at, revoked_at, created_at
			FROM api_keys
			WHERE id = $1 AND org_id = $2 AND revoked_at IS NULL`
		k := &models.APIKey{}
		row := tx.QueryRow(ctx, q, keyID, orgID)
		if err := row.Scan(
			&k.ID, &k.OrgID, &k.Name, &k.KeyPrefix, &k.KeyHash,
			&k.Scopes, &k.PodID, &k.LastUsedAt, &k.ExpiresAt, &k.RevokedAt, &k.CreatedAt,
		); err != nil {
			if dbpkg.IsNotFound(err) {
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

func revokeKey(ctx context.Context, orgID, keyID uuid.UUID) error {
	return dbpkg.WithOrgTx(ctx, pool, orgID, func(tx pgx.Tx) error {
		const q = `
			UPDATE api_keys
			SET revoked_at = NOW()
			WHERE id = $1 AND org_id = $2 AND revoked_at IS NULL`
		tag, err := tx.Exec(ctx, q, keyID, orgID)
		if err != nil {
			return fmt.Errorf("revoke api key: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return fmt.Errorf("api key not found or already revoked")
		}
		return nil
	})
}

func main() {
	lambda.Start(handler)
}
