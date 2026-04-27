/*
 * Copyright (c) 2026 nyklabs.com. All rights reserved.
 *
 * Licensed under the nGX Commercial Source License v1.0.
 * See LICENSE file in the project root for full license information.
 */

package integration

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// ── nGX-67z: Thread label attach and detach ───────────────────────────────────

// TestThreadLabelAttachDetach verifies the label management cycle:
//
//	POST /v1/labels → PUT thread/labels/{id} → GET thread (labels present) →
//	DELETE thread/labels/{id} → GET thread (labels absent).
func TestThreadLabelAttachDetach(t *testing.T) {
	c := newClient(t)

	// Create a label for this test.
	code, body, err := c.post("/v1/labels", map[string]any{
		"name": uniqueName("lbl-attach"), "color": "#112233",
	})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	labelID := mustStr(t, body, "id")
	t.Cleanup(func() { c.delete("/v1/labels/" + labelID) }) //nolint

	// Create an inbox and send a message to create a thread.
	code, body, err = c.post("/v1/inboxes", map[string]any{"address": uniqueName("lbl-inbox")})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	inboxID := mustStr(t, body, "id")
	t.Cleanup(func() { c.delete("/v1/inboxes/" + inboxID) }) //nolint

	code, body, err = c.post(fmt.Sprintf("/v1/inboxes/%s/messages/send", inboxID), map[string]any{
		"to":        []map[string]any{{"email": "label-test@example.com"}},
		"subject":   "Label attach test",
		"body_text": "Testing label attach/detach",
	})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	threadID := mustStr(t, body, "thread_id")

	threadURL := fmt.Sprintf("/v1/inboxes/%s/threads/%s", inboxID, threadID)
	labelURL := fmt.Sprintf("%s/labels/%s", threadURL, labelID)

	t.Run("attach_label", func(t *testing.T) {
		code, _, err := c.do("PUT", labelURL, nil)
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 204, nil)
	})

	t.Run("get_thread_has_label", func(t *testing.T) {
		code, body, err := c.get(threadURL)
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 200, body)
		labels := listOf(body, "labels")
		found := false
		for _, l := range labels {
			if str(asMap(l), "id") == labelID {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("label %s not found in thread.labels after attach: %v", labelID, labels)
		}
	})

	t.Run("detach_label", func(t *testing.T) {
		code, _, err := c.delete(labelURL)
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 204, nil)
	})

	t.Run("get_thread_label_gone", func(t *testing.T) {
		code, body, err := c.get(threadURL)
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 200, body)
		for _, l := range listOf(body, "labels") {
			if str(asMap(l), "id") == labelID {
				t.Errorf("label %s still in thread.labels after detach", labelID)
			}
		}
	})
}

// ── nGX-5sr: Cursor pagination ────────────────────────────────────────────────

// TestCursorPagination verifies that ?limit= and next_cursor work for both
// threads and messages list endpoints.
func TestCursorPagination(t *testing.T) {
	c := newClient(t)

	// Create a dedicated inbox.
	code, body, err := c.post("/v1/inboxes", map[string]any{"address": uniqueName("page-inbox")})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	inboxID := mustStr(t, body, "id")
	t.Cleanup(func() { c.delete("/v1/inboxes/" + inboxID) }) //nolint

	t.Run("thread_pagination", func(t *testing.T) {
		// Send 3 messages (each creates its own thread).
		for i := 0; i < 3; i++ {
			code, body, err := c.post(fmt.Sprintf("/v1/inboxes/%s/messages/send", inboxID), map[string]any{
				"to":        []map[string]any{{"email": "page-test@example.com"}},
				"subject":   fmt.Sprintf("Page test thread %d", i),
				"body_text": "Pagination test",
			})
			if err != nil {
				t.Fatal(err)
			}
			mustCode(t, code, 201, body)
		}

		// Page 1: limit=2 should return 2 threads and a next_cursor.
		code, body, err := c.get(fmt.Sprintf("/v1/inboxes/%s/threads?limit=2", inboxID))
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 200, body)
		page1 := listOf(body, "threads")
		if len(page1) != 2 {
			t.Fatalf("expected 2 threads on page 1, got %d", len(page1))
		}
		cursor := str(body, "next_cursor")
		if cursor == "" {
			t.Fatal("expected next_cursor on page 1, got empty string")
		}

		// Page 2: use cursor → should return remaining thread(s), no next_cursor.
		code, body, err = c.get(fmt.Sprintf("/v1/inboxes/%s/threads?limit=2&cursor=%s", inboxID, cursor))
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 200, body)
		page2 := listOf(body, "threads")
		if len(page2) == 0 {
			t.Fatal("expected at least 1 thread on page 2")
		}
		// No more cursor expected.
		if c2 := str(body, "next_cursor"); c2 != "" {
			t.Errorf("expected no next_cursor on final page, got %q", c2)
		}

		// No duplicates between pages.
		seen := map[string]bool{}
		for _, th := range page1 {
			seen[str(asMap(th), "id")] = true
		}
		for _, th := range page2 {
			if seen[str(asMap(th), "id")] {
				t.Errorf("duplicate thread id %s across pages", str(asMap(th), "id"))
			}
		}
	})

	t.Run("message_pagination", func(t *testing.T) {
		// Send first message to create a new thread.
		code, body, err := c.post(fmt.Sprintf("/v1/inboxes/%s/messages/send", inboxID), map[string]any{
			"to":        []map[string]any{{"email": "page-msg@example.com"}},
			"subject":   "Message pagination test",
			"body_text": "Message 1",
		})
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 201, body)
		threadID := mustStr(t, body, "thread_id")
		msgID1 := mustStr(t, body, "id")

		// Reply to add message 2.
		code, body, err = c.post(fmt.Sprintf("/v1/inboxes/%s/messages/send", inboxID), map[string]any{
			"to":          []map[string]any{{"email": "page-msg@example.com"}},
			"subject":     "Message pagination test",
			"body_text":   "Message 2",
			"reply_to_id": msgID1,
		})
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 201, body)
		msgID2 := mustStr(t, body, "id")

		// Reply to add message 3.
		code, body, err = c.post(fmt.Sprintf("/v1/inboxes/%s/messages/send", inboxID), map[string]any{
			"to":          []map[string]any{{"email": "page-msg@example.com"}},
			"subject":     "Message pagination test",
			"body_text":   "Message 3",
			"reply_to_id": msgID2,
		})
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 201, body)

		msgsURL := fmt.Sprintf("/v1/inboxes/%s/threads/%s/messages", inboxID, threadID)

		// Page 1: limit=2 → 2 messages and a cursor.
		code, body, err = c.get(msgsURL + "?limit=2")
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 200, body)
		page1 := listOf(body, "messages")
		if len(page1) != 2 {
			t.Fatalf("expected 2 messages on page 1, got %d", len(page1))
		}
		cursor := str(body, "next_cursor")
		if cursor == "" {
			t.Fatal("expected next_cursor after page 1, got empty string")
		}

		// Page 2: use cursor → remaining message, no next_cursor.
		code, body, err = c.get(fmt.Sprintf("%s?limit=2&cursor=%s", msgsURL, cursor))
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 200, body)
		page2 := listOf(body, "messages")
		if len(page2) == 0 {
			t.Fatal("expected at least 1 message on page 2")
		}
		if c2 := str(body, "next_cursor"); c2 != "" {
			t.Errorf("expected no next_cursor on final page, got %q", c2)
		}

		// No duplicates.
		seen := map[string]bool{}
		for _, m := range page1 {
			seen[str(asMap(m), "id")] = true
		}
		for _, m := range page2 {
			if seen[str(asMap(m), "id")] {
				t.Errorf("duplicate message id %s across pages", str(asMap(m), "id"))
			}
		}
	})
}

// ── nGX-46y: Domain verify endpoint ──────────────────────────────────────────

