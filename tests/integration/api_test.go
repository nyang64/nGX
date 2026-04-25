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

// TestOrg verifies GET and PATCH /v1/org.
func TestOrg(t *testing.T) {
	c := newClient(t)

	t.Run("get", func(t *testing.T) {
		code, body, err := c.get("/v1/org")
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 200, body)
		if str(body, "id") == "" {
			t.Fatal("org missing id")
		}
	})

	t.Run("patch", func(t *testing.T) {
		code, body, err := c.patch("/v1/org", map[string]any{"name": "nyklabs"})
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 200, body)
	})
}

// TestPods verifies full CRUD on /v1/pods.
func TestPods(t *testing.T) {
	c := newClient(t)
	slug := uniqueName("test-pod")

	t.Run("create", func(t *testing.T) {
		code, body, err := c.post("/v1/pods", map[string]any{
			"name": "Test Pod", "slug": slug, "description": "integration test pod",
		})
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 201, body)
		id := mustStr(t, body, "id")

		t.Cleanup(func() { c.delete("/v1/pods/" + id) }) //nolint

		t.Run("get", func(t *testing.T) {
			code, body, err := c.get("/v1/pods/" + id)
			if err != nil {
				t.Fatal(err)
			}
			mustCode(t, code, 200, body)
			if str(body, "id") != id {
				t.Fatalf("pod id mismatch: %s", str(body, "id"))
			}
		})

		t.Run("list", func(t *testing.T) {
			code, body, err := c.get("/v1/pods")
			if err != nil {
				t.Fatal(err)
			}
			mustCode(t, code, 200, body)
			pods := listOf(body, "pods")
			found := false
			for _, p := range pods {
				if str(asMap(p), "id") == id {
					found = true
					break
				}
			}
			if !found {
				t.Fatal("created pod not found in list")
			}
		})

		t.Run("patch", func(t *testing.T) {
			code, body, err := c.patch("/v1/pods/"+id, map[string]any{"name": "Updated Pod"})
			if err != nil {
				t.Fatal(err)
			}
			mustCode(t, code, 200, body)
			if str(body, "name") != "Updated Pod" {
				t.Fatalf("name not updated: %s", str(body, "name"))
			}
		})

		t.Run("delete", func(t *testing.T) {
			code, _, err := c.delete("/v1/pods/" + id)
			if err != nil {
				t.Fatal(err)
			}
			mustCode(t, code, 204, nil)
		})
	})
}

// TestAPIKeys verifies full CRUD on /v1/keys.
func TestAPIKeys(t *testing.T) {
	c := newClient(t)

	code, body, err := c.post("/v1/keys", map[string]any{
		"name": uniqueName("test-key"), "scopes": []string{"inbox:read"},
	})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	id := mustStr(t, body, "id")
	if str(body, "key") == "" {
		t.Fatal("key plaintext not returned on create")
	}
	t.Cleanup(func() { c.delete("/v1/keys/" + id) }) //nolint

	t.Run("get", func(t *testing.T) {
		code, body, err := c.get("/v1/keys/" + id)
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 200, body)
	})

	t.Run("list", func(t *testing.T) {
		code, body, err := c.get("/v1/keys")
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 200, body)
	})

	t.Run("delete", func(t *testing.T) {
		code, _, err := c.delete("/v1/keys/" + id)
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 204, nil)
	})
}

// TestLabels verifies full CRUD on /v1/labels.
func TestLabels(t *testing.T) {
	c := newClient(t)

	code, body, err := c.post("/v1/labels", map[string]any{
		"name": uniqueName("lbl"), "color": "#aabbcc",
	})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	id := mustStr(t, body, "id")
	t.Cleanup(func() { c.delete("/v1/labels/" + id) }) //nolint

	t.Run("list", func(t *testing.T) {
		code, body, err := c.get("/v1/labels")
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 200, body)
	})

	t.Run("patch", func(t *testing.T) {
		code, body, err := c.patch("/v1/labels/"+id, map[string]any{"name": "renamed"})
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 200, body)
	})

	t.Run("delete", func(t *testing.T) {
		code, _, err := c.delete("/v1/labels/" + id)
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 204, nil)
	})
}

