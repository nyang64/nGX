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
	"agentmail/pkg/embedder"
	"agentmail/pkg/s3"
	"agentmail/pkg/telemetry"
	embconsumer "agentmail/services/embedder/consumer"
	"agentmail/services/embedder/store"
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

	s3Client, err := s3.NewClient(ctx, cfg.S3)
	if err != nil {
		slog.Error("connect to S3", "error", err)
		os.Exit(1)
	}

	embClient := embedder.New(cfg.EmbedderURL, cfg.EmbedderModel, 256)
	st := store.New(pool)
	consumer := embconsumer.New(cfg.Kafka.Brokers, cfg.Kafka.GroupID, st, s3Client, embClient)

	slog.Info("embedder service starting",
		"embedder_url", cfg.EmbedderURL,
		"model", cfg.EmbedderModel,
	)

	go func() {
		if err := consumer.Run(ctx); err != nil {
			slog.Error("consumer stopped", "error", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("shutting down embedder service")
	_ = consumer.Close()
}
