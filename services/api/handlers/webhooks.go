package handlers

import (
	"net/http"

	"agentmail/services/api/clients"
)

// WebhookHandler proxies webhook CRUD to the webhook service.
type WebhookHandler struct {
	webhook *clients.WebhookClient
}

// NewWebhookHandler creates a WebhookHandler.
func NewWebhookHandler(webhook *clients.WebhookClient) *WebhookHandler {
	return &WebhookHandler{webhook: webhook}
}

// ListWebhooks proxies GET /v1/webhooks.
func (h *WebhookHandler) ListWebhooks(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromCtx(r)
	data, status, err := h.webhook.ListWebhooks(r.Context(), r.URL.RawQuery, claims)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, errResp(err.Error()))
		return
	}
	writeProxied(w, status, data)
}

// CreateWebhook proxies POST /v1/webhooks.
func (h *WebhookHandler) CreateWebhook(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromCtx(r)
	var body map[string]any
	if err := decode(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, errResp("invalid request body"))
		return
	}
	data, status, err := h.webhook.CreateWebhook(r.Context(), body, claims)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, errResp(err.Error()))
		return
	}
	writeProxied(w, status, data)
}

// GetWebhook proxies GET /v1/webhooks/{webhookID}.
func (h *WebhookHandler) GetWebhook(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromCtx(r)
	webhookID, err := pathUUID(r, "webhookID")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errResp("invalid webhook ID"))
		return
	}
	data, status, err := h.webhook.GetWebhook(r.Context(), webhookID, claims)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, errResp(err.Error()))
		return
	}
	writeProxied(w, status, data)
}

// UpdateWebhook proxies PATCH /v1/webhooks/{webhookID}.
func (h *WebhookHandler) UpdateWebhook(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromCtx(r)
	webhookID, err := pathUUID(r, "webhookID")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errResp("invalid webhook ID"))
		return
	}
	var body map[string]any
	if err := decode(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, errResp("invalid request body"))
		return
	}
	data, status, err := h.webhook.UpdateWebhook(r.Context(), webhookID, body, claims)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, errResp(err.Error()))
		return
	}
	writeProxied(w, status, data)
}

// DeleteWebhook proxies DELETE /v1/webhooks/{webhookID}.
func (h *WebhookHandler) DeleteWebhook(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromCtx(r)
	webhookID, err := pathUUID(r, "webhookID")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errResp("invalid webhook ID"))
		return
	}
	data, status, err := h.webhook.DeleteWebhook(r.Context(), webhookID, claims)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, errResp(err.Error()))
		return
	}
	writeProxied(w, status, data)
}

// ListDeliveries proxies GET /v1/webhooks/{webhookID}/deliveries.
func (h *WebhookHandler) ListDeliveries(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromCtx(r)
	webhookID, err := pathUUID(r, "webhookID")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errResp("invalid webhook ID"))
		return
	}
	data, status, err := h.webhook.ListDeliveries(r.Context(), webhookID, r.URL.RawQuery, claims)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, errResp(err.Error()))
		return
	}
	writeProxied(w, status, data)
}