// TestInboxes verifies full CRUD on /v1/inboxes.
func TestInboxes(t *testing.T) {
	c := newClient(t)
	addr := uniqueName("inttest")

	code, body, err := c.post("/v1/inboxes", map[string]any{"address": addr})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	id := mustStr(t, body, "id")
	t.Cleanup(func() { c.delete("/v1/inboxes/" + id) }) //nolint

	if str(body, "email") == "" || str(body, "email") == "@" {
		t.Fatalf("inbox email invalid: %q", str(body, "email"))
	}

	t.Run("get", func(t *testing.T) {
		code, body, err := c.get("/v1/inboxes/" + id)
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 200, body)
	})

	t.Run("patch", func(t *testing.T) {
		code, body, err := c.patch("/v1/inboxes/"+id, map[string]any{"display_name": "Test Inbox"})
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 200, body)
		if str(body, "display_name") != "Test Inbox" {
			t.Fatalf("display_name not updated: %s", str(body, "display_name"))
		}
	})

	t.Run("list", func(t *testing.T) {
		code, body, err := c.get("/v1/inboxes")
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 200, body)
	})

	t.Run("delete", func(t *testing.T) {
		code, _, err := c.delete("/v1/inboxes/" + id)
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 204, nil)
	})
}

// TestThreadsAndMessages verifies thread/message reads after sending a message.
func TestThreadsAndMessages(t *testing.T) {
	c := newClient(t)

	// Create a dedicated inbox
	code, body, err := c.post("/v1/inboxes", map[string]any{"address": uniqueName("thr")})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	inboxID := mustStr(t, body, "id")
	t.Cleanup(func() { c.delete("/v1/inboxes/" + inboxID) }) //nolint

	// Send a message to create a thread
	code, body, err = c.post(fmt.Sprintf("/v1/inboxes/%s/messages/send", inboxID), map[string]any{
		"to":        []map[string]any{{"email": "thread-test@example.com"}},
		"subject":   "Thread test " + uniqueName("subj"),
		"body_text": "Integration test message",
	})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	msgID := mustStr(t, body, "id")
	threadID := mustStr(t, body, "thread_id")

	t.Run("list_threads", func(t *testing.T) {
		code, body, err := c.get(fmt.Sprintf("/v1/inboxes/%s/threads", inboxID))
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 200, body)
		threads := listOf(body, "threads")
		if len(threads) == 0 {
			t.Fatal("expected at least one thread")
		}
	})

	t.Run("get_thread", func(t *testing.T) {
		code, body, err := c.get(fmt.Sprintf("/v1/inboxes/%s/threads/%s", inboxID, threadID))
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 200, body)
		if str(body, "id") != threadID {
			t.Fatalf("thread id mismatch")
		}
	})

	t.Run("patch_thread", func(t *testing.T) {
		code, body, err := c.patch(
			fmt.Sprintf("/v1/inboxes/%s/threads/%s", inboxID, threadID),
			map[string]any{"is_read": true, "is_starred": true},
		)
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 200, body)
	})

	t.Run("list_messages", func(t *testing.T) {
		code, body, err := c.get(fmt.Sprintf("/v1/inboxes/%s/threads/%s/messages", inboxID, threadID))
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 200, body)
		msgs := listOf(body, "messages")
		if len(msgs) == 0 {
			t.Fatal("expected at least one message")
		}
	})

	t.Run("get_message", func(t *testing.T) {
		code, body, err := c.get(fmt.Sprintf("/v1/inboxes/%s/threads/%s/messages/%s", inboxID, threadID, msgID))
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 200, body)
		if str(body, "id") != msgID {
			t.Fatalf("message id mismatch")
		}
	})
}

