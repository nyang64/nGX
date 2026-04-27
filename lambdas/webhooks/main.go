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
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"agentmail/lambdas/shared"
	authpkg "agentmail/pkg/auth"
	"agentmail/pkg/crypto"
	"agentmail/pkg/models"
	whstore "agentmail/services/webhook-service/store"
)

var (
	pool   *pgxpool.Pool
	whs    *whstore.DeliveryStore
	encKey []byte
)

func init() {
	pool = shared.InitDB()
	whs = whstore.NewDeliveryStore(pool)
	keyHex := os.Getenv("WEBHOOK_ENCRYPTION_KEY")
	if keyHex != "" {
		var err error
		encKey, err = crypto.KeyFromHex(keyHex)
		if err != nil {
			slog.Error("webhooks: invalid WEBHOOK_ENCRYPTION_KEY", "error", err)
			os.Exit(1)
		}
	}
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
			respHooks := make([]map[string]any, len(hooks))
			for i, h := range hooks {
				respHooks[i] = webhookResponse(h)
			}
			return shared.JSON(200, map[string]any{"webhooks": respHooks}), nil
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
			if req.AuthHeader != nil {
				name, ok1 := req.AuthHeader["name"]
				value, ok2 := req.AuthHeader["value"]
				if ok1 && ok2 && name != "" && value != "" {
					if encKey == nil {
						return shared.Error(500, "webhook encryption not configured"), nil
					}
					enc, err := crypto.Encrypt(encKey, []byte(value))
					if err != nil {
						return shared.Error(500, "failed to encrypt auth header"), nil
					}
					hook.AuthHeaderName = &name
					hook.AuthHeaderValueEnc = enc
				}
			}
			if err := whs.CreateWebhook(ctx, hook); err != nil {
				return shared.Error(400, err.Error()), nil
			}
			return shared.JSON(201, webhookResponse(hook)), nil
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
			return shared.JSON(200, webhookResponse(hook)), nil
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
			return shared.JSON(200, webhookResponse(hook)), nil
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
			limit := 0
			if l := event.QueryStringParameters["limit"]; l != "" {
				fmt.Sscanf(l, "%d", &limit)
			}
			deliveries, nextCursor, err := whs.ListDeliveries(ctx, webhookID, claims.OrgID, limit, event.QueryStringParameters["cursor"])
			if err != nil {
				if strings.Contains(err.Error(), "invalid cursor") {
					return shared.Error(400, "invalid cursor"), nil
				}
				return shared.Error(500, err.Error()), nil
			}
			resp := map[string]any{"deliveries": deliveries}
			if nextCursor != "" {
				resp["next_cursor"] = nextCursor
			}
			return shared.JSON(200, resp), nil
		}
	}

	return shared.Error(404, "not found"), nil
}

// webhookResponse builds the JSON response for a webhook, exposing auth_header_name
// but never auth_header_value (write-only field).
func webhookResponse(wh *models.Webhook) map[string]any {
	resp := map[string]any{
		"id":              wh.ID,
		"org_id":          wh.OrgID,
		"url":             wh.URL,
		"events":          wh.Events,
		"pod_id":          wh.PodID,
		"inbox_id":        wh.InboxID,
		"is_active":       wh.IsActive,
		"failure_count":   wh.FailureCount,
		"last_success_at": wh.LastSuccessAt,
		"last_failure_at": wh.LastFailureAt,
		"created_at":      wh.CreatedAt,
		"updated_at":      wh.UpdatedAt,
	}
	if wh.AuthHeaderName != nil {
		resp["auth_header_name"] = *wh.AuthHeaderName
	}
	return resp
}

func main() {
	lambda.Start(handler)
}
