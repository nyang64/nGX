package handlers

import (
	"net/http"

	"agentmail/services/api/clients"
)

// DraftHandler proxies draft operations to the inbox service.
type DraftHandler struct {
	inbox *clients.InboxClient
}

// NewDraftHandler creates a DraftHandler.
func NewDraftHandler(inbox *clients.InboxClient) *DraftHandler {
	return &DraftHandler{inbox: inbox}
}

// ListDrafts proxies GET /v1/inboxes/{inboxID}/drafts.
func (h *DraftHandler) ListDrafts(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromCtx(r)
	inboxID, err := pathUUID(r, "inboxID")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errResp("invalid inbox ID"))
		return
	}
	data, status, err := h.inbox.ListDrafts(r.Context(), inboxID, r.URL.RawQuery, claims)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, errResp(err.Error()))
		return
	}
	writeProxied(w, status, data)
}

// CreateDraft proxies POST /v1/inboxes/{inboxID}/drafts.
func (h *DraftHandler) CreateDraft(w http.ResponseWriter, r *http.Request) {
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
	data, status, err := h.inbox.CreateDraft(r.Context(), inboxID, body, claims)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, errResp(err.Error()))
		return
	}
	writeProxied(w, status, data)
}

// GetDraft proxies GET /v1/inboxes/{inboxID}/drafts/{draftID}.
func (h *DraftHandler) GetDraft(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromCtx(r)
	inboxID, err := pathUUID(r, "inboxID")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errResp("invalid inbox ID"))
		return
	}
	draftID, err := pathUUID(r, "draftID")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errResp("invalid draft ID"))
		return
	}
	data, status, err := h.inbox.GetDraft(r.Context(), inboxID, draftID, claims)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, errResp(err.Error()))
		return
	}
	writeProxied(w, status, data)
}

// UpdateDraft proxies PATCH /v1/inboxes/{inboxID}/drafts/{draftID}.
func (h *DraftHandler) UpdateDraft(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromCtx(r)
	inboxID, err := pathUUID(r, "inboxID")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errResp("invalid inbox ID"))
		return
	}
	draftID, err := pathUUID(r, "draftID")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errResp("invalid draft ID"))
		return
	}
	var body map[string]any
	if err := decode(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, errResp("invalid request body"))
		return
	}
	data, status, err := h.inbox.UpdateDraft(r.Context(), inboxID, draftID, body, claims)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, errResp(err.Error()))
		return
	}
	writeProxied(w, status, data)
}

// DeleteDraft proxies DELETE /v1/inboxes/{inboxID}/drafts/{draftID}.
func (h *DraftHandler) DeleteDraft(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromCtx(r)
	inboxID, err := pathUUID(r, "inboxID")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errResp("invalid inbox ID"))
		return
	}
	draftID, err := pathUUID(r, "draftID")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errResp("invalid draft ID"))
		return
	}
	data, status, err := h.inbox.DeleteDraft(r.Context(), inboxID, draftID, claims)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, errResp(err.Error()))
		return
	}
	writeProxied(w, status, data)
}

// ApproveDraft proxies POST /v1/inboxes/{inboxID}/drafts/{draftID}/approve.
func (h *DraftHandler) ApproveDraft(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromCtx(r)
	inboxID, err := pathUUID(r, "inboxID")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errResp("invalid inbox ID"))
		return
	}
	draftID, err := pathUUID(r, "draftID")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errResp("invalid draft ID"))
		return
	}
	var body map[string]any
	// body is optional for approve
	_ = decode(r, &body)
	data, status, err := h.inbox.ApproveDraft(r.Context(), inboxID, draftID, body, claims)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, errResp(err.Error()))
		return
	}
	writeProxied(w, status, data)
}

// RejectDraft proxies POST /v1/inboxes/{inboxID}/drafts/{draftID}/reject.
func (h *DraftHandler) RejectDraft(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromCtx(r)
	inboxID, err := pathUUID(r, "inboxID")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errResp("invalid inbox ID"))
		return
	}
	draftID, err := pathUUID(r, "draftID")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errResp("invalid draft ID"))
		return
	}
	var body map[string]any
	_ = decode(r, &body)
	data, status, err := h.inbox.RejectDraft(r.Context(), inboxID, draftID, body, claims)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, errResp(err.Error()))
		return
	}
	writeProxied(w, status, data)
}
