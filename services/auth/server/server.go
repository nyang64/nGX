package server

import (
	"log/slog"
	"net/http"

	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/chi/v5"

	"agentmail/pkg/config"
	"agentmail/pkg/middleware"
	"agentmail/services/auth/handlers"
	"agentmail/services/auth/service"
)

// Server wires together the HTTP router for the auth service.
type Server struct {
	cfg    *config.Config
	svc    *service.AuthService
	logger *slog.Logger
}

// New creates a new Server.
func New(cfg *config.Config, svc *service.AuthService, logger *slog.Logger) *Server {
	return &Server{cfg: cfg, svc: svc, logger: logger}
}

// Handler builds and returns the HTTP handler with all routes registered.
func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.Logger(s.logger))
	r.Use(middleware.Recover)
	r.Use(chimw.Compress(5))

	keyHandler := handlers.NewAPIKeyHandler(s.svc)

	r.Get("/health", handlers.Health)
	r.Get("/ready", handlers.Ready)

	r.Route("/api-keys", func(r chi.Router) {
		r.Post("/", keyHandler.Create)
		r.Get("/", keyHandler.List)
		r.Delete("/{keyID}", keyHandler.Revoke)
	})

	r.Post("/validate", keyHandler.Validate)

	return r
}
