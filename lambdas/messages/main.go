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
	pool      *pgxpool.Pool
	messageSv *inboxsvc.MessageService
)

func init() {
	ctx := context.Background()
	pool = shared.InitDB()
	awsConf, err := awscfg.LoadDefaultConfig(ctx)
	if err != nil {
		slog.Error("messages: AWS config", "error", err)
		os.Exit(1)
	}
	pub := sqspkg.NewPublisher(sqssdk.NewFromConfig(awsConf))

	attachmentsS3, err := s3pkg.NewFromAWS(ctx, os.Getenv("S3_BUCKET_ATTACHMENTS"))
	if err != nil {
		slog.Error("messages: init attachments S3 client", "error", err)
		os.Exit(1)
	}

	messageSv = inboxsvc.NewMessageService(pool,
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
	threadID, _ := uuid.Parse(event.PathParameters["threadId"])
	messageID, _ := uuid.Parse(event.PathParameters["messageId"])

	switch event.Resource {
	case "/v1/inboxes/{inboxId}/threads/{threadId}/messages":
		if event.HTTPMethod == "GET" {
			if !claims.HasScope(authpkg.ScopeInboxRead) {
				return shared.Error(403, "insufficient scope"), nil
			}
			limit := 0
			if l := event.QueryStringParameters["limit"]; l != "" {
				fmt.Sscanf(l, "%d", &limit)
			}
			msgs, nextCursor, err := messageSv.List(ctx, claims, threadID, limit, event.QueryStringParameters["cursor"])
			if err != nil {
				return shared.Error(500, err.Error()), nil
			}
			resp := map[string]any{"messages": msgs}
			if nextCursor != "" {
				resp["next_cursor"] = nextCursor
			}
			return shared.JSON(200, resp), nil
		}

	case "/v1/inboxes/{inboxId}/threads/{threadId}/messages/{messageId}":
		switch event.HTTPMethod {
		case "GET":
			if !claims.HasScope(authpkg.ScopeInboxRead) {
				return shared.Error(403, "insufficient scope"), nil
			}
			msg, err := messageSv.Get(ctx, claims, messageID)
			if err != nil {
				return shared.Error(404, "message not found"), nil
			}
			return shared.JSON(200, msg), nil
		case "PATCH":
			if !claims.HasScope(authpkg.ScopeInboxWrite) {
				return shared.Error(403, "insufficient scope"), nil
			}
			var req struct {
				Unread   *bool          `json:"unread"`
				Starred  *bool          `json:"starred"`
				Metadata map[string]any `json:"metadata"`
			}
			if err := shared.Decode(event, &req); err != nil {
				return shared.Error(400, "invalid request body"), nil
			}
			patch := inboxstore.MessagePatch{
				IsRead:    req.Unread,
				IsStarred: req.Starred,
				Metadata:  req.Metadata,
			}
			// unread=true means is_read=false and vice versa.
			if req.Unread != nil {
				v := !*req.Unread
				patch.IsRead = &v
			}
			msg, err := messageSv.UpdateMessage(ctx, claims, messageID, patch)
			if err != nil {
				return shared.Error(404, "message not found"), nil
			}
			return shared.JSON(200, msg), nil
		}

	case "/v1/inboxes/{inboxId}/messages/send":
		if event.HTTPMethod == "POST" {
			if !claims.HasScope(authpkg.ScopeInboxWrite) {
				return shared.Error(403, "insufficient scope"), nil
			}
			var req inboxsvc.SendMessageRequest
			if err := shared.Decode(event, &req); err != nil {
				return shared.Error(400, "invalid request body"), nil
			}
			msg, err := messageSv.Send(ctx, claims, inboxID, req)
			if err != nil {
				return shared.Error(400, err.Error()), nil
			}
			return shared.JSON(201, msg), nil
		}
	}

	_ = threadID
	return shared.Error(404, "not found"), nil
}

func main() {
	lambda.Start(handler)
}
