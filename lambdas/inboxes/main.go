/*
 * Copyright (c) 2026 nyklabs.com. All rights reserved.
 *
 * Licensed under the nGX Commercial Source License v1.0.
 * See LICENSE file in the project root for full license information.
 */

package main

import (
	"context"
	"log/slog"
	"os"
	"strings"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	awscfg "github.com/aws/aws-sdk-go-v2/config"
	sqssdk "github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"agentmail/lambdas/shared"
	authpkg "agentmail/pkg/auth"
	"agentmail/pkg/models"
	sqspkg "agentmail/pkg/sqs"
	inboxsvc "agentmail/services/inbox/service"
	inboxstore "agentmail/services/inbox/store"
)

var (
	pool    *pgxpool.Pool
	inboxSv *inboxsvc.InboxService
)

func init() {
	ctx := context.Background()
	pool = shared.InitDB()

	awsConf, err := awscfg.LoadDefaultConfig(ctx)
	if err != nil {
		slog.Error("inboxes: load AWS config", "error", err)
		os.Exit(1)
	}
	sqsClient := sqssdk.NewFromConfig(awsConf)
	publisher := sqspkg.NewPublisher(sqsClient)

	inboxSt := inboxstore.NewPostgresInboxStore(pool)
	mailDomain := os.Getenv("MAIL_DOMAIN")
	inboxSv = inboxsvc.NewInboxService(pool, inboxSt, publisher, mailDomain)
}

func handler(ctx context.Context, event events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	claims, err := shared.ExtractClaims(event)
	if err != nil {
		return shared.Error(401, "unauthorized"), nil
	}

	switch event.Resource {
	case "/v1/inboxes":
		switch event.HTTPMethod {
		case "GET":
			if !claims.HasScope(authpkg.ScopeInboxRead) {
				return shared.Error(403, "insufficient scope"), nil
			}
			// For org-admin keys, honour the ?pod_id= query param as a filter.
			// Pod-scoped keys already enforce their own pod via claims.PodID inside the service.
			podID := claims.PodID
			if podID == nil {
				if raw := event.QueryStringParameters["pod_id"]; raw != "" {
					if id, err := uuid.Parse(raw); err == nil {
						podID = &id
					} else {
						return shared.Error(400, "invalid pod_id"), nil
					}
				}
			}
			inboxes, _, err := inboxSv.List(ctx, claims, podID, 50, event.QueryStringParameters["cursor"])
			if err != nil {
				if strings.Contains(err.Error(), "invalid cursor") {
					return shared.Error(400, "invalid cursor"), nil
				}
				return shared.Error(500, err.Error()), nil
			}
			if inboxes == nil {
				inboxes = []*models.Inbox{}
			}
			return shared.JSON(200, map[string]any{"inboxes": inboxes}), nil
		case "POST":
			if !claims.HasScope(authpkg.ScopeInboxWrite) {
				return shared.Error(403, "insufficient scope"), nil
			}
			var req inboxsvc.CreateInboxRequest
			if err := shared.Decode(event, &req); err != nil {
				return shared.Error(400, "invalid request body"), nil
			}
			inbox, err := inboxSv.Create(ctx, claims, req)
			if err != nil {
				return shared.Error(400, err.Error()), nil
			}
			return shared.JSON(201, inbox), nil
		}

	case "/v1/inboxes/{inboxId}":
		inboxID, err := shared.ParseUUID(shared.PathParam(event, "inboxId"))
		if err != nil {
			return shared.Error(400, "invalid inbox ID"), nil
		}
		switch event.HTTPMethod {
		case "GET":
			if !claims.HasScope(authpkg.ScopeInboxRead) {
				return shared.Error(403, "insufficient scope"), nil
			}
			inbox, err := inboxSv.Get(ctx, claims, inboxID)
			if err != nil {
				return shared.Error(404, "inbox not found"), nil
			}
			return shared.JSON(200, inbox), nil
		case "PATCH":
			if !claims.HasScope(authpkg.ScopeInboxWrite) {
				return shared.Error(403, "insufficient scope"), nil
			}
			var req inboxsvc.UpdateInboxRequest
			if err := shared.Decode(event, &req); err != nil {
				return shared.Error(400, "invalid request body"), nil
			}
			inbox, err := inboxSv.Update(ctx, claims, inboxID, req)
			if err != nil {
				return shared.Error(400, err.Error()), nil
			}
			return shared.JSON(200, inbox), nil
		case "DELETE":
			if !claims.HasScope(authpkg.ScopeInboxWrite) {
				return shared.Error(403, "insufficient scope"), nil
			}
			if err := inboxSv.Delete(ctx, claims, inboxID); err != nil {
				return shared.Error(404, "inbox not found"), nil
			}
			return events.APIGatewayProxyResponse{StatusCode: 204}, nil
		}
	}

	return shared.Error(404, "not found"), nil
}

func main() {
	lambda.Start(handler)
}
