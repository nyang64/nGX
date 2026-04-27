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

// scopedClient creates a new API key with the given scopes and returns a client
// using that key. The key is revoked at test cleanup.
func scopedClient(t *testing.T, admin *client, scopes []string) *client {
	t.Helper()
	code, body, err := admin.post("/v1/keys", map[string]any{
		"name":   uniqueName("scope-test"),
		"scopes": scopes,
	})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	keyID := mustStr(t, body, "id")
	plaintext := mustStr(t, body, "key")

	t.Cleanup(func() { admin.delete("/v1/keys/" + keyID) }) //nolint

	return &client{
		baseURL:    admin.baseURL,
		apiKey:     plaintext,
		httpClient: admin.httpClient,
	}
}

// ── nGX-670: Scope enforcement ────────────────────────────────────────────────

// TestScopeEnforcementInboxes verifies that inbox:read is required for GET
// and inbox:write is required for POST/PATCH/DELETE on /v1/inboxes.
func TestScopeEnforcementInboxes(t *testing.T) {
	admin := newClient(t)

	t.Run("read_scope_required_for_list", func(t *testing.T) {
		c := scopedClient(t, admin, []string{"webhook:read"})
		code, _, err := c.get("/v1/inboxes")
		if err != nil {
			t.Fatal(err)
		}
		if code != 403 {
			t.Fatalf("expected 403, got %d", code)
		}
	})

	t.Run("read_scope_allows_list", func(t *testing.T) {
		c := scopedClient(t, admin, []string{"inbox:read"})
		code, _, err := c.get("/v1/inboxes")
		if err != nil {
			t.Fatal(err)
		}
		if code != 200 {
			t.Fatalf("expected 200, got %d", code)
		}
	})

	t.Run("write_scope_required_for_create", func(t *testing.T) {
		c := scopedClient(t, admin, []string{"inbox:read"})
		code, _, err := c.post("/v1/inboxes", map[string]any{"address": uniqueName("scope-inbox")})
		if err != nil {
			t.Fatal(err)
		}
		if code != 403 {
			t.Fatalf("expected 403, got %d", code)
		}
	})
}

// TestScopeEnforcementThreads verifies scope enforcement on thread endpoints.
func TestScopeEnforcementThreads(t *testing.T) {
	admin := newClient(t)

	// Create an inbox + thread with the admin key.
	code, body, err := admin.post("/v1/inboxes", map[string]any{"address": uniqueName("scope-thr")})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	inboxID := mustStr(t, body, "id")
	t.Cleanup(func() { admin.delete("/v1/inboxes/" + inboxID) }) //nolint

	code, body, err = admin.post(fmt.Sprintf("/v1/inboxes/%s/messages/send", inboxID), map[string]any{
		"to":        []map[string]any{{"email": "success@simulator.amazonses.com"}},
		"subject":   "scope test",
		"body_text": "scope enforcement test",
	})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	threadID := mustStr(t, body, "thread_id")

	threadsURL := fmt.Sprintf("/v1/inboxes/%s/threads", inboxID)
	threadURL := fmt.Sprintf("/v1/inboxes/%s/threads/%s", inboxID, threadID)

	t.Run("read_scope_required_for_list_threads", func(t *testing.T) {
		c := scopedClient(t, admin, []string{"draft:read"})
		code, _, err := c.get(threadsURL)
		if err != nil {
			t.Fatal(err)
		}
		if code != 403 {
			t.Fatalf("expected 403, got %d", code)
		}
	})

	t.Run("read_scope_allows_list_threads", func(t *testing.T) {
		c := scopedClient(t, admin, []string{"inbox:read"})
		code, _, err := c.get(threadsURL)
		if err != nil {
			t.Fatal(err)
		}
		if code != 200 {
			t.Fatalf("expected 200, got %d", code)
		}
	})

	t.Run("read_scope_allows_get_thread", func(t *testing.T) {
		c := scopedClient(t, admin, []string{"inbox:read"})
		code, _, err := c.get(threadURL)
		if err != nil {
			t.Fatal(err)
		}
		if code != 200 {
			t.Fatalf("expected 200, got %d", code)
		}
	})

	t.Run("write_scope_required_for_patch_thread", func(t *testing.T) {
		c := scopedClient(t, admin, []string{"inbox:read"})
		code, _, err := c.patch(threadURL, map[string]any{"is_read": true})
		if err != nil {
			t.Fatal(err)
		}
		if code != 403 {
			t.Fatalf("expected 403, got %d", code)
		}
	})
}

