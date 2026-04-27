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
	sqspkg "agentmail/pkg/sqs"
	inboxsvc "agentmail/services/inbox/service"
	inboxstore "agentmail/services/inbox/store"
)

var (
	pool     *pgxpool.Pool
	threadSv *inboxsvc.ThreadService
	labelSv  *inboxsvc.LabelService
)

func init() {
	ctx := context.Background()
	pool = shared.InitDB()
	awsConf, err := awscfg.LoadDefaultConfig(ctx)
	if err != nil {
		slog.Error("threads: AWS config", "error", err)
		os.Exit(1)
	}
	pub := sqspkg.NewPublisher(sqssdk.NewFromConfig(awsConf))
	threadSv = inboxsvc.NewThreadService(pool,
		inboxstore.NewPostgresThreadStore(pool),
		inboxstore.NewPostgresInboxStore(pool),
		pub)
	labelSv = inboxsvc.NewLabelService(pool, inboxstore.NewPostgresLabelStore(pool))
}

func handler(ctx context.Context, event events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	claims, err := shared.ExtractClaims(event)
	if err != nil {
		return shared.Error(401, "unauthorized"), nil
	}

	inboxID, _ := uuid.Parse(event.PathParameters["inboxId"])
	threadID, _ := uuid.Parse(event.PathParameters["threadId"])
	labelID, _ := uuid.Parse(event.PathParameters["labelId"])

	switch event.Resource {
	case "/v1/inboxes/{inboxId}/threads":
		if event.HTTPMethod != "GET" {
			break
		}
		if !claims.HasScope(authpkg.ScopeInboxRead) {
			return shared.Error(403, "insufficient scope"), nil
		}
		limit := 0
		if l := event.QueryStringParameters["limit"]; l != "" {
			fmt.Sscanf(l, "%d", &limit)
		}
		q := inboxstore.ThreadListQuery{InboxID: inboxID, Limit: limit, Cursor: event.QueryStringParameters["cursor"]}
		if s := event.QueryStringParameters["status"]; s != "" {
			q.Status = &s
		}
		if u := event.QueryStringParameters["unread"]; u == "true" {
			f := false
			q.IsRead = &f
		} else if u == "false" {
			t := true
			q.IsRead = &t
		}
		if s := event.QueryStringParameters["starred"]; s == "true" {
			t := true
			q.IsStarred = &t
		} else if s == "false" {
			f := false
			q.IsStarred = &f
		}
		if lid := event.QueryStringParameters["label"]; lid != "" {
			if id, err := uuid.Parse(lid); err == nil {
				q.LabelID = &id
			}
		}
		threads, nextCursor, err := threadSv.List(ctx, claims, q)
		if err != nil {
			return shared.Error(500, err.Error()), nil
		}
		resp := map[string]any{"threads": threads}
		if nextCursor != "" {
			resp["next_cursor"] = nextCursor
		}
		return shared.JSON(200, resp), nil

	case "/v1/inboxes/{inboxId}/threads/{threadId}":
		switch event.HTTPMethod {
		case "GET":
			if !claims.HasScope(authpkg.ScopeInboxRead) {
				return shared.Error(403, "insufficient scope"), nil
			}
			thread, err := threadSv.Get(ctx, claims, threadID)
			if err != nil {
				return shared.Error(404, "thread not found"), nil
			}
			return shared.JSON(200, thread), nil
		case "PATCH":
			if !claims.HasScope(authpkg.ScopeInboxWrite) {
				return shared.Error(403, "insufficient scope"), nil
			}
			var req struct {
				Status    *string `json:"status"`
				IsRead    *bool   `json:"is_read"`
				IsStarred *bool   `json:"is_starred"`
			}
			if err := shared.Decode(event, &req); err != nil {
				return shared.Error(400, "invalid request body"), nil
			}
			if req.Status != nil {
				t, err := threadSv.UpdateStatus(ctx, claims, threadID, *req.Status)
				if err != nil {
					return shared.Error(400, err.Error()), nil
				}
				return shared.JSON(200, t), nil
			}
			if req.IsRead != nil {
				t, err := threadSv.MarkRead(ctx, claims, threadID, *req.IsRead)
				if err != nil {
					return shared.Error(400, err.Error()), nil
				}
				return shared.JSON(200, t), nil
			}
			if req.IsStarred != nil {
				t, err := threadSv.MarkStarred(ctx, claims, threadID, *req.IsStarred)
				if err != nil {
					return shared.Error(400, err.Error()), nil
				}
				return shared.JSON(200, t), nil
			}
		}

	case "/v1/inboxes/{inboxId}/threads/{threadId}/labels/{labelId}":
		if !claims.HasScope(authpkg.ScopeInboxWrite) {
			return shared.Error(403, "insufficient scope"), nil
		}
		switch event.HTTPMethod {
		case "PUT":
			if err := threadSv.ApplyLabel(ctx, claims, threadID, labelID); err != nil {
				return shared.Error(400, err.Error()), nil
			}
			return events.APIGatewayProxyResponse{StatusCode: 204}, nil
		case "DELETE":
			if err := threadSv.RemoveLabel(ctx, claims, threadID, labelID); err != nil {
				return shared.Error(400, err.Error()), nil
			}
			return events.APIGatewayProxyResponse{StatusCode: 204}, nil
		}

	case "/v1/labels":
		switch event.HTTPMethod {
		case "GET":
			if !claims.HasScope(authpkg.ScopeInboxRead) {
				return shared.Error(403, "insufficient scope"), nil
			}
			labels, err := labelSv.List(ctx, claims)
			if err != nil {
				return shared.Error(500, err.Error()), nil
			}
			return shared.JSON(200, map[string]any{"labels": labels}), nil
		case "POST":
			if !claims.HasScope(authpkg.ScopeInboxWrite) {
				return shared.Error(403, "insufficient scope"), nil
			}
			var req inboxsvc.CreateLabelRequest
			if err := shared.Decode(event, &req); err != nil {
				return shared.Error(400, "invalid request body"), nil
			}
			label, err := labelSv.Create(ctx, claims, req)
			if err != nil {
				return shared.Error(400, err.Error()), nil
			}
			return shared.JSON(201, label), nil
		}

	case "/v1/labels/{labelId}":
		switch event.HTTPMethod {
		case "PATCH":
			if !claims.HasScope(authpkg.ScopeInboxWrite) {
				return shared.Error(403, "insufficient scope"), nil
			}
			var req inboxsvc.UpdateLabelRequest
			if err := shared.Decode(event, &req); err != nil {
				return shared.Error(400, "invalid request body"), nil
			}
			label, err := labelSv.Update(ctx, claims, labelID, req)
			if err != nil {
				return shared.Error(400, err.Error()), nil
			}
			return shared.JSON(200, label), nil
		case "DELETE":
			if !claims.HasScope(authpkg.ScopeInboxWrite) {
				return shared.Error(403, "insufficient scope"), nil
			}
			if err := labelSv.Delete(ctx, claims, labelID); err != nil {
				return shared.Error(404, "label not found"), nil
			}
			return events.APIGatewayProxyResponse{StatusCode: 204}, nil
		}
	}

	_ = inboxID // used in query
	return shared.Error(404, "not found"), nil
}

func main() {
	lambda.Start(handler)
}
