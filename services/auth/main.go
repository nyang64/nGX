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
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"agentmail/pkg/config"
	"agentmail/pkg/db"
	"agentmail/pkg/telemetry"
	"agentmail/services/auth/server"
	"agentmail/services/auth/service"
	"agentmail/services/auth/store"
)

const defaultPort = 8081

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

	keyStore := store.NewPostgresAPIKeyStore(pool)
	authSvc := service.NewAuthService(keyStore)
	srv := server.New(cfg, authSvc, logger)

	addr := fmt.Sprintf("%s:%d", cfg.API.Host, defaultPort)
	httpSrv := &http.Server{
		Addr:         addr,
		Handler:      srv.Handler(),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		slog.Info("auth service starting", "addr", addr)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("shutting down auth service")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		slog.Error("shutdown error", "error", err)
	}
}
