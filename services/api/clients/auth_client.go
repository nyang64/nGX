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
	"net/http"
	"time"

	authpkg "agentmail/pkg/auth"
)

// AuthClient calls the auth service to validate API keys.
type AuthClient struct {
	baseURL string
	http    *http.Client
}

// NewAuthClient creates a new AuthClient pointed at baseURL.
func NewAuthClient(baseURL string) *AuthClient {
	return &AuthClient{
		baseURL: baseURL,
		http:    &http.Client{Timeout: 5 * time.Second},
	}
}

// BaseURL returns the base URL of the auth service.
func (c *AuthClient) BaseURL() string { return c.baseURL }

// ValidateKey calls the auth service to validate key and returns the Claims.
func (c *AuthClient) ValidateKey(ctx context.Context, key string) (*authpkg.Claims, error) {
	body, _ := json.Marshal(map[string]string{"key": key})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/validate", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("auth service request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("invalid key")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("auth service error: %d", resp.StatusCode)
	}

	var claims authpkg.Claims
	if err := json.NewDecoder(resp.Body).Decode(&claims); err != nil {
		return nil, fmt.Errorf("decode claims: %w", err)
	}
	return &claims, nil
}
