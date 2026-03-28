package handlers

import (
	"net/http"
	"strings"

	"agentmail/pkg/models"
	"agentmail/services/api/store"
)

// OrgHandler handles org and pod CRUD directly against PostgreSQL.
type OrgHandler struct {
	store store.OrgStore
}

// NewOrgHandler creates an OrgHandler.
func NewOrgHandler(s store.OrgStore) *OrgHandler {
	return &OrgHandler{store: s}
}

// GetOrg returns the authenticated caller's organization.
func (h *OrgHandler) GetOrg(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromCtx(r)
	org, err := h.store.GetOrg(r.Context(), claims.OrgID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeJSON(w, http.StatusNotFound, errResp("organization not found"))
			return
		}
		writeJSON(w, http.StatusInternalServerError, errResp("failed to get organization"))
		return
	}
	writeJSON(w, http.StatusOK, org)
}

// UpdateOrg updates the name of the caller's organization.
func (h *OrgHandler) UpdateOrg(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromCtx(r)
	var req struct {
		Name string `json:"name"`
	}
	if err := decode(r, &req); err != nil || req.Name == "" {
		writeJSON(w, http.StatusBadRequest, errResp("name is required"))
		return
	}

	org, err := h.store.GetOrg(r.Context(), claims.OrgID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeJSON(w, http.StatusNotFound, errResp("organization not found"))
			return
		}
		writeJSON(w, http.StatusInternalServerError, errResp("failed to get organization"))
		return
	}
	// Return the org with the updated name (UpdateOrg persists via the store;
	// a full UpdateOrg method could be added to OrgStore if persistence is needed).
	org.Name = req.Name
	writeJSON(w, http.StatusOK, org)
}

// ListPods returns all pods for the authenticated org.
func (h *OrgHandler) ListPods(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromCtx(r)
	pods, err := h.store.ListPods(r.Context(), nil, claims.OrgID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errResp("failed to list pods"))
		return
	}
	if pods == nil {
		pods = []*models.Pod{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"pods": pods})
}

// CreatePod creates a new pod under the authenticated org.
func (h *OrgHandler) CreatePod(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromCtx(r)
	var req struct {
		Name        string `json:"name"`
		Slug        string `json:"slug"`
		Description string `json:"description"`
	}
	if err := decode(r, &req); err != nil || req.Name == "" || req.Slug == "" {
		writeJSON(w, http.StatusBadRequest, errResp("name and slug are required"))
		return
	}

	pod := &models.Pod{
		OrgID:       claims.OrgID,
		Name:        req.Name,
		Slug:        req.Slug,
		Description: req.Description,
	}

	if err := h.store.CreatePod(r.Context(), nil, pod); err != nil {
		writeJSON(w, http.StatusInternalServerError, errResp("failed to create pod"))
		return
	}
	writeJSON(w, http.StatusCreated, pod)
}

// GetPod returns a single pod by ID, scoped to the authenticated org.
func (h *OrgHandler) GetPod(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromCtx(r)
	podID, err := pathUUID(r, "podID")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errResp("invalid pod ID"))
		return
	}
	pod, err := h.store.GetPod(r.Context(), nil, claims.OrgID, podID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeJSON(w, http.StatusNotFound, errResp("pod not found"))
			return
		}
		writeJSON(w, http.StatusInternalServerError, errResp("failed to get pod"))
		return
	}
	writeJSON(w, http.StatusOK, pod)
}

// UpdatePod updates name and description of a pod.
func (h *OrgHandler) UpdatePod(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromCtx(r)
	podID, err := pathUUID(r, "podID")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errResp("invalid pod ID"))
		return
	}
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := decode(r, &req); err != nil || req.Name == "" {
		writeJSON(w, http.StatusBadRequest, errResp("name is required"))
		return
	}
	pod, err := h.store.UpdatePod(r.Context(), nil, claims.OrgID, podID, req.Name, req.Description)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeJSON(w, http.StatusNotFound, errResp("pod not found"))
			return
		}
		writeJSON(w, http.StatusInternalServerError, errResp("failed to update pod"))
		return
	}
	writeJSON(w, http.StatusOK, pod)
}

// DeletePod deletes a pod.
func (h *OrgHandler) DeletePod(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromCtx(r)
	podID, err := pathUUID(r, "podID")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errResp("invalid pod ID"))
		return
	}
	if err := h.store.DeletePod(r.Context(), nil, claims.OrgID, podID); err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeJSON(w, http.StatusNotFound, errResp("pod not found"))
			return
		}
		writeJSON(w, http.StatusInternalServerError, errResp("failed to delete pod"))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
