package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"agentmail/pkg/config"
	"agentmail/pkg/crypto"
	"agentmail/pkg/db"
	"agentmail/pkg/telemetry"
	"agentmail/services/webhook-service/consumer"
	"agentmail/services/webhook-service/delivery"
	"agentmail/services/webhook-service/server"
	"agentmail/services/webhook-service/store"
)

func main() {
	cfg := config.Load()
	logger := telemetry.SetupLogger(cfg.LogLevel, cfg.LogFormat)
	slog.SetDefault(logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pool, err := db.Connect(ctx, cfg.Database)
	if err != nil {
		slog.Error("connect to database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	// Parse the encryption key; log a warning if not set (don't crash).
	var encKey []byte
	if cfg.Webhook.EncryptionKey != "" {
		encKey, err = crypto.KeyFromHex(cfg.Webhook.EncryptionKey)
		if err != nil {
			slog.Error("invalid WEBHOOK_ENCRYPTION_KEY", "error", err)
			os.Exit(1)
		}
	} else {
		slog.Warn("WEBHOOK_ENCRYPTION_KEY is not set; auth header encryption/decryption is disabled")
	}

	deliveryStore := store.NewDeliveryStore(pool)
	deliverer := delivery.NewDeliverer()

	retryScheduler := delivery.NewRetryScheduler(pool, deliverer, deliveryStore, cfg.Webhook.MaxRetries, encKey)
	c := consumer.New(cfg.Kafka.Brokers, cfg.Kafka.GroupID, deliveryStore, deliverer, pool, encKey)

	httpServer := server.New(cfg.Webhook.Port, deliveryStore, encKey)

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		slog.Info("webhook-service consumer starting")
		if err := c.Run(ctx); err != nil {
			slog.Error("webhook consumer error", "error", err)
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		slog.Info("webhook-service retry scheduler starting")
		retryScheduler.Run(ctx)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := httpServer.Start(); err != nil {
			slog.Error("webhook HTTP server error", "error", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("shutting down webhook-service")
	cancel()

	// Gracefully shut down the HTTP server.
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		slog.Error("HTTP server shutdown error", "error", err)
	}

	wg.Wait()
}
