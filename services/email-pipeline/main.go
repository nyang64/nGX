package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"agentmail/pkg/config"
	"agentmail/pkg/db"
	"agentmail/pkg/kafka"
	"agentmail/pkg/s3"
	"agentmail/pkg/telemetry"
	"agentmail/services/email-pipeline/emailauth"
	"agentmail/services/email-pipeline/inbound"
	"agentmail/services/email-pipeline/outbound"
	"agentmail/services/email-pipeline/store"
)

func main() {
	cfg := config.Load()
	logger := telemetry.SetupLogger(cfg.LogLevel, cfg.LogFormat)
	slog.SetDefault(logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Database.
	pool, err := db.Connect(ctx, cfg.Database)
	if err != nil {
		slog.Error("connect to database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	// S3 / MinIO.
	s3Client, err := s3.NewClient(ctx, cfg.S3)
	if err != nil {
		slog.Error("connect to S3", "error", err)
		os.Exit(1)
	}

	// Producer for email.inbound.raw — used by the SMTP enqueuer.
	inboundProducer := kafka.NewProducer(cfg.Kafka.Brokers, kafka.TopicEmailInboundRaw)
	defer inboundProducer.Close()

	// Producer for events.fanout — used by both inbound and outbound processors.
	eventsProducer := kafka.NewProducer(cfg.Kafka.Brokers, kafka.TopicEventsFanout)
	defer eventsProducer.Close()

	emailStore := store.NewEmailStore(pool)

	// ---- Inbound ----
	// SMTP server enqueues raw emails to Kafka immediately (fast path).
	enqueuer := inbound.NewEnqueuer(s3Client, inboundProducer)
	smtpSrv := inbound.NewSMTPServer(cfg, enqueuer)

	// Inbound consumer parses, persists, and fires domain events (slow path).
	inboundConsumer := inbound.NewInboundConsumer(
		cfg.Kafka.Brokers,
		cfg.Kafka.GroupID+"-inbound",
		pool,
		emailStore,
		s3Client,
		eventsProducer,
	)

	// ---- DKIM signer (shared by outbound sender) ----
	dkimSigner, err := emailauth.NewDKIMSigner(
		cfg.SMTP.DKIMPrivateKeyPEM,
		cfg.SMTP.DKIMSelector,
		cfg.SMTP.DKIMDomain,
	)
	if err != nil {
		slog.Error("failed to load DKIM private key", "error", err)
		os.Exit(1)
	}
	if dkimSigner == nil {
		slog.Warn("DKIM signing disabled — set DKIM_PRIVATE_KEY_PEM, DKIM_SELECTOR, DKIM_DOMAIN to enable")
	}

	// ---- Outbound ----
	sender := outbound.NewSender(s3Client, dkimSigner, cfg.SMTP.RelayHost)
	outboundConsumer := outbound.NewQueueConsumer(
		cfg.Kafka.Brokers,
		cfg.Kafka.GroupID+"-outbound",
		sender,
		emailStore,
		pool,
		eventsProducer,
	)

	// Start inbound SMTP server.
	go func() {
		slog.Info("inbound SMTP server starting", "addr", cfg.SMTP.ListenAddr, "domain", cfg.SMTP.Hostname)
		if err := smtpSrv.ListenAndServe(); err != nil {
			slog.Error("SMTP server error", "error", err)
			cancel()
		}
	}()

	// Start inbound Kafka consumer.
	go func() {
		slog.Info("inbound consumer starting")
		if err := inboundConsumer.Start(ctx); err != nil {
			slog.Error("inbound consumer error", "error", err)
			cancel()
		}
	}()

	// Start outbound Kafka consumer.
	go func() {
		slog.Info("outbound consumer starting")
		if err := outboundConsumer.Start(ctx); err != nil {
			slog.Error("outbound consumer error", "error", err)
			cancel()
		}
	}()

	// Wait for termination signal.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-quit:
		slog.Info("received shutdown signal", "signal", sig)
	case <-ctx.Done():
		slog.Info("context cancelled, shutting down")
	}

	slog.Info("shutting down email-pipeline")

	if err := inboundConsumer.Close(); err != nil {
		slog.Error("error closing inbound consumer", "error", err)
	}
	if err := outboundConsumer.Close(); err != nil {
		slog.Error("error closing outbound consumer", "error", err)
	}
	if err := smtpSrv.Close(); err != nil {
		slog.Error("error closing SMTP server", "error", err)
	}
}
