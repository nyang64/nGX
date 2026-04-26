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

// helper: test that a string contains a substring (for validation error messages)
func containsStr(t *testing.T, haystack, needle string) bool {
	t.Helper()
	return strings.Contains(haystack, needle)
}
