package clients

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	authpkg "agentmail/pkg/auth"

	"github.com/google/uuid"
)

// WebhookClient proxies requests to the webhook service.
type WebhookClient struct {
	baseURL string
	http    *http.Client
}

// NewWebhookClient creates a new WebhookClient pointed at baseURL.
func NewWebhookClient(baseURL string) *WebhookClient {
	return &WebhookClient{
		baseURL: baseURL,
		http:    &http.Client{Timeout: 15 * time.Second},
	}
}

// proxy forwards a request to the webhook service with auth headers set from claims.
func (c *WebhookClient) proxy(ctx context.Context, method, path string, body any, claims *authpkg.Claims) ([]byte, int, error) {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, 0, fmt.Errorf("marshal body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bodyReader)
	if err != nil {
		return nil, 0, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Org-ID", claims.OrgID.String())
	req.Header.Set("X-Key-ID", claims.KeyID.String())

	scopes := make([]string, len(claims.Scopes))
	for i, s := range claims.Scopes {
		scopes[i] = string(s)
	}
	req.Header.Set("X-Scopes", strings.Join(scopes, ","))

	if claims.PodID != nil {
		req.Header.Set("X-Pod-ID", claims.PodID.String())
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("webhook service request: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("read response: %w", err)
	}
	return data, resp.StatusCode, nil
}

func (c *WebhookClient) CreateWebhook(ctx context.Context, body any, claims *authpkg.Claims) ([]byte, int, error) {
	return c.proxy(ctx, http.MethodPost, "/webhooks", body, claims)
}

func (c *WebhookClient) ListWebhooks(ctx context.Context, query string, claims *authpkg.Claims) ([]byte, int, error) {
	path := "/webhooks"
	if query != "" {
		path += "?" + query
	}
	return c.proxy(ctx, http.MethodGet, path, nil, claims)
}

func (c *WebhookClient) GetWebhook(ctx context.Context, webhookID uuid.UUID, claims *authpkg.Claims) ([]byte, int, error) {
	return c.proxy(ctx, http.MethodGet, "/webhooks/"+webhookID.String(), nil, claims)
}

func (c *WebhookClient) UpdateWebhook(ctx context.Context, webhookID uuid.UUID, body any, claims *authpkg.Claims) ([]byte, int, error) {
	return c.proxy(ctx, http.MethodPatch, "/webhooks/"+webhookID.String(), body, claims)
}

func (c *WebhookClient) DeleteWebhook(ctx context.Context, webhookID uuid.UUID, claims *authpkg.Claims) ([]byte, int, error) {
	return c.proxy(ctx, http.MethodDelete, "/webhooks/"+webhookID.String(), nil, claims)
}

func (c *WebhookClient) ListDeliveries(ctx context.Context, webhookID uuid.UUID, query string, claims *authpkg.Claims) ([]byte, int, error) {
	path := "/webhooks/" + webhookID.String() + "/deliveries"
	if query != "" {
		path += "?" + query
	}
	return c.proxy(ctx, http.MethodGet, path, nil, claims)
}
