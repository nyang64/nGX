/*
 * Copyright (c) 2026 nyklabs.com. All rights reserved.
 *
 * Licensed under the nGX Commercial Source License v1.0.
 * See LICENSE file in the project root for full license information.
 */

package delivery

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"agentmail/pkg/models"
)

// --- computeSignature tests ---

func TestComputeSignature_Known(t *testing.T) {
	secret := "secret"
	payload := []byte("payload")

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	expected := hex.EncodeToString(mac.Sum(nil))

	got := computeSignature(secret, payload)
	if got != expected {
		t.Errorf("want %q, got %q", expected, got)
	}
}

func TestComputeSignature_EmptyPayload(t *testing.T) {
	got := computeSignature("key", []byte{})
	if len(got) != 64 {
		t.Errorf("expected 64-char hex string, got len %d: %q", len(got), got)
	}
	// Verify it's valid hex
	if _, err := hex.DecodeString(got); err != nil {
		t.Errorf("result is not valid hex: %v", err)
	}
}

func TestComputeSignature_DifferentSecrets(t *testing.T) {
	payload := []byte("same payload")
	sig1 := computeSignature("secret-one", payload)
	sig2 := computeSignature("secret-two", payload)
	if sig1 == sig2 {
		t.Errorf("expected different signatures for different secrets, both got %q", sig1)
	}
}

// --- nextBackoff tests ---

func TestNextBackoff(t *testing.T) {
	cases := []struct {
		attempt  int
		expected time.Duration
	}{
		{0, 1 * time.Second},
		{1, 2 * time.Second},
		{2, 4 * time.Second},
		{10, 1024 * time.Second},
		{12, 4096 * time.Second},
		{13, 4096 * time.Second}, // capped at attempt=12
		{100, 4096 * time.Second},
	}

	for _, tc := range cases {
		got := nextBackoff(tc.attempt)
		if got != tc.expected {
			t.Errorf("nextBackoff(%d): want %v, got %v", tc.attempt, tc.expected, got)
		}
	}
}

// --- Deliver tests ---

func TestDeliver_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	d := NewDeliverer()
	webhook := &models.Webhook{
		URL:    srv.URL,
		Secret: "test-secret",
	}
	result := d.Deliver(context.Background(), webhook, []byte(`{"event":"test"}`))

	if !result.Success {
		t.Errorf("expected Success=true, got false (StatusCode=%d, Error=%q)", result.StatusCode, result.Error)
	}
	if result.StatusCode != http.StatusOK {
		t.Errorf("expected StatusCode=200, got %d", result.StatusCode)
	}
}

func TestDeliver_FailureStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	d := NewDeliverer()
	webhook := &models.Webhook{
		URL:    srv.URL,
		Secret: "test-secret",
	}
	result := d.Deliver(context.Background(), webhook, []byte(`{"event":"test"}`))

	if result.Success {
		t.Error("expected Success=false for 500 response")
	}
	if result.StatusCode != http.StatusInternalServerError {
		t.Errorf("expected StatusCode=500, got %d", result.StatusCode)
	}
}

func TestDeliver_SignatureHeader(t *testing.T) {
	secret := "my-webhook-secret"
	payload := []byte(`{"event":"test"}`)

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	expectedSig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	var gotSig string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSig = r.Header.Get("X-nGX-Signature")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	d := NewDeliverer()
	webhook := &models.Webhook{
		URL:    srv.URL,
		Secret: secret,
	}
	d.Deliver(context.Background(), webhook, payload)

	if gotSig != expectedSig {
		t.Errorf("X-nGX-Signature: want %q, got %q", expectedSig, gotSig)
	}
}

func TestDeliver_AuthHeader(t *testing.T) {
	authHeaderName := "X-API-Key"
	authHeaderValue := "token123"

	var gotAuthValue string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuthValue = r.Header.Get(authHeaderName)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	d := NewDeliverer()
	webhook := &models.Webhook{
		URL:             srv.URL,
		Secret:          "secret",
		AuthHeaderName:  &authHeaderName,
		AuthHeaderValue: authHeaderValue,
	}
	d.Deliver(context.Background(), webhook, []byte(`{}`))

	if gotAuthValue != authHeaderValue {
		t.Errorf("auth header %q: want %q, got %q", authHeaderName, authHeaderValue, gotAuthValue)
	}
}

func TestDeliver_NoAuthHeader(t *testing.T) {
	var gotAuthValue string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuthValue = r.Header.Get("X-API-Key")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	d := NewDeliverer()
	webhook := &models.Webhook{
		URL:    srv.URL,
		Secret: "secret",
		// AuthHeaderName is nil — no auth header should be sent
	}
	d.Deliver(context.Background(), webhook, []byte(`{}`))

	if !strings.EqualFold(gotAuthValue, "") {
		t.Errorf("expected X-API-Key header to be absent, got %q", gotAuthValue)
	}
}

