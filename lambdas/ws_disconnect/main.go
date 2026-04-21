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

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/jackc/pgx/v5/pgxpool"

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
		slog.Error("ws_disconnect: connect to database", "error", err)
		os.Exit(1)
	}
}

func handler(ctx context.Context, event events.APIGatewayWebsocketProxyRequest) (events.APIGatewayProxyResponse, error) {
	connectionID := event.RequestContext.ConnectionID

	_, err := pool.Exec(ctx,
		`DELETE FROM websocket_connections WHERE connection_id = $1`,
		connectionID,
	)
	if err != nil {
		slog.Error("ws_disconnect: delete connection", "error", err, "connection_id", connectionID)
		// Return 200 anyway — API Gateway ignores disconnect errors.
	}

	slog.Info("ws_disconnect: disconnected", "connection_id", connectionID)
	return events.APIGatewayProxyResponse{StatusCode: 200, Body: "Disconnected"}, nil
}

func main() {
	lambda.Start(handler)
}
