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
	"time"

	"agentmail/pkg/config"
	"agentmail/pkg/db"
	redispkg "agentmail/pkg/redis"
	"agentmail/pkg/telemetry"
	"agentmail/services/api/clients"
	"agentmail/services/api/server"
	"agentmail/services/api/store"
)

func main() {
	cfg := config.Load()

	logger := telemetry.SetupLogger(cfg.LogLevel, cfg.LogFormat)
	slog.SetDefault(logger)

	slog.Info("starting api gateway",
		"env", cfg.Environment,
		"port", cfg.API.Port,
	)

	// Connect to PostgreSQL.
	pool, err := db.Connect(context.Background(), cfg.Database)
	if err != nil {
		slog.Error("connect to database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()
	slog.Info("connected to postgres")

	// Connect to Redis.
	redisClient, err := redispkg.NewClient(cfg.Redis.URL)
	if err != nil {
		slog.Error("connect to redis", "error", err)
		os.Exit(1)
	}
	defer redisClient.Close()
	slog.Info("connected to redis")

	// Build service clients.
	authClient := clients.NewAuthClient(cfg.AuthServiceURL)
	inboxClient := clients.NewInboxClient(cfg.InboxServiceURL)
	webhookClient := clients.NewWebhookClient(cfg.WebhookServiceURL)

	// Build the WebSocket hub.
	hub := server.NewHub(redisClient)

	// Build the org store.
	orgStore := store.NewOrgStore(pool)

	// Build the HTTP server.
	srv := server.New(cfg, authClient, inboxClient, webhookClient, hub, orgStore)

	// Start hub in background.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go hub.Run(ctx)

	// Start HTTP server in background.
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- srv.Start()
	}()

	// Wait for shutdown signal or server error.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-quit:
		slog.Info("shutdown signal received", "signal", sig)
	case err := <-serverErr:
		if err != nil {
			slog.Error("server error", "error", err)
		}
	}

	// Graceful shutdown with a 30-second timeout.
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	cancel() // stop the hub

	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("graceful shutdown failed", "error", err)
		os.Exit(1)
	}
	slog.Info("api gateway stopped")
}
