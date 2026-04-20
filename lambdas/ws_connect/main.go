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
	"log/slog"
	"os"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	authpkg "agentmail/pkg/auth"
	cfgpkg "agentmail/pkg/config"
	dbpkg "agentmail/pkg/db"
)

var pool *pgxpool.Pool

func init() {
	ctx := context.Background()
	cfg := cfgpkg.DatabaseConfig{
		URL:      os.Getenv("DATABASE_URL"),
		MaxConns: 3,
		MinConns: 1,
	}
	var err error
	pool, err = dbpkg.Connect(ctx, cfg)
	if err != nil {
		slog.Error("ws_connect: connect to database", "error", err)
		os.Exit(1)
	}
}

func handler(ctx context.Context, event events.APIGatewayWebsocketProxyRequest) (events.APIGatewayProxyResponse, error) {
	token := event.QueryStringParameters["token"]
	if token == "" {
		return events.APIGatewayProxyResponse{StatusCode: 401, Body: "Unauthorized"}, nil
	}

	claims, err := validateKey(ctx, token)
	if err != nil {
		slog.Warn("ws_connect: invalid key", "error", err)
		return events.APIGatewayProxyResponse{StatusCode: 401, Body: "Unauthorized"}, nil
	}

	connectionID := event.RequestContext.ConnectionID
	now := time.Now().UTC()
	ttl := now.Add(24 * time.Hour)

	_, err = pool.Exec(ctx,
		`INSERT INTO websocket_connections(connection_id, org_id, connected_at, ttl)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (connection_id) DO UPDATE SET connected_at = $3, ttl = $4`,
		connectionID, claims.OrgID, now, ttl,
	)
	if err != nil {
		slog.Error("ws_connect: insert connection", "error", err, "connection_id", connectionID)
		return events.APIGatewayProxyResponse{StatusCode: 500, Body: "Internal Server Error"}, nil
	}

	slog.Info("ws_connect: connected", "connection_id", connectionID, "org_id", claims.OrgID)
	return events.APIGatewayProxyResponse{StatusCode: 200, Body: "Connected"}, nil
}

func validateKey(ctx context.Context, plaintextKey string) (*authpkg.Claims, error) {
	hash := authpkg.HashAPIKey(plaintextKey)

	const q = `
		SELECT id, org_id, key_prefix, key_hash, scopes, pod_id
		FROM api_keys
		WHERE key_hash = $1
		  AND revoked_at IS NULL
		  AND (expires_at IS NULL OR expires_at > NOW())`

	row := pool.QueryRow(ctx, q, hash)

	var (
		keyID  uuid.UUID
		orgID  uuid.UUID
		prefix string
		h      string
		scopes []string
		podID  *uuid.UUID
	)
	if err := row.Scan(&keyID, &orgID, &prefix, &h, &scopes, &podID); err != nil {
		return nil, fmt.Errorf("key not found: %w", err)
	}

	// Update last_used_at (best-effort).
	_, _ = pool.Exec(ctx, `UPDATE api_keys SET last_used_at = NOW() WHERE id = $1`, keyID)

	claims := &authpkg.Claims{
		OrgID: orgID,
		KeyID: keyID,
		PodID: podID,
	}
	for _, s := range scopes {
		claims.Scopes = append(claims.Scopes, authpkg.Scope(s))
	}
	return claims, nil
}

func main() {
	lambda.Start(handler)
}
