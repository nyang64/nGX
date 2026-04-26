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

	switch {
	case method == "GET" && resource == "/v1/org":
		return getOrg(ctx, claims)
	case method == "PATCH" && resource == "/v1/org":
		return patchOrg(ctx, event, claims)
	case method == "GET" && resource == "/v1/pods":
		return listPods(ctx, claims)
	case method == "POST" && resource == "/v1/pods":
		return createPod(ctx, event, claims)
	case method == "GET" && resource == "/v1/pods/{podId}":
		return getPod(ctx, event, claims)
	case method == "PATCH" && resource == "/v1/pods/{podId}":
		return patchPod(ctx, event, claims)
	case method == "DELETE" && resource == "/v1/pods/{podId}":
		return deletePod(ctx, event, claims)
	default:
		return shared.Error(404, "not found"), nil
	}
}

func getOrg(ctx context.Context, claims *authpkg.Claims) (events.APIGatewayProxyResponse, error) {
	org, err := fetchOrg(ctx, claims.OrgID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return shared.Error(404, "organization not found"), nil
		}
		return shared.Error(500, "failed to get organization"), nil
	}
	return shared.JSON(200, org), nil
}

func patchOrg(ctx context.Context, event events.APIGatewayProxyRequest, claims *authpkg.Claims) (events.APIGatewayProxyResponse, error) {
	var req struct {
		Name string `json:"name"`
	}
	if err := shared.Decode(event, &req); err != nil || req.Name == "" {
		return shared.Error(400, "name is required"), nil
	}
	org, err := fetchOrg(ctx, claims.OrgID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return shared.Error(404, "organization not found"), nil
		}
		return shared.Error(500, "failed to get organization"), nil
	}
	org.Name = req.Name
	return shared.JSON(200, org), nil
}

func listPods(ctx context.Context, claims *authpkg.Claims) (events.APIGatewayProxyResponse, error) {
	pods, err := fetchPods(ctx, claims.OrgID)
	if err != nil {
		slog.Error("orgs: list pods", "error", err)
		return shared.Error(500, "failed to list pods"), nil
	}
	if pods == nil {
		pods = []*models.Pod{}
	}
	return shared.JSON(200, map[string]any{"pods": pods}), nil
}

func createPod(ctx context.Context, event events.APIGatewayProxyRequest, claims *authpkg.Claims) (events.APIGatewayProxyResponse, error) {
	var req struct {
		Name        string `json:"name"`
		Slug        string `json:"slug"`
		Description string `json:"description"`
	}
	if err := shared.Decode(event, &req); err != nil || req.Name == "" || req.Slug == "" {
		return shared.Error(400, "name and slug are required"), nil
	}
	pod := &models.Pod{
		ID:          uuid.New(),
		OrgID:       claims.OrgID,
		Name:        req.Name,
		Slug:        req.Slug,
		Description: req.Description,
		Settings:    map[string]any{},
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	if err := insertPod(ctx, pod); err != nil {
		if dbpkg.IsDuplicateKey(err) {
			return shared.Error(409, "pod with this slug already exists"), nil
		}
		return shared.Error(500, "failed to create pod"), nil
	}
	return shared.JSON(201, pod), nil
}

func getPod(ctx context.Context, event events.APIGatewayProxyRequest, claims *authpkg.Claims) (events.APIGatewayProxyResponse, error) {
	podID, err := shared.ParseUUID(shared.PathParam(event, "podId"))
	if err != nil {
		return shared.Error(400, "invalid pod ID"), nil
	}
	pod, err := fetchPod(ctx, claims.OrgID, podID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return shared.Error(404, "pod not found"), nil
		}
		return shared.Error(500, "failed to get pod"), nil
	}
	return shared.JSON(200, pod), nil
}

func patchPod(ctx context.Context, event events.APIGatewayProxyRequest, claims *authpkg.Claims) (events.APIGatewayProxyResponse, error) {
	podID, err := shared.ParseUUID(shared.PathParam(event, "podId"))
	if err != nil {
		return shared.Error(400, "invalid pod ID"), nil
	}
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := shared.Decode(event, &req); err != nil || req.Name == "" {
		return shared.Error(400, "name is required"), nil
	}
	pod, err := updatePod(ctx, claims.OrgID, podID, req.Name, req.Description)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return shared.Error(404, "pod not found"), nil
		}
		return shared.Error(500, "failed to update pod"), nil
	}
	return shared.JSON(200, pod), nil
}

