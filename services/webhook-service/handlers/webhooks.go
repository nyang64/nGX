package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"agentmail/pkg/crypto"
	"agentmail/pkg/events"
	"agentmail/pkg/models"
	"agentmail/services/webhook-service/store"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// validEventTypes is the set of event type strings a webhook may subscribe to.
var validEventTypes = func() map[string]bool {
	valid := map[string]bool{}
	for _, t := range []events.EventType{
		events.EventMessageReceived,
		events.EventMessageSent,
		events.EventMessageBounced,
		events.EventThreadCreated,
		events.EventThreadStatusChanged,
		events.EventDraftCreated,
		events.EventDraftApproved,
		events.EventDraftRejected,
		events.EventInboxCreated,
		events.EventLabelApplied,
	} {
		valid[string(t)] = true
	}
	return valid
}()

// validateEvents returns an error if any event string is not a known EventType.
func validateEvents(evts []string) error {
	for _, e := range evts {
		if !validEventTypes[e] {
			return fmt.Errorf("unknown event type %q", e)
		}
	}
	return nil
}

// WebhookHandler handles webhook CRUD HTTP requests.
type WebhookHandler struct {
	store  *store.DeliveryStore
	encKey []byte // may be nil if encryption is not configured
}

// NewWebhookHandler creates a WebhookHandler.
func NewWebhookHandler(s *store.DeliveryStore, encKey []byte) *WebhookHandler {
	return &WebhookHandler{store: s, encKey: encKey}
}

// webhookResponse is the public representation of a webhook.
// Secret is only populated on creation (one-time reveal).
type webhookResponse struct {
	ID            uuid.UUID           `json:"id"`
	OrgID         uuid.UUID           `json:"org_id"`
	URL           string              `json:"url"`
	Secret        string              `json:"secret,omitempty"`
	Events        []string            `json:"events"`
	PodID         *uuid.UUID          `json:"pod_id"`
	InboxID       *uuid.UUID          `json:"inbox_id"`
	IsActive      bool                `json:"is_active"`
	FailureCount  int                 `json:"failure_count"`
	LastSuccessAt interface{}         `json:"last_success_at"`
	LastFailureAt interface{}         `json:"last_failure_at"`
	AuthHeader    *authHeaderResponse `json:"auth_header,omitempty"`
	CreatedAt     interface{}         `json:"created_at"`
	UpdatedAt     interface{}         `json:"updated_at"`
}

type authHeaderResponse struct {
	Name string `json:"name"`
}

func toWebhookResponse(wh *models.Webhook, includeSecret bool) webhookResponse {
	r := webhookResponse{
		ID:            wh.ID,
		OrgID:         wh.OrgID,
		URL:           wh.URL,
		Events:        wh.Events,
		PodID:         wh.PodID,
		InboxID:       wh.InboxID,
		IsActive:      wh.IsActive,
		FailureCount:  wh.FailureCount,
		LastSuccessAt: wh.LastSuccessAt,
		LastFailureAt: wh.LastFailureAt,
		CreatedAt:     wh.CreatedAt,
		UpdatedAt:     wh.UpdatedAt,
	}
	if includeSecret {
		r.Secret = wh.Secret
	}
	if wh.AuthHeaderName != nil {
		r.AuthHeader = &authHeaderResponse{Name: *wh.AuthHeaderName}
	}
	return r
}

