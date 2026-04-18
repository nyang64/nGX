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
	"os/signal"
	"syscall"

	"agentmail/pkg/config"
	dbpkg "agentmail/pkg/db"
	"agentmail/pkg/kafka"
	"agentmail/pkg/telemetry"
	"agentmail/services/inbox/handlers"
	"agentmail/services/inbox/server"
	"agentmail/services/inbox/service"
	"agentmail/services/inbox/store"
)

func main() {
	cfg := config.Load()

	logger := telemetry.SetupLogger(cfg.LogLevel, cfg.LogFormat)
	slog.SetDefault(logger)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Connect to PostgreSQL.
	pool, err := dbpkg.Connect(ctx, cfg.Database)
	if err != nil {
		slog.Error("connect to database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()
	slog.Info("connected to database")

	// Create Kafka producer for the events fanout topic.
	eventProducer := kafka.NewProducer(cfg.Kafka.Brokers, kafka.TopicEventsFanout)
	defer eventProducer.Close()

	// Create Kafka producer for the outbound email queue.
	outboundProducer := kafka.NewProducer(cfg.Kafka.Brokers, kafka.TopicEmailOutboundQueue)
	defer outboundProducer.Close()

	// Build stores.
	inboxStore := store.NewPostgresInboxStore(pool)
	threadStore := store.NewPostgresThreadStore(pool)
	messageStore := store.NewPostgresMessageStore(pool)
	draftStore := store.NewPostgresDraftStore(pool)
	labelStore := store.NewPostgresLabelStore(pool)

	// Build services.
	inboxSvc := service.NewInboxService(pool, inboxStore, eventProducer, cfg.MailDomain)
	threadSvc := service.NewThreadService(pool, threadStore, inboxStore, eventProducer)
	messageSvc := service.NewMessageService(pool, messageStore, threadStore, inboxStore, outboundProducer)
	draftSvc := service.NewDraftService(pool, draftStore, messageStore, threadStore, inboxStore, eventProducer, outboundProducer)
	labelSvc := service.NewLabelService(pool, labelStore)

	// Build handlers.
	inboxH := handlers.NewInboxHandler(inboxSvc)
	threadH := handlers.NewThreadHandler(threadSvc)
	messageH := handlers.NewMessageHandler(messageSvc)
	draftH := handlers.NewDraftHandler(draftSvc)
	labelH := handlers.NewLabelHandler(labelSvc)

	// Start HTTP server.
	addr := fmt.Sprintf("%s:%d", cfg.API.Host, 8082)
	srv := server.New(addr, logger, inboxH, threadH, messageH, draftH, labelH)

	if err := srv.ListenAndServe(ctx); err != nil {
		slog.Error("server error", "error", err)
		os.Exit(1)
	}
	slog.Info("inbox service stopped")
}
