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

// InboxHandler proxies inbox CRUD to the inbox service.
type InboxHandler struct {
	inbox *clients.InboxClient
}

// NewInboxHandler creates an InboxHandler.
func NewInboxHandler(inbox *clients.InboxClient) *InboxHandler {
	return &InboxHandler{inbox: inbox}
}

// CreateInbox proxies POST /v1/inboxes.
func (h *InboxHandler) CreateInbox(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromCtx(r)
	var body map[string]any
	if err := decode(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, errResp("invalid request body"))
		return
	}
	data, status, err := h.inbox.CreateInbox(r.Context(), body, claims)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, errResp(err.Error()))
		return
	}
	writeProxied(w, status, data)
}

// GetInbox proxies GET /v1/inboxes/{inboxID}.
func (h *InboxHandler) GetInbox(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromCtx(r)
	inboxID, err := pathUUID(r, "inboxID")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errResp("invalid inbox ID"))
		return
	}
	data, status, err := h.inbox.GetInbox(r.Context(), inboxID, claims)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, errResp(err.Error()))
		return
	}
	writeProxied(w, status, data)
}

// ListInboxes proxies GET /v1/inboxes.
func (h *InboxHandler) ListInboxes(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromCtx(r)
	data, status, err := h.inbox.ListInboxes(r.Context(), r.URL.RawQuery, claims)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, errResp(err.Error()))
		return
	}
	writeProxied(w, status, data)
}

// UpdateInbox proxies PATCH /v1/inboxes/{inboxID}.
func (h *InboxHandler) UpdateInbox(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromCtx(r)
	inboxID, err := pathUUID(r, "inboxID")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errResp("invalid inbox ID"))
		return
	}
	var body map[string]any
	if err := decode(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, errResp("invalid request body"))
		return
	}
	data, status, err := h.inbox.UpdateInbox(r.Context(), inboxID, body, claims)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, errResp(err.Error()))
		return
	}
	writeProxied(w, status, data)
}

// DeleteInbox proxies DELETE /v1/inboxes/{inboxID}.
func (h *InboxHandler) DeleteInbox(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromCtx(r)
	inboxID, err := pathUUID(r, "inboxID")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errResp("invalid inbox ID"))
		return
	}
	data, status, err := h.inbox.DeleteInbox(r.Context(), inboxID, claims)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, errResp(err.Error()))
		return
	}
	writeProxied(w, status, data)
}

// errResp is a small helper used across handlers.
func errResp(msg string) map[string]string { return map[string]string{"error": msg} }
