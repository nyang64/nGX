/*
 * Copyright (c) 2026 nyklabs.com. All rights reserved.
 *
 * Licensed under the nGX Commercial Source License v1.0.
 * See LICENSE file in the project root for full license information.
 */

// Package integration provides end-to-end tests against the deployed AWS stack.
// Run with:
//
//	TEST_API_KEY=am_live_... TEST_BASE_URL=https://... go test ./tests/integration/... -v -timeout 120s
package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"
)

// client is a thin HTTP wrapper around the nGX REST API.
type client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

func newClient(t *testing.T) *client {
	t.Helper()
	baseURL := os.Getenv("TEST_BASE_URL")
	apiKey := os.Getenv("TEST_API_KEY")
	if baseURL == "" || apiKey == "" {
		t.Skip("TEST_BASE_URL and TEST_API_KEY must be set")
	}
	return &client{
		baseURL: baseURL,
		apiKey:  apiKey,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (c *client) do(method, path string, body any) (int, map[string]any, error) {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.baseURL+path, r)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, err
	}
	var result map[string]any
	_ = json.Unmarshal(raw, &result)
	return resp.StatusCode, result, nil
}

func (c *client) get(path string) (int, map[string]any, error) {
	return c.do("GET", path, nil)
}

func (c *client) post(path string, body any) (int, map[string]any, error) {
	return c.do("POST", path, body)
}

func (c *client) patch(path string, body any) (int, map[string]any, error) {
	return c.do("PATCH", path, body)
}

func (c *client) delete(path string) (int, map[string]any, error) {
	return c.do("DELETE", path, nil)
}

// getBytes performs a GET and returns the raw response bytes and status code.
// Used for binary endpoints (raw message, attachment download). Sends
// Accept: application/octet-stream so API Gateway decodes the base64 body
// before forwarding to the client.
func (c *client) getBytes(path string) (int, []byte, http.Header, error) {
	req, err := http.NewRequest("GET", c.baseURL+path, nil)
	if err != nil {
		return 0, nil, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "application/octet-stream")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, nil, nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	return resp.StatusCode, data, resp.Header, err
}

// mustStr extracts a string field from a response map, failing the test if missing.
func mustStr(t *testing.T, m map[string]any, key string) string {
	t.Helper()
	v, ok := m[key]
	if !ok {
		t.Fatalf("response missing field %q: %v", key, m)
	}
	s, ok := v.(string)
	if !ok {
		t.Fatalf("field %q is not a string: %T %v", key, v, v)
	}
	return s
}

// mustCode asserts the HTTP status code, printing the body on failure.
func mustCode(t *testing.T, got, want int, body map[string]any) {
	t.Helper()
	if got != want {
		t.Fatalf("expected HTTP %d, got %d: %v", want, got, body)
	}
}

// pollUntil retries fn every interval until it returns true or timeout elapses.
func pollUntil(t *testing.T, timeout, interval time.Duration, fn func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return true
		}
		time.Sleep(interval)
	}
	return false
}

// str extracts a string field, returning "" if missing or wrong type.
func str(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, _ := m[key].(string)
	return v
}

// listOf extracts a slice from a response map key.
func listOf(m map[string]any, key string) []any {
	if m == nil {
		return nil
	}
	v, _ := m[key].([]any)
	return v
}

// asMap casts an any to map[string]any.
func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

// uniqueName returns a short unique name for test resources.
func uniqueName(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano()%1_000_000)
}