// TestScopeEnforcementMessages verifies scope enforcement on message endpoints.
func TestScopeEnforcementMessages(t *testing.T) {
	admin := newClient(t)

	code, body, err := admin.post("/v1/inboxes", map[string]any{"address": uniqueName("scope-msg")})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	inboxID := mustStr(t, body, "id")
	t.Cleanup(func() { admin.delete("/v1/inboxes/" + inboxID) }) //nolint

	t.Run("read_scope_required_for_send", func(t *testing.T) {
		// inbox:read is NOT sufficient for send (requires inbox:write)
		c := scopedClient(t, admin, []string{"inbox:read"})
		code, _, err := c.post(fmt.Sprintf("/v1/inboxes/%s/messages/send", inboxID), map[string]any{
			"to":        []map[string]any{{"email": "success@simulator.amazonses.com"}},
			"subject":   "scope test",
			"body_text": "body",
		})
		if err != nil {
			t.Fatal(err)
		}
		if code != 403 {
			t.Fatalf("expected 403, got %d", code)
		}
	})

	t.Run("write_scope_allows_send", func(t *testing.T) {
		c := scopedClient(t, admin, []string{"inbox:write"})
		code, _, err := c.post(fmt.Sprintf("/v1/inboxes/%s/messages/send", inboxID), map[string]any{
			"to":        []map[string]any{{"email": "success@simulator.amazonses.com"}},
			"subject":   "scope test " + uniqueName("s"),
			"body_text": "body",
		})
		if err != nil {
			t.Fatal(err)
		}
		if code != 201 {
			t.Fatalf("expected 201, got %d", code)
		}
	})
}

// TestScopeEnforcementDrafts verifies scope enforcement on draft endpoints.
func TestScopeEnforcementDrafts(t *testing.T) {
	admin := newClient(t)

	code, body, err := admin.post("/v1/inboxes", map[string]any{"address": uniqueName("scope-dft")})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	inboxID := mustStr(t, body, "id")
	t.Cleanup(func() { admin.delete("/v1/inboxes/" + inboxID) }) //nolint

	draftsURL := fmt.Sprintf("/v1/inboxes/%s/drafts", inboxID)

	t.Run("read_scope_required_for_list_drafts", func(t *testing.T) {
		c := scopedClient(t, admin, []string{"inbox:read"}) // inbox:read is NOT draft:read
		code, _, err := c.get(draftsURL)
		if err != nil {
			t.Fatal(err)
		}
		if code != 403 {
			t.Fatalf("expected 403, got %d", code)
		}
	})

	t.Run("draft_read_scope_allows_list", func(t *testing.T) {
		c := scopedClient(t, admin, []string{"draft:read"})
		code, _, err := c.get(draftsURL)
		if err != nil {
			t.Fatal(err)
		}
		if code != 200 {
			t.Fatalf("expected 200, got %d", code)
		}
	})

	t.Run("draft_write_scope_required_for_create", func(t *testing.T) {
		c := scopedClient(t, admin, []string{"draft:read"})
		code, _, err := c.post(draftsURL, map[string]any{
			"to":      []map[string]any{{"email": "x@example.com"}},
			"subject": "draft scope test",
		})
		if err != nil {
			t.Fatal(err)
		}
		if code != 403 {
			t.Fatalf("expected 403, got %d", code)
		}
	})
}