// TestDomainVerify registers a domain and calls POST .../verify to exercise
// the verify endpoint. DNS records are NOT added so the domain remains pending;
// the test verifies the response shape and that the domain record is updated.
func TestDomainVerify(t *testing.T) {
	c := newClient(t)
	const testDomain = "domain-verify-test.nyklabs.com"

	// Need a pod ID for domain registration.
	_, podsBody, err := c.get("/v1/pods")
	if err != nil {
		t.Fatal(err)
	}
	pods := listOf(podsBody, "pods")
	if len(pods) == 0 {
		t.Skip("no pods available, skipping")
	}
	podID := str(asMap(pods[0]), "id")

	// Register the domain.
	code, body, err := c.post("/v1/domains", map[string]any{"Domain": testDomain, "PodID": podID})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	domainID := str(asMap(body["Domain"]), "id")
	if domainID == "" {
		t.Fatal("register response missing domain id")
	}
	t.Cleanup(func() { c.delete("/v1/domains/" + domainID) }) //nolint

	// Call verify — DNS records not set so status stays pending.
	t.Run("verify_returns_200", func(t *testing.T) {
		code, body, err := c.post(fmt.Sprintf("/v1/domains/%s/verify", domainID), nil)
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 200, body)
	})

	t.Run("verify_response_has_domain", func(t *testing.T) {
		_, body, err := c.post(fmt.Sprintf("/v1/domains/%s/verify", domainID), nil)
		if err != nil {
			t.Fatal(err)
		}
		domain := asMap(body["Domain"])
		if domain == nil {
			t.Fatal("verify response missing 'Domain' object")
		}
		if str(domain, "id") != domainID {
			t.Errorf("Domain.id mismatch: got %q want %q", str(domain, "id"), domainID)
		}
		if str(domain, "domain") != testDomain {
			t.Errorf("Domain.domain mismatch: got %q want %q", str(domain, "domain"), testDomain)
		}
	})

	t.Run("verify_response_has_dns_records", func(t *testing.T) {
		_, body, err := c.post(fmt.Sprintf("/v1/domains/%s/verify", domainID), nil)
		if err != nil {
			t.Fatal(err)
		}
		records := listOf(body, "DNSRecords")
		if len(records) == 0 {
			t.Fatal("verify response missing DNSRecords")
		}
	})

	t.Run("verify_nonexistent_domain_is_400", func(t *testing.T) {
		code, _, err := c.post("/v1/domains/00000000-0000-0000-0000-000000000000/verify", nil)
		if err != nil {
			t.Fatal(err)
		}
		if code != 400 && code != 404 {
			t.Errorf("expected 400 or 404 for non-existent domain, got %d", code)
		}
	})
}

// ── nGX-gj7: RLS org isolation ────────────────────────────────────────────────

// TestRLSIsolation verifies row-level security isolation properties:
//
//   - A second API key for the same org can access the same resources (org-scoped, not key-scoped).
//   - A key restricted to pod P cannot see inboxes in a different pod.
//   - An invalid bearer token is rejected with 401.
func TestRLSIsolation(t *testing.T) {
	c := newClient(t)

	// Create a pod to use for isolation testing.
	slug1 := uniqueName("rls-p1")
	code, body, err := c.post("/v1/pods", map[string]any{"name": "RLS Test Pod 1", "slug": slug1})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	pod1ID := mustStr(t, body, "id")
	t.Cleanup(func() { c.delete("/v1/pods/" + pod1ID) }) //nolint

	slug2 := uniqueName("rls-p2")
	code, body, err = c.post("/v1/pods", map[string]any{"name": "RLS Test Pod 2", "slug": slug2})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	pod2ID := mustStr(t, body, "id")
	t.Cleanup(func() { c.delete("/v1/pods/" + pod2ID) }) //nolint

	// Create inbox in pod 1 using admin key.
	// Note: CreateInboxRequest has no json tags so field names are PascalCase.
	code, body, err = c.post("/v1/inboxes", map[string]any{
		"Address": uniqueName("rls-inbox"),
		"PodID":   pod1ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	inboxID := mustStr(t, body, "id")
	t.Cleanup(func() { c.delete("/v1/inboxes/" + inboxID) }) //nolint

	t.Run("second_key_same_org_can_read", func(t *testing.T) {
		// Create a second org:admin key.
		code, body, err := c.post("/v1/keys", map[string]any{
			"name":   uniqueName("rls-key2"),
			"scopes": []string{"org:admin"},
		})
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 201, body)
		key2ID := mustStr(t, body, "id")
		key2 := str(body, "key")
		t.Cleanup(func() { c.delete("/v1/keys/" + key2ID) }) //nolint

		c2 := &client{baseURL: c.baseURL, apiKey: key2, httpClient: c.httpClient}
		code, body2, err := c2.get("/v1/inboxes/" + inboxID)
		if err != nil {
			t.Fatal(err)
		}
		if code != 200 {
			t.Errorf("second key (same org) should read inbox: got %d %v", code, body2)
		}
	})

	t.Run("pod_scoped_key_cannot_list_other_pod_inboxes", func(t *testing.T) {
		// Create a key scoped to pod 2.
		// Note: pod isolation is enforced at the list-query level (WHERE pod_id = $X),
		// not via DB RLS on individual row GET. The GET /v1/inboxes/{id} query filters
		// only by org_id, so a direct GET is org-scoped. LIST enforces pod isolation.
		code, body, err := c.post("/v1/keys", map[string]any{
			"name":   uniqueName("rls-pod2-key"),
			"scopes": []string{"inbox:read"},
			"pod_id": pod2ID,
		})
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 201, body)
		keyPod2ID := mustStr(t, body, "id")
		keyPod2 := str(body, "key")
		t.Cleanup(func() { c.delete("/v1/keys/" + keyPod2ID) }) //nolint

		// Pod 2 key lists inboxes → should NOT see the pod-1 inbox.
		c2 := &client{baseURL: c.baseURL, apiKey: keyPod2, httpClient: c.httpClient}
		code, body2, err := c2.get("/v1/inboxes")
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 200, body2)
		for _, inbox := range listOf(body2, "inboxes") {
			if str(asMap(inbox), "id") == inboxID {
				t.Errorf("pod-2-scoped key should not see pod-1 inbox in list, but found it: %v", inbox)
			}
		}
	})

	t.Run("invalid_key_rejected", func(t *testing.T) {
		bogus := &client{
			baseURL:    c.baseURL,
			apiKey:     "am_live_totallyinvalidkeyXXXXXXXXXXXXXXXXXXXX",
			httpClient: &http.Client{Timeout: 15 * time.Second},
		}
		code, _, err := bogus.get("/v1/inboxes")
		if err != nil {
			t.Fatal(err)
		}
		if code != 401 && code != 403 {
			t.Errorf("invalid key should be 401/403, got %d", code)
		}
	})
}

// ── nGX-gon: Scope and pod enforcement ───────────────────────────────────────

// TestScopeEnforcement verifies that:
//   - A key with only inbox:read cannot POST (write) to inboxes (403).
//   - A key with only inbox:write cannot GET (read) inboxes (403).
//   - A key with org:admin passes all scope checks.
//   - A key with inbox:read can GET inboxes (200).
func TestScopeEnforcement(t *testing.T) {
	c := newClient(t)

	// Key with inbox:read only.
	code, body, err := c.post("/v1/keys", map[string]any{
		"name":   uniqueName("scope-read"),
		"scopes": []string{"inbox:read"},
	})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	readKeyID := mustStr(t, body, "id")
	readKey := str(body, "key")
	t.Cleanup(func() { c.delete("/v1/keys/" + readKeyID) }) //nolint

	// Key with inbox:write only.
	code, body, err = c.post("/v1/keys", map[string]any{
		"name":   uniqueName("scope-write"),
		"scopes": []string{"inbox:write"},
	})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	writeKeyID := mustStr(t, body, "id")
	writeKey := str(body, "key")
	t.Cleanup(func() { c.delete("/v1/keys/" + writeKeyID) }) //nolint

	readClient := &client{baseURL: c.baseURL, apiKey: readKey, httpClient: c.httpClient}
	writeClient := &client{baseURL: c.baseURL, apiKey: writeKey, httpClient: c.httpClient}

	t.Run("read_scope_allows_list", func(t *testing.T) {
		code, body, err := readClient.get("/v1/inboxes")
		if err != nil {
			t.Fatal(err)
		}
		if code != 200 {
			t.Errorf("inbox:read key should list inboxes (200), got %d: %v", code, body)
		}
	})

	t.Run("read_scope_blocks_create", func(t *testing.T) {
		code, body, err := readClient.post("/v1/inboxes", map[string]any{
			"address": uniqueName("scope-blocked"),
		})
		if err != nil {
			t.Fatal(err)
		}
		if code != 403 {
			t.Errorf("inbox:read key should be blocked from POST (403), got %d: %v", code, body)
		}
	})

	t.Run("write_scope_blocks_list", func(t *testing.T) {
		code, body, err := writeClient.get("/v1/inboxes")
		if err != nil {
			t.Fatal(err)
		}
		if code != 403 {
			t.Errorf("inbox:write key should be blocked from GET (403), got %d: %v", code, body)
		}
	})

	t.Run("admin_key_passes_all_checks", func(t *testing.T) {
		// The main client (org:admin) should still work fine.
		code, body, err := c.get("/v1/inboxes")
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 200, body)
	})
}

// ── nGX-uyz: Outbound email input validation ──────────────────────────────────

