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
	"strings"

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
		slog.Error("authorizer: connect to database", "error", err)
		os.Exit(1)
	}
}

func handler(ctx context.Context, event events.APIGatewayCustomAuthorizerRequest) (events.APIGatewayCustomAuthorizerResponse, error) {
	token := event.AuthorizationToken
	// Strip "Bearer " prefix if present.
	if strings.HasPrefix(token, "Bearer ") {
		token = strings.TrimPrefix(token, "Bearer ")
	}
	if token == "" {
		return deny("anonymous", event.MethodArn), nil
	}

	claims, err := validateKey(ctx, token)
	if err != nil {
		slog.Warn("authorizer: invalid key", "error", err)
		return deny("anonymous", event.MethodArn), nil
	}

	// Allow invocation of all methods in this API (wildcard resource ARN).
	// The methodArn is: arn:aws:execute-api:region:account:api-id/stage/METHOD/resource
	// We allow: arn:aws:execute-api:region:account:api-id/stage/*/*
	resource := wildcardArn(event.MethodArn)

	resp := events.APIGatewayCustomAuthorizerResponse{
		PrincipalID: claims.KeyID.String(),
		PolicyDocument: events.APIGatewayCustomAuthorizerPolicy{
			Version: "2012-10-17",
			Statement: []events.IAMPolicyStatement{
				{
					Action:   []string{"execute-api:Invoke"},
					Effect:   "Allow",
					Resource: []string{resource},
				},
			},
		},
		Context: map[string]interface{}{
			"org_id": claims.OrgID.String(),
			"key_id": claims.KeyID.String(),
			"scopes": strings.Join(scopeStrings(claims.Scopes), ","),
		},
	}
	if claims.PodID != nil {
		resp.Context["pod_id"] = claims.PodID.String()
	}
	return resp, nil
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

func deny(principalID, methodArn string) events.APIGatewayCustomAuthorizerResponse {
	return events.APIGatewayCustomAuthorizerResponse{
		PrincipalID: principalID,
		PolicyDocument: events.APIGatewayCustomAuthorizerPolicy{
			Version: "2012-10-17",
			Statement: []events.IAMPolicyStatement{
				{
					Action:   []string{"execute-api:Invoke"},
					Effect:   "Deny",
					Resource: []string{methodArn},
				},
			},
		},
	}
}

func wildcardArn(methodArn string) string {
	// methodArn: arn:aws:execute-api:us-east-1:123456789:abcdef123/prod/GET/v1/org
	// We want:   arn:aws:execute-api:us-east-1:123456789:abcdef123/prod/*/*
	parts := strings.Split(methodArn, "/")
	if len(parts) >= 2 {
		return parts[0] + "/" + parts[1] + "/*/*"
	}
	return methodArn
}

func scopeStrings(scopes []authpkg.Scope) []string {
	out := make([]string, len(scopes))
	for i, s := range scopes {
		out[i] = string(s)
	}
	return out
}

func main() {
	lambda.Start(handler)
}
