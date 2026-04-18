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
	"os/signal"
	"syscall"

	"agentmail/pkg/config"
	"agentmail/pkg/db"
	"agentmail/pkg/telemetry"
	"agentmail/services/scheduler/jobs"
	"agentmail/services/scheduler/runner"
)

func main() {
	cfg := config.Load()
	logger := telemetry.SetupLogger(cfg.LogLevel, cfg.LogFormat)
	slog.SetDefault(logger)

	ctx := context.Background()

	pool, err := db.Connect(ctx, cfg.Database)
	if err != nil {
		slog.Error("connect to database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	bounceCheck := jobs.NewBounceCheck(pool)
	draftExpiry := jobs.NewDraftExpiry(pool)

	cr := runner.New()

	// Bounce check runs every hour (at minute 0).
	cr.Add("0 0 * * * *", "bounce_check", bounceCheck.Run)

	// Draft expiry runs every 5 minutes.
	cr.Add("0 */5 * * * *", "draft_expiry", draftExpiry.Run)

	cr.Start()
	slog.Info("scheduler started")

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("shutting down scheduler")
	cr.Stop()
}