// TestSendValidation verifies that POST .../messages/send rejects invalid inputs.
func TestSendValidation(t *testing.T) {
	c := newClient(t)

	// Create a dedicated inbox.
	code, body, err := c.post("/v1/inboxes", map[string]any{"address": uniqueName("val-inbox")})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	inboxID := mustStr(t, body, "id")
	t.Cleanup(func() { c.delete("/v1/inboxes/" + inboxID) }) //nolint

	sendURL := fmt.Sprintf("/v1/inboxes/%s/messages/send", inboxID)

	t.Run("empty_to_rejected", func(t *testing.T) {
		code, body, err := c.post(sendURL, map[string]any{
			"to":        []map[string]any{},
			"subject":   "test",
			"body_text": "test",
		})
		if err != nil {
			t.Fatal(err)
		}
		if code != 400 {
			t.Errorf("empty to should be 400, got %d: %v", code, body)
		}
	})

	t.Run("invalid_email_rejected", func(t *testing.T) {
		code, body, err := c.post(sendURL, map[string]any{
			"to":        []map[string]any{{"email": "not-an-email"}},
			"subject":   "test",
			"body_text": "test",
		})
		if err != nil {
			t.Fatal(err)
		}
		if code != 400 {
			t.Errorf("invalid email should be 400, got %d: %v", code, body)
		}
	})

	t.Run("empty_body_rejected", func(t *testing.T) {
		code, body, err := c.post(sendURL, map[string]any{
			"to":      []map[string]any{{"email": "test@example.com"}},
			"subject": "test",
			// body_text and body_html both absent
		})
		if err != nil {
			t.Fatal(err)
		}
		if code != 400 {
			t.Errorf("empty body should be 400, got %d: %v", code, body)
		}
	})

	t.Run("oversized_attachment_rejected", func(t *testing.T) {
		// Generate 11 MiB of data (exceeds the 10 MiB limit).
		bigData := make([]byte, 11*1024*1024)
		for i := range bigData {
			bigData[i] = 'A'
		}
		b64 := base64.StdEncoding.EncodeToString(bigData)

		code, body, err := c.post(sendURL, map[string]any{
			"to":      []map[string]any{{"email": "test@example.com"}},
			"subject": "test",
			"body_text": "test",
			"attachments": []map[string]any{
				{"filename": "big.bin", "content_type": "application/octet-stream", "content": b64},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		// API Gateway itself returns 413 when the request body exceeds its 10 MB
		// limit. The Lambda validation returns 400. Both indicate rejection.
		if code != 400 && code != 413 {
			t.Errorf("oversized attachment should be 400 or 413, got %d: %v", code, body)
		}
	})

	t.Run("valid_send_succeeds", func(t *testing.T) {
		code, body, err := c.post(sendURL, map[string]any{
			"to":        []map[string]any{{"email": "success@simulator.amazonses.com"}},
			"subject":   "Validation test " + uniqueName("v"),
			"body_text": "This is a valid message",
		})
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 201, body)
		if str(body, "id") == "" {
			t.Fatal("expected message id in response")
		}
	})

	t.Run("no_to_field_rejected", func(t *testing.T) {
		// Omit to entirely (will deserialize as nil/empty slice).
		code, body, err := c.post(sendURL, map[string]any{
			"subject":   "test",
			"body_text": "test",
		})
		if err != nil {
			t.Fatal(err)
		}
		if code != 400 {
			t.Errorf("missing to field should be 400, got %d: %v", code, body)
		}
	})
}

// ── nGX-6z6: Invalid pagination cursor returns 400 ───────────────────────────

// TestInvalidCursorReturns400 verifies that passing a syntactically invalid
// base64 cursor string to list endpoints returns 400 (not 500).
func TestInvalidCursorReturns400(t *testing.T) {
	admin := newClient(t)
	const badCursor = "notvalidbase64!!!"

	// Create an inbox so the list endpoints have something to operate on.
	code, body, err := admin.post("/v1/inboxes", map[string]any{"address": uniqueName("invalid-cursor")})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	inboxID := mustStr(t, body, "id")
	t.Cleanup(func() { admin.delete("/v1/inboxes/" + inboxID) }) //nolint

	// Send a message to create a thread (needed for the messages endpoint).
	code, body, err = admin.post(fmt.Sprintf("/v1/inboxes/%s/messages/send", inboxID), map[string]any{
		"to":        []map[string]any{{"email": "cursor-test@example.com"}},
		"subject":   "Invalid cursor test",
		"body_text": "Testing invalid cursor handling",
	})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)

	// Retrieve the thread ID from the threads list.
	code, body, err = admin.get(fmt.Sprintf("/v1/inboxes/%s/threads", inboxID))
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 200, body)
	threads := listOf(body, "threads")
	if len(threads) == 0 {
		t.Fatal("expected at least one thread after sending a message")
	}
	threadID := str(asMap(threads[0]), "id")

	t.Run("inboxes_list", func(t *testing.T) {
		code, body, err := admin.get("/v1/inboxes?cursor=" + badCursor)
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 400, body)
	})

	t.Run("threads_list", func(t *testing.T) {
		code, body, err := admin.get(fmt.Sprintf("/v1/inboxes/%s/threads?cursor=%s", inboxID, badCursor))
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 400, body)
	})

	t.Run("messages_list", func(t *testing.T) {
		code, body, err := admin.get(fmt.Sprintf("/v1/inboxes/%s/threads/%s/messages?cursor=%s", inboxID, threadID, badCursor))
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 400, body)
	})

	t.Run("drafts_list", func(t *testing.T) {
		code, body, err := admin.get(fmt.Sprintf("/v1/inboxes/%s/drafts?cursor=%s", inboxID, badCursor))
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 400, body)
	})
}

// helper: test that a string contains a substring (for validation error messages)
func containsStr(t *testing.T, haystack, needle string) bool {
	t.Helper()
	return strings.Contains(haystack, needle)
}

// ── nGX-xob: Duplicate and conflict error handling ────────────────────────────

// TestDuplicateConflicts verifies that creating resources with duplicate
// unique fields returns 400/409 instead of leaking a raw DB error (500).
func TestDuplicateConflicts(t *testing.T) {
	c := newClient(t)

	t.Run("duplicate_inbox_address", func(t *testing.T) {
		addr := uniqueName("dup-inbox")
		code, body, err := c.post("/v1/inboxes", map[string]any{"address": addr})
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 201, body)
		id := mustStr(t, body, "id")
		t.Cleanup(func() { c.delete("/v1/inboxes/" + id) }) //nolint

		// Second inbox with identical address.
		code, body2, err := c.post("/v1/inboxes", map[string]any{"address": addr})
		if err != nil {
			t.Fatal(err)
		}
		if code == 500 {
			t.Errorf("duplicate inbox address should not return 500 (raw DB error leaked): %v", body2)
		}
		if code != 400 && code != 409 {
			t.Errorf("duplicate inbox address: expected 400 or 409, got %d: %v", code, body2)
		}
	})

	t.Run("duplicate_pod_slug", func(t *testing.T) {
		slug := uniqueName("dup-pod")
		code, body, err := c.post("/v1/pods", map[string]any{"name": "Dup Pod", "slug": slug})
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 201, body)
		id := mustStr(t, body, "id")
		t.Cleanup(func() { c.delete("/v1/pods/" + id) }) //nolint

		// Second pod with identical slug.
		code, body2, err := c.post("/v1/pods", map[string]any{"name": "Dup Pod 2", "slug": slug})
		if err != nil {
			t.Fatal(err)
		}
		if code == 500 {
			t.Errorf("duplicate pod slug should not return 500 (raw DB error leaked): %v", body2)
		}
		if code != 400 && code != 409 {
			t.Errorf("duplicate pod slug: expected 400 or 409, got %d: %v", code, body2)
		}
	})

	t.Run("duplicate_domain", func(t *testing.T) {
		_, podsBody, err := c.get("/v1/pods")
		if err != nil {
			t.Fatal(err)
		}
		pods := listOf(podsBody, "pods")
		if len(pods) == 0 {
			t.Skip("no pods available")
		}
		podID := str(asMap(pods[0]), "id")
		const testDomain = "dup-domain-test.nyklabs.com"

		// First registration.
		code, body, err := c.post("/v1/domains", map[string]any{"Domain": testDomain, "PodID": podID})
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 201, body)
		domainID := str(asMap(body["Domain"]), "id")
		t.Cleanup(func() { c.delete("/v1/domains/" + domainID) }) //nolint

		// Second registration of same domain.
		code, body2, err := c.post("/v1/domains", map[string]any{"Domain": testDomain, "PodID": podID})
		if err != nil {
			t.Fatal(err)
		}
		if code == 500 {
			t.Errorf("duplicate domain should not return 500 (raw DB error leaked): %v", body2)
		}
		if code != 400 && code != 409 {
			t.Errorf("duplicate domain: expected 400 or 409, got %d: %v", code, body2)
		}
	})

	t.Run("duplicate_label_name", func(t *testing.T) {
		name := uniqueName("dup-label")
		code, body, err := c.post("/v1/labels", map[string]any{"name": name, "color": "#aabbcc"})
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 201, body)
		id := mustStr(t, body, "id")
		t.Cleanup(func() { c.delete("/v1/labels/" + id) }) //nolint

		// Second label with identical name.
		code, body2, err := c.post("/v1/labels", map[string]any{"name": name, "color": "#112233"})
		if err != nil {
			t.Fatal(err)
		}
		if code == 500 {
			t.Errorf("duplicate label name should not return 500 (raw DB error leaked): %v", body2)
		}
		if code != 400 && code != 409 {
			t.Errorf("duplicate label name: expected 400 or 409, got %d: %v", code, body2)
		}
	})
}