func TestRetryScheduler_Run_CancelledContext(t *testing.T) {
	// Pre-cancelled context: Run should return immediately via ctx.Done().
	rs := NewRetryScheduler(nil, NewDeliverer(), nil, 3, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rs.Run(ctx) // must not block
}

func TestDeliver_InvalidURL(t *testing.T) {
	d := NewDeliverer()
	webhook := &models.Webhook{
		URL:    "http://\x00invalid-url",
		Secret: "secret",
	}
	result := d.Deliver(context.Background(), webhook, []byte(`{}`))
	if result.Success {
		t.Error("expected failure for invalid URL")
	}
	if result.Error == "" {
		t.Error("expected non-empty error string")
	}
}

func TestDeliver_ConnectionRefused(t *testing.T) {
	d := NewDeliverer()
	webhook := &models.Webhook{
		// Port 1 is almost certainly unused and will refuse the connection.
		URL:    "http://127.0.0.1:1/webhook",
		Secret: "secret",
	}
	result := d.Deliver(context.Background(), webhook, []byte(`{}`))
	if result.Success {
		t.Error("expected failure when connection refused")
	}
	if result.Error == "" {
		t.Error("expected non-empty error string")
	}
}

// TestDeliver_AllnGXHeaders verifies that every required nGX header is sent
// on each delivery: Content-Type, X-nGX-Signature, X-nGX-Event, User-Agent.
func TestDeliver_AllnGXHeaders(t *testing.T) {
	var (
		gotContentType string
		gotSig         string
		gotEvent       string
		gotUserAgent   string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		gotSig = r.Header.Get("X-nGX-Signature")
		gotEvent = r.Header.Get("X-nGX-Event")
		gotUserAgent = r.Header.Get("User-Agent")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	d := NewDeliverer()
	webhook := &models.Webhook{URL: srv.URL, Secret: "s"}
	result := d.Deliver(context.Background(), webhook, []byte(`{"event":"test"}`))
	if !result.Success {
		t.Fatalf("expected Success=true, got error: %s", result.Error)
	}

	if gotContentType != "application/json" {
		t.Errorf("Content-Type: got %q, want %q", gotContentType, "application/json")
	}
	if !strings.HasPrefix(gotSig, "sha256=") {
		t.Errorf("X-nGX-Signature: got %q, want sha256=... prefix", gotSig)
	}
	if gotEvent != "webhook.delivery" {
		t.Errorf("X-nGX-Event: got %q, want %q", gotEvent, "webhook.delivery")
	}
	if gotUserAgent != "nGX-Webhook/1.0" {
		t.Errorf("User-Agent: got %q, want %q", gotUserAgent, "nGX-Webhook/1.0")
	}
}

// TestDeliver_PayloadArrivesVerbatim verifies that the payload bytes are
// delivered to the receiver unchanged.
func TestDeliver_PayloadArrivesVerbatim(t *testing.T) {
	payload := []byte(`{"type":"message.received","inbox":"agent@example.com","subject":"Hello"}`)

	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	d := NewDeliverer()
	webhook := &models.Webhook{URL: srv.URL, Secret: "secret"}
	d.Deliver(context.Background(), webhook, payload)

	if string(gotBody) != string(payload) {
		t.Errorf("payload mismatch\n got: %s\nwant: %s", gotBody, payload)
	}
}

// TestDeliver_ReceiverCanVerifyHMAC verifies the receiver-side workflow:
// extract X-nGX-Signature, recompute HMAC over the raw body, confirm they match.
// This is how an agent endpoint should authenticate webhook calls.
func TestDeliver_ReceiverCanVerifyHMAC(t *testing.T) {
	secret := "agent-webhook-secret"
	payload := []byte(`{"type":"message.received","message_id":"abc-123"}`)

	var verified bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		sig := r.Header.Get("X-nGX-Signature")

		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write(body)
		expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
		verified = sig == expected
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	d := NewDeliverer()
	webhook := &models.Webhook{URL: srv.URL, Secret: secret}
	result := d.Deliver(context.Background(), webhook, payload)

	if !result.Success {
		t.Fatalf("delivery failed: %s", result.Error)
	}
	if !verified {
		t.Error("receiver could not verify HMAC: X-nGX-Signature did not match")
	}
}

// TestDeliver_ResponseBodyCaptured verifies that the receiver's response body
// is captured in DeliveryResult (up to 1024 bytes) for diagnostics.
func TestDeliver_ResponseBodyCaptured(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"received"}`))
	}))
	defer srv.Close()

	d := NewDeliverer()
	webhook := &models.Webhook{URL: srv.URL, Secret: "s"}
	result := d.Deliver(context.Background(), webhook, []byte(`{}`))

	if !strings.Contains(result.ResponseBody, "received") {
		t.Errorf("response body not captured; got: %q", result.ResponseBody)
	}
}
