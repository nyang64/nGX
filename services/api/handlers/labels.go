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

// LabelHandler proxies label CRUD to the inbox service.
type LabelHandler struct {
	inbox *clients.InboxClient
}

// NewLabelHandler creates a LabelHandler.
func NewLabelHandler(inbox *clients.InboxClient) *LabelHandler {
	return &LabelHandler{inbox: inbox}
}

// ListLabels proxies GET /v1/labels.
func (h *LabelHandler) ListLabels(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromCtx(r)
	data, status, err := h.inbox.ListLabels(r.Context(), r.URL.RawQuery, claims)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, errResp(err.Error()))
		return
	}
	writeProxied(w, status, data)
}

// CreateLabel proxies POST /v1/labels.
func (h *LabelHandler) CreateLabel(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromCtx(r)
	var body map[string]any
	if err := decode(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, errResp("invalid request body"))
		return
	}
	data, status, err := h.inbox.CreateLabel(r.Context(), body, claims)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, errResp(err.Error()))
		return
	}
	writeProxied(w, status, data)
}

// UpdateLabel proxies PATCH /v1/labels/{labelID}.
func (h *LabelHandler) UpdateLabel(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromCtx(r)
	labelID, err := pathUUID(r, "labelID")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errResp("invalid label ID"))
		return
	}
	var body map[string]any
	if err := decode(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, errResp("invalid request body"))
		return
	}
	data, status, err := h.inbox.UpdateLabel(r.Context(), labelID, body, claims)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, errResp(err.Error()))
		return
	}
	writeProxied(w, status, data)
}

// DeleteLabel proxies DELETE /v1/labels/{labelID}.
func (h *LabelHandler) DeleteLabel(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromCtx(r)
	labelID, err := pathUUID(r, "labelID")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errResp("invalid label ID"))
		return
	}
	data, status, err := h.inbox.DeleteLabel(r.Context(), labelID, claims)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, errResp(err.Error()))
		return
	}
	writeProxied(w, status, data)
}