// ── nGX-drm: Attachment edge cases ───────────────────────────────────────────

// TestAttachmentEdgeCases verifies attachment failure modes and metadata correctness.
func TestAttachmentEdgeCases(t *testing.T) {
	c := newClient(t)

	code, body, err := c.post("/v1/inboxes", map[string]any{"address": uniqueName("att-edge")})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	inboxID := mustStr(t, body, "id")
	t.Cleanup(func() { c.delete("/v1/inboxes/" + inboxID) }) //nolint

	t.Run("invalid_base64_rejected", func(t *testing.T) {
		code, body, err := c.post(fmt.Sprintf("/v1/inboxes/%s/messages/send", inboxID), map[string]any{
			"to":        []map[string]any{{"email": "test@example.com"}},
			"subject":   "invalid b64 test",
			"body_text": "test",
			"attachments": []map[string]any{
				{"filename": "bad.bin", "content_type": "application/octet-stream", "content": "NOT_VALID_BASE64!!!"},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if code != 400 {
			t.Errorf("invalid base64 attachment should return 400, got %d: %v", code, body)
		}
	})

	t.Run("s3_key_format_in_response", func(t *testing.T) {
		smallData := base64.StdEncoding.EncodeToString([]byte("hello attachment"))
		code, body, err := c.post(fmt.Sprintf("/v1/inboxes/%s/messages/send", inboxID), map[string]any{
			"to":        []map[string]any{{"email": "success@simulator.amazonses.com"}},
			"subject":   "S3 key test " + uniqueName("s"),
			"body_text": "attachment s3 key test",
			"attachments": []map[string]any{
				{"filename": "test.txt", "content_type": "text/plain", "content": smallData},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 201, body)
		msgID := mustStr(t, body, "id")
		threadID := mustStr(t, body, "thread_id")

		// GET the message to check attachment s3_key format: orgID/msgID/filename
		_, msgBody, err := c.get(fmt.Sprintf("/v1/inboxes/%s/threads/%s/messages/%s", inboxID, threadID, msgID))
		if err != nil {
			t.Fatal(err)
		}
		atts := listOf(msgBody, "attachments")
		if len(atts) == 0 {
			t.Fatal("expected attachment in message response")
		}
		s3Key := str(asMap(atts[0]), "s3_key")
		if s3Key == "" {
			t.Fatal("attachment s3_key is empty")
		}
		// S3 key format: {orgID}/{msgID}/{filename}
		if !strings.Contains(s3Key, msgID) {
			t.Errorf("s3_key %q should contain msgID %q", s3Key, msgID)
		}
		if !strings.HasSuffix(s3Key, "test.txt") {
			t.Errorf("s3_key %q should end with filename 'test.txt'", s3Key)
		}
	})

	t.Run("draft_attachment_cascade_on_delete", func(t *testing.T) {
		smallData := base64.StdEncoding.EncodeToString([]byte("draft attachment data"))
		// Create a draft with an attachment.
		code, body, err := c.post(fmt.Sprintf("/v1/inboxes/%s/drafts", inboxID), map[string]any{
			"to":      []map[string]any{{"email": "cascade-test@example.com"}},
			"subject": "Draft cascade test",
			"body_text": "Testing cascade delete",
			"attachments": []map[string]any{
				{"filename": "cascade.txt", "content_type": "text/plain", "content": smallData},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 201, body)
		draftID := mustStr(t, body, "id")

		// Verify the attachment is present in the GET response.
		code, body, err = c.get(fmt.Sprintf("/v1/inboxes/%s/drafts/%s", inboxID, draftID))
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 200, body)
		if len(listOf(body, "attachments")) == 0 {
			t.Fatal("expected attachment in draft GET response before delete")
		}

		// Delete the draft.
		code, _, err = c.delete(fmt.Sprintf("/v1/inboxes/%s/drafts/%s", inboxID, draftID))
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 204, nil)

		// Draft should be gone (404).
		code, _, err = c.get(fmt.Sprintf("/v1/inboxes/%s/drafts/%s", inboxID, draftID))
		if err != nil {
			t.Fatal(err)
		}
		if code != 404 {
			t.Errorf("deleted draft should return 404, got %d", code)
		}
	})

	t.Run("empty_filename_handled", func(t *testing.T) {
		smallData := base64.StdEncoding.EncodeToString([]byte("data"))
		code, body, err := c.post(fmt.Sprintf("/v1/inboxes/%s/messages/send", inboxID), map[string]any{
			"to":        []map[string]any{{"email": "success@simulator.amazonses.com"}},
			"subject":   "Empty filename test " + uniqueName("f"),
			"body_text": "test",
			"attachments": []map[string]any{
				{"filename": "", "content_type": "application/octet-stream", "content": smallData},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		// Empty filename should either succeed (stored as-is) or return 400 — not 500.
		if code == 500 {
			t.Errorf("empty filename should not cause 500, got 500: %v", body)
		}
	})
}

// ── nGX-nl0: Inbox status transitions ────────────────────────────────────────

// TestInboxStatusTransitions verifies PATCH inbox status transitions and
// that DELETE removes the inbox (all associated resources become inaccessible).
func TestInboxStatusTransitions(t *testing.T) {
	c := newClient(t)

	// Create a fresh inbox for transition testing.
	code, body, err := c.post("/v1/inboxes", map[string]any{"address": uniqueName("status-inbox")})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	inboxID := mustStr(t, body, "id")
	// Note: no cleanup here — we test DELETE within the test.

	t.Run("initial_status_is_active", func(t *testing.T) {
		code, body, err := c.get("/v1/inboxes/" + inboxID)
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 200, body)
		if got := str(body, "status"); got != "active" {
			t.Errorf("expected status=active on creation, got %q", got)
		}
	})

	t.Run("suspend_inbox", func(t *testing.T) {
		code, body, err := c.patch("/v1/inboxes/"+inboxID, map[string]any{"status": "suspended"})
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 200, body)
		if got := str(body, "status"); got != "suspended" {
			t.Errorf("expected status=suspended after patch, got %q", got)
		}
	})

	t.Run("get_shows_suspended", func(t *testing.T) {
		code, body, err := c.get("/v1/inboxes/" + inboxID)
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 200, body)
		if got := str(body, "status"); got != "suspended" {
			t.Errorf("expected GET to show status=suspended, got %q", got)
		}
	})

	t.Run("reactivate_inbox", func(t *testing.T) {
		code, body, err := c.patch("/v1/inboxes/"+inboxID, map[string]any{"status": "active"})
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 200, body)
		if got := str(body, "status"); got != "active" {
			t.Errorf("expected status=active after reactivation, got %q", got)
		}
	})

	t.Run("invalid_status_rejected", func(t *testing.T) {
		code, _, err := c.patch("/v1/inboxes/"+inboxID, map[string]any{"status": "archived"})
		if err != nil {
			t.Fatal(err)
		}
		if code != 400 {
			t.Errorf("invalid status should be 400, got %d", code)
		}
	})

	t.Run("send_to_active_inbox_succeeds", func(t *testing.T) {
		code, body, err := c.post(fmt.Sprintf("/v1/inboxes/%s/messages/send", inboxID), map[string]any{
			"to":        []map[string]any{{"email": "success@simulator.amazonses.com"}},
			"subject":   "Status test " + uniqueName("s"),
			"body_text": "test",
		})
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 201, body)
	})

	t.Run("delete_inbox", func(t *testing.T) {
		code, _, err := c.delete("/v1/inboxes/" + inboxID)
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 204, nil)
	})

	t.Run("deleted_inbox_returns_404", func(t *testing.T) {
		code, _, err := c.get("/v1/inboxes/" + inboxID)
		if err != nil {
			t.Fatal(err)
		}
		if code != 404 {
			t.Errorf("deleted inbox should return 404, got %d", code)
		}
	})
}

// ── nGX-06l: Search edge cases ────────────────────────────────────────────────

// TestSearchEdgeCases verifies that the search endpoint handles degenerate
// inputs gracefully — no 500 errors, correct empty-result shapes.
func TestSearchEdgeCases(t *testing.T) {
	c := newClient(t)

	t.Run("empty_query_returns_400", func(t *testing.T) {
		code, body, err := c.get("/v1/search?q=")
		if err != nil {
			t.Fatal(err)
		}
		if code != 400 {
			t.Errorf("empty query should return 400, got %d: %v", code, body)
		}
	})

	t.Run("missing_q_returns_400", func(t *testing.T) {
		code, body, err := c.get("/v1/search")
		if err != nil {
			t.Fatal(err)
		}
		if code != 400 {
			t.Errorf("missing q should return 400, got %d: %v", code, body)
		}
	})

	t.Run("no_results_returns_empty_array", func(t *testing.T) {
		code, body, err := c.get("/v1/search?q=xyzzy_no_such_term_42_zzz")
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 200, body)
		items := listOf(body, "items")
		// items should be an empty array (not null) — the API initialises to []
		if items == nil {
			t.Error("items should be empty array, not null/missing")
		}
		if len(items) != 0 {
			t.Errorf("expected 0 results for nonsense query, got %d", len(items))
		}
		// has_more should be present and false.
		if _, ok := body["has_more"]; !ok {
			t.Error("response missing has_more field")
		}
	})

	t.Run("sql_special_chars_no_error", func(t *testing.T) {
		// Queries with SQL-sensitive characters should not cause 500.
		for _, q := range []string{"%test%", "it's a test", `"quoted"`, "a; DROP TABLE messages;--"} {
			code, body, err := c.get("/v1/search?q=" + q)
			if err != nil {
				t.Fatal(err)
			}
			if code == 500 {
				t.Errorf("special-char query %q caused 500: %v", q, body)
			}
		}
	})

	t.Run("long_query_no_error", func(t *testing.T) {
		// A 1200-character query should not cause 500.
		longQ := strings.Repeat("searchterm ", 100)
		code, body, err := c.get("/v1/search?q=" + longQ)
		if err != nil {
			t.Fatal(err)
		}
		if code == 500 {
			t.Errorf("long query caused 500: %v", body)
		}
	})

	t.Run("search_before_any_messages_returns_empty", func(t *testing.T) {
		// Create a fresh inbox (no messages). Filter search to it.
		_, ibody, err := c.post("/v1/inboxes", map[string]any{"address": uniqueName("search-empty")})
		if err != nil {
			t.Fatal(err)
		}
		inboxID := mustStr(t, ibody, "id")
		t.Cleanup(func() { c.delete("/v1/inboxes/" + inboxID) }) //nolint

		code, body, err := c.get(fmt.Sprintf("/v1/search?q=anything&inbox_id=%s", inboxID))
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 200, body)
		items := listOf(body, "items")
		if len(items) != 0 {
			t.Errorf("new inbox should have 0 search results, got %d", len(items))
		}
	})
}