// orgIDFromRequest extracts the X-Org-ID header and parses it as a UUID.
func orgIDFromRequest(r *http.Request) (uuid.UUID, error) {
	raw := r.Header.Get("X-Org-ID")
	if raw == "" {
		return uuid.UUID{}, errors.New("missing X-Org-ID header")
	}
	return uuid.Parse(raw)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("write JSON response", "error", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// webhookIDFromPath extracts the webhook ID from the URL path.
// Expects paths like /webhooks/{id} or /webhooks/{id}/deliveries.
func webhookIDFromPath(path string) (uuid.UUID, error) {
	// Strip leading slash and split.
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	// parts[0] = "webhooks", parts[1] = "{id}", parts[2] (optional) = "deliveries"
	if len(parts) < 2 {
		return uuid.UUID{}, errors.New("missing webhook id in path")
	}
	return uuid.Parse(parts[1])
}

// createWebhookRequest is the request body for POST /webhooks.
type createWebhookRequest struct {
	URL     string     `json:"url"`
	Events  []string   `json:"events"`
	PodID   *uuid.UUID `json:"pod_id"`
	InboxID *uuid.UUID `json:"inbox_id"`
	AuthHeader *struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	} `json:"auth_header"`
}

// updateWebhookRequest is the request body for PATCH /webhooks/{id}.
// AuthHeader uses json.RawMessage to distinguish absent vs explicit null.
type updateWebhookRequest struct {
	URL        *string         `json:"url"`
	Events     []string        `json:"events"`
	IsActive   *bool           `json:"is_active"`
	AuthHeader json.RawMessage `json:"auth_header"`
}

// ListWebhooks handles GET /webhooks.
func (h *WebhookHandler) ListWebhooks(w http.ResponseWriter, r *http.Request) {
	orgID, err := orgIDFromRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	webhooks, err := h.store.ListWebhooks(r.Context(), orgID)
	if err != nil {
		slog.Error("list webhooks", "org_id", orgID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list webhooks")
		return
	}

	resp := make([]webhookResponse, 0, len(webhooks))
	for _, wh := range webhooks {
		resp = append(resp, toWebhookResponse(wh, false))
	}
	writeJSON(w, http.StatusOK, resp)
}

// CreateWebhook handles POST /webhooks.
func (h *WebhookHandler) CreateWebhook(w http.ResponseWriter, r *http.Request) {
	orgID, err := orgIDFromRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	var req createWebhookRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.URL == "" {
		writeError(w, http.StatusBadRequest, "url is required")
		return
	}
	if len(req.Events) == 0 {
		writeError(w, http.StatusBadRequest, "events is required")
		return
	}
	if err := validateEvents(req.Events); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Generate a random HMAC secret (32 bytes, hex-encoded = 64 chars).
	secret, err := generateSecret()
	if err != nil {
		slog.Error("generate webhook secret", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to generate secret")
		return
	}

	wh := &models.Webhook{
		ID:       uuid.New(),
		OrgID:    orgID,
		URL:      req.URL,
		Secret:   secret,
		Events:   req.Events,
		PodID:    req.PodID,
		InboxID:  req.InboxID,
		IsActive: true,
	}

	// Handle caller-supplied auth header.
	if req.AuthHeader != nil && req.AuthHeader.Name != "" && req.AuthHeader.Value != "" {
		if len(h.encKey) == 0 {
			slog.Warn("auth_header provided but WEBHOOK_ENCRYPTION_KEY is not set; ignoring auth_header")
		} else {
			enc, encErr := crypto.Encrypt(h.encKey, []byte(req.AuthHeader.Value))
			if encErr != nil {
				slog.Error("encrypt webhook auth header", "error", encErr)
				writeError(w, http.StatusInternalServerError, "failed to encrypt auth header")
				return
			}
			name := req.AuthHeader.Name
			wh.AuthHeaderName = &name
			wh.AuthHeaderValueEnc = enc
		}
	}

	if err := h.store.CreateWebhook(r.Context(), wh); err != nil {
		slog.Error("create webhook", "org_id", orgID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create webhook")
		return
	}

	writeJSON(w, http.StatusCreated, toWebhookResponse(wh, true))
}

// GetWebhook handles GET /webhooks/{id}.
func (h *WebhookHandler) GetWebhook(w http.ResponseWriter, r *http.Request) {
	orgID, err := orgIDFromRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	id, err := webhookIDFromPath(r.URL.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid webhook id")
		return
	}

	wh, err := h.store.GetWebhookByIDAndOrg(r.Context(), id, orgID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "webhook not found")
			return
		}
		slog.Error("get webhook", "id", id, "org_id", orgID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to get webhook")
		return
	}

	writeJSON(w, http.StatusOK, toWebhookResponse(wh, false))
}

// UpdateWebhook handles PATCH /webhooks/{id}.
func (h *WebhookHandler) UpdateWebhook(w http.ResponseWriter, r *http.Request) {
	orgID, err := orgIDFromRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	id, err := webhookIDFromPath(r.URL.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid webhook id")
		return
	}

	// Fetch existing webhook first.
	wh, err := h.store.GetWebhookByIDAndOrg(r.Context(), id, orgID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "webhook not found")
			return
		}
		slog.Error("get webhook for update", "id", id, "org_id", orgID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to get webhook")
		return
	}

	var req updateWebhookRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Apply partial updates.
	if req.URL != nil {
		wh.URL = *req.URL
	}
	if req.Events != nil {
		if err := validateEvents(req.Events); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		wh.Events = req.Events
	}
	if req.IsActive != nil {
		wh.IsActive = *req.IsActive
	}

	// Handle auth_header field:
	//   - absent from JSON: leave existing value unchanged (req.AuthHeader is nil)
	//   - explicit null:    clear both columns
	//   - object with name+value: re-encrypt and update
	if len(req.AuthHeader) > 0 {
		raw := strings.TrimSpace(string(req.AuthHeader))
		if raw == "null" {
			// Explicit null: clear auth header.
			wh.AuthHeaderName = nil
			wh.AuthHeaderValueEnc = nil
		} else {
			var ah struct {
				Name  string `json:"name"`
				Value string `json:"value"`
			}
			if err := json.Unmarshal(req.AuthHeader, &ah); err != nil {
				writeError(w, http.StatusBadRequest, "invalid auth_header")
				return
			}
			if ah.Name != "" && ah.Value != "" {
				if len(h.encKey) == 0 {
					slog.Warn("auth_header provided but WEBHOOK_ENCRYPTION_KEY is not set; ignoring auth_header update")
				} else {
					enc, encErr := crypto.Encrypt(h.encKey, []byte(ah.Value))
					if encErr != nil {
						slog.Error("encrypt webhook auth header", "error", encErr)
						writeError(w, http.StatusInternalServerError, "failed to encrypt auth header")
						return
					}
					name := ah.Name
					wh.AuthHeaderName = &name
					wh.AuthHeaderValueEnc = enc
				}
			}
		}
	}

	if err := h.store.UpdateWebhook(r.Context(), wh); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "webhook not found")
			return
		}
		slog.Error("update webhook", "id", id, "org_id", orgID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update webhook")
		return
	}

	writeJSON(w, http.StatusOK, toWebhookResponse(wh, false))
}

// DeleteWebhook handles DELETE /webhooks/{id}.
func (h *WebhookHandler) DeleteWebhook(w http.ResponseWriter, r *http.Request) {
	orgID, err := orgIDFromRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	id, err := webhookIDFromPath(r.URL.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid webhook id")
		return
	}

	if err := h.store.DeleteWebhook(r.Context(), id, orgID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "webhook not found")
			return
		}
		slog.Error("delete webhook", "id", id, "org_id", orgID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to delete webhook")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ListDeliveries handles GET /webhooks/{id}/deliveries.
func (h *WebhookHandler) ListDeliveries(w http.ResponseWriter, r *http.Request) {
	orgID, err := orgIDFromRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	id, err := webhookIDFromPath(r.URL.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid webhook id")
		return
	}

	deliveries, err := h.store.ListDeliveries(r.Context(), id, orgID)
	if err != nil {
		slog.Error("list deliveries", "webhook_id", id, "org_id", orgID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list deliveries")
		return
	}

	if deliveries == nil {
		deliveries = []*models.WebhookDelivery{}
	}
	writeJSON(w, http.StatusOK, deliveries)
}

// generateSecret generates a random 32-byte HMAC secret as a hex string.
func generateSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
