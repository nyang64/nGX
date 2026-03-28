package handlers

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	dbpkg "agentmail/pkg/db"
	"agentmail/services/inbox/service"
)

// writeJSON encodes v as JSON and writes it with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("encode response", "error", err)
	}
}

// writeError writes a JSON error response with an appropriate status code.
func writeError(w http.ResponseWriter, err error) {
	switch {
	case dbpkg.IsNotFound(err):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	case dbpkg.IsDuplicateKey(err):
		writeJSON(w, http.StatusConflict, map[string]string{"error": "already exists"})
	case errors.Is(err, service.ErrInvalidReviewStatus):
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
	default:
		slog.Error("internal error", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
	}
}

// decode reads JSON from r into dst. Returns false and writes a 400 if decoding fails.
func decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body: " + err.Error()})
		return false
	}
	return true
}
