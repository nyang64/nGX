/*
 * Copyright (c) 2026 nyklabs.com. All rights reserved.
 *
 * Licensed under the nGX Commercial Source License v1.0.
 * See LICENSE file in the project root for full license information.
 */

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

// InboxClient proxies requests to the inbox service.
type InboxClient struct {
	baseURL string
	http    *http.Client
}

// NewInboxClient creates a new InboxClient pointed at baseURL.
func NewInboxClient(baseURL string) *InboxClient {
	return &InboxClient{
		baseURL: baseURL,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

// proxy forwards a request to the inbox service with auth headers set from claims.
func (c *InboxClient) proxy(ctx context.Context, method, path string, body any, claims *authpkg.Claims) ([]byte, int, error) {
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
		return nil, 0, fmt.Errorf("inbox service request: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("read response: %w", err)
	}
	return data, resp.StatusCode, nil
}

// ------- Inbox methods -------

func (c *InboxClient) CreateInbox(ctx context.Context, body any, claims *authpkg.Claims) ([]byte, int, error) {
	return c.proxy(ctx, http.MethodPost, "/inboxes", body, claims)
}

func (c *InboxClient) GetInbox(ctx context.Context, inboxID uuid.UUID, claims *authpkg.Claims) ([]byte, int, error) {
	return c.proxy(ctx, http.MethodGet, "/inboxes/"+inboxID.String(), nil, claims)
}

func (c *InboxClient) ListInboxes(ctx context.Context, query string, claims *authpkg.Claims) ([]byte, int, error) {
	path := "/inboxes"
	if query != "" {
		path += "?" + query
	}
	return c.proxy(ctx, http.MethodGet, path, nil, claims)
}

func (c *InboxClient) UpdateInbox(ctx context.Context, inboxID uuid.UUID, body any, claims *authpkg.Claims) ([]byte, int, error) {
	return c.proxy(ctx, http.MethodPatch, "/inboxes/"+inboxID.String(), body, claims)
}

func (c *InboxClient) DeleteInbox(ctx context.Context, inboxID uuid.UUID, claims *authpkg.Claims) ([]byte, int, error) {
	return c.proxy(ctx, http.MethodDelete, "/inboxes/"+inboxID.String(), nil, claims)
}

// ------- Thread methods -------

func (c *InboxClient) ListThreads(ctx context.Context, inboxID uuid.UUID, query string, claims *authpkg.Claims) ([]byte, int, error) {
	path := "/inboxes/" + inboxID.String() + "/threads"
	if query != "" {
		path += "?" + query
	}
	return c.proxy(ctx, http.MethodGet, path, nil, claims)
}

func (c *InboxClient) GetThread(ctx context.Context, inboxID, threadID uuid.UUID, claims *authpkg.Claims) ([]byte, int, error) {
	return c.proxy(ctx, http.MethodGet, "/inboxes/"+inboxID.String()+"/threads/"+threadID.String(), nil, claims)
}

func (c *InboxClient) UpdateThread(ctx context.Context, inboxID, threadID uuid.UUID, body any, claims *authpkg.Claims) ([]byte, int, error) {
	return c.proxy(ctx, http.MethodPatch, "/inboxes/"+inboxID.String()+"/threads/"+threadID.String(), body, claims)
}

func (c *InboxClient) ApplyLabel(ctx context.Context, inboxID, threadID, labelID uuid.UUID, claims *authpkg.Claims) ([]byte, int, error) {
	return c.proxy(ctx, http.MethodPut, "/inboxes/"+inboxID.String()+"/threads/"+threadID.String()+"/labels/"+labelID.String(), nil, claims)
}

func (c *InboxClient) RemoveLabel(ctx context.Context, inboxID, threadID, labelID uuid.UUID, claims *authpkg.Claims) ([]byte, int, error) {
	return c.proxy(ctx, http.MethodDelete, "/inboxes/"+inboxID.String()+"/threads/"+threadID.String()+"/labels/"+labelID.String(), nil, claims)
}

// ------- Message methods -------

func (c *InboxClient) ListMessages(ctx context.Context, inboxID, threadID uuid.UUID, query string, claims *authpkg.Claims) ([]byte, int, error) {
	path := "/inboxes/" + inboxID.String() + "/threads/" + threadID.String() + "/messages"
	if query != "" {
		path += "?" + query
	}
	return c.proxy(ctx, http.MethodGet, path, nil, claims)
}

func (c *InboxClient) GetMessage(ctx context.Context, inboxID, threadID, messageID uuid.UUID, claims *authpkg.Claims) ([]byte, int, error) {
	return c.proxy(ctx, http.MethodGet, "/inboxes/"+inboxID.String()+"/threads/"+threadID.String()+"/messages/"+messageID.String(), nil, claims)
}

func (c *InboxClient) SendMessage(ctx context.Context, inboxID uuid.UUID, body any, claims *authpkg.Claims) ([]byte, int, error) {
	return c.proxy(ctx, http.MethodPost, "/inboxes/"+inboxID.String()+"/messages/send", body, claims)
}

// ------- Draft methods -------

func (c *InboxClient) CreateDraft(ctx context.Context, inboxID uuid.UUID, body any, claims *authpkg.Claims) ([]byte, int, error) {
	return c.proxy(ctx, http.MethodPost, "/inboxes/"+inboxID.String()+"/drafts", body, claims)
}

func (c *InboxClient) GetDraft(ctx context.Context, inboxID, draftID uuid.UUID, claims *authpkg.Claims) ([]byte, int, error) {
	return c.proxy(ctx, http.MethodGet, "/inboxes/"+inboxID.String()+"/drafts/"+draftID.String(), nil, claims)
}

func (c *InboxClient) ListDrafts(ctx context.Context, inboxID uuid.UUID, query string, claims *authpkg.Claims) ([]byte, int, error) {
	path := "/inboxes/" + inboxID.String() + "/drafts"
	if query != "" {
		path += "?" + query
	}
	return c.proxy(ctx, http.MethodGet, path, nil, claims)
}

func (c *InboxClient) UpdateDraft(ctx context.Context, inboxID, draftID uuid.UUID, body any, claims *authpkg.Claims) ([]byte, int, error) {
	return c.proxy(ctx, http.MethodPatch, "/inboxes/"+inboxID.String()+"/drafts/"+draftID.String(), body, claims)
}

func (c *InboxClient) ApproveDraft(ctx context.Context, inboxID, draftID uuid.UUID, body any, claims *authpkg.Claims) ([]byte, int, error) {
	return c.proxy(ctx, http.MethodPost, "/inboxes/"+inboxID.String()+"/drafts/"+draftID.String()+"/approve", body, claims)
}

func (c *InboxClient) RejectDraft(ctx context.Context, inboxID, draftID uuid.UUID, body any, claims *authpkg.Claims) ([]byte, int, error) {
	return c.proxy(ctx, http.MethodPost, "/inboxes/"+inboxID.String()+"/drafts/"+draftID.String()+"/reject", body, claims)
}

func (c *InboxClient) DeleteDraft(ctx context.Context, inboxID, draftID uuid.UUID, claims *authpkg.Claims) ([]byte, int, error) {
	return c.proxy(ctx, http.MethodDelete, "/inboxes/"+inboxID.String()+"/drafts/"+draftID.String(), nil, claims)
}

// ------- Label methods -------

func (c *InboxClient) CreateLabel(ctx context.Context, body any, claims *authpkg.Claims) ([]byte, int, error) {
	return c.proxy(ctx, http.MethodPost, "/labels", body, claims)
}

func (c *InboxClient) ListLabels(ctx context.Context, query string, claims *authpkg.Claims) ([]byte, int, error) {
	path := "/labels"
	if query != "" {
		path += "?" + query
	}
	return c.proxy(ctx, http.MethodGet, path, nil, claims)
}

func (c *InboxClient) UpdateLabel(ctx context.Context, labelID uuid.UUID, body any, claims *authpkg.Claims) ([]byte, int, error) {
	return c.proxy(ctx, http.MethodPatch, "/labels/"+labelID.String(), body, claims)
}

func (c *InboxClient) DeleteLabel(ctx context.Context, labelID uuid.UUID, claims *authpkg.Claims) ([]byte, int, error) {
	return c.proxy(ctx, http.MethodDelete, "/labels/"+labelID.String(), nil, claims)
}
