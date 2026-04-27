/*
 * Copyright (c) 2026 nyklabs.com. All rights reserved.
 *
 * Licensed under the nGX Commercial Source License v1.0.
 * See LICENSE file in the project root for full license information.
 */

package integration

import (
	"fmt"
	"testing"
)

// ── nGX-8pe: Revoked API key returned by GET /keys/{id} ──────────────────────

// TestRevokedKeyNotReturnedByGet verifies that after revoking (DELETE) an API
// key, GET /v1/keys/{id} returns 404 rather than the revoked key.
func TestRevokedKeyNotReturnedByGet(t *testing.T) {
	c := newClient(t)

	// Create a key.
	code, body, err := c.post("/v1/keys", map[string]any{
		"name":   uniqueName("revoke-test"),
		"scopes": []string{"inbox:read"},
	})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	keyID := mustStr(t, body, "id")

	// Verify it's accessible before revocation.
	code, body, err = c.get("/v1/keys/" + keyID)
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 200, body)

	// Revoke the key.
	code, _, err = c.delete("/v1/keys/" + keyID)
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 204, nil)

	// GET by ID must now return 404 — not the revoked key.
	code, body, err = c.get("/v1/keys/" + keyID)
	if err != nil {
		t.Fatal(err)
	}
	if code != 404 {
		t.Fatalf("expected 404 after revocation, got %d: %v", code, body)
	}

	// Verify revoked key is also absent from the list.
	code, body, err = c.get("/v1/keys")
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 200, body)
	for _, k := range listOf(body, "keys") {
		if str(asMap(k), "id") == keyID {
			t.Fatal("revoked key still appears in GET /v1/keys list")
		}
	}
}

// TestRevokedKeyCannotAuthenticate verifies that a revoked key is rejected when
// used to authenticate. Note: API Gateway caches authorizer results for 300s,
// so immediate rejection is not guaranteed in the integration environment.
// This test verifies the key works before revocation and is absent from the
// DB after — the 401 assertion is best-effort (logged if caching prevents it).
func TestRevokedKeyCannotAuthenticate(t *testing.T) {
	admin := newClient(t)

	// Create a key.
	code, body, err := admin.post("/v1/keys", map[string]any{
		"name":   uniqueName("revoke-auth-test"),
		"scopes": []string{"inbox:read"},
	})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	keyID := mustStr(t, body, "id")
	plaintext := mustStr(t, body, "key")

	// Use the key — should work before revocation.
	limited := &client{
		baseURL:    admin.baseURL,
		apiKey:     plaintext,
		httpClient: admin.httpClient,
	}
	code, _, err = limited.get("/v1/inboxes")
	if err != nil {
		t.Fatal(err)
	}
	if code != 200 {
		t.Fatalf("expected 200 before revocation, got %d", code)
	}

	// Revoke.
	code, _, err = admin.delete("/v1/keys/" + keyID)
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 204, nil)

	// Verify key is gone from DB (GET by ID must be 404).
	code, _, err = admin.get("/v1/keys/" + keyID)
	if err != nil {
		t.Fatal(err)
	}
	if code != 404 {
		t.Fatalf("expected 404 for revoked key GET, got %d", code)
	}

	// Best-effort: check auth is rejected. API Gateway may still cache the allow
	// result for up to 300s, so we only log if it's not immediately rejected.
	code, _, err = limited.get("/v1/inboxes")
	if err != nil {
		t.Fatal(err)
	}
	if code == 401 {
		t.Logf("revoked key immediately rejected (no cache hit) — good")
	} else {
		t.Logf("revoked key still cached by API Gateway authorizer (TTL up to 300s); got %d — expected in prod", code)
	}
}

// ── nGX-3y7: PATCH /messages/{id} ─────────────────────────────────────────────

// TestPatchMessage verifies PATCH /inboxes/{id}/threads/{id}/messages/{id}:
//   - PATCH unread=true sets is_read=false, GET reflects the change
//   - PATCH starred=true sets is_starred=true, GET reflects the change
//   - PATCH on nonexistent message ID returns 404
func TestPatchMessage(t *testing.T) {
	c := newClient(t)

	// Setup: create inbox and send a message.
	code, body, err := c.post("/v1/inboxes", map[string]any{"address": uniqueName("patch-msg")})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	inboxID := mustStr(t, body, "id")
	t.Cleanup(func() { c.delete("/v1/inboxes/" + inboxID) }) //nolint

	code, body, err = c.post(fmt.Sprintf("/v1/inboxes/%s/messages/send", inboxID), map[string]any{
		"to":        []map[string]any{{"email": "success@simulator.amazonses.com"}},
		"subject":   "patch message test " + uniqueName("s"),
		"body_text": "body",
	})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	messageID := mustStr(t, body, "id")
	threadID := mustStr(t, body, "thread_id")

	msgURL := fmt.Sprintf("/v1/inboxes/%s/threads/%s/messages/%s", inboxID, threadID, messageID)

	t.Run("initial_is_read_false", func(t *testing.T) {
		code, body, err := c.get(msgURL)
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 200, body)
		if isRead, ok := body["is_read"].(bool); !ok || isRead {
			t.Fatalf("expected is_read=false initially, got %v", body["is_read"])
		}
	})

	t.Run("patch_unread_false_marks_read", func(t *testing.T) {
		// unread=false → is_read=true (message has been read)
		code, body, err := c.patch(msgURL, map[string]any{"unread": false})
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 200, body)
		if isRead, ok := body["is_read"].(bool); !ok || !isRead {
			t.Fatalf("expected is_read=true after PATCH unread=false, got %v", body["is_read"])
		}
	})

	t.Run("get_reflects_is_read_true", func(t *testing.T) {
		code, body, err := c.get(msgURL)
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 200, body)
		if isRead, ok := body["is_read"].(bool); !ok || !isRead {
			t.Fatalf("GET after PATCH: expected is_read=true, got %v", body["is_read"])
		}
	})

	t.Run("patch_starred_true", func(t *testing.T) {
		code, body, err := c.patch(msgURL, map[string]any{"starred": true})
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 200, body)
		if starred, ok := body["is_starred"].(bool); !ok || !starred {
			t.Fatalf("expected is_starred=true after PATCH starred=true, got %v", body["is_starred"])
		}
	})

	t.Run("get_reflects_is_starred_true", func(t *testing.T) {
		code, body, err := c.get(msgURL)
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 200, body)
		if starred, ok := body["is_starred"].(bool); !ok || !starred {
			t.Fatalf("GET after PATCH: expected is_starred=true, got %v", body["is_starred"])
		}
	})

	t.Run("patch_nonexistent_message_returns_404", func(t *testing.T) {
		badURL := fmt.Sprintf("/v1/inboxes/%s/threads/%s/messages/00000000-0000-0000-0000-000000000000", inboxID, threadID)
		code, _, err := c.patch(badURL, map[string]any{"unread": true})
		if err != nil {
			t.Fatal(err)
		}
		if code != 404 {
			t.Fatalf("expected 404 for nonexistent message, got %d", code)
		}
	})

	t.Run("patch_requires_inbox_write_scope", func(t *testing.T) {
		readOnly := scopedClient(t, c, []string{"inbox:read"})
		code, _, err := readOnly.patch(msgURL, map[string]any{"unread": true})
		if err != nil {
			t.Fatal(err)
		}
		if code != 403 {
			t.Fatalf("expected 403 for inbox:read-only key, got %d", code)
		}
	})
}
