package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"agentmail/services/webhook-service/handlers"
	"agentmail/services/webhook-service/store"
)

// Server is the HTTP server for webhook CRUD operations.
type Server struct {
	httpServer *http.Server
	handler    *handlers.WebhookHandler
}

// New creates a Server on the given port.
func New(port int, s *store.DeliveryStore, encKey []byte) *Server {
	h := handlers.NewWebhookHandler(s, encKey)
	mux := http.NewServeMux()

	srv := &Server{
		handler: h,
		httpServer: &http.Server{
			Addr:         fmt.Sprintf(":%d", port),
			Handler:      mux,
			ReadTimeout:  15 * time.Second,
			WriteTimeout: 15 * time.Second,
			IdleTimeout:  60 * time.Second,
		},
	}

	mux.HandleFunc("/health", srv.health)
	mux.HandleFunc("/webhooks", srv.routeWebhooks)
	mux.HandleFunc("/webhooks/", srv.routeWebhooksByID)

	return srv
}

// Start begins listening for HTTP requests.
func (s *Server) Start() error {
	slog.Info("webhook HTTP server starting", "addr", s.httpServer.Addr)
	if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("http server: %w", err)
	}
	return nil
}

// Shutdown gracefully stops the server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

// routeWebhooks handles /webhooks (collection routes).
func (s *Server) routeWebhooks(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handler.ListWebhooks(w, r)
	case http.MethodPost:
		s.handler.CreateWebhook(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// routeWebhooksByID handles /webhooks/{id} and /webhooks/{id}/deliveries.
func (s *Server) routeWebhooksByID(w http.ResponseWriter, r *http.Request) {
	// Determine if this is a deliveries sub-resource.
	path := strings.TrimPrefix(r.URL.Path, "/webhooks/")
	// path is now either "{id}" or "{id}/deliveries"
	parts := strings.SplitN(path, "/", 2)

	if len(parts) == 2 && parts[1] == "deliveries" {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.handler.ListDeliveries(w, r)
		return
	}

	// Single resource: /webhooks/{id}
	switch r.Method {
	case http.MethodGet:
		s.handler.GetWebhook(w, r)
	case http.MethodPatch:
		s.handler.UpdateWebhook(w, r)
	case http.MethodDelete:
		s.handler.DeleteWebhook(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
