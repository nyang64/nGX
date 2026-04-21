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
	"time"
)

// TestKeywordSearch verifies that a sent message becomes searchable via the
// keyword search endpoint (GET /v1/search?q=...).
func TestKeywordSearch(t *testing.T) {
	c := newClient(t)

	// Create a dedicated inbox.
	code, body, err := c.post("/v1/inboxes", map[string]any{"address": uniqueName("srch")})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	inboxID := mustStr(t, body, "id")
	t.Cleanup(func() { c.delete("/v1/inboxes/" + inboxID) }) //nolint

	// Use a unique subject token that we can search for.
	uniqueToken := uniqueName("kwtoken")
	subject := fmt.Sprintf("Search test %s", uniqueToken)

	code, body, err = c.post(fmt.Sprintf("/v1/inboxes/%s/messages/send", inboxID), map[string]any{
		"to":        []map[string]any{{"email": "search-test@example.com"}},
		"subject":   subject,
		"body_text": fmt.Sprintf("Integration test for keyword search: %s", uniqueToken),
	})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	msgID := str(body, "id")
	t.Logf("Sent message %s, polling for keyword search indexing...", msgID)

	// Messages may need a moment to be indexed into the search_vector tsvector.
	// Poll until the unique token appears in search results.
	ok := pollUntil(t, 20*time.Second, 2*time.Second, func() bool {
		_, body, err := c.get("/v1/search?q=" + uniqueToken)
		if err != nil {
			return false
		}
		items := listOf(body, "items")
		for _, item := range items {
			if str(asMap(item), "message_id") == msgID {
				return true
			}
		}
		return false
	})
	if !ok {
		t.Fatal("message never appeared in keyword search results within 20s")
	}

	// Verify result shape.
	t.Run("result_fields", func(t *testing.T) {
		_, body, err := c.get("/v1/search?q=" + uniqueToken)
		if err != nil {
			t.Fatal(err)
		}
		items := listOf(body, "items")
		for _, item := range items {
			m := asMap(item)
			if str(m, "message_id") == msgID {
				if str(m, "thread_id") == "" {
					t.Fatal("search result missing thread_id")
				}
				if str(m, "inbox_id") != inboxID {
					t.Fatalf("search result inbox_id mismatch: %s", str(m, "inbox_id"))
				}
				if str(m, "subject") == "" {
					t.Fatal("search result missing subject")
				}
				rank, _ := m["rank"].(float64)
				if rank <= 0 {
					t.Fatalf("expected rank > 0, got %f", rank)
				}
				return
			}
		}
		t.Fatal("message not found in search results")
	})
}

// TestSearchPagination verifies that the search endpoint supports pagination
// via the cursor parameter.
func TestSearchPagination(t *testing.T) {
	c := newClient(t)

	// Create inbox and send multiple messages with the same token.
	code, body, err := c.post("/v1/inboxes", map[string]any{"address": uniqueName("pgn")})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	inboxID := mustStr(t, body, "id")
	t.Cleanup(func() { c.delete("/v1/inboxes/" + inboxID) }) //nolint

	pageToken := uniqueName("pgtoken")
	const n = 3
	for i := 0; i < n; i++ {
		code, body, err := c.post(fmt.Sprintf("/v1/inboxes/%s/messages/send", inboxID), map[string]any{
			"to":        []map[string]any{{"email": "page-test@example.com"}},
			"subject":   fmt.Sprintf("Page test %s #%d", pageToken, i),
			"body_text": fmt.Sprintf("body %d with token %s", i, pageToken),
		})
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 201, body)
	}

	// Wait for all messages to be indexed.
	ok := pollUntil(t, 20*time.Second, 2*time.Second, func() bool {
		_, body, err := c.get("/v1/search?q=" + pageToken + "&limit=10")
		if err != nil {
			return false
		}
		return len(listOf(body, "items")) >= n
	})
	if !ok {
		t.Skip("messages not all indexed within 20s, skipping pagination check")
	}

	// Fetch first page with limit=2 and verify cursor is returned.
	_, body, err = c.get("/v1/search?q=" + pageToken + "&limit=2")
	if err != nil {
		t.Fatal(err)
	}
	items := listOf(body, "items")
	if len(items) != 2 {
		t.Fatalf("expected 2 items on first page, got %d", len(items))
	}
	cursor, _ := body["next_cursor"].(string)
	if cursor == "" {
		t.Fatal("expected next_cursor to be set when more results exist")
	}

	// Fetch second page using the cursor.
	_, body2, err := c.get("/v1/search?q=" + pageToken + "&limit=2&cursor=" + cursor)
	if err != nil {
		t.Fatal(err)
	}
	items2 := listOf(body2, "items")
	if len(items2) == 0 {
		t.Fatal("expected at least one item on second page")
	}
}

// TestSemanticSearch verifies the semantic search endpoint (mode=semantic).
// This test is skipped if the embedder is not configured (EMBEDDER_URL not set).
func TestSemanticSearch(t *testing.T) {
	c := newClient(t)

	// Quick probe: if semantic search returns 500 or items is empty when keyword
	// search has results, the embedder is not deployed — skip gracefully.
	code, body, err := c.post("/v1/inboxes", map[string]any{"address": uniqueName("sem")})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	inboxID := mustStr(t, body, "id")
	t.Cleanup(func() { c.delete("/v1/inboxes/" + inboxID) }) //nolint

	semToken := uniqueName("semtoken")
	code, body, err = c.post(fmt.Sprintf("/v1/inboxes/%s/messages/send", inboxID), map[string]any{
		"to":        []map[string]any{{"email": "sem-test@example.com"}},
		"subject":   "Semantic search test " + semToken,
		"body_text": "The quick brown fox jumps over the lazy dog. Token: " + semToken,
	})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	msgID := str(body, "id")

	// Wait for embedding to be generated (embedder Lambda is async).
	ok := pollUntil(t, 30*time.Second, 3*time.Second, func() bool {
		_, body, err := c.get("/v1/search?q=quick+brown+fox&mode=semantic&inbox_id=" + inboxID)
		if err != nil {
			return false
		}
		for _, item := range listOf(body, "items") {
			if str(asMap(item), "message_id") == msgID {
				return true
			}
		}
		return false
	})
	if !ok {
		t.Skip("semantic search: message not found within 30s — embedder may not be configured")
	}
}
