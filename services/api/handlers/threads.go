/*
 * Copyright (c) 2026 nyklabs.com. All rights reserved.
 *
 * Licensed under the nGX Commercial Source License v1.0.
 * See LICENSE file in the project root for full license information.
 */

package handlers

import (
	"net/http"

	"agentmail/services/api/clients"
)

// ThreadHandler proxies thread operations to the inbox service.
type ThreadHandler struct {
	inbox *clients.InboxClient
}

// NewThreadHandler creates a ThreadHandler.
func NewThreadHandler(inbox *clients.InboxClient) *ThreadHandler {
	return &ThreadHandler{inbox: inbox}
}

// ListThreads proxies GET /v1/inboxes/{inboxID}/threads.
func (h *ThreadHandler) ListThreads(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromCtx(r)
	inboxID, err := pathUUID(r, "inboxID")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errResp("invalid inbox ID"))
		return
	}
	data, status, err := h.inbox.ListThreads(r.Context(), inboxID, r.URL.RawQuery, claims)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, errResp(err.Error()))
		return
	}
	writeProxied(w, status, data)
}

// GetThread proxies GET /v1/inboxes/{inboxID}/threads/{threadID}.
func (h *ThreadHandler) GetThread(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromCtx(r)
	inboxID, err := pathUUID(r, "inboxID")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errResp("invalid inbox ID"))
		return
	}
	threadID, err := pathUUID(r, "threadID")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errResp("invalid thread ID"))
		return
	}
	data, status, err := h.inbox.GetThread(r.Context(), inboxID, threadID, claims)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, errResp(err.Error()))
		return
	}
	writeProxied(w, status, data)
}

// UpdateThread proxies PATCH /v1/inboxes/{inboxID}/threads/{threadID}.
func (h *ThreadHandler) UpdateThread(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromCtx(r)
	inboxID, err := pathUUID(r, "inboxID")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errResp("invalid inbox ID"))
		return
	}
	threadID, err := pathUUID(r, "threadID")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errResp("invalid thread ID"))
		return
	}
	var body map[string]any
	if err := decode(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, errResp("invalid request body"))
		return
	}
	data, status, err := h.inbox.UpdateThread(r.Context(), inboxID, threadID, body, claims)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, errResp(err.Error()))
		return
	}
	writeProxied(w, status, data)
}

// ApplyLabel proxies PUT /v1/inboxes/{inboxID}/threads/{threadID}/labels/{labelID}.
func (h *ThreadHandler) ApplyLabel(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromCtx(r)
	inboxID, err := pathUUID(r, "inboxID")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errResp("invalid inbox ID"))
		return
	}
	threadID, err := pathUUID(r, "threadID")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errResp("invalid thread ID"))
		return
	}
	labelID, err := pathUUID(r, "labelID")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errResp("invalid label ID"))
		return
	}
	data, status, err := h.inbox.ApplyLabel(r.Context(), inboxID, threadID, labelID, claims)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, errResp(err.Error()))
		return
	}
	writeProxied(w, status, data)
}

// RemoveLabel proxies DELETE /v1/inboxes/{inboxID}/threads/{threadID}/labels/{labelID}.
func (h *ThreadHandler) RemoveLabel(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromCtx(r)
	inboxID, err := pathUUID(r, "inboxID")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errResp("invalid inbox ID"))
		return
	}
	threadID, err := pathUUID(r, "threadID")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errResp("invalid thread ID"))
		return
	}
	labelID, err := pathUUID(r, "labelID")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errResp("invalid label ID"))
		return
	}
	data, status, err := h.inbox.RemoveLabel(r.Context(), inboxID, threadID, labelID, claims)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, errResp(err.Error()))
		return
	}
	writeProxied(w, status, data)
}