// TestDrafts verifies draft CRUD + approve flow.
func TestDrafts(t *testing.T) {
	c := newClient(t)

	code, body, err := c.post("/v1/inboxes", map[string]any{"address": uniqueName("dft")})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	inboxID := mustStr(t, body, "id")
	t.Cleanup(func() { c.delete("/v1/inboxes/" + inboxID) }) //nolint

	// Create draft
	code, body, err = c.post(fmt.Sprintf("/v1/inboxes/%s/drafts", inboxID), map[string]any{
		"to":        []map[string]any{{"email": "draft-test@example.com"}},
		"subject":   "Draft test",
		"body_text": "Draft body",
	})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	draftID := mustStr(t, body, "id")
	if str(body, "review_status") != "pending" {
		t.Fatalf("expected review_status=pending, got %s", str(body, "review_status"))
	}

	t.Run("get", func(t *testing.T) {
		code, body, err := c.get(fmt.Sprintf("/v1/inboxes/%s/drafts/%s", inboxID, draftID))
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 200, body)
	})

	t.Run("list", func(t *testing.T) {
		code, body, err := c.get(fmt.Sprintf("/v1/inboxes/%s/drafts", inboxID))
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 200, body)
		if len(listOf(body, "drafts")) == 0 {
			t.Fatal("expected draft in list")
		}
	})

	t.Run("patch", func(t *testing.T) {
		code, body, err := c.patch(
			fmt.Sprintf("/v1/inboxes/%s/drafts/%s", inboxID, draftID),
			map[string]any{"subject": "Updated Draft"},
		)
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 200, body)
		if str(body, "subject") != "Updated Draft" {
			t.Fatalf("subject not updated: %s", str(body, "subject"))
		}
	})

	t.Run("approve_sends_message", func(t *testing.T) {
		code, body, err := c.post(fmt.Sprintf("/v1/inboxes/%s/drafts/%s/approve", inboxID, draftID), nil)
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 200, body)
		// After approve, draft should be gone — list should be empty
		_, listBody, _ := c.get(fmt.Sprintf("/v1/inboxes/%s/drafts", inboxID))
		for _, d := range listOf(listBody, "drafts") {
			if str(asMap(d), "id") == draftID {
				t.Fatal("draft still present after approve")
			}
		}
	})
}

// TestDraftRejection verifies the draft reject flow:
// create draft → reject with reason → draft status transitions to rejected.
func TestDraftRejection(t *testing.T) {
	c := newClient(t)

	code, body, err := c.post("/v1/inboxes", map[string]any{"address": uniqueName("rej")})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	inboxID := mustStr(t, body, "id")
	t.Cleanup(func() { c.delete("/v1/inboxes/" + inboxID) }) //nolint

	// Create a pending draft.
	code, body, err = c.post(fmt.Sprintf("/v1/inboxes/%s/drafts", inboxID), map[string]any{
		"to":        []map[string]any{{"email": "reject-test@example.com"}},
		"subject":   "Draft to reject",
		"body_text": "This draft will be rejected",
	})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	draftID := mustStr(t, body, "id")
	if str(body, "review_status") != "pending" {
		t.Fatalf("expected review_status=pending, got %s", str(body, "review_status"))
	}

	// Reject the draft.
	code, body, err = c.post(
		fmt.Sprintf("/v1/inboxes/%s/drafts/%s/reject", inboxID, draftID),
		map[string]any{"reason": "Content policy violation"},
	)
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 200, body)

	t.Run("status_is_rejected", func(t *testing.T) {
		if str(body, "review_status") != "rejected" {
			t.Fatalf("expected review_status=rejected after reject, got %s", str(body, "review_status"))
		}
	})

	t.Run("get_returns_rejected_draft", func(t *testing.T) {
		code, body, err := c.get(fmt.Sprintf("/v1/inboxes/%s/drafts/%s", inboxID, draftID))
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 200, body)
		if str(body, "review_status") != "rejected" {
			t.Fatalf("expected review_status=rejected on GET, got %s", str(body, "review_status"))
		}
	})

	t.Run("rejected_draft_not_in_pending_list", func(t *testing.T) {
		_, listBody, err := c.get(fmt.Sprintf("/v1/inboxes/%s/drafts?status=pending", inboxID))
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range listOf(listBody, "drafts") {
			if str(asMap(d), "id") == draftID {
				t.Fatal("rejected draft still appears in pending list")
			}
		}
	})

	t.Run("cannot_approve_rejected_draft", func(t *testing.T) {
		code, _, err := c.post(fmt.Sprintf("/v1/inboxes/%s/drafts/%s/approve", inboxID, draftID), nil)
		if err != nil {
			t.Fatal(err)
		}
		if code == 200 {
			t.Fatal("expected approval to fail for a rejected draft, got 200")
		}
	})
}

// TestWebhookCRUD verifies webhook create/read/update/delete.
func TestWebhookCRUD(t *testing.T) {
	c := newClient(t)

	code, body, err := c.post("/v1/webhooks", map[string]any{
		"url":       "https://webhook.site/test-crud",
		"events":    []string{"message.received"},
		"is_active": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	id := mustStr(t, body, "id")
	t.Cleanup(func() { c.delete("/v1/webhooks/" + id) }) //nolint

	t.Run("get", func(t *testing.T) {
		code, body, err := c.get("/v1/webhooks/" + id)
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 200, body)
	})

	t.Run("list", func(t *testing.T) {
		code, body, err := c.get("/v1/webhooks")
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 200, body)
	})

	t.Run("patch", func(t *testing.T) {
		code, body, err := c.patch("/v1/webhooks/"+id, map[string]any{"is_active": false})
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 200, body)
	})

	t.Run("list_deliveries", func(t *testing.T) {
		code, body, err := c.get("/v1/webhooks/" + id + "/deliveries")
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 200, body)
	})

	t.Run("delete", func(t *testing.T) {
		code, _, err := c.delete("/v1/webhooks/" + id)
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 204, nil)
	})
}

