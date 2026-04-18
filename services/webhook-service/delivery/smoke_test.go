/*
 * Copyright (c) 2026 nyklabs.com. All rights reserved.
 *
 * Licensed under the nGX Commercial Source License v1.0.
 * See LICENSE file in the project root for full license information.
 */

//go:build smoke

package delivery

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"agentmail/pkg/models"
)

// ---------------------------------------------------------------------------
// Smoke tests for webhook delivery — end-to-end invocation scenarios.
// ---------------------------------------------------------------------------

// messageReceivedPayload is a realistic event payload matching what nGX
// publishes when an agent's inbox receives a new email.
type messageReceivedPayload struct {
	Type      string `json:"type"`
	MessageID string `json:"message_id"`
	InboxID   string `json:"inbox_id"`
	OrgID     string `json:"org_id"`
	From      string `json:"from"`
	Subject   string `json:"subject"`
	Snippet   string `json:"snippet"`
}

// TestSmoke_Webhook_RealisticEventDelivery simulates a full webhook invocation
// for an agent inbox receiving an email. Verifies:
//   - All nGX headers are present and correct
//   - Payload arrives verbatim
//   - Receiver can authenticate the request via HMAC
func TestSmoke_Webhook_RealisticEventDelivery(t *testing.T) {
	secret := "agent-webhook-secret-abc123"

	event := messageReceivedPayload{
		Type:      "message.received",
		MessageID: "msg-550e8400-e29b-41d4-a716-446655440000",
		InboxID:   "inbox-6ba7b810-9dad-11d1-80b4-00c04fd430c8",
		OrgID:     "org-6ba7b811-9dad-11d1-80b4-00c04fd430c8",
		From:      "customer@acme.com",
		Subject:   "Help with my order",
		Snippet:   "Hi, I need help with order #1234...",
	}
	payloadBytes, _ := json.Marshal(event)

	var (
		receivedBody    []byte
		receivedHeaders http.Header
		hmacVerified    bool
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedBody, _ = io.ReadAll(r.Body)
		receivedHeaders = r.Header.Clone()

		// Receiver verifies HMAC — the standard agent-side verification.
		sig := r.Header.Get("X-nGX-Signature")
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write(receivedBody)
		expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
		hmacVerified = sig == expected

		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	d := NewDeliverer()
	webhook := &models.Webhook{
		URL:    srv.URL + "/webhooks/nGX",
		Secret: secret,
	}
	result := d.Deliver(context.Background(), webhook, payloadBytes)

	if !result.Success {
		t.Fatalf("delivery failed: status=%d error=%q", result.StatusCode, result.Error)
	}

	// Verify payload arrived verbatim.
	if string(receivedBody) != string(payloadBytes) {
		t.Errorf("payload mismatch\n got: %s\nwant: %s", receivedBody, payloadBytes)
	}

	// Verify the event is parseable JSON at the receiver.
	var got messageReceivedPayload
	if err := json.Unmarshal(receivedBody, &got); err != nil {
		t.Fatalf("receiver could not parse payload: %v", err)
	}
	if got.Type != "message.received" {
		t.Errorf("event type: got %q, want %q", got.Type, "message.received")
	}
	if got.From != "customer@acme.com" {
		t.Errorf("from: got %q, want %q", got.From, "customer@acme.com")
	}

	// Verify all nGX headers.
	if receivedHeaders.Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type: got %q, want application/json", receivedHeaders.Get("Content-Type"))
	}
	if !strings.HasPrefix(receivedHeaders.Get("X-nGX-Signature"), "sha256=") {
		t.Errorf("X-nGX-Signature: got %q, want sha256=... prefix", receivedHeaders.Get("X-nGX-Signature"))
	}
	if receivedHeaders.Get("X-nGX-Event") != "webhook.delivery" {
		t.Errorf("X-nGX-Event: got %q, want webhook.delivery", receivedHeaders.Get("X-nGX-Event"))
	}
	if receivedHeaders.Get("User-Agent") != "nGX-Webhook/1.0" {
		t.Errorf("User-Agent: got %q, want nGX-Webhook/1.0", receivedHeaders.Get("User-Agent"))
	}

	// Verify HMAC verification succeeded from the receiver's perspective.
	if !hmacVerified {
		t.Errorf("receiver HMAC verification failed: signature=%q", receivedHeaders.Get("X-nGX-Signature"))
	}
}

