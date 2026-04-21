/*
 * Copyright (c) 2026 nyklabs.com. All rights reserved.
 *
 * Licensed under the nGX Commercial Source License v1.0.
 * See LICENSE file in the project root for full license information.
 */

package integration

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// TestWebSocketEventDelivery verifies:
//  1. A client can establish a WebSocket connection using an API key as ?token=
//  2. A REST API action (send message) produces an event that arrives over the
//     WebSocket connection within the poll timeout.
//
// Requires TEST_WS_URL (e.g. wss://abc.execute-api.us-east-1.amazonaws.com/prod).
func TestWebSocketEventDelivery(t *testing.T) {
	c := newClient(t)

	wsURL := os.Getenv("TEST_WS_URL")
	apiKey := os.Getenv("TEST_API_KEY")
	if wsURL == "" {
		t.Skip("TEST_WS_URL must be set")
	}

	// Connect to the WebSocket API with the API key as ?token=
	connectURL := wsURL + "?token=" + apiKey
	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}
	conn, resp, err := dialer.Dial(connectURL, http.Header{})
	if err != nil {
		t.Fatalf("WebSocket dial failed (status %v): %v", statusCode(resp), err)
	}
	defer conn.Close()

	t.Logf("WebSocket connected to %s", wsURL)

	// Channel to collect incoming events.
	events := make(chan map[string]any, 20)
	wsErr := make(chan error, 1)
	go func() {
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				wsErr <- err
				return
			}
			var evt map[string]any
			if jsonErr := json.Unmarshal(msg, &evt); jsonErr == nil {
				events <- evt
			}
		}
	}()

	// Create a dedicated inbox and send a message to produce domain events.
	code, body, err := c.post("/v1/inboxes", map[string]any{"address": uniqueName("ws")})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	inboxID := mustStr(t, body, "id")
	t.Cleanup(func() { c.delete("/v1/inboxes/" + inboxID) }) //nolint

	// Short pause to ensure the WS connection is registered in the DB before
	// the event is published.
	time.Sleep(500 * time.Millisecond)

	code, body, err = c.post(fmt.Sprintf("/v1/inboxes/%s/messages/send", inboxID), map[string]any{
		"to":        []map[string]any{{"email": "ws-test@example.com"}},
		"subject":   "WS event test " + uniqueName("subj"),
		"body_text": "Integration test WebSocket event",
	})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	sentMsgID := str(body, "id")
	t.Logf("Sent message %s, waiting for WS event...", sentMsgID)

	// Wait up to 15 seconds for the message.sent or message.received event.
	deadline := time.After(15 * time.Second)
	for {
		select {
		case evt := <-events:
			evtType, _ := evt["type"].(string)
			t.Logf("Received WS event: %s", evtType)
			if evtType == "message.sent" || evtType == "message.received" || evtType == "thread.created" {
				// Verify the event has expected structure.
				if evt["data"] == nil {
					t.Fatal("event missing data field")
				}
				return // success
			}
		case err := <-wsErr:
			t.Fatalf("WebSocket read error: %v", err)
		case <-deadline:
			t.Fatal("timed out waiting for domain event over WebSocket (15s)")
		}
	}
}

// TestWebSocketConnect verifies that the WS endpoint accepts a valid token and
// rejects an invalid one.
func TestWebSocketConnect(t *testing.T) {
	wsURL := os.Getenv("TEST_WS_URL")
	if wsURL == "" {
		t.Skip("TEST_WS_URL must be set")
	}

	t.Run("valid_token_accepted", func(t *testing.T) {
		apiKey := os.Getenv("TEST_API_KEY")
		conn, resp, err := websocket.DefaultDialer.Dial(wsURL+"?token="+apiKey, nil)
		if err != nil {
			t.Fatalf("expected successful connect, got (status %v): %v", statusCode(resp), err)
		}
		conn.Close()
	})

	t.Run("missing_token_rejected", func(t *testing.T) {
		_, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err == nil {
			t.Fatal("expected connection to be rejected without token")
		}
		if resp != nil && resp.StatusCode != 401 && resp.StatusCode != 403 {
			t.Fatalf("expected 401/403 for missing token, got %d", resp.StatusCode)
		}
	})

	t.Run("invalid_token_rejected", func(t *testing.T) {
		_, resp, err := websocket.DefaultDialer.Dial(wsURL+"?token=am_live_invalid", nil)
		if err == nil {
			t.Fatal("expected connection to be rejected with invalid token")
		}
		if resp != nil && resp.StatusCode != 401 && resp.StatusCode != 403 {
			t.Fatalf("expected 401/403 for invalid token, got %d", resp.StatusCode)
		}
	})
}

func statusCode(resp *http.Response) int {
	if resp == nil {
		return 0
	}
	return resp.StatusCode
}
