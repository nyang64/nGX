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
	s3pkg "agentmail/pkg/s3"
	sqspkg "agentmail/pkg/sqs"
	inboxsvc "agentmail/services/inbox/service"
	inboxstore "agentmail/services/inbox/store"
)

var (
	pool    *pgxpool.Pool
	draftSv *inboxsvc.DraftService
)

func init() {
	ctx := context.Background()
	pool = shared.InitDB()
	awsConf, err := awscfg.LoadDefaultConfig(ctx)
	if err != nil {
		slog.Error("drafts: AWS config", "error", err)
		os.Exit(1)
	}
	pub := sqspkg.NewPublisher(sqssdk.NewFromConfig(awsConf))

	attachmentsS3, err := s3pkg.NewFromAWS(ctx, os.Getenv("S3_BUCKET_ATTACHMENTS"))
	if err != nil {
		slog.Error("drafts: init attachments S3 client", "error", err)
		os.Exit(1)
	}

	draftSv = inboxsvc.NewDraftService(pool,
		inboxstore.NewPostgresDraftStore(pool),
		inboxstore.NewPostgresMessageStore(pool),
		inboxstore.NewPostgresThreadStore(pool),
		inboxstore.NewPostgresInboxStore(pool),
		pub, pub, attachmentsS3)
}

func handler(ctx context.Context, event events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	claims, err := shared.ExtractClaims(event)
	if err != nil {
		return shared.Error(401, "unauthorized"), nil
	}

	inboxID, _ := uuid.Parse(event.PathParameters["inboxId"])
	draftID, _ := uuid.Parse(event.PathParameters["draftId"])

	switch event.Resource {
	case "/v1/inboxes/{inboxId}/drafts":
		switch event.HTTPMethod {
		case "GET":
			if !claims.HasScope(authpkg.ScopeDraftRead) {
				return shared.Error(403, "insufficient scope"), nil
			}
			drafts, _, err := draftSv.List(ctx, claims, inboxID, 50, event.QueryStringParameters["cursor"])
			if err != nil {
				if strings.Contains(err.Error(), "invalid cursor") {
					return shared.Error(400, "invalid cursor"), nil
				}
				return shared.Error(500, err.Error()), nil
			}
			return shared.JSON(200, map[string]any{"drafts": drafts}), nil
		case "POST":
			if !claims.HasScope(authpkg.ScopeDraftWrite) {
				return shared.Error(403, "insufficient scope"), nil
			}
			var req inboxsvc.CreateDraftRequest
			if err := shared.Decode(event, &req); err != nil {
				return shared.Error(400, "invalid request body"), nil
			}
			req.InboxID = inboxID
			draft, err := draftSv.Create(ctx, claims, req)
			if err != nil {
				return shared.Error(400, err.Error()), nil
			}
			return shared.JSON(201, draft), nil
		}

	case "/v1/inboxes/{inboxId}/drafts/{draftId}":
		switch event.HTTPMethod {
		case "GET":
			if !claims.HasScope(authpkg.ScopeDraftRead) {
				return shared.Error(403, "insufficient scope"), nil
			}
			draft, err := draftSv.Get(ctx, claims, draftID)
			if err != nil {
				return shared.Error(404, "draft not found"), nil
			}
			return shared.JSON(200, draft), nil
		case "PATCH":
			if !claims.HasScope(authpkg.ScopeDraftWrite) {
				return shared.Error(403, "insufficient scope"), nil
			}
			var req inboxsvc.UpdateDraftRequest
			if err := shared.Decode(event, &req); err != nil {
				return shared.Error(400, "invalid request body"), nil
			}
			draft, err := draftSv.Update(ctx, claims, draftID, req)
			if err != nil {
				return shared.Error(400, err.Error()), nil
			}
			return shared.JSON(200, draft), nil
		case "DELETE":
			if !claims.HasScope(authpkg.ScopeDraftWrite) {
				return shared.Error(403, "insufficient scope"), nil
			}
			if err := draftSv.Delete(ctx, claims, draftID); err != nil {
				return shared.Error(404, "draft not found"), nil
			}
			return events.APIGatewayProxyResponse{StatusCode: 204}, nil
		}

	case "/v1/inboxes/{inboxId}/drafts/{draftId}/approve":
		if event.HTTPMethod == "POST" {
			if !claims.HasScope(authpkg.ScopeDraftWrite) {
				return shared.Error(403, "insufficient scope"), nil
			}
			var req struct {
				Note string `json:"note"`
			}
			_ = shared.Decode(event, &req)
			draft, err := draftSv.Approve(ctx, claims, draftID, req.Note)
			if err != nil {
				return shared.Error(400, err.Error()), nil
			}
			return shared.JSON(200, draft), nil
		}

	case "/v1/inboxes/{inboxId}/drafts/{draftId}/reject":
		if event.HTTPMethod == "POST" {
			if !claims.HasScope(authpkg.ScopeDraftWrite) {
				return shared.Error(403, "insufficient scope"), nil
			}
			var req struct {
				Reason string `json:"reason"`
			}
			_ = shared.Decode(event, &req)
			draft, err := draftSv.Reject(ctx, claims, draftID, req.Reason)
			if err != nil {
				return shared.Error(400, err.Error()), nil
			}
			return shared.JSON(200, draft), nil
		}
	}

	_ = inboxID
	return shared.Error(404, "not found"), nil
}

func main() {
	lambda.Start(handler)
}
