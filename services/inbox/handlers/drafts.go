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

// DraftHandler holds dependencies for draft HTTP handlers.
type DraftHandler struct {
	svc *service.DraftService
}

// NewDraftHandler creates a new DraftHandler.
func NewDraftHandler(svc *service.DraftService) *DraftHandler {
	return &DraftHandler{svc: svc}
}

// Create handles POST /inboxes/{inboxID}/drafts.
func (h *DraftHandler) Create(w http.ResponseWriter, r *http.Request) {
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
		ThreadID  *uuid.UUID            `json:"thread_id"`
		Subject   string                `json:"subject"`
		To        []models.EmailAddress `json:"to"`
		Cc        []models.EmailAddress `json:"cc"`
		Bcc       []models.EmailAddress `json:"bcc"`
		TextBody  string                `json:"text_body"`
		HtmlBody  string                `json:"html_body"`
		InReplyTo string                `json:"in_reply_to"`
		Metadata  map[string]any        `json:"metadata"`
	}
	if !decode(w, r, &body) {
		return
	}

	draft, err := h.svc.Create(r.Context(), claims, service.CreateDraftRequest{
		InboxID:   inboxID,
		ThreadID:  body.ThreadID,
		Subject:   body.Subject,
		To:        body.To,
		Cc:        body.Cc,
		Bcc:       body.Bcc,
		TextBody:  body.TextBody,
		HtmlBody:  body.HtmlBody,
		InReplyTo: body.InReplyTo,
		Metadata:  body.Metadata,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, draft)
}

// Get handles GET /drafts/{draftID}.
func (h *DraftHandler) Get(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromCtx(r.Context())
	if claims == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	draftID, err := uuid.Parse(chi.URLParam(r, "draftID"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid draft ID"})
		return
	}

	draft, err := h.svc.Get(r.Context(), claims, draftID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, draft)
}

// List handles GET /inboxes/{inboxID}/drafts.
func (h *DraftHandler) List(w http.ResponseWriter, r *http.Request) {
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

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	cursor := r.URL.Query().Get("cursor")

	drafts, nextCursor, err := h.svc.List(r.Context(), claims, inboxID, limit, cursor)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"drafts":      drafts,
		"next_cursor": nextCursor,
	})
}

// Update handles PATCH /drafts/{draftID}.
func (h *DraftHandler) Update(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromCtx(r.Context())
	if claims == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	draftID, err := uuid.Parse(chi.URLParam(r, "draftID"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid draft ID"})
		return
	}

	var body struct {
		Subject  *string               `json:"subject"`
		To       []models.EmailAddress `json:"to"`
		Cc       []models.EmailAddress `json:"cc"`
		Bcc      []models.EmailAddress `json:"bcc"`
		TextBody *string               `json:"text_body"`
		HtmlBody *string               `json:"html_body"`
		Metadata map[string]any        `json:"metadata"`
	}
	if !decode(w, r, &body) {
		return
	}

	draft, err := h.svc.Update(r.Context(), claims, draftID, service.UpdateDraftRequest{
		Subject:  body.Subject,
		To:       body.To,
		Cc:       body.Cc,
		Bcc:      body.Bcc,
		TextBody: body.TextBody,
		HtmlBody: body.HtmlBody,
		Metadata: body.Metadata,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, draft)
}

// Delete handles DELETE /drafts/{draftID}.
func (h *DraftHandler) Delete(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromCtx(r.Context())
	if claims == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	draftID, err := uuid.Parse(chi.URLParam(r, "draftID"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid draft ID"})
		return
	}

	if err := h.svc.Delete(r.Context(), claims, draftID); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Approve handles POST /drafts/{draftID}/approve.
func (h *DraftHandler) Approve(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromCtx(r.Context())
	if claims == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	draftID, err := uuid.Parse(chi.URLParam(r, "draftID"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid draft ID"})
		return
	}

	var body struct {
		Note string `json:"note"`
	}
	if r.ContentLength > 0 {
		_ = decode(w, r, &body) // note is optional
	}

	draft, err := h.svc.Approve(r.Context(), claims, draftID, body.Note)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, draft)
}

// Reject handles POST /drafts/{draftID}/reject.
func (h *DraftHandler) Reject(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromCtx(r.Context())
	if claims == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	draftID, err := uuid.Parse(chi.URLParam(r, "draftID"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid draft ID"})
		return
	}

	var body struct {
		Reason string `json:"reason"`
	}
	if !decode(w, r, &body) {
		return
	}

	draft, err := h.svc.Reject(r.Context(), claims, draftID, body.Reason)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, draft)
}
