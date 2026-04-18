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
	"agentmail/pkg/models"
	"agentmail/services/inbox/service"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// MessageHandler holds dependencies for message HTTP handlers.
type MessageHandler struct {
	svc *service.MessageService
}

// NewMessageHandler creates a new MessageHandler.
func NewMessageHandler(svc *service.MessageService) *MessageHandler {
	return &MessageHandler{svc: svc}
}

// List handles GET /threads/{threadID}/messages.
func (h *MessageHandler) List(w http.ResponseWriter, r *http.Request) {
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

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	cursor := r.URL.Query().Get("cursor")

	msgs, nextCursor, err := h.svc.List(r.Context(), claims, threadID, limit, cursor)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"messages":    msgs,
		"next_cursor": nextCursor,
	})
}

// Get handles GET /messages/{messageID}.
func (h *MessageHandler) Get(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromCtx(r.Context())
	if claims == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	messageID, err := uuid.Parse(chi.URLParam(r, "messageID"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid message ID"})
		return
	}

	msg, err := h.svc.Get(r.Context(), claims, messageID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, msg)
}

// Send handles POST /inboxes/{inboxID}/messages.
func (h *MessageHandler) Send(w http.ResponseWriter, r *http.Request) {
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
		To        []models.EmailAddress `json:"to"`
		CC        []models.EmailAddress `json:"cc"`
		BCC       []models.EmailAddress `json:"bcc"`
		Subject   string                `json:"subject"`
		BodyText  string                `json:"body_text"`
		BodyHTML  string                `json:"body_html"`
		ReplyToID *uuid.UUID            `json:"reply_to_id"`
		Metadata  map[string]any        `json:"metadata"`
	}
	if !decode(w, r, &body) {
		return
	}
	if len(body.To) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "at least one recipient required"})
		return
	}

	msg, err := h.svc.Send(r.Context(), claims, inboxID, service.SendMessageRequest{
		To:        body.To,
		CC:        body.CC,
		BCC:       body.BCC,
		Subject:   body.Subject,
		BodyText:  body.BodyText,
		BodyHTML:  body.BodyHTML,
		ReplyToID: body.ReplyToID,
		Metadata:  body.Metadata,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, msg)
}
