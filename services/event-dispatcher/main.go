package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"agentmail/pkg/config"
	"agentmail/pkg/db"
	pkgredis "agentmail/pkg/redis"
	"agentmail/pkg/telemetry"
	"agentmail/services/event-dispatcher/consumer"
	"agentmail/services/event-dispatcher/fanout"
	"agentmail/services/event-dispatcher/store"
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

	redisClient, err := pkgredis.NewClient(cfg.Redis.URL)
	if err != nil {
		slog.Error("connect to redis", "error", err)
		os.Exit(1)
	}
	defer redisClient.Close()

	webhookStore := store.NewWebhookSubscriptionStore(pool)
	webhookFanout := fanout.NewWebhookFanout(cfg.Kafka.Brokers, webhookStore)
	wsFanout := fanout.NewWebSocketFanout(redisClient)

	c := consumer.New(cfg.Kafka.Brokers, cfg.Kafka.GroupID, webhookFanout, wsFanout)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		slog.Info("event-dispatcher starting")
		if err := c.Run(ctx); err != nil {
			slog.Error("consumer error", "error", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("shutting down event-dispatcher")
	cancel()
	wg.Wait()

	if err := webhookFanout.Close(); err != nil {
		slog.Error("close webhook fanout producer", "error", err)
	}
}
