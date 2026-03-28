package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"agentmail/services/auth/service"
)

// APIKeyHandler handles HTTP requests for API key management.
type APIKeyHandler struct {
	svc *service.AuthService
}

// NewAPIKeyHandler creates a new APIKeyHandler.
func NewAPIKeyHandler(svc *service.AuthService) *APIKeyHandler {
	return &APIKeyHandler{svc: svc}
}

// Create handles POST /api-keys.
// Body: { "org_id": "...", "name": "...", "scopes": [...], "pod_id": "..." }
func (h *APIKeyHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OrgID  string   `json:"org_id"`
		Name   string   `json:"name"`
		Scopes []string `json:"scopes"`
		PodID  *string  `json:"pod_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	orgID, err := uuid.Parse(req.OrgID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid org_id")
		return
	}

	var podID *uuid.UUID
	if req.PodID != nil {
		id, err := uuid.Parse(*req.PodID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid pod_id")
			return
		}
		podID = &id
	}

	svcReq := service.CreateKeyRequest{
		OrgID:  orgID,
		Name:   req.Name,
		Scopes: req.Scopes,
		PodID:  podID,
	}

	key, plaintext, err := h.svc.CreateKey(r.Context(), svcReq)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"id":         key.ID,
		"name":       key.Name,
		"key":        plaintext, // shown only once
		"key_prefix": key.KeyPrefix,
		"scopes":     key.Scopes,
		"created_at": key.CreatedAt,
	})
}

// List handles GET /api-keys?org_id=...
func (h *APIKeyHandler) List(w http.ResponseWriter, r *http.Request) {
	orgIDStr := r.URL.Query().Get("org_id")
	orgID, err := uuid.Parse(orgIDStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid org_id")
		return
	}

	keys, err := h.svc.ListKeys(r.Context(), orgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"items": keys})
}

// Revoke handles DELETE /api-keys/{keyID}?org_id=...
func (h *APIKeyHandler) Revoke(w http.ResponseWriter, r *http.Request) {
	orgIDStr := r.URL.Query().Get("org_id")
	orgID, err := uuid.Parse(orgIDStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid org_id")
		return
	}

	keyID, err := uuid.Parse(chi.URLParam(r, "keyID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid key id")
		return
	}

	if err := h.svc.RevokeKey(r.Context(), orgID, keyID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// Validate handles POST /validate — internal endpoint for API gateway key validation.
func (h *APIKeyHandler) Validate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Key string `json:"key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	claims, err := h.svc.ValidateKey(r.Context(), req.Key)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid key")
		return
	}

	writeJSON(w, http.StatusOK, claims)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
