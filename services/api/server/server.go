package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"agentmail/pkg/config"
	"agentmail/services/api/clients"
	"agentmail/services/api/handlers"
	"agentmail/services/api/store"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"
)

// Server is the HTTP server for the API gateway.
type Server struct {
	cfg          *config.Config
	router       *chi.Mux
	httpServer   *http.Server
	authClient   *clients.AuthClient
	inboxClient  *clients.InboxClient
	webhookClient *clients.WebhookClient
	hub          *Hub
	orgStore     store.OrgStore
}

// New creates and configures the API gateway server.
func New(
	cfg *config.Config,
	authClient *clients.AuthClient,
	inboxClient *clients.InboxClient,
	webhookClient *clients.WebhookClient,
	hub *Hub,
	orgStore store.OrgStore,
) *Server {
	s := &Server{
		cfg:           cfg,
		authClient:    authClient,
		inboxClient:   inboxClient,
		webhookClient: webhookClient,
		hub:           hub,
		orgStore:      orgStore,
	}
	s.router = s.buildRouter()
	s.httpServer = &http.Server{
		Addr:    fmt.Sprintf("%s:%d", cfg.API.Host, cfg.API.Port),
		Handler: s.router,
	}
	return s
}

// Start begins serving requests. It blocks until the server is shut down.
func (s *Server) Start() error {
	slog.Info("api server listening", "addr", s.httpServer.Addr)
	if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("http server: %w", err)
	}
	return nil
}

// Shutdown gracefully stops the server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

// buildRouter constructs the chi router with all middleware and routes.
func (s *Server) buildRouter() *chi.Mux {
	r := chi.NewRouter()

	// Global middleware.
	r.Use(recoverMiddleware)
	r.Use(loggingMiddleware)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Authorization", "Content-Type", "X-Request-ID"},
		AllowCredentials: false,
		MaxAge:           300,
	}))

	// Build handlers.
	healthH := handlers.NewHealthHandler()
	orgH := handlers.NewOrgHandler(s.orgStore)
	inboxH := handlers.NewInboxHandler(s.inboxClient)
	threadH := handlers.NewThreadHandler(s.inboxClient)
	messageH := handlers.NewMessageHandler(s.inboxClient)
	draftH := handlers.NewDraftHandler(s.inboxClient)
	labelH := handlers.NewLabelHandler(s.inboxClient)
	webhookH := handlers.NewWebhookHandler(s.webhookClient)
	apiKeyH := handlers.NewAPIKeyHandler(s.authClient)
	searchH := handlers.NewSearchHandler(s.cfg.SearchServiceURL)
	wsH := handlers.NewWSHandler(s.hub)

	registerRoutes(r, s.authClient, healthH, orgH, inboxH, threadH, messageH, draftH, labelH, webhookH, apiKeyH, searchH, wsH)

	return r
}