func deletePod(ctx context.Context, event events.APIGatewayProxyRequest, claims *authpkg.Claims) (events.APIGatewayProxyResponse, error) {
	podID, err := shared.ParseUUID(shared.PathParam(event, "podId"))
	if err != nil {
		return shared.Error(400, "invalid pod ID"), nil
	}
	if err := removePod(ctx, claims.OrgID, podID); err != nil {
		if strings.Contains(err.Error(), "not found") {
			return shared.Error(404, "pod not found"), nil
		}
		return shared.Error(500, "failed to delete pod"), nil
	}
	return events.APIGatewayProxyResponse{StatusCode: 204}, nil
}

// --- store functions ---

func fetchOrg(ctx context.Context, orgID uuid.UUID) (*models.Organization, error) {
	var org models.Organization
	err := pool.QueryRow(ctx,
		`SELECT id, name, slug, plan, settings, created_at, updated_at
		 FROM organizations WHERE id = $1`,
		orgID,
	).Scan(&org.ID, &org.Name, &org.Slug, &org.Plan, &org.Settings, &org.CreatedAt, &org.UpdatedAt)
	if err != nil {
		if dbpkg.IsNotFound(err) {
			return nil, fmt.Errorf("organization not found")
		}
		return nil, fmt.Errorf("get organization: %w", err)
	}
	return &org, nil
}

func fetchPods(ctx context.Context, orgID uuid.UUID) ([]*models.Pod, error) {
	var pods []*models.Pod
	err := dbpkg.WithOrgTx(ctx, pool, orgID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx,
			`SELECT id, org_id, name, slug, description, settings, created_at, updated_at
			 FROM pods WHERE org_id = $1 ORDER BY created_at ASC`,
			orgID,
		)
		if err != nil {
			return fmt.Errorf("list pods: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var pod models.Pod
			var desc *string
			if err := rows.Scan(&pod.ID, &pod.OrgID, &pod.Name, &pod.Slug, &desc, &pod.Settings, &pod.CreatedAt, &pod.UpdatedAt); err != nil {
				return fmt.Errorf("scan pod: %w", err)
			}
			if desc != nil {
				pod.Description = *desc
			}
			pods = append(pods, &pod)
		}
		return rows.Err()
	})
	return pods, err
}

func insertPod(ctx context.Context, pod *models.Pod) error {
	_, err := pool.Exec(ctx,
		`INSERT INTO pods (id, org_id, name, slug, description, settings, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		pod.ID, pod.OrgID, pod.Name, pod.Slug, pod.Description, pod.Settings, pod.CreatedAt, pod.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert pod: %w", err)
	}
	return nil
}

func fetchPod(ctx context.Context, orgID, podID uuid.UUID) (*models.Pod, error) {
	var pod models.Pod
	var desc *string
	err := dbpkg.WithOrgTx(ctx, pool, orgID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT id, org_id, name, slug, description, settings, created_at, updated_at
			 FROM pods WHERE org_id = $1 AND id = $2`,
			orgID, podID,
		).Scan(&pod.ID, &pod.OrgID, &pod.Name, &pod.Slug, &desc, &pod.Settings, &pod.CreatedAt, &pod.UpdatedAt)
	})
	if err != nil {
		if dbpkg.IsNotFound(err) {
			return nil, fmt.Errorf("pod not found")
		}
		return nil, fmt.Errorf("get pod: %w", err)
	}
	if desc != nil {
		pod.Description = *desc
	}
	return &pod, nil
}

func updatePod(ctx context.Context, orgID, podID uuid.UUID, name, desc string) (*models.Pod, error) {
	now := time.Now().UTC()
	err := dbpkg.WithOrgTx(ctx, pool, orgID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			`UPDATE pods SET name = $1, description = $2, updated_at = $3
			 WHERE org_id = $4 AND id = $5`,
			name, desc, now, orgID, podID,
		)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("update pod: %w", err)
	}
	return fetchPod(ctx, orgID, podID)
}

func removePod(ctx context.Context, orgID, podID uuid.UUID) error {
	tag, err := pool.Exec(ctx,
		`DELETE FROM pods WHERE org_id = $1 AND id = $2`,
		orgID, podID,
	)
	if err != nil {
		return fmt.Errorf("delete pod: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("pod not found")
	}
	return nil
}

func main() {
	lambda.Start(handler)
}
