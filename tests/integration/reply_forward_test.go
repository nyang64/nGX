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

// ── nGX-76w: POST /messages/{id}/reply-all ───────────────────────────────────

// TestReplyAll verifies POST /inboxes/{id}/threads/{id}/messages/{id}/reply-all:
//   - Creates a new message in the same thread
//   - The new message has In-Reply-To matching the original's message_id
//   - Requires inbox:write scope; inbox:read only gets 403
func TestReplyAll(t *testing.T) {
	c := newClient(t)

	// Create inbox and send original message.
	code, body, err := c.post("/v1/inboxes", map[string]any{"address": uniqueName("reply-all")})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	inboxID := mustStr(t, body, "id")
	t.Cleanup(func() { c.delete("/v1/inboxes/" + inboxID) }) //nolint

	code, body, err = c.post(fmt.Sprintf("/v1/inboxes/%s/messages/send", inboxID), map[string]any{
		"to":        []map[string]any{{"email": "success@simulator.amazonses.com", "name": "SES Success"}},
		"cc":        []map[string]any{{"email": "bounce@simulator.amazonses.com", "name": "SES Bounce"}},
		"subject":   "reply-all test " + uniqueName("s"),
		"body_text": "original body",
	})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	origID := mustStr(t, body, "id")
	origThreadID := mustStr(t, body, "thread_id")
	origMessageID := mustStr(t, body, "message_id") // RFC 5322 header value

	replyAllURL := fmt.Sprintf("/v1/inboxes/%s/threads/%s/messages/%s/reply-all", inboxID, origThreadID, origID)

	t.Run("reply_all_creates_message_in_same_thread", func(t *testing.T) {
		code, body, err := c.post(replyAllURL, map[string]any{
			"body_text": "reply-all body",
		})
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 201, body)

		// Must be in the same thread.
		if gotThread := mustStr(t, body, "thread_id"); gotThread != origThreadID {
			t.Fatalf("expected thread_id=%s, got %s", origThreadID, gotThread)
		}

		// In-Reply-To must match original message_id.
		if inReplyTo := str(body, "in_reply_to"); inReplyTo != origMessageID {
			t.Fatalf("expected in_reply_to=%s, got %q", origMessageID, inReplyTo)
		}
	})

	t.Run("reply_all_requires_inbox_write_scope", func(t *testing.T) {
		readOnly := scopedClient(t, c, []string{"inbox:read"})
		code, _, err := readOnly.post(replyAllURL, map[string]any{"body_text": "x"})
		if err != nil {
			t.Fatal(err)
		}
		if code != 403 {
			t.Fatalf("expected 403 for inbox:read-only key, got %d", code)
		}
	})

	t.Run("reply_all_nonexistent_message_returns_404_or_400", func(t *testing.T) {
		badURL := fmt.Sprintf("/v1/inboxes/%s/threads/%s/messages/00000000-0000-0000-0000-000000000000/reply-all", inboxID, origThreadID)
		code, _, err := c.post(badURL, map[string]any{"body_text": "x"})
		if err != nil {
			t.Fatal(err)
		}
		if code != 404 && code != 400 {
			t.Fatalf("expected 404 or 400 for nonexistent message, got %d", code)
		}
	})
}

// ── nGX-owm: POST /messages/{id}/forward ─────────────────────────────────────

// TestForward verifies POST /inboxes/{id}/threads/{id}/messages/{id}/forward:
//   - Creates a message in a NEW thread (different thread_id from original)
//   - Subject is prefixed with "Fwd: " if not already present
//   - Requires inbox:write scope; inbox:read only gets 403
func TestForward(t *testing.T) {
	c := newClient(t)

	// Create inbox and send original message.
	code, body, err := c.post("/v1/inboxes", map[string]any{"address": uniqueName("forward")})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	inboxID := mustStr(t, body, "id")
	t.Cleanup(func() { c.delete("/v1/inboxes/" + inboxID) }) //nolint

	code, body, err = c.post(fmt.Sprintf("/v1/inboxes/%s/messages/send", inboxID), map[string]any{
		"to":        []map[string]any{{"email": "success@simulator.amazonses.com"}},
		"subject":   "forward test " + uniqueName("s"),
		"body_text": "original body",
	})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	origID := mustStr(t, body, "id")
	origThreadID := mustStr(t, body, "thread_id")
	origSubject := mustStr(t, body, "subject")

	fwdURL := fmt.Sprintf("/v1/inboxes/%s/threads/%s/messages/%s/forward", inboxID, origThreadID, origID)

	t.Run("forward_creates_message_in_new_thread", func(t *testing.T) {
		code, body, err := c.post(fwdURL, map[string]any{
			"to":        []map[string]any{{"email": "success@simulator.amazonses.com"}},
			"body_text": "forwarding this along",
		})
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 201, body)

		// Must be in a DIFFERENT thread.
		newThreadID := mustStr(t, body, "thread_id")
		if newThreadID == origThreadID {
			t.Fatalf("forward must create a new thread, but got the same thread_id %s", origThreadID)
		}

		// Subject must have "Fwd: " prefix.
		gotSubject := mustStr(t, body, "subject")
		if gotSubject != "Fwd: "+origSubject {
			t.Fatalf("expected subject %q, got %q", "Fwd: "+origSubject, gotSubject)
		}
	})

	t.Run("forward_requires_inbox_write_scope", func(t *testing.T) {
		readOnly := scopedClient(t, c, []string{"inbox:read"})
		code, _, err := readOnly.post(fwdURL, map[string]any{
			"to":        []map[string]any{{"email": "success@simulator.amazonses.com"}},
			"body_text": "x",
		})
		if err != nil {
			t.Fatal(err)
		}
		if code != 403 {
			t.Fatalf("expected 403 for inbox:read-only key, got %d", code)
		}
	})

	t.Run("forward_missing_to_returns_400", func(t *testing.T) {
		code, _, err := c.post(fwdURL, map[string]any{"body_text": "x"})
		if err != nil {
			t.Fatal(err)
		}
		if code != 400 {
			t.Fatalf("expected 400 when to is missing, got %d", code)
		}
	})

	t.Run("forward_nonexistent_message_returns_400_or_404", func(t *testing.T) {
		badURL := fmt.Sprintf("/v1/inboxes/%s/threads/%s/messages/00000000-0000-0000-0000-000000000000/forward", inboxID, origThreadID)
		code, _, err := c.post(badURL, map[string]any{
			"to":        []map[string]any{{"email": "success@simulator.amazonses.com"}},
			"body_text": "x",
		})
		if err != nil {
			t.Fatal(err)
		}
		if code != 404 && code != 400 {
			t.Fatalf("expected 404 or 400 for nonexistent message, got %d", code)
		}
	})
}
