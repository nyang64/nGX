package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"agentmail/services/api/clients"
)

// APIKeyHandler proxies API key management to the auth service.
type APIKeyHandler struct {
	proxy *authProxy
}

// authProxy is a thin HTTP client for auth service endpoints beyond /validate.
type authProxy struct {
	baseURL string
	http    *http.Client
}

func newAuthProxy(authClient *clients.AuthClient) *authProxy {
	return &authProxy{
		baseURL: authClient.BaseURL(),
		http:    &http.Client{Timeout: 10 * time.Second},
	}
}

func (p *authProxy) do(ctx context.Context, method, path string, body any, orgID string) ([]byte, int, error) {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, 0, err
		}
		bodyReader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, p.baseURL+path, bodyReader)
	if err != nil {
		return nil, 0, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Org-ID", orgID)

	resp, err := p.http.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("auth service request: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	return data, resp.StatusCode, err
}

// NewAPIKeyHandler creates an APIKeyHandler.
func NewAPIKeyHandler(authClient *clients.AuthClient) *APIKeyHandler {
	return &APIKeyHandler{
		proxy: newAuthProxy(authClient),
	}
}

// ListKeys proxies GET /v1/keys.
func (h *APIKeyHandler) ListKeys(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromCtx(r)
	data, status, err := h.proxy.do(r.Context(), http.MethodGet, "/keys", nil, claims.OrgID.String())
	if err != nil {
		writeJSON(w, http.StatusBadGateway, errResp(err.Error()))
		return
	}
	writeProxied(w, status, data)
}

// CreateKey proxies POST /v1/keys.
func (h *APIKeyHandler) CreateKey(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromCtx(r)
	var body map[string]any
	if err := decode(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, errResp("invalid request body"))
		return
	}
	data, status, err := h.proxy.do(r.Context(), http.MethodPost, "/keys", body, claims.OrgID.String())
	if err != nil {
		writeJSON(w, http.StatusBadGateway, errResp(err.Error()))
		return
	}
	writeProxied(w, status, data)
}

// GetKey proxies GET /v1/keys/{keyID}.
func (h *APIKeyHandler) GetKey(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromCtx(r)
	keyID, err := pathUUID(r, "keyID")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errResp("invalid key ID"))
		return
	}
	data, status, err := h.proxy.do(r.Context(), http.MethodGet, "/keys/"+keyID.String(), nil, claims.OrgID.String())
	if err != nil {
		writeJSON(w, http.StatusBadGateway, errResp(err.Error()))
		return
	}
	writeProxied(w, status, data)
}

// RevokeKey proxies DELETE /v1/keys/{keyID}.
func (h *APIKeyHandler) RevokeKey(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromCtx(r)
	keyID, err := pathUUID(r, "keyID")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errResp("invalid key ID"))
		return
	}
	data, status, err := h.proxy.do(r.Context(), http.MethodDelete, "/keys/"+keyID.String(), nil, claims.OrgID.String())
	if err != nil {
		writeJSON(w, http.StatusBadGateway, errResp(err.Error()))
		return
	}
	writeProxied(w, status, data)
}