// ── nGX-9jo: GET /inboxes?pod_id= filter ─────────────────────────────────────

// TestInboxPodIDFilter verifies that an org-admin key can filter GET /inboxes
// by pod_id and that inboxes from other pods are excluded from the results.
func TestInboxPodIDFilter(t *testing.T) {
	admin := newClient(t)

	// Create two pods.
	code, body, err := admin.post("/v1/pods", map[string]any{"name": "Pod Filter 1", "slug": uniqueName("pod")})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	pod1ID := mustStr(t, body, "id")
	t.Cleanup(func() { admin.delete("/v1/pods/" + pod1ID) }) //nolint

	code, body, err = admin.post("/v1/pods", map[string]any{"name": "Pod Filter 2", "slug": uniqueName("pod")})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	pod2ID := mustStr(t, body, "id")
	t.Cleanup(func() { admin.delete("/v1/pods/" + pod2ID) }) //nolint

	// Create an inbox in pod1.
	code, body, err = admin.post("/v1/inboxes", map[string]any{
		"Address": uniqueName("filter-p1"),
		"PodID":   pod1ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	inbox1ID := mustStr(t, body, "id")
	t.Cleanup(func() { admin.delete("/v1/inboxes/" + inbox1ID) }) //nolint

	// Create an inbox in pod2.
	code, body, err = admin.post("/v1/inboxes", map[string]any{
		"Address": uniqueName("filter-p2"),
		"PodID":   pod2ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	inbox2ID := mustStr(t, body, "id")
	t.Cleanup(func() { admin.delete("/v1/inboxes/" + inbox2ID) }) //nolint

	t.Run("filter_by_pod1_returns_only_pod1_inbox", func(t *testing.T) {
		code, body, err := admin.get("/v1/inboxes?pod_id=" + pod1ID)
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 200, body)
		inboxes := listOf(body, "inboxes")
		foundPod1 := false
		for _, inbox := range inboxes {
			id := str(asMap(inbox), "id")
			if id == inbox2ID {
				t.Errorf("pod2 inbox %s should not appear in ?pod_id=%s results", inbox2ID, pod1ID)
			}
			if id == inbox1ID {
				foundPod1 = true
			}
		}
		if !foundPod1 {
			t.Errorf("pod1 inbox %s not found in ?pod_id=%s results", inbox1ID, pod1ID)
		}
	})

	t.Run("filter_by_pod2_returns_only_pod2_inbox", func(t *testing.T) {
		code, body, err := admin.get("/v1/inboxes?pod_id=" + pod2ID)
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 200, body)
		inboxes := listOf(body, "inboxes")
		foundPod2 := false
		for _, inbox := range inboxes {
			id := str(asMap(inbox), "id")
			if id == inbox1ID {
				t.Errorf("pod1 inbox %s should not appear in ?pod_id=%s results", inbox1ID, pod2ID)
			}
			if id == inbox2ID {
				foundPod2 = true
			}
		}
		if !foundPod2 {
			t.Errorf("pod2 inbox %s not found in ?pod_id=%s results", inbox2ID, pod2ID)
		}
	})

	t.Run("invalid_pod_id_returns_400", func(t *testing.T) {
		code, _, err := admin.get("/v1/inboxes?pod_id=not-a-uuid")
		if err != nil {
			t.Fatal(err)
		}
		if code != 400 {
			t.Errorf("invalid pod_id should return 400, got %d", code)
		}
	})
}

// ── nGX-oou: Inbox single GET is org-scoped ───────────────────────────────────

// TestInboxSingleGetIsOrgScoped documents that GET /v1/inboxes/{id} is org-scoped,
// not pod-scoped. A pod-2 key can directly GET a pod-1 inbox by ID.
// This is intentional: the route is used for cross-pod inbox lookups (e.g. routing).
// Pod isolation is enforced only on the LIST endpoint (WHERE pod_id = $X in SQL).
// If pod-level isolation on GET is desired in the future, add a pod_id check to
// InboxStore.GetByID and update this test accordingly.
func TestInboxSingleGetIsOrgScoped(t *testing.T) {
	admin := newClient(t)

	// Create two pods.
	code, body, err := admin.post("/v1/pods", map[string]any{"name": "Pod X", "slug": uniqueName("pod")})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	pod1ID := mustStr(t, body, "id")
	t.Cleanup(func() { admin.delete("/v1/pods/" + pod1ID) }) //nolint

	code, body, err = admin.post("/v1/pods", map[string]any{"name": "Pod X", "slug": uniqueName("pod")})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	pod2ID := mustStr(t, body, "id")
	t.Cleanup(func() { admin.delete("/v1/pods/" + pod2ID) }) //nolint

	// Create an inbox in pod1.
	code, body, err = admin.post("/v1/inboxes", map[string]any{
		"Address": uniqueName("sgt"),
		"PodID":   pod1ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	pod1InboxID := mustStr(t, body, "id")
	t.Cleanup(func() { admin.delete("/v1/inboxes/" + pod1InboxID) }) //nolint

	// Create a pod2-scoped key with inbox:read.
	code, body, err = admin.post("/v1/keys", map[string]any{
		"name":   uniqueName("pod2-key"),
		"scopes": []string{"inbox:read"},
		"pod_id": pod2ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	pod2KeyID := mustStr(t, body, "id")
	pod2Key := str(body, "key")
	t.Cleanup(func() { admin.delete("/v1/keys/" + pod2KeyID) }) //nolint

	c2 := &client{baseURL: admin.baseURL, apiKey: pod2Key, httpClient: admin.httpClient}

	t.Run("pod2_key_can_get_pod1_inbox_directly", func(t *testing.T) {
		// org-scoped; pod check is intentionally absent on single GET
		code, body, err := c2.get("/v1/inboxes/" + pod1InboxID)
		if err != nil {
			t.Fatal(err)
		}
		if code != 200 {
			t.Errorf("pod-2 key should be able to GET a pod-1 inbox directly (org-scoped): got %d %v", code, body)
		}
	})

	t.Run("pod2_key_cannot_list_pod1_inbox", func(t *testing.T) {
		// pod isolation on list is enforced (WHERE pod_id = $X in SQL)
		code, body, err := c2.get("/v1/inboxes")
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 200, body)
		for _, inbox := range listOf(body, "inboxes") {
			if str(asMap(inbox), "id") == pod1InboxID {
				t.Errorf("pod-2-scoped key should not see pod-1 inbox in list, but found it: %v", inbox)
			}
		}
	})
}

// ── nGX-mde: Parent-path integrity ───────────────────────────────────────────

// TestParentPathIntegrity verifies that nested routes check parent-path
// ownership — e.g. a thread belonging to inbox1 cannot be accessed via
// inbox2's path, and a message belonging to thread1 cannot be accessed via
// thread2's path.
func TestParentPathIntegrity(t *testing.T) {
	c := newClient(t)

	// Create two inboxes.
	code, body, err := c.post("/v1/inboxes", map[string]any{"address": uniqueName("ppi-inbox1")})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	inbox1ID := mustStr(t, body, "id")
	t.Cleanup(func() { c.delete("/v1/inboxes/" + inbox1ID) }) //nolint

	code, body, err = c.post("/v1/inboxes", map[string]any{"address": uniqueName("ppi-inbox2")})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	inbox2ID := mustStr(t, body, "id")
	t.Cleanup(func() { c.delete("/v1/inboxes/" + inbox2ID) }) //nolint

	// Send a message to inbox1 — creates thread1 containing message1.
	code, body, err = c.post(fmt.Sprintf("/v1/inboxes/%s/messages/send", inbox1ID), map[string]any{
		"to":        []map[string]any{{"email": "ppi-test@example.com"}},
		"subject":   "Parent path integrity test " + uniqueName("t"),
		"body_text": "Testing parent-path integrity",
	})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	thread1ID := mustStr(t, body, "thread_id")
	message1ID := mustStr(t, body, "id")

	// Create a draft in inbox1.
	code, body, err = c.post(fmt.Sprintf("/v1/inboxes/%s/drafts", inbox1ID), map[string]any{
		"to":        []map[string]any{{"email": "ppi-draft@example.com"}},
		"subject":   "Parent path integrity draft",
		"body_text": "Draft for ppi test",
	})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	draft1ID := mustStr(t, body, "id")

	t.Run("thread_wrong_inbox_returns_404", func(t *testing.T) {
		// thread1 belongs to inbox1; accessing it via inbox2 should be 404.
		code, body, err := c.get(fmt.Sprintf("/v1/inboxes/%s/threads/%s", inbox2ID, thread1ID))
		if err != nil {
			t.Fatal(err)
		}
		if code != 404 {
			t.Errorf("expected 404 for thread1 accessed via inbox2, got %d: %v", code, body)
		}
	})

	t.Run("message_wrong_thread_returns_404", func(t *testing.T) {
		// Send a second message to inbox1 — creates thread2.
		code, body, err := c.post(fmt.Sprintf("/v1/inboxes/%s/messages/send", inbox1ID), map[string]any{
			"to":        []map[string]any{{"email": "ppi-test2@example.com"}},
			"subject":   "Parent path integrity test 2 " + uniqueName("t"),
			"body_text": "Thread 2 for ppi test",
		})
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 201, body)
		thread2ID := mustStr(t, body, "thread_id")

		// message1 is in thread1; accessing it via thread2 should be 404.
		code, body, err = c.get(fmt.Sprintf("/v1/inboxes/%s/threads/%s/messages/%s", inbox1ID, thread2ID, message1ID))
		if err != nil {
			t.Fatal(err)
		}
		if code != 404 {
			t.Errorf("expected 404 for message1 accessed via thread2, got %d: %v", code, body)
		}
	})

	t.Run("draft_wrong_inbox_returns_404", func(t *testing.T) {
		// draft1 belongs to inbox1; accessing it via inbox2 should be 404.
		code, body, err := c.get(fmt.Sprintf("/v1/inboxes/%s/drafts/%s", inbox2ID, draft1ID))
		if err != nil {
			t.Fatal(err)
		}
		if code != 404 {
			t.Errorf("expected 404 for draft1 accessed via inbox2, got %d: %v", code, body)
		}
	})
}

// ── nGX-29n: Thread PATCH multi-field ────────────────────────────────────────

// TestThreadPatchMultiField verifies that PATCH /threads/{id} applies all
// provided fields (is_read, is_starred) in a single request, not just the first.
func TestThreadPatchMultiField(t *testing.T) {
	c := newClient(t)

	// Create inbox and send a message to get a thread.
	code, body, err := c.post("/v1/inboxes", map[string]any{"address": uniqueName("tpmf")})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	inboxID := mustStr(t, body, "id")
	t.Cleanup(func() { c.delete("/v1/inboxes/" + inboxID) }) //nolint

	code, body, err = c.post(fmt.Sprintf("/v1/inboxes/%s/messages/send", inboxID), map[string]any{
		"to":        []map[string]any{{"email": "success@simulator.amazonses.com"}},
		"subject":   "Multi-field patch test",
		"body_text": "body",
	})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	threadID := mustStr(t, body, "thread_id")

	t.Run("both_is_read_and_is_starred_applied", func(t *testing.T) {
		// PATCH with both fields simultaneously.
		code, body, err := c.patch(fmt.Sprintf("/v1/inboxes/%s/threads/%s", inboxID, threadID), map[string]any{
			"is_read":    true,
			"is_starred": true,
		})
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 200, body)

		// GET the thread and verify both fields were applied.
		code, body, err = c.get(fmt.Sprintf("/v1/inboxes/%s/threads/%s", inboxID, threadID))
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 200, body)
		if isRead, _ := body["is_read"].(bool); !isRead {
			t.Errorf("expected is_read=true after multi-field PATCH, got %v", body["is_read"])
		}
		if isStarred, _ := body["is_starred"].(bool); !isStarred {
			t.Errorf("expected is_starred=true after multi-field PATCH, got %v", body["is_starred"])
		}
	})

	t.Run("reset_both_fields", func(t *testing.T) {
		code, body, err := c.patch(fmt.Sprintf("/v1/inboxes/%s/threads/%s", inboxID, threadID), map[string]any{
			"is_read":    false,
			"is_starred": false,
		})
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 200, body)

		code, body, err = c.get(fmt.Sprintf("/v1/inboxes/%s/threads/%s", inboxID, threadID))
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 200, body)
		if isRead, _ := body["is_read"].(bool); isRead {
			t.Errorf("expected is_read=false after reset, got %v", body["is_read"])
		}
		if isStarred, _ := body["is_starred"].(bool); isStarred {
			t.Errorf("expected is_starred=false after reset, got %v", body["is_starred"])
		}
	})
}

