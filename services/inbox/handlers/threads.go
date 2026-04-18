/*
 * Copyright (c) 2026 nyklabs.com. All rights reserved.
 *
 * Licensed under the nGX Commercial Source License v1.0.
 * See LICENSE file in the project root for full license information.
 */

package handlers

import (
	"net/http"
	"strconv"

	"agentmail/pkg/auth"
	"agentmail/services/inbox/service"
	"agentmail/services/inbox/store"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// ThreadHandler holds dependencies for thread HTTP handlers.
type ThreadHandler struct {
	svc *service.ThreadService
}

// NewThreadHandler creates a new ThreadHandler.
func NewThreadHandler(svc *service.ThreadService) *ThreadHandler {
	return &ThreadHandler{svc: svc}
}

// List handles GET /inboxes/{inboxID}/threads.
func (h *ThreadHandler) List(w http.ResponseWriter, r *http.Request) {
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

	q := store.ThreadListQuery{
		InboxID: inboxID,
		Cursor:  r.URL.Query().Get("cursor"),
	}
	q.Limit, _ = strconv.Atoi(r.URL.Query().Get("limit"))

	if status := r.URL.Query().Get("status"); status != "" {
		q.Status = &status
	}
	if labelIDStr := r.URL.Query().Get("label_id"); labelIDStr != "" {
		id, err := uuid.Parse(labelIDStr)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid label_id"})
			return
		}
		q.LabelID = &id
	}
	if isReadStr := r.URL.Query().Get("is_read"); isReadStr != "" {
		val := isReadStr == "true"
		q.IsRead = &val
	}

	threads, nextCursor, err := h.svc.List(r.Context(), claims, q)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"threads":     threads,
		"next_cursor": nextCursor,
	})
}

// Get handles GET /threads/{threadID}.
func (h *ThreadHandler) Get(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromCtx(r.Context())
	if claims == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	threadID, err := uuid.Parse(chi.URLParam(r, "threadID"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid thread ID"})
		return
	}

	thread, err := h.svc.Get(r.Context(), claims, threadID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, thread)
}

// Update handles PATCH /threads/{threadID}.
func (h *ThreadHandler) Update(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromCtx(r.Context())
	if claims == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	threadID, err := uuid.Parse(chi.URLParam(r, "threadID"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid thread ID"})
		return
	}

	var body struct {
		Status    *string `json:"status"`
		IsRead    *bool   `json:"is_read"`
		IsStarred *bool   `json:"is_starred"`
	}
	if !decode(w, r, &body) {
		return
	}

	var thread interface{}
	var updateErr error

	if body.Status != nil {
		thread, updateErr = h.svc.UpdateStatus(r.Context(), claims, threadID, *body.Status)
	} else if body.IsRead != nil {
		thread, updateErr = h.svc.MarkRead(r.Context(), claims, threadID, *body.IsRead)
	} else if body.IsStarred != nil {
		thread, updateErr = h.svc.MarkStarred(r.Context(), claims, threadID, *body.IsStarred)
	} else {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no updatable fields provided"})
		return
	}

	if updateErr != nil {
		writeError(w, updateErr)
		return
	}
	writeJSON(w, http.StatusOK, thread)
}

// ApplyLabel handles POST /threads/{threadID}/labels/{labelID}.
func (h *ThreadHandler) ApplyLabel(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromCtx(r.Context())
	if claims == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	threadID, err := uuid.Parse(chi.URLParam(r, "threadID"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid thread ID"})
		return
	}
	labelID, err := uuid.Parse(chi.URLParam(r, "labelID"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid label ID"})
		return
	}

	if err := h.svc.ApplyLabel(r.Context(), claims, threadID, labelID); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// RemoveLabel handles DELETE /threads/{threadID}/labels/{labelID}.
func (h *ThreadHandler) RemoveLabel(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromCtx(r.Context())
	if claims == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	threadID, err := uuid.Parse(chi.URLParam(r, "threadID"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid thread ID"})
		return
	}
	labelID, err := uuid.Parse(chi.URLParam(r, "labelID"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid label ID"})
		return
	}

	if err := h.svc.RemoveLabel(r.Context(), claims, threadID, labelID); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
