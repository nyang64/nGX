package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	authpkg "agentmail/pkg/auth"
)

// RemoteValidator calls the auth service to validate API keys.
type RemoteValidator struct {
	baseURL string
	http    *http.Client
}

// NewRemoteValidator creates a RemoteValidator pointed at baseURL.
func NewRemoteValidator(baseURL string) *RemoteValidator {
	return &RemoteValidator{
		baseURL: baseURL,
		http:    &http.Client{Timeout: 5 * time.Second},
	}
}

// ValidateKey calls POST /validate on the auth service and returns Claims.
func (v *RemoteValidator) ValidateKey(ctx context.Context, key string) (*authpkg.Claims, error) {
	body, _ := json.Marshal(map[string]string{"key": key})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, v.baseURL+"/validate", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build auth request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := v.http.Do(req)
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
