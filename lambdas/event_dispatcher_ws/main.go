/*
 * Copyright (c) 2026 nyklabs.com. All rights reserved.
 *
 * Licensed under the nGX Commercial Source License v1.0.
 * See LICENSE file in the project root for full license information.
 */

package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"

	awsevents "github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	awscfg "github.com/aws/aws-sdk-go-v2/config"
	apigwmgmt "github.com/aws/aws-sdk-go-v2/service/apigatewaymanagementapi"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"agentmail/lambdas/shared"
	domainevents "agentmail/pkg/events"
)

var (
	pool       *pgxpool.Pool
	mgmtClient *apigwmgmt.Client
)

func init() {
	ctx := context.Background()
	pool = shared.InitDB()

	awsConf, err := awscfg.LoadDefaultConfig(ctx)
	if err != nil {
		slog.Error("event_dispatcher_ws: load AWS config", "error", err)
		os.Exit(1)
	}

	wsEndpoint := os.Getenv("APIGW_WEBSOCKET_ENDPOINT")
	mgmtClient = apigwmgmt.NewFromConfig(awsConf, func(o *apigwmgmt.Options) {
		if wsEndpoint != "" {
			o.BaseEndpoint = aws.String(wsEndpoint)
		}
	})
}

func handler(ctx context.Context, sqsEvent awsevents.SQSEvent) (awsevents.SQSEventResponse, error) {
	var failures []awsevents.SQSBatchItemFailure
	for _, record := range sqsEvent.Records {
		if err := processRecord(ctx, record); err != nil {
			slog.Error("event_dispatcher_ws: process record failed", "id", record.MessageId, "error", err)
			failures = append(failures, awsevents.SQSBatchItemFailure{ItemIdentifier: record.MessageId})
		}
	}
	return awsevents.SQSEventResponse{BatchItemFailures: failures}, nil
}

func processRecord(ctx context.Context, record awsevents.SQSMessage) error {
	evt, err := domainevents.Unmarshal([]byte(record.Body))
	if err != nil {
		slog.Warn("event_dispatcher_ws: unmarshal event, skipping", "error", err)
		return nil
	}

	base := evt.GetBase()
	orgID, err := uuid.Parse(base.OrgID)
	if err != nil {
		slog.Warn("event_dispatcher_ws: invalid org_id", "org_id", base.OrgID)
		return nil
	}

	// Find all active WebSocket connections for this org.
	rows, err := pool.Query(ctx,
		`SELECT connection_id FROM websocket_connections
		 WHERE org_id = $1 AND (ttl IS NULL OR ttl > NOW())`,
		orgID,
	)
	if err != nil {
		return err
	}
	defer rows.Close()

	var connIDs []string
	for rows.Next() {
		var connID string
		if err := rows.Scan(&connID); err != nil {
			slog.Warn("event_dispatcher_ws: scan connection_id", "error", err)
			continue
		}
		connIDs = append(connIDs, connID)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	if len(connIDs) == 0 {
		return nil
	}

	payload, err := json.Marshal(evt)
	if err != nil {
		return err
	}

	for _, connID := range connIDs {
		_, err := mgmtClient.PostToConnection(ctx, &apigwmgmt.PostToConnectionInput{
			ConnectionId: aws.String(connID),
			Data:         payload,
		})
		if err != nil {
			// GoneException means the connection is closed — remove it.
			slog.Warn("event_dispatcher_ws: PostToConnection failed, removing stale connection",
				"connection_id", connID, "error", err)
			_, _ = pool.Exec(ctx,
				`DELETE FROM websocket_connections WHERE connection_id = $1`,
				connID,
			)
		}
	}
	return nil
}

func main() {
	lambda.Start(handler)
}