// TestSmoke_Webhook_AgentAuthHeader verifies that when an agent registers a
// webhook with a custom auth header (e.g. Bearer token), it is forwarded in
// every delivery alongside the nGX signature.
func TestSmoke_Webhook_AgentAuthHeader(t *testing.T) {
	authName := "Authorization"
	authValue := "Bearer agent-token-xyz"

	var (
		gotAuth string
		gotSig  string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get(authName)
		gotSig = r.Header.Get("X-nGX-Signature")
		w.WriteHeader(http.StatusAccepted) // 202 is also a 2xx success
	}))
	defer srv.Close()

	d := NewDeliverer()
	webhook := &models.Webhook{
		URL:             srv.URL,
		Secret:          "sig-secret",
		AuthHeaderName:  &authName,
		AuthHeaderValue: authValue,
	}
	payload := []byte(`{"type":"message.received"}`)
	result := d.Deliver(context.Background(), webhook, payload)

	if !result.Success {
		t.Fatalf("expected success for 202 response, got: status=%d error=%q", result.StatusCode, result.Error)
	}
	if gotAuth != authValue {
		t.Errorf("Authorization header: got %q, want %q", gotAuth, authValue)
	}
	if !strings.HasPrefix(gotSig, "sha256=") {
		t.Errorf("X-nGX-Signature missing alongside auth header: got %q", gotSig)
	}
}

// TestSmoke_Webhook_NonSuccessResponse verifies that 4xx/5xx responses from
// the agent endpoint are correctly recorded as failures (not successes).
func TestSmoke_Webhook_NonSuccessResponse(t *testing.T) {
	cases := []struct {
		status int
		name   string
	}{
		{http.StatusBadRequest, "400 Bad Request"},
		{http.StatusUnauthorized, "401 Unauthorized"},
		{http.StatusTooManyRequests, "429 Too Many Requests"},
		{http.StatusInternalServerError, "500 Internal Server Error"},
		{http.StatusServiceUnavailable, "503 Service Unavailable"},
	}

	d := NewDeliverer()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				w.Write([]byte(`{"error":"not ready"}`))
			}))
			defer srv.Close()

			result := d.Deliver(context.Background(), &models.Webhook{
				URL:    srv.URL,
				Secret: "s",
			}, []byte(`{}`))

			if result.Success {
				t.Errorf("status %d should be a failure, got Success=true", tc.status)
			}
			if result.StatusCode != tc.status {
				t.Errorf("StatusCode: got %d, want %d", result.StatusCode, tc.status)
			}
		})
	}
}

// TestSmoke_Webhook_RetryThenSucceed simulates the retry pattern: the agent
// endpoint is temporarily unavailable then recovers. Verifies that Deliver
// returns a failure on the first call and success on the second, with the same
// HMAC signature on both — as the RetryScheduler would do when rescheduling.
func TestSmoke_Webhook_RetryThenSucceed(t *testing.T) {
	secret := "retry-secret"
	payload := []byte(`{"type":"message.received","attempt":"retry-test"}`)

	var callCount atomic.Int32
	var (
		firstSig  string
		secondSig string
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := callCount.Add(1)
		sig := r.Header.Get("X-nGX-Signature")
		if n == 1 {
			firstSig = sig
			w.WriteHeader(http.StatusServiceUnavailable) // transient failure
		} else {
			secondSig = sig
			w.WriteHeader(http.StatusOK) // recovered
		}
	}))
	defer srv.Close()

	d := NewDeliverer()
	webhook := &models.Webhook{URL: srv.URL, Secret: secret}

	// First attempt — simulates initial delivery failure.
	r1 := d.Deliver(context.Background(), webhook, payload)
	if r1.Success {
		t.Error("first attempt should fail (503)")
	}
	if r1.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("first attempt status: got %d, want 503", r1.StatusCode)
	}

	// Second attempt — simulates RetryScheduler re-attempting the same payload.
	r2 := d.Deliver(context.Background(), webhook, payload)
	if !r2.Success {
		t.Errorf("second attempt should succeed: status=%d error=%q", r2.StatusCode, r2.Error)
	}

	// Both attempts must carry the same signature (same payload + same secret).
	if firstSig != secondSig {
		t.Errorf("signature changed between attempts:\n  attempt 1: %s\n  attempt 2: %s", firstSig, secondSig)
	}
	if !strings.HasPrefix(firstSig, "sha256=") {
		t.Errorf("signature missing sha256= prefix: %q", firstSig)
	}

	if callCount.Load() != 2 {
		t.Errorf("expected exactly 2 HTTP calls, got %d", callCount.Load())
	}
}
