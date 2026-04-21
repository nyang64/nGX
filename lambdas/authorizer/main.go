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

// authorizerEvent merges TOKEN and REQUEST authorizer payloads so one handler
// covers both the REST API (Authorization header / TOKEN mode) and the
// WebSocket API ($connect REQUEST mode with ?token= query parameter).
type authorizerEvent struct {
	// TOKEN authorizer: API GW injects the raw Authorization header value here.
	AuthorizationToken string `json:"authorizationToken"`
	// Both modes supply methodArn.
	MethodArn string `json:"methodArn"`
	// REQUEST authorizer: query string parameters forwarded as-is.
	QueryStringParameters map[string]string `json:"queryStringParameters"`
}

func handler(ctx context.Context, event authorizerEvent) (events.APIGatewayCustomAuthorizerResponse, error) {
	// Support both REST (Authorization header) and WebSocket (?token= query param).
	token := event.AuthorizationToken
	if token == "" {
		token = event.QueryStringParameters["token"]
	}
	// Strip "Bearer " prefix if present.
	if strings.HasPrefix(token, "Bearer ") {
		token = strings.TrimPrefix(token, "Bearer ")
	}
	if token == "" {
		return deny("anonymous", event.MethodArn), nil
	}

	slog.Info("authorizer: validating", "method_arn", event.MethodArn, "has_token", token != "")
	claims, err := validateKey(ctx, token)
	if err != nil {
		slog.Warn("authorizer: invalid key", "error", err, "method_arn", event.MethodArn)
		return deny("anonymous", event.MethodArn), nil
	}

	// Allow invocation of all methods in this API (wildcard resource ARNs).
	// REST:      arn:aws:execute-api:region:account:api-id/stage/METHOD/resource  → needs prod/*/*
	// WebSocket: arn:aws:execute-api:region:account:api-id/stage/$connect         → needs prod/*
	// Include both patterns so the same authorizer works for both API types.
	resources := wildcardArns(event.MethodArn)

	resp := events.APIGatewayCustomAuthorizerResponse{
		PrincipalID: claims.KeyID.String(),
		PolicyDocument: events.APIGatewayCustomAuthorizerPolicy{
			Version: "2012-10-17",
			Statement: []events.IAMPolicyStatement{
				{
					Action:   []string{"execute-api:Invoke"},
					Effect:   "Allow",
					Resource: resources,
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

func wildcardArns(methodArn string) []string {
	// methodArn examples:
	//   REST:      arn:aws:execute-api:us-east-1:123:abc/prod/GET/v1/org
	//   WebSocket: arn:aws:execute-api:us-east-1:123:abc/prod/$connect
	// Return both single and double wildcard patterns so the policy covers both.
	parts := strings.Split(methodArn, "/")
	if len(parts) >= 2 {
		base := parts[0] + "/" + parts[1]
		return []string{base + "/*", base + "/*/*"}
	}
	return []string{methodArn}
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
