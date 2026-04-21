/*
 * Copyright (c) 2026 nyklabs.com. All rights reserved.
 *
 * Licensed under the nGX Commercial Source License v1.0.
 * See LICENSE file in the project root for full license information.
 */

package main

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/aws/aws-lambda-go/events"
)

// sampleEventBridgeSESEvent returns a realistic EventBridge SES event body
// as it arrives in the SQS message after the EventBridge rule routes it.
func sampleEventBridgeSESEvent(detailType, sesMessageID, rfc5322MsgID string) string {
	evt := map[string]any{
		"version":     "0",
		"id":          "test-event-id",
		"source":      "aws.ses",
		"account":     "123456789012",
		"time":        "2026-04-21T00:00:00Z",
		"region":      "us-east-1",
		"detail-type": detailType,
		"detail": map[string]any{
			"mail": map[string]any{
				"messageId": sesMessageID,
				"headers": []map[string]any{
					{"name": "From", "value": "sender@example.com"},
					{"name": "To", "value": "recipient@example.com"},
					{"name": "Message-ID", "value": rfc5322MsgID},
					{"name": "Subject", "value": "Test"},
				},
			},
		},
	}
	b, _ := json.Marshal(evt)
	return string(b)
}

// TestEbSESEventUnmarshal verifies that the EventBridge envelope parses correctly
// for each of the three SES detail-types.
func TestEbSESEventUnmarshal(t *testing.T) {
	cases := []struct {
		detailType      string
		sesMessageID    string
		rfc5322MsgID    string
		wantDetailType  string
		wantRFC5322ID   string // after angle-bracket stripping
	}{
		{
			detailType:    "SES Bounce",
			sesMessageID:  "ses-abc123",
			rfc5322MsgID:  "<bounce-test@mail.example.com>",
			wantDetailType: "SES Bounce",
			wantRFC5322ID: "bounce-test@mail.example.com",
		},
		{
			detailType:    "SES Complaint",
			sesMessageID:  "ses-def456",
			rfc5322MsgID:  "<complaint-test@mail.example.com>",
			wantDetailType: "SES Complaint",
			wantRFC5322ID: "complaint-test@mail.example.com",
		},
		{
			detailType:    "SES Message Delivery",
			sesMessageID:  "ses-ghi789",
			rfc5322MsgID:  "<delivery-test@mail.example.com>",
			wantDetailType: "SES Message Delivery",
			wantRFC5322ID: "delivery-test@mail.example.com",
		},
	}

	for _, tc := range cases {
		t.Run(tc.detailType, func(t *testing.T) {
			body := sampleEventBridgeSESEvent(tc.detailType, tc.sesMessageID, tc.rfc5322MsgID)

			var evt ebSESEvent
			if err := json.Unmarshal([]byte(body), &evt); err != nil {
				t.Fatalf("unmarshal failed: %v", err)
			}

			if evt.DetailType != tc.wantDetailType {
				t.Errorf("DetailType: got %q, want %q", evt.DetailType, tc.wantDetailType)
			}
			if evt.Detail.Mail.MessageID != tc.sesMessageID {
				t.Errorf("Mail.MessageID: got %q, want %q", evt.Detail.Mail.MessageID, tc.sesMessageID)
			}

			// Verify Message-ID header is present and extractable.
			var found string
			for _, h := range evt.Detail.Mail.Headers {
				if h.Name == "Message-ID" {
					found = h.Value
					break
				}
			}
			if found != tc.rfc5322MsgID {
				t.Errorf("Message-ID header: got %q, want %q", found, tc.rfc5322MsgID)
			}

			// Verify angle-bracket stripping (mirrors processRecord logic).
			if len(found) > 2 && found[0] == '<' {
				found = found[1 : len(found)-1]
			}
			if found != tc.wantRFC5322ID {
				t.Errorf("stripped Message-ID: got %q, want %q", found, tc.wantRFC5322ID)
			}
		})
	}
}

// TestProcessRecordNoMessageID verifies that a record with no Message-ID header
// is silently skipped (returns nil) without touching the database.
func TestProcessRecordNoMessageID(t *testing.T) {
	body := map[string]any{
		"detail-type": "SES Bounce",
		"detail": map[string]any{
			"mail": map[string]any{
				"messageId": "ses-noid",
				"headers":   []map[string]any{},
			},
		},
	}
	b, _ := json.Marshal(body)

	// pool is nil — if processRecord tries to hit the DB it will panic.
	// A nil pool proves the early-return path is exercised correctly.
	savedPool := pool
	pool = nil
	defer func() { pool = savedPool }()

	rec := events.SQSMessage{MessageId: "test-sqs-id", Body: string(b)}
	if err := processRecord(context.Background(), rec); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

// TestProcessRecordUnknownDetailType verifies that an unrecognised detail-type
// is skipped without error and without touching the database.
func TestProcessRecordUnknownDetailType(t *testing.T) {
	body := map[string]any{
		"detail-type": "SES Send",
		"detail": map[string]any{
			"mail": map[string]any{
				"messageId": "ses-unknown",
				"headers": []map[string]any{
					{"name": "Message-ID", "value": "<unknown@example.com>"},
				},
			},
		},
	}
	b, _ := json.Marshal(body)

	savedPool := pool
	pool = nil
	defer func() { pool = savedPool }()

	rec := events.SQSMessage{MessageId: "test-sqs-id", Body: string(b)}
	if err := processRecord(context.Background(), rec); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

// TestProcessRecordMalformedJSON verifies that a non-JSON SQS body is skipped
// gracefully (returns nil, not an error) so the record isn't retried forever.
func TestProcessRecordMalformedJSON(t *testing.T) {
	savedPool := pool
	pool = nil
	defer func() { pool = savedPool }()

	rec := events.SQSMessage{MessageId: "test-sqs-id", Body: "not json at all"}
	if err := processRecord(context.Background(), rec); err != nil {
		t.Fatalf("expected nil for malformed JSON, got %v", err)
	}
}

// TestHandlerEmptySQSEvent verifies the handler returns no failures for an
// empty event batch.
func TestHandlerEmptySQSEvent(t *testing.T) {
	resp, err := handler(context.Background(), events.SQSEvent{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.BatchItemFailures) != 0 {
		t.Fatalf("expected 0 failures, got %d", len(resp.BatchItemFailures))
	}
}
