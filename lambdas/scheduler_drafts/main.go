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
	draftSvc *inboxsvc.DraftService
)

func init() {
	ctx := context.Background()
	pool = shared.InitDB()

	awsConf, err := awscfg.LoadDefaultConfig(ctx)
	if err != nil {
		slog.Error("scheduler_drafts: load AWS config", "error", err)
		os.Exit(1)
	}
	sqsClient := sqssdk.NewFromConfig(awsConf)
	publisher := sqspkg.NewPublisher(sqsClient)

	draftSvc = inboxsvc.NewDraftService(
		pool,
		inboxstore.NewPostgresDraftStore(pool),
		inboxstore.NewPostgresMessageStore(pool),
		inboxstore.NewPostgresThreadStore(pool),
		inboxstore.NewPostgresInboxStore(pool),
		publisher, // eventProducer
		publisher, // outboundProducer (same instance implements both)
		nil,       // attachmentsS3 — scheduler doesn't handle inline attachment uploads
	)
}

func handler(ctx context.Context, _ map[string]interface{}) error {
	// Find drafts with scheduled_at due and still pending (no RLS — scheduler is trusted system).
	rows, err := pool.Query(ctx, `
		SELECT id, org_id FROM drafts
		WHERE scheduled_at <= NOW()
		  AND review_status = 'pending'
		LIMIT 100
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	type draftRow struct {
		id    uuid.UUID
		orgID uuid.UUID
	}
	var pending []draftRow
	for rows.Next() {
		var r draftRow
		if err := rows.Scan(&r.id, &r.orgID); err != nil {
			slog.Error("scheduler_drafts: scan draft row", "error", err)
			continue
		}
		pending = append(pending, r)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	slog.Info("scheduler_drafts: processing scheduled drafts", "count", len(pending))

	for _, d := range pending {
		// Construct minimal claims — DraftService.Approve only needs OrgID for RLS scoping.
		claims := &authpkg.Claims{OrgID: d.orgID}
		if _, err := draftSvc.Approve(ctx, claims, d.id, "scheduled send"); err != nil {
			slog.Error("scheduler_drafts: approve failed", "draft_id", d.id, "org_id", d.orgID, "error", err)
			// Continue — don't block other drafts on one failure.
		} else {
			slog.Info("scheduler_drafts: approved", "draft_id", d.id)
		}
	}
	return nil
}

func main() {
	lambda.Start(handler)
}
