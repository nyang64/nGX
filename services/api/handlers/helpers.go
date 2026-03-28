package handlers

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"

	authpkg "agentmail/pkg/auth"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// writeJSON encodes v as JSON and writes it with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("write json response", "error", err)
	}
}

// writeProxied writes a proxied response body and status code directly to w.
func writeProxied(w http.ResponseWriter, status int, body []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// decode decodes JSON from r.Body into v.
func decode(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(v)
}

// claimsFromCtx retrieves Claims from the request context.
// Callers can assume this is non-nil because the auth middleware runs first.
func claimsFromCtx(r *http.Request) *authpkg.Claims {
	return authpkg.ClaimsFromCtx(r.Context())
}

// pathUUID parses a chi URL parameter as a UUID.
func pathUUID(r *http.Request, param string) (uuid.UUID, error) {
	return uuid.Parse(chi.URLParam(r, param))
}
