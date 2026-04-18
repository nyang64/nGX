/*
 * Copyright (c) 2026 nyklabs.com. All rights reserved.
 *
 * Licensed under the nGX Commercial Source License v1.0.
 * See LICENSE file in the project root for full license information.
 */

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	authpkg "agentmail/pkg/auth"
	"agentmail/pkg/config"
	"agentmail/pkg/db"
	"agentmail/pkg/embedder"
	"agentmail/pkg/telemetry"
	searchauth "agentmail/services/search/auth"
	"agentmail/services/search/handlers"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
)

const defaultPort = 8084

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

	// Embedder client is optional: semantic search is disabled when EMBEDDER_URL is empty.
	var embClient *embedder.Client
	if cfg.EmbedderURL != "" {
		embClient = embedder.New(cfg.EmbedderURL, cfg.EmbedderModel, 256)
	}

	searchHandler := handlers.NewSearchHandler(pool, embClient)
	validator := searchauth.NewRemoteValidator(cfg.AuthServiceURL)

	r := chi.NewRouter()
	r.Use(chimiddleware.RealIP)
	r.Use(chimiddleware.RequestID)
	r.Use(chimiddleware.Recoverer)
	r.Use(loggingMiddleware)

	// Health check — unauthenticated.
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Authenticated search endpoint.
	r.Group(func(r chi.Router) {
		r.Use(buildAuthMiddleware(validator))
		r.Get("/search", searchHandler.Search)
	})

	addr := fmt.Sprintf("%s:%d", cfg.API.Host, defaultPort)
	httpSrv := &http.Server{
		Addr:         addr,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		slog.Info("search service starting", "addr", addr)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("shutting down search service")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		slog.Error("shutdown error", "error", err)
	}
}

// buildAuthMiddleware validates Bearer tokens and injects Claims into the request context.
func buildAuthMiddleware(validator *searchauth.RemoteValidator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := r.Header.Get("Authorization")
			key := ""
			if strings.HasPrefix(header, "Bearer ") {
				key = strings.TrimPrefix(header, "Bearer ")
			}
			if key == "" {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing or invalid authorization header"})
				return
			}
			claims, err := validator.ValidateKey(r.Context(), key)
			if err != nil {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid or expired api key"})
				return
			}
			ctx := authpkg.WithClaims(r.Context(), claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		slog.Info("http request",
			"method", r.Method,
			"path", r.URL.Path,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