// TestDomains verifies domain listing and verify endpoint.
func TestDomains(t *testing.T) {
	c := newClient(t)

	code, body, err := c.get("/v1/domains")
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 200, body)
	domains := listOf(body, "domains")
	if len(domains) == 0 {
		t.Skip("no domains configured, skipping")
	}
	domainID := str(asMap(domains[0]), "id")

	t.Run("get", func(t *testing.T) {
		code, body, err := c.get("/v1/domains/" + domainID)
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 200, body)
	})
}

// TestCustomDomainProvisioning verifies the full bring-your-own-domain lifecycle:
// register → list → get → delete. Uses mail.nyklabs.com as a throwaway test domain;
// DNS records are NOT added so the domain stays in "pending" status throughout.
// The DELETE at the end removes all SES resources (identity + receipt rule) and the DB record.
func TestCustomDomainProvisioning(t *testing.T) {
	c := newClient(t)
	const testDomain = "mail.nyklabs.com"

	// Resolve the pod ID from the first available pod — required by the domain_configs schema.
	_, podsBody, err := c.get("/v1/pods")
	if err != nil {
		t.Fatal(err)
	}
	pods := listOf(podsBody, "pods")
	if len(pods) == 0 {
		t.Skip("no pods available, skipping")
	}
	podID := str(asMap(pods[0]), "id")

	// Step 1: register the domain — creates SES identity, DKIM tokens, receipt rule.
	code, body, err := c.post("/v1/domains", map[string]any{"Domain": testDomain, "PodID": podID})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)

	domainID := str(asMap(body["Domain"]), "id")
	if domainID == "" {
		t.Fatal("register response did not include domain id")
	}

	// Ensure cleanup runs even if subtests fail.
	t.Cleanup(func() {
		c.delete("/v1/domains/" + domainID) //nolint:errcheck
	})

	t.Run("register_returns_dns_records", func(t *testing.T) {
		dnsRecords := listOf(body, "DNSRecords")
		if len(dnsRecords) != 5 {
			t.Fatalf("expected 5 DNS records (1 TXT + 1 MX + 3 CNAME), got %d", len(dnsRecords))
		}
		types := map[string]int{}
		for _, r := range dnsRecords {
			types[str(asMap(r), "type")]++
		}

		if types["TXT"] != 1 {
			t.Errorf("expected 1 TXT record, got %d", types["TXT"])
		}
		if types["MX"] != 1 {
			t.Errorf("expected 1 MX record, got %d", types["MX"])
		}
		if types["CNAME"] != 3 {
			t.Errorf("expected 3 CNAME records, got %d", types["CNAME"])
		}
	})

	t.Run("register_status_is_pending", func(t *testing.T) {
		status := str(asMap(body["Domain"]), "status")
		if status != "pending" {
			t.Errorf("expected status=pending, got %q", status)
		}
	})

	t.Run("appears_in_list", func(t *testing.T) {
		code, body, err := c.get("/v1/domains")
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 200, body)
		found := false
		for _, d := range listOf(body, "domains") {
			if str(asMap(d), "id") == domainID {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("domain %s not found in list after registration", testDomain)
		}
	})

	t.Run("get_by_id", func(t *testing.T) {
		code, body, err := c.get("/v1/domains/" + domainID)
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 200, body)
		if got := str(body, "domain"); got != testDomain {
			t.Errorf("expected domain=%q, got %q", testDomain, got)
		}
	})

	t.Run("delete_removes_domain", func(t *testing.T) {
		code, _, err := c.delete("/v1/domains/" + domainID)
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 204, nil)

		// Confirm it's gone.
		code, body, err = c.get("/v1/domains/" + domainID)
		if err != nil {
			t.Fatal(err)
		}
		if code != 404 {
			t.Errorf("expected 404 after delete, got %d: %v", code, body)
		}
	})
}