// ── nGX-8xw: GET /inboxes ?limit= pagination ─────────────────────────────────

// TestInboxListPagination verifies that GET /inboxes respects the ?limit= param
// and returns next_cursor when more results exist.
func TestInboxListPagination(t *testing.T) {
	c := newClient(t)

	// Create 3 inboxes with known addresses.
	var ids []string
	for i := 0; i < 3; i++ {
		code, body, err := c.post("/v1/inboxes", map[string]any{"address": uniqueName("ilp")})
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 201, body)
		id := mustStr(t, body, "id")
		ids = append(ids, id)
		t.Cleanup(func() { c.delete("/v1/inboxes/" + id) }) //nolint
	}

	// First page: limit=2.
	code, body, err := c.get("/v1/inboxes?limit=2")
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 200, body)
	page1 := listOf(body, "inboxes")
	if len(page1) != 2 {
		t.Fatalf("expected 2 inboxes with limit=2, got %d", len(page1))
	}
	cursor := str(body, "next_cursor")
	if cursor == "" {
		t.Fatal("expected next_cursor with limit=2 when more inboxes exist")
	}

	// Second page: follow cursor.
	code, body, err = c.get("/v1/inboxes?cursor=" + cursor)
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 200, body)
	page2 := listOf(body, "inboxes")
	if len(page2) == 0 {
		t.Fatal("expected at least 1 inbox on second page")
	}

	// No duplicate IDs across pages.
	seen := map[string]bool{}
	for _, item := range page1 {
		seen[str(asMap(item), "id")] = true
	}
	for _, item := range page2 {
		id := str(asMap(item), "id")
		if seen[id] {
			t.Errorf("duplicate inbox id %s across pages", id)
		}
	}
}

