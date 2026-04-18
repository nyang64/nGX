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

// MessageHandler proxies message operations to the inbox service.
type MessageHandler struct {
	inbox *clients.InboxClient
}

// NewMessageHandler creates a MessageHandler.
func NewMessageHandler(inbox *clients.InboxClient) *MessageHandler {
	return &MessageHandler{inbox: inbox}
}

// ListMessages proxies GET /v1/inboxes/{inboxID}/threads/{threadID}/messages.
func (h *MessageHandler) ListMessages(w http.ResponseWriter, r *http.Request) {
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
	data, status, err := h.inbox.ListMessages(r.Context(), inboxID, threadID, r.URL.RawQuery, claims)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, errResp(err.Error()))
		return
	}
	writeProxied(w, status, data)
}

// GetMessage proxies GET /v1/inboxes/{inboxID}/threads/{threadID}/messages/{messageID}.
func (h *MessageHandler) GetMessage(w http.ResponseWriter, r *http.Request) {
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
	messageID, err := pathUUID(r, "messageID")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errResp("invalid message ID"))
		return
	}
	data, status, err := h.inbox.GetMessage(r.Context(), inboxID, threadID, messageID, claims)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, errResp(err.Error()))
		return
	}
	writeProxied(w, status, data)
}

// SendMessage proxies POST /v1/inboxes/{inboxID}/messages/send.
func (h *MessageHandler) SendMessage(w http.ResponseWriter, r *http.Request) {
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
	data, status, err := h.inbox.SendMessage(r.Context(), inboxID, body, claims)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, errResp(err.Error()))
		return
	}
	writeProxied(w, status, data)
}
