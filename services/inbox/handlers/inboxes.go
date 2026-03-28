package handlers

import (
	"net/http"
	"strconv"

	"agentmail/pkg/auth"
	"agentmail/services/inbox/service"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// InboxHandler holds dependencies for inbox HTTP handlers.
type InboxHandler struct {
	svc *service.InboxService
}

// NewInboxHandler creates a new InboxHandler.
func NewInboxHandler(svc *service.InboxService) *InboxHandler {
	return &InboxHandler{svc: svc}
}

// Create handles POST /inboxes.
func (h *InboxHandler) Create(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromCtx(r.Context())
	if claims == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	var body struct {
		PodID       *uuid.UUID     `json:"pod_id"`
		Address     string         `json:"address"`
		DisplayName string         `json:"display_name"`
		Settings    map[string]any `json:"settings"`
	}
	if !decode(w, r, &body) {
		return
	}
	if body.Address == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "address is required"})
		return
	}

	inbox, err := h.svc.Create(r.Context(), claims, service.CreateInboxRequest{
		PodID:       body.PodID,
		Address:     body.Address,
		DisplayName: body.DisplayName,
		Settings:    body.Settings,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, inbox)
}

// Get handles GET /inboxes/{inboxID}.
func (h *InboxHandler) Get(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromCtx(r.Context())
	if claims == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	inboxID, err := uuid.Parse(chi.URLParam(r, "inboxID"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid inbox ID"})
		return
	}

	inbox, err := h.svc.Get(r.Context(), claims, inboxID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, inbox)
}

// List handles GET /inboxes.
func (h *InboxHandler) List(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromCtx(r.Context())
	if claims == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	var podID *uuid.UUID
	if podIDStr := r.URL.Query().Get("pod_id"); podIDStr != "" {
		id, err := uuid.Parse(podIDStr)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid pod_id"})
			return
		}
		podID = &id
	}

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	cursor := r.URL.Query().Get("cursor")

	inboxes, nextCursor, err := h.svc.List(r.Context(), claims, podID, limit, cursor)
	if err != nil {
		writeError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"inboxes":     inboxes,
		"next_cursor": nextCursor,
	})
}

// Update handles PATCH /inboxes/{inboxID}.
func (h *InboxHandler) Update(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromCtx(r.Context())
	if claims == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	inboxID, err := uuid.Parse(chi.URLParam(r, "inboxID"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid inbox ID"})
		return
	}

	var body struct {
		DisplayName *string        `json:"display_name"`
		Status      *string        `json:"status"`
		Settings    map[string]any `json:"settings"`
	}
	if !decode(w, r, &body) {
		return
	}

	inbox, err := h.svc.Update(r.Context(), claims, inboxID, service.UpdateInboxRequest{
		DisplayName: body.DisplayName,
		Status:      body.Status,
		Settings:    body.Settings,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, inbox)
}

// Delete handles DELETE /inboxes/{inboxID}.
func (h *InboxHandler) Delete(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromCtx(r.Context())
	if claims == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	inboxID, err := uuid.Parse(chi.URLParam(r, "inboxID"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid inbox ID"})
		return
	}

	if err := h.svc.Delete(r.Context(), claims, inboxID); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
