package handlers

import (
	"net/http"

	"agentmail/pkg/auth"
	"agentmail/services/inbox/service"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// LabelHandler holds dependencies for label HTTP handlers.
type LabelHandler struct {
	svc *service.LabelService
}

// NewLabelHandler creates a new LabelHandler.
func NewLabelHandler(svc *service.LabelService) *LabelHandler {
	return &LabelHandler{svc: svc}
}

// Create handles POST /labels.
func (h *LabelHandler) Create(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromCtx(r.Context())
	if claims == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	var body struct {
		Name        string `json:"name"`
		Color       string `json:"color"`
		Description string `json:"description"`
	}
	if !decode(w, r, &body) {
		return
	}
	if body.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}

	label, err := h.svc.Create(r.Context(), claims, service.CreateLabelRequest{
		Name:        body.Name,
		Color:       body.Color,
		Description: body.Description,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, label)
}

// Get handles GET /labels/{labelID}.
func (h *LabelHandler) Get(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromCtx(r.Context())
	if claims == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	labelID, err := uuid.Parse(chi.URLParam(r, "labelID"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid label ID"})
		return
	}

	label, err := h.svc.Get(r.Context(), claims, labelID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, label)
}

// List handles GET /labels.
func (h *LabelHandler) List(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromCtx(r.Context())
	if claims == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	labels, err := h.svc.List(r.Context(), claims)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"labels": labels})
}

// Update handles PATCH /labels/{labelID}.
func (h *LabelHandler) Update(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromCtx(r.Context())
	if claims == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	labelID, err := uuid.Parse(chi.URLParam(r, "labelID"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid label ID"})
		return
	}

	var body struct {
		Name        *string `json:"name"`
		Color       *string `json:"color"`
		Description *string `json:"description"`
	}
	if !decode(w, r, &body) {
		return
	}

	label, err := h.svc.Update(r.Context(), claims, labelID, service.UpdateLabelRequest{
		Name:        body.Name,
		Color:       body.Color,
		Description: body.Description,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, label)
}

// Delete handles DELETE /labels/{labelID}.
func (h *LabelHandler) Delete(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromCtx(r.Context())
	if claims == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	labelID, err := uuid.Parse(chi.URLParam(r, "labelID"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid label ID"})
		return
	}

	if err := h.svc.Delete(r.Context(), claims, labelID); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