// TestScopeEnforcementWebhooks verifies scope enforcement on webhook endpoints.
func TestScopeEnforcementWebhooks(t *testing.T) {
	admin := newClient(t)

	t.Run("read_scope_required_for_list", func(t *testing.T) {
		c := scopedClient(t, admin, []string{"inbox:read"})
		code, _, err := c.get("/v1/webhooks")
		if err != nil {
			t.Fatal(err)
		}
		if code != 403 {
			t.Fatalf("expected 403, got %d", code)
		}
	})

	t.Run("webhook_read_scope_allows_list", func(t *testing.T) {
		c := scopedClient(t, admin, []string{"webhook:read"})
		code, _, err := c.get("/v1/webhooks")
		if err != nil {
			t.Fatal(err)
		}
		if code != 200 {
			t.Fatalf("expected 200, got %d", code)
		}
	})

	t.Run("write_scope_required_for_create", func(t *testing.T) {
		c := scopedClient(t, admin, []string{"webhook:read"})
		code, _, err := c.post("/v1/webhooks", map[string]any{
			"url":       "https://httpbin.org/post",
			"events":    []string{"message.sent"},
			"is_active": true,
		})
		if err != nil {
			t.Fatal(err)
		}
		if code != 403 {
			t.Fatalf("expected 403, got %d", code)
		}
	})
}

// TestScopeEnforcementOrgAdmin verifies that org:admin is required for
// /v1/org, /v1/pods, /v1/keys, and /v1/domains.
func TestScopeEnforcementOrgAdmin(t *testing.T) {
	admin := newClient(t)
	// A key with inbox:read only — should not be able to access admin endpoints.
	limited := scopedClient(t, admin, []string{"inbox:read"})

	cases := []struct {
		name   string
		method string
		path   string
		body   any
	}{
		{"get org", "GET", "/v1/org", nil},
		{"list pods", "GET", "/v1/pods", nil},
		{"create pod", "POST", "/v1/pods", map[string]any{"name": "x", "slug": uniqueName("p")}},
		{"list keys", "GET", "/v1/keys", nil},
		{"list domains", "GET", "/v1/domains", nil},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			code, _, err := limited.do(tc.method, tc.path, tc.body)
			if err != nil {
				t.Fatal(err)
			}
			if code != 403 {
				t.Fatalf("%s %s: expected 403, got %d", tc.method, tc.path, code)
			}
		})
	}
}

// TestScopeEnforcementSearch verifies that search:read is required for GET /v1/search.
func TestScopeEnforcementSearch(t *testing.T) {
	admin := newClient(t)

	t.Run("missing_search_scope_gets_403", func(t *testing.T) {
		c := scopedClient(t, admin, []string{"inbox:read"})
		code, _, err := c.get("/v1/search?q=test")
		if err != nil {
			t.Fatal(err)
		}
		if code != 403 {
			t.Fatalf("expected 403, got %d", code)
		}
	})

	t.Run("search_read_scope_allows_search", func(t *testing.T) {
		c := scopedClient(t, admin, []string{"search:read"})
		code, _, err := c.get("/v1/search?q=test")
		if err != nil {
			t.Fatal(err)
		}
		if code != 200 {
			t.Fatalf("expected 200, got %d", code)
		}
	})
}

// ── nGX-ove: Thread list filter params ────────────────────────────────────────

