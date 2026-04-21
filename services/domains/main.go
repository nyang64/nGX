/*
 * Copyright (c) 2026 nyklabs.com. All rights reserved.
 *
 * Licensed under the nGX Commercial Source License v1.0.
 * See LICENSE file in the project root for full license information.
 */

// domains Lambda — manages custom domain lifecycle for enterprise orgs.
//
// Environment variables:
//
//	DATABASE_URL            postgres://...  (via RDS Proxy)
//	SES_RULE_SET_NAME       ngx-prod-receipt-rules
//	S3_EMAILS_BUCKET        ngx-prod-emails
//	AWS_REGION              us-east-1
//	LOG_LEVEL               info
//	LOG_FORMAT              json
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"agentmail/pkg/config"
	dbpkg "agentmail/pkg/db"
	"agentmail/pkg/telemetry"
	"agentmail/services/domains/handlers"
	"agentmail/services/domains/server"
	"agentmail/services/domains/service"
	"agentmail/services/domains/store"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ses"
)

func main() {
	cfg := config.Load()

	logger := telemetry.SetupLogger(cfg.LogLevel, cfg.LogFormat)
	slog.SetDefault(logger)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Connect to Aurora via RDS Proxy.
	pool, err := dbpkg.Connect(ctx, cfg.Database)
	if err != nil {
		slog.Error("connect to database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()
	slog.Info("connected to database")

	// AWS SES client (region from environment / IAM role).
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		slog.Error("load AWS config", "error", err)
		os.Exit(1)
	}
	sesClient := ses.NewFromConfig(awsCfg)

	// Required environment variables.
	ruleSetName := requireEnv("SES_RULE_SET_NAME")
	s3Bucket := requireEnv("S3_EMAILS_BUCKET")
	awsRegion := cfg.AWSRegion // from config or AWS_REGION env

	// Wire up layers.
	domainStore := store.NewPostgresDomainStore(pool)
	domainSvc := service.New(pool, domainStore, sesClient, ruleSetName, s3Bucket, awsRegion)
	domainH := handlers.NewDomainHandler(domainSvc)

	addr := fmt.Sprintf("0.0.0.0:%d", 8090)
	srv := server.New(addr, logger, domainH)

	if err := srv.ListenAndServe(ctx); err != nil {
		slog.Error("server error", "error", err)
		os.Exit(1)
	}
	slog.Info("domains service stopped")
}

func requireEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		slog.Error("required environment variable not set", "key", key)
		os.Exit(1)
	}
	return v
}