// ── nGX-r7e: Draft list pagination ───────────────────────────────────────────

// TestDraftListPagination verifies limit/cursor pagination on GET /drafts.
func TestDraftListPagination(t *testing.T) {
	c := newClient(t)

	code, body, err := c.post("/v1/inboxes", map[string]any{"address": uniqueName("dlp")})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	inboxID := mustStr(t, body, "id")
	t.Cleanup(func() { c.delete("/v1/inboxes/" + inboxID) }) //nolint

	// Create 3 drafts.
	for i := 0; i < 3; i++ {
		code, body, err := c.post(fmt.Sprintf("/v1/inboxes/%s/drafts", inboxID), map[string]any{
			"to":        []map[string]any{{"email": "x@example.com"}},
			"subject":   uniqueName("draft-subject"),
			"body_text": "pagination test",
		})
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 201, body)
	}

	// First page: limit=2.
	code, body, err = c.get(fmt.Sprintf("/v1/inboxes/%s/drafts?limit=2", inboxID))
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 200, body)
	page1 := listOf(body, "drafts")
	if len(page1) != 2 {
		t.Fatalf("expected 2 drafts with limit=2, got %d", len(page1))
	}
	cursor := str(body, "next_cursor")
	if cursor == "" {
		t.Fatal("expected next_cursor when more drafts exist")
	}

	// Second page.
	code, body, err = c.get(fmt.Sprintf("/v1/inboxes/%s/drafts?cursor=%s", inboxID, cursor))
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 200, body)
	page2 := listOf(body, "drafts")
	if len(page2) == 0 {
		t.Fatal("expected at least 1 draft on second page")
	}

	// No duplicates.
	seen := map[string]bool{}
	for _, item := range page1 {
		seen[str(asMap(item), "id")] = true
	}
	for _, item := range page2 {
		id := str(asMap(item), "id")
		if seen[id] {
			t.Errorf("duplicate draft id %s across pages", id)
		}
	}
}

// ── nGX-43o: Pod slug validation ─────────────────────────────────────────────

// TestPodSlugValidation verifies that POST /pods rejects slugs that don't
// match ^[a-z0-9-]+$.
func TestPodSlugValidation(t *testing.T) {
	c := newClient(t)

	t.Run("invalid_slug_uppercase_returns_400", func(t *testing.T) {
		code, body, err := c.post("/v1/pods", map[string]any{
			"name": "Test Pod",
			"slug": "UPPERCASE",
		})
		if err != nil {
			t.Fatal(err)
		}
		if code != 400 {
			t.Errorf("expected 400 for uppercase slug, got %d: %v", code, body)
		}
	})

	t.Run("invalid_slug_special_chars_returns_400", func(t *testing.T) {
		code, body, err := c.post("/v1/pods", map[string]any{
			"name": "Test Pod",
			"slug": "my pod!",
		})
		if err != nil {
			t.Fatal(err)
		}
		if code != 400 {
			t.Errorf("expected 400 for slug with spaces/special chars, got %d: %v", code, body)
		}
	})

	t.Run("valid_slug_accepted", func(t *testing.T) {
		slug := uniqueName("valid-slug")
		code, body, err := c.post("/v1/pods", map[string]any{
			"name": "Valid Slug Pod",
			"slug": slug,
		})
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 201, body)
		id := mustStr(t, body, "id")
		t.Cleanup(func() { c.delete("/v1/pods/" + id) }) //nolint
	})
}

// ── nGX-dqd: Draft recipient validation ──────────────────────────────────────

// TestDraftRecipientValidation verifies that draft create/update enforce
// recipient constraints: at least one To address, valid email format.
func TestDraftRecipientValidation(t *testing.T) {
	c := newClient(t)

	code, body, err := c.post("/v1/inboxes", map[string]any{"address": uniqueName("drv")})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	inboxID := mustStr(t, body, "id")
	t.Cleanup(func() { c.delete("/v1/inboxes/" + inboxID) }) //nolint

	t.Run("missing_to_returns_400", func(t *testing.T) {
		code, body, err := c.post(fmt.Sprintf("/v1/inboxes/%s/drafts", inboxID), map[string]any{
			"subject":   "No recipient",
			"body_text": "body",
		})
		if err != nil {
			t.Fatal(err)
		}
		if code != 400 {
			t.Errorf("expected 400 for draft with no recipients, got %d: %v", code, body)
		}
	})

	t.Run("invalid_email_in_to_returns_400", func(t *testing.T) {
		code, body, err := c.post(fmt.Sprintf("/v1/inboxes/%s/drafts", inboxID), map[string]any{
			"to":        []map[string]any{{"email": "not-an-email"}},
			"subject":   "Bad email",
			"body_text": "body",
		})
		if err != nil {
			t.Fatal(err)
		}
		if code != 400 {
			t.Errorf("expected 400 for invalid email in to, got %d: %v", code, body)
		}
	})

	t.Run("valid_recipient_accepted", func(t *testing.T) {
		code, body, err := c.post(fmt.Sprintf("/v1/inboxes/%s/drafts", inboxID), map[string]any{
			"to":        []map[string]any{{"email": "valid@example.com"}},
			"subject":   "Valid draft",
			"body_text": "body",
		})
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 201, body)
		draftID := mustStr(t, body, "id")
		t.Cleanup(func() {
			c.delete(fmt.Sprintf("/v1/inboxes/%s/drafts/%s", inboxID, draftID))
		}) //nolint

		// Update with invalid cc email should fail.
		t.Run("invalid_cc_on_update_returns_400", func(t *testing.T) {
			code, body, err := c.patch(fmt.Sprintf("/v1/inboxes/%s/drafts/%s", inboxID, draftID), map[string]any{
				"cc": []map[string]any{{"email": "bad-email"}},
			})
			if err != nil {
				t.Fatal(err)
			}
			if code != 400 {
				t.Errorf("expected 400 for invalid cc email on update, got %d: %v", code, body)
			}
		})
	})
}

// ── nGX-5jg: Draft scheduled_at round-trip ───────────────────────────────────

// TestDraftScheduledAtRoundTrip verifies that scheduled_at is stored and returned
// correctly in GET /drafts/{id} before the scheduler runs.
func TestDraftScheduledAtRoundTrip(t *testing.T) {
	c := newClient(t)

	_, inboxesBody, err := c.get("/v1/inboxes")
	if err != nil {
		t.Fatal(err)
	}
	inboxes := listOf(inboxesBody, "inboxes")
	if len(inboxes) == 0 {
		t.Skip("no inboxes available")
	}
	inboxID := str(asMap(inboxes[0]), "id")
	inboxEmail := str(asMap(inboxes[0]), "email")

	// 5 minutes in the future so the scheduler won't process it during the test.
	futureTime := time.Now().UTC().Add(5 * time.Minute).Truncate(time.Second)
	scheduledAt := futureTime.Format(time.RFC3339)

	code, body, err := c.post(fmt.Sprintf("/v1/inboxes/%s/drafts", inboxID), map[string]any{
		"to":           []map[string]any{{"email": inboxEmail}},
		"subject":      uniqueName("sched-draft"),
		"body_text":    "scheduled draft test",
		"scheduled_at": scheduledAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	draftID := mustStr(t, body, "id")
	t.Cleanup(func() {
		c.delete(fmt.Sprintf("/v1/inboxes/%s/drafts/%s", inboxID, draftID))
	})

	t.Run("scheduled_at_in_response", func(t *testing.T) {
		if str(body, "scheduled_at") == "" {
			t.Fatal("scheduled_at missing from POST response")
		}
	})

	t.Run("scheduled_at_round_trips_via_get", func(t *testing.T) {
		code, got, err := c.get(fmt.Sprintf("/v1/inboxes/%s/drafts/%s", inboxID, draftID))
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 200, got)
		gotAt := str(got, "scheduled_at")
		if gotAt == "" {
			t.Fatal("scheduled_at missing from GET response")
		}
		// Parse both as RFC3339 and compare to second precision.
		gotParsed, err := time.Parse(time.RFC3339, gotAt)
		if err != nil {
			// try RFC3339Nano
			gotParsed, err = time.Parse(time.RFC3339Nano, gotAt)
			if err != nil {
				t.Fatalf("scheduled_at not RFC3339: %q", gotAt)
			}
		}
		if !gotParsed.Truncate(time.Second).Equal(futureTime) {
			t.Errorf("scheduled_at mismatch: got %v, want %v", gotParsed.Truncate(time.Second), futureTime)
		}
	})

	t.Run("review_status_is_pending_before_scheduler", func(t *testing.T) {
		code, got, err := c.get(fmt.Sprintf("/v1/inboxes/%s/drafts/%s", inboxID, draftID))
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 200, got)
		if s := str(got, "review_status"); s != "pending" {
			t.Errorf("expected review_status=pending, got %q", s)
		}
	})
}