// TestThreadListFilters verifies that ?unread=, ?starred=, and ?label= correctly
// filter thread results via the threads Lambda.
func TestThreadListFilters(t *testing.T) {
	c := newClient(t)

	// Create an inbox and send two messages to generate two threads.
	code, body, err := c.post("/v1/inboxes", map[string]any{"address": uniqueName("thr-filter")})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	inboxID := mustStr(t, body, "id")
	t.Cleanup(func() { c.delete("/v1/inboxes/" + inboxID) }) //nolint

	// Send message 1 → thread 1.
	code, body, err = c.post(fmt.Sprintf("/v1/inboxes/%s/messages/send", inboxID), map[string]any{
		"to":        []map[string]any{{"email": "success@simulator.amazonses.com"}},
		"subject":   "filter test A " + uniqueName("a"),
		"body_text": "body A",
	})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	threadID1 := mustStr(t, body, "thread_id")

	// Send message 2 → thread 2.
	code, body, err = c.post(fmt.Sprintf("/v1/inboxes/%s/messages/send", inboxID), map[string]any{
		"to":        []map[string]any{{"email": "success@simulator.amazonses.com"}},
		"subject":   "filter test B " + uniqueName("b"),
		"body_text": "body B",
	})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	threadID2 := mustStr(t, body, "thread_id")

	threadsURL := fmt.Sprintf("/v1/inboxes/%s/threads", inboxID)

	// Explicitly set known states: thread1=read+not-starred, thread2=unread+starred.
	// We set both explicitly so the test is independent of the outbound-send default.
	code, _, err = c.patch(fmt.Sprintf("%s/%s", threadsURL, threadID1), map[string]any{"is_read": true})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 200, nil)

	code, _, err = c.patch(fmt.Sprintf("%s/%s", threadsURL, threadID2), map[string]any{"is_read": false})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 200, nil)

	// Star thread2, unstar thread1 (ensure thread1 is not starred).
	code, _, err = c.patch(fmt.Sprintf("%s/%s", threadsURL, threadID1), map[string]any{"is_starred": false})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 200, nil)

	code, _, err = c.patch(fmt.Sprintf("%s/%s", threadsURL, threadID2), map[string]any{"is_starred": true})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 200, nil)

	// Create a label and attach it to thread1.
	code, body, err = c.post("/v1/labels", map[string]any{"name": uniqueName("filter-lbl"), "color": "#aabbcc"})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	labelID := mustStr(t, body, "id")
	t.Cleanup(func() { c.delete("/v1/labels/" + labelID) }) //nolint

	code, _, err = c.do("PUT", fmt.Sprintf("%s/%s/labels/%s", threadsURL, threadID1, labelID), nil)
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 204, nil)

	t.Run("unread_true_excludes_read_thread", func(t *testing.T) {
		// ?unread=true → only threads where is_read=false → should NOT include threadID1
		code, body, err := c.get(threadsURL + "?unread=true")
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 200, body)
		threads := listOf(body, "threads")
		for _, th := range threads {
			if str(asMap(th), "id") == threadID1 {
				t.Fatal("?unread=true returned the read thread (threadID1)")
			}
		}
		// thread2 (unread) should appear
		found := false
		for _, th := range threads {
			if str(asMap(th), "id") == threadID2 {
				found = true
				break
			}
		}
		if !found {
			t.Fatal("?unread=true did not return the unread thread (threadID2)")
		}
	})

	t.Run("unread_false_excludes_unread_thread", func(t *testing.T) {
		// ?unread=false → only threads where is_read=true → should include threadID1 only
		code, body, err := c.get(threadsURL + "?unread=false")
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 200, body)
		threads := listOf(body, "threads")
		for _, th := range threads {
			if str(asMap(th), "id") == threadID2 {
				t.Fatal("?unread=false returned the unread thread (threadID2)")
			}
		}
		found := false
		for _, th := range threads {
			if str(asMap(th), "id") == threadID1 {
				found = true
				break
			}
		}
		if !found {
			t.Fatal("?unread=false did not return the read thread (threadID1)")
		}
	})

	t.Run("starred_true_returns_only_starred", func(t *testing.T) {
		code, body, err := c.get(threadsURL + "?starred=true")
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 200, body)
		threads := listOf(body, "threads")
		for _, th := range threads {
			if str(asMap(th), "id") == threadID1 {
				t.Fatal("?starred=true returned the non-starred thread (threadID1)")
			}
		}
		found := false
		for _, th := range threads {
			if str(asMap(th), "id") == threadID2 {
				found = true
				break
			}
		}
		if !found {
			t.Fatal("?starred=true did not return the starred thread (threadID2)")
		}
	})

	t.Run("starred_false_excludes_starred_thread", func(t *testing.T) {
		code, body, err := c.get(threadsURL + "?starred=false")
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 200, body)
		threads := listOf(body, "threads")
		for _, th := range threads {
			if str(asMap(th), "id") == threadID2 {
				t.Fatal("?starred=false returned the starred thread (threadID2)")
			}
		}
	})

	t.Run("label_filter_returns_only_labeled_thread", func(t *testing.T) {
		code, body, err := c.get(threadsURL + "?label=" + labelID)
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 200, body)
		threads := listOf(body, "threads")
		if len(threads) == 0 {
			t.Fatal("?label= filter returned no threads; expected threadID1")
		}
		for _, th := range threads {
			if str(asMap(th), "id") == threadID2 {
				t.Fatal("?label= filter returned threadID2 which has no label")
			}
		}
		found := false
		for _, th := range threads {
			if str(asMap(th), "id") == threadID1 {
				found = true
				break
			}
		}
		if !found {
			t.Fatal("?label= filter did not return threadID1 which has the label")
		}
	})
}
