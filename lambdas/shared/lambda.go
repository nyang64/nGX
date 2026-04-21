/*
 * Copyright (c) 2026 nyklabs.com. All rights reserved.
 *
 * Licensed under the nGX Commercial Source License v1.0.
 * See LICENSE file in the project root for full license information.
 */

// Package shared provides common utilities for Lambda handlers.
package shared

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/aws/aws-lambda-go/events"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	authpkg "agentmail/pkg/auth"
	cfgpkg "agentmail/pkg/config"
	dbpkg "agentmail/pkg/db"
)

// InitDB creates a singleton database connection pool using DATABASE_URL env var.
func InitDB() *pgxpool.Pool {
	ctx := context.Background()
	cfg := cfgpkg.DatabaseConfig{
		URL:      os.Getenv("DATABASE_URL"),
		MaxConns: 5,
		MinConns: 1,
	}
	pool, err := dbpkg.Connect(ctx, cfg)
	if err != nil {
		slog.Error("connect to database", "error", err)
		os.Exit(1)
	}
	return pool
}

// ExtractClaims gets auth claims from the Lambda request context (set by the authorizer).
func ExtractClaims(event events.APIGatewayProxyRequest) (*authpkg.Claims, error) {
	auth := event.RequestContext.Authorizer

	orgIDStr, _ := auth["org_id"].(string)
	keyIDStr, _ := auth["key_id"].(string)
	scopesStr, _ := auth["scopes"].(string)
	podIDStr, _ := auth["pod_id"].(string)

	orgID, err := uuid.Parse(orgIDStr)
	if err != nil {
		return nil, fmt.Errorf("invalid org_id: %w", err)
	}
	keyID, err := uuid.Parse(keyIDStr)
	if err != nil {
		return nil, fmt.Errorf("invalid key_id: %w", err)
	}

	var scopes []authpkg.Scope
	for _, s := range strings.Split(scopesStr, ",") {
		s = strings.TrimSpace(s)
		if s != "" {
			scopes = append(scopes, authpkg.Scope(s))
		}
	}

	claims := &authpkg.Claims{
		OrgID:  orgID,
		KeyID:  keyID,
		Scopes: scopes,
	}
	if podIDStr != "" {
		podID, err := uuid.Parse(podIDStr)
		if err == nil {
			claims.PodID = &podID
		}
	}
	return claims, nil
}

// JSON returns a JSON APIGatewayProxyResponse.
func JSON(statusCode int, body any) events.APIGatewayProxyResponse {
	data, _ := json.Marshal(body)
	return events.APIGatewayProxyResponse{
		StatusCode: statusCode,
		Headers: map[string]string{
			"Content-Type":                "application/json",
			"Access-Control-Allow-Origin": "*",
		},
		Body: string(data),
	}
}

// Error returns a JSON error response.
func Error(statusCode int, message string) events.APIGatewayProxyResponse {
	return JSON(statusCode, map[string]string{"error": message})
}

// PathParam extracts a path parameter from the event.
func PathParam(event events.APIGatewayProxyRequest, name string) string {
	return event.PathParameters[name]
}

// ParseUUID parses a UUID string, returning an error if invalid.
func ParseUUID(s string) (uuid.UUID, error) {
	return uuid.Parse(s)
}

// Decode unmarshals the event body into dst.
func Decode(event events.APIGatewayProxyRequest, dst any) error {
	return json.Unmarshal([]byte(event.Body), dst)
}