// ── nGX-ad9: Pod PATCH settings field ────────────────────────────────────────

// TestPodPatchSettings verifies that settings can be updated via PATCH /pods/{id}.
func TestPodPatchSettings(t *testing.T) {
	c := newClient(t)

	code, body, err := c.post("/v1/pods", map[string]any{
		"name": uniqueName("pod-settings"),
		"slug": uniqueName("pod-settings"),
	})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	podID := mustStr(t, body, "id")
	t.Cleanup(func() {
		c.delete("/v1/pods/" + podID)
	})

	t.Run("patch_settings_field", func(t *testing.T) {
		code, got, err := c.patch("/v1/pods/"+podID, map[string]any{
			"name":     str(body, "name"),
			"settings": map[string]any{"theme": "dark", "notifications": true},
		})
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 200, got)
	})

	t.Run("settings_persisted_in_get", func(t *testing.T) {
		code, got, err := c.get("/v1/pods/" + podID)
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 200, got)
		settings, ok := got["settings"].(map[string]any)
		if !ok {
			t.Fatalf("settings field not a map: %T %v", got["settings"], got["settings"])
		}
		if settings["theme"] != "dark" {
			t.Errorf("expected settings.theme=dark, got %v", settings["theme"])
		}
	})

	t.Run("patch_without_settings_preserves_existing", func(t *testing.T) {
		// PATCH with only name — settings should remain unchanged.
		code, _, err := c.patch("/v1/pods/"+podID, map[string]any{
			"name": str(body, "name"),
		})
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 200, map[string]any{})

		code, got, err := c.get("/v1/pods/" + podID)
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 200, got)
		settings, ok := got["settings"].(map[string]any)
		if !ok {
			t.Fatalf("settings field not a map after name-only patch: %T", got["settings"])
		}
		if settings["theme"] != "dark" {
			t.Errorf("settings should be preserved after name-only patch, got %v", settings)
		}
	})
}

// ── nGX-egf: Label hex color validation ──────────────────────────────────────

// TestLabelColorValidation verifies that invalid color strings are rejected.
func TestLabelColorValidation(t *testing.T) {
	c := newClient(t)

	t.Run("non_hex_string_returns_400", func(t *testing.T) {
		code, body, err := c.post("/v1/labels", map[string]any{
			"name":  uniqueName("label-color"),
			"color": "not-a-hex",
		})
		if err != nil {
			t.Fatal(err)
		}
		if code != 400 {
			t.Errorf("expected 400 for non-hex color, got %d: %v", code, body)
		}
	})

	t.Run("invalid_hex_chars_returns_400", func(t *testing.T) {
		code, body, err := c.post("/v1/labels", map[string]any{
			"name":  uniqueName("label-color"),
			"color": "#gg0000",
		})
		if err != nil {
			t.Fatal(err)
		}
		if code != 400 {
			t.Errorf("expected 400 for invalid hex chars, got %d: %v", code, body)
		}
	})

	t.Run("valid_hex_color_returns_201", func(t *testing.T) {
		name := uniqueName("label-color-valid")
		code, body, err := c.post("/v1/labels", map[string]any{
			"name":  name,
			"color": "#ff5733",
		})
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 201, body)
		labelID := mustStr(t, body, "id")
		t.Cleanup(func() {
			c.delete("/v1/labels/" + labelID)
		})
		if str(body, "color") != "#ff5733" {
			t.Errorf("expected color=#ff5733, got %q", str(body, "color"))
		}
	})

	t.Run("update_with_invalid_color_returns_400", func(t *testing.T) {
		// Create valid label first.
		name := uniqueName("label-update-color")
		code, body, err := c.post("/v1/labels", map[string]any{
			"name":  name,
			"color": "#aabbcc",
		})
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 201, body)
		labelID := mustStr(t, body, "id")
		t.Cleanup(func() {
			c.delete("/v1/labels/" + labelID)
		})

		code, body, err = c.patch("/v1/labels/"+labelID, map[string]any{
			"color": "badcolor",
		})
		if err != nil {
			t.Fatal(err)
		}
		if code != 400 {
			t.Errorf("expected 400 for invalid color on update, got %d: %v", code, body)
		}
	})
}

// ── nGX-hwl: Thread PATCH empty body ─────────────────────────────────────────

// TestThreadPatchEmptyBody documents and asserts the behavior of PATCH /threads/{id}
// with no recognized fields — the handler returns 400 "at least one field required".
func TestThreadPatchEmptyBody(t *testing.T) {
	c := newClient(t)

	// Create an inbox and send a message to get a thread.
	code, body, err := c.post("/v1/inboxes", map[string]any{"address": uniqueName("hwl")})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	inboxID := mustStr(t, body, "id")
	t.Cleanup(func() { c.delete("/v1/inboxes/" + inboxID) })

	code, body, err = c.post(fmt.Sprintf("/v1/inboxes/%s/messages/send", inboxID), map[string]any{
		"to":        []map[string]any{{"email": "hwl-test@example.com"}},
		"subject":   uniqueName("hwl-thread"),
		"body_text": "thread patch empty body test",
	})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	threadID := mustStr(t, body, "thread_id")

	t.Run("empty_body_returns_400", func(t *testing.T) {
		code, body, err := c.patch(fmt.Sprintf("/v1/inboxes/%s/threads/%s", inboxID, threadID), map[string]any{})
		if err != nil {
			t.Fatal(err)
		}
		if code != 400 {
			t.Errorf("expected 400 for empty PATCH body, got %d: %v", code, body)
		}
	})

	t.Run("unknown_field_only_returns_400", func(t *testing.T) {
		code, body, err := c.patch(fmt.Sprintf("/v1/inboxes/%s/threads/%s", inboxID, threadID), map[string]any{
			"unknown_field": "some_value",
		})
		if err != nil {
			t.Fatal(err)
		}
		if code != 400 {
			t.Errorf("expected 400 for unrecognized-only PATCH body, got %d: %v", code, body)
		}
	})
}

// ── nGX-lc0: GET /keys pagination ────────────────────────────────────────────

// TestKeysListPagination verifies that GET /keys supports cursor-based pagination.
func TestKeysListPagination(t *testing.T) {
	c := newClient(t)

	// Create enough keys to exceed limit=2 (need ≥3).
	const total = 3
	var keyIDs []string
	for i := 0; i < total; i++ {
		code, body, err := c.post("/v1/keys", map[string]any{
			"name":   fmt.Sprintf("pagination-test-key-%d-%d", time.Now().UnixNano(), i),
			"scopes": []string{"inbox:read"},
		})
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 201, body)
		keyIDs = append(keyIDs, mustStr(t, body, "id"))
	}
	t.Cleanup(func() {
		for _, id := range keyIDs {
			c.delete("/v1/keys/" + id)
		}
	})

	t.Run("limit_returns_fewer_items_and_cursor", func(t *testing.T) {
		code, body, err := c.get("/v1/keys?limit=2")
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 200, body)
		keys := listOf(body, "keys")
		if len(keys) != 2 {
			t.Fatalf("expected 2 keys, got %d", len(keys))
		}
		if str(body, "next_cursor") == "" {
			t.Fatal("expected next_cursor to be set when more keys exist")
		}
	})

	t.Run("cursor_returns_next_page_no_duplicates", func(t *testing.T) {
		code, body, err := c.get("/v1/keys?limit=2")
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 200, body)
		page1Keys := listOf(body, "keys")
		cursor := str(body, "next_cursor")
		if cursor == "" {
			t.Skip("no cursor returned; not enough keys to paginate")
		}

		seen := map[string]bool{}
		for _, k := range page1Keys {
			seen[str(asMap(k), "id")] = true
		}

		code, body, err = c.get("/v1/keys?limit=2&cursor=" + cursor)
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 200, body)
		page2Keys := listOf(body, "keys")
		if len(page2Keys) == 0 {
			t.Fatal("expected at least one key on second page")
		}
		for _, k := range page2Keys {
			id := str(asMap(k), "id")
			if seen[id] {
				t.Errorf("duplicate key %s found across pages", id)
			}
		}
	})
}
