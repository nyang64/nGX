/*
 * Copyright (c) 2026 nyklabs.com. All rights reserved.
 *
 * Licensed under the nGX Commercial Source License v1.0.
 * See LICENSE file in the project root for full license information.
 */

package main

import (
	"context"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"agentmail/lambdas/shared"
	authpkg "agentmail/pkg/auth"
	"agentmail/pkg/models"
	whstore "agentmail/services/webhook-service/store"
)

var (
	pool *pgxpool.Pool
	whs  *whstore.DeliveryStore
)

func init() {
	pool = shared.InitDB()
	whs = whstore.NewDeliveryStore(pool)
}

func handler(ctx context.Context, event events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	claims, err := shared.ExtractClaims(event)
	if err != nil {
		return shared.Error(401, "unauthorized"), nil
	}

	webhookID, _ := uuid.Parse(event.PathParameters["webhookId"])

	switch event.Resource {
	case "/v1/webhooks":
		switch event.HTTPMethod {
		case "GET":
			if !claims.HasScope(authpkg.ScopeWebhookRead) {
				return shared.Error(403, "insufficient scope"), nil
			}
			hooks, err := whs.ListWebhooks(ctx, claims.OrgID)
			if err != nil {
				return shared.Error(500, err.Error()), nil
			}
			return shared.JSON(200, map[string]any{"webhooks": hooks}), nil
		case "POST":
			if !claims.HasScope(authpkg.ScopeWebhookWrite) {
				return shared.Error(403, "insufficient scope"), nil
			}
			var req struct {
				URL        string            `json:"url"`
				Events     []string          `json:"events"`
				Secret     string            `json:"secret"`
				AuthHeader map[string]string `json:"auth_header"`
			}
			if err := shared.Decode(event, &req); err != nil {
				return shared.Error(400, "invalid request body"), nil
			}
			hook := &models.Webhook{
				ID:        uuid.New(),
				OrgID:     claims.OrgID,
				URL:       req.URL,
				Events:    req.Events,
				IsActive:  true,
				CreatedAt: time.Now().UTC(),
				UpdatedAt: time.Now().UTC(),
			}
			if err := whs.CreateWebhook(ctx, hook); err != nil {
				return shared.Error(400, err.Error()), nil
			}
			return shared.JSON(201, hook), nil
		}

	case "/v1/webhooks/{webhookId}":
		switch event.HTTPMethod {
		case "GET":
			if !claims.HasScope(authpkg.ScopeWebhookRead) {
				return shared.Error(403, "insufficient scope"), nil
			}
			hook, err := whs.GetWebhookByIDAndOrg(ctx, webhookID, claims.OrgID)
			if err != nil {
				return shared.Error(404, "webhook not found"), nil
			}
			return shared.JSON(200, hook), nil
		case "PATCH":
			if !claims.HasScope(authpkg.ScopeWebhookWrite) {
				return shared.Error(403, "insufficient scope"), nil
			}
			hook, err := whs.GetWebhookByIDAndOrg(ctx, webhookID, claims.OrgID)
			if err != nil {
				return shared.Error(404, "webhook not found"), nil
			}
			var req struct {
				URL    *string  `json:"url"`
				Events []string `json:"events"`
				Active *bool    `json:"active"`
			}
			if err := shared.Decode(event, &req); err != nil {
				return shared.Error(400, "invalid request body"), nil
			}
			if req.URL != nil {
				hook.URL = *req.URL
			}
			if req.Events != nil {
				hook.Events = req.Events
			}
			if req.Active != nil {
				hook.IsActive = *req.Active
			}
			hook.UpdatedAt = time.Now().UTC()
			if err := whs.UpdateWebhook(ctx, hook); err != nil {
				return shared.Error(400, err.Error()), nil
			}
			return shared.JSON(200, hook), nil
		case "DELETE":
			if !claims.HasScope(authpkg.ScopeWebhookWrite) {
				return shared.Error(403, "insufficient scope"), nil
			}
			if err := whs.DeleteWebhook(ctx, webhookID, claims.OrgID); err != nil {
				return shared.Error(404, "webhook not found"), nil
			}
			return events.APIGatewayProxyResponse{StatusCode: 204}, nil
		}

	case "/v1/webhooks/{webhookId}/deliveries":
		if event.HTTPMethod == "GET" {
			if !claims.HasScope(authpkg.ScopeWebhookRead) {
				return shared.Error(403, "insufficient scope"), nil
			}
			deliveries, err := whs.ListDeliveries(ctx, webhookID, claims.OrgID)
			if err != nil {
				return shared.Error(500, err.Error()), nil
			}
			return shared.JSON(200, map[string]any{"deliveries": deliveries}), nil
		}
	}

	return shared.Error(404, "not found"), nil
}

func main() {
	lambda.Start(handler)
}
