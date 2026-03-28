package server

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"agentmail/pkg/auth"
	"agentmail/pkg/middleware"
	"agentmail/services/inbox/handlers"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
)

// Server wires together the HTTP router and all handlers.
type Server struct {
	router *chi.Mux
	addr   string
}

// New creates and configures the HTTP server.
func New(
	addr string,
	logger *slog.Logger,
	inboxH *handlers.InboxHandler,
	threadH *handlers.ThreadHandler,
	messageH *handlers.MessageHandler,
	draftH *handlers.DraftHandler,
	labelH *handlers.LabelHandler,
) *Server {
	r := chi.NewRouter()

	// Global middleware
	r.Use(chimw.RealIP)
	r.Use(middleware.RequestID)
	r.Use(middleware.Logger(logger))
	r.Use(middleware.Recover)
	r.Use(internalAuthMiddleware)

	// Health check (no auth required)
	r.Get("/health", handlers.Health)

	// Inbox CRUD
	r.Post("/inboxes", inboxH.Create)
	r.Get("/inboxes", inboxH.List)
	r.Get("/inboxes/{inboxID}", inboxH.Get)
	r.Patch("/inboxes/{inboxID}", inboxH.Update)
	r.Delete("/inboxes/{inboxID}", inboxH.Delete)

	// Threads nested under inboxes
	r.Get("/inboxes/{inboxID}/threads", threadH.List)
	r.Get("/inboxes/{inboxID}/threads/{threadID}", threadH.Get)
	r.Patch("/inboxes/{inboxID}/threads/{threadID}", threadH.Update)
	r.Put("/inboxes/{inboxID}/threads/{threadID}/labels/{labelID}", threadH.ApplyLabel)
	r.Delete("/inboxes/{inboxID}/threads/{threadID}/labels/{labelID}", threadH.RemoveLabel)

	// Messages
	r.Post("/inboxes/{inboxID}/messages/send", messageH.Send)
	r.Get("/inboxes/{inboxID}/threads/{threadID}/messages", messageH.List)
	r.Get("/inboxes/{inboxID}/threads/{threadID}/messages/{messageID}", messageH.Get)

	// Drafts
	r.Post("/inboxes/{inboxID}/drafts", draftH.Create)
	r.Get("/inboxes/{inboxID}/drafts", draftH.List)
	r.Get("/drafts/{draftID}", draftH.Get)
	r.Patch("/drafts/{draftID}", draftH.Update)
	r.Delete("/drafts/{draftID}", draftH.Delete)
	r.Post("/drafts/{draftID}/approve", draftH.Approve)
	r.Post("/drafts/{draftID}/reject", draftH.Reject)

	// Labels
	r.Post("/labels", labelH.Create)
	r.Get("/labels", labelH.List)
	r.Get("/labels/{labelID}", labelH.Get)
	r.Patch("/labels/{labelID}", labelH.Update)
	r.Delete("/labels/{labelID}", labelH.Delete)

	return &Server{router: r, addr: addr}
}

// Handler returns the underlying http.Handler (useful for testing).
func (s *Server) Handler() http.Handler {
	return s.router
}

// ListenAndServe starts the HTTP server and blocks until ctx is cancelled.
func (s *Server) ListenAndServe(ctx context.Context) error {
	srv := &http.Server{
		Addr:    s.addr,
		Handler: s.router,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("inbox service listening", "addr", s.addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		return srv.Shutdown(context.Background())
	case err := <-errCh:
		return err
	}
}

// internalAuthMiddleware reads X-Org-ID, X-Key-ID, and X-Scopes headers
// set by the API gateway and injects a Claims into the request context.
func internalAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		orgIDStr := r.Header.Get("X-Org-ID")
		keyIDStr := r.Header.Get("X-Key-ID")
		scopesStr := r.Header.Get("X-Scopes")

		if orgIDStr == "" || keyIDStr == "" {
			// Allow health check without auth
			next.ServeHTTP(w, r)
			return
		}

		orgID, err := uuid.Parse(orgIDStr)
		if err != nil {
			http.Error(w, "invalid X-Org-ID header", http.StatusBadRequest)
			return
		}
		keyID, err := uuid.Parse(keyIDStr)
		if err != nil {
			http.Error(w, "invalid X-Key-ID header", http.StatusBadRequest)
			return
		}

		var scopes []auth.Scope
		if scopesStr != "" {
			for _, s := range strings.Split(scopesStr, ",") {
				s = strings.TrimSpace(s)
				if s != "" {
					scopes = append(scopes, auth.Scope(s))
				}
			}
		}

		claims := &auth.Claims{
			OrgID:  orgID,
			KeyID:  keyID,
			Scopes: scopes,
		}

		// Propagate pod scoping if present
		if podIDStr := r.Header.Get("X-Pod-ID"); podIDStr != "" {
			podID, err := uuid.Parse(podIDStr)
			if err == nil {
				claims.PodID = &podID
			}
		}

		ctx := auth.WithClaims(r.Context(), claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
