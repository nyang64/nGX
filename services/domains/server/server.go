/*
 * Copyright (c) 2026 nyklabs.com. All rights reserved.
 *
 * Licensed under the nGX Commercial Source License v1.0.
 * See LICENSE file in the project root for full license information.
 */

package server

import (
	"context"
	"log/slog"
	"net/http"

	"agentmail/pkg/auth"
	"agentmail/pkg/middleware"
	"agentmail/services/domains/handlers"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
)

// Server wires the HTTP router for the domains Lambda.
type Server struct {
	router *chi.Mux
	addr   string
}

func New(addr string, logger *slog.Logger, domainH *handlers.DomainHandler) *Server {
	r := chi.NewRouter()

	r.Use(chimw.RealIP)
	r.Use(middleware.RequestID)
	r.Use(middleware.Logger(logger))
	r.Use(middleware.Recover)
	r.Use(auth.InjectClaimsFromHeader) // API Gateway injects X-Claims header from authorizer

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	r.Route("/v1/domains", func(r chi.Router) {
		r.Post("/", domainH.Register)
		r.Get("/", domainH.List)
		r.Get("/{domain_id}", domainH.Get)
		r.Post("/{domain_id}/verify", domainH.Verify)
		r.Delete("/{domain_id}", domainH.Delete)
	})

	return &Server{router: r, addr: addr}
}

func (s *Server) ListenAndServe(ctx context.Context) error {
	srv := &http.Server{Addr: s.addr, Handler: s.router}
	go func() {
		<-ctx.Done()
		_ = srv.Shutdown(context.Background())
	}()
	slog.Info("domains service listening", "addr", s.addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}
