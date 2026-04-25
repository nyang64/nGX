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
	return sampleEventBridgeSESEventFull(detailType, sesMessageID, rfc5322MsgID, nil)
}

// sampleEventBridgeSESEventFull builds an EventBridge SES event with optional
// extra detail fields (for click/open sub-objects).
func sampleEventBridgeSESEventFull(detailType, sesMessageID, rfc5322MsgID string, extraDetail map[string]any) string {
	detail := map[string]any{
		"mail": map[string]any{
			"messageId": sesMessageID,
			"headers": []map[string]any{
				{"name": "From", "value": "sender@example.com"},
				{"name": "To", "value": "recipient@example.com"},
				{"name": "Message-ID", "value": rfc5322MsgID},
				{"name": "Subject", "value": "Test"},
			},
		},
	}
	for k, v := range extraDetail {
		detail[k] = v
	}
	evt := map[string]any{
		"version":     "0",
		"id":          "test-event-id",
		"source":      "aws.ses",
		"account":     "123456789012",
		"time":        "2026-04-21T00:00:00Z",
		"region":      "us-east-1",
		"detail-type": detailType,
		"detail":      detail,
	}
	b, _ := json.Marshal(evt)
	return string(b)
}

// TestEbSESEventUnmarshal verifies that the EventBridge envelope parses correctly
// for the official SES EventBridge detail-type strings.
// Reference: https://docs.aws.amazon.com/ses/latest/dg/monitoring-eventbridge.html
func TestEbSESEventUnmarshal(t *testing.T) {
	cases := []struct {
		detailType     string
		sesMessageID   string
		rfc5322MsgID   string
		wantDetailType string
		wantRFC5322ID  string // after angle-bracket stripping
	}{
		{
			detailType:     "Email Bounced",
			sesMessageID:   "ses-abc123",
			rfc5322MsgID:   "<bounce-test@mail.example.com>",
			wantDetailType: "Email Bounced",
			wantRFC5322ID:  "bounce-test@mail.example.com",
		},
		{
			detailType:     "Email Complaint Received",
			sesMessageID:   "ses-def456",
			rfc5322MsgID:   "<complaint-test@mail.example.com>",
			wantDetailType: "Email Complaint Received",
			wantRFC5322ID:  "complaint-test@mail.example.com",
		},
		{
			detailType:     "Email Delivered",
			sesMessageID:   "ses-ghi789",
			rfc5322MsgID:   "<delivery-test@mail.example.com>",
			wantDetailType: "Email Delivered",
			wantRFC5322ID:  "delivery-test@mail.example.com",
		},
		{
			detailType:     "Email Rejected",
			sesMessageID:   "ses-jkl012",
			rfc5322MsgID:   "<rejected-test@mail.example.com>",
			wantDetailType: "Email Rejected",
			wantRFC5322ID:  "rejected-test@mail.example.com",
		},
		{
			detailType:     "Email Rendering Failed",
			sesMessageID:   "ses-mno345",
			rfc5322MsgID:   "<rendering-failed@mail.example.com>",
			wantDetailType: "Email Rendering Failed",
			wantRFC5322ID:  "rendering-failed@mail.example.com",
		},
		{
			detailType:     "Email Delivery Delayed",
			sesMessageID:   "ses-pqr678",
			rfc5322MsgID:   "<delayed-test@mail.example.com>",
			wantDetailType: "Email Delivery Delayed",
			wantRFC5322ID:  "delayed-test@mail.example.com",
		},
		{
			detailType:     "Email Clicked",
			sesMessageID:   "ses-stu901",
			rfc5322MsgID:   "<click-test@mail.example.com>",
			wantDetailType: "Email Clicked",
			wantRFC5322ID:  "click-test@mail.example.com",
		},
		{
			detailType:     "Email Opened",
			sesMessageID:   "ses-vwx234",
			rfc5322MsgID:   "<open-test@mail.example.com>",
			wantDetailType: "Email Opened",
			wantRFC5322ID:  "open-test@mail.example.com",
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
		"detail-type": "Email Bounced",
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

// TestProcessRecordClickSubObject verifies that click sub-object fields parse correctly.
func TestProcessRecordClickSubObject(t *testing.T) {
	body := sampleEventBridgeSESEventFull("Email Clicked", "ses-click", "<click@example.com>", map[string]any{
		"click": map[string]any{
			"ipAddress": "1.2.3.4",
			"link":      "https://example.com/track?x=1",
			"userAgent": "Mozilla/5.0",
		},
	})

	var evt ebSESEvent
	if err := json.Unmarshal([]byte(body), &evt); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if evt.Detail.Click.IPAddress != "1.2.3.4" {
		t.Errorf("Click.IPAddress: got %q, want %q", evt.Detail.Click.IPAddress, "1.2.3.4")
	}
	if evt.Detail.Click.Link != "https://example.com/track?x=1" {
		t.Errorf("Click.Link: got %q, want %q", evt.Detail.Click.Link, "https://example.com/track?x=1")
	}
	if evt.Detail.Click.UserAgent != "Mozilla/5.0" {
		t.Errorf("Click.UserAgent: got %q, want %q", evt.Detail.Click.UserAgent, "Mozilla/5.0")
	}

	// publisher == nil: engagement path skips publish without error.
	savedPool := pool
	pool = nil
	defer func() { pool = savedPool }()

	rec := events.SQSMessage{MessageId: "test-sqs-id", Body: body}
	if err := processRecord(context.Background(), rec); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

// TestProcessRecordOpenSubObject verifies that open sub-object fields parse correctly.
func TestProcessRecordOpenSubObject(t *testing.T) {
	body := sampleEventBridgeSESEventFull("Email Opened", "ses-open", "<open@example.com>", map[string]any{
		"open": map[string]any{
			"ipAddress": "5.6.7.8",
			"userAgent": "Gmail",
		},
	})

	var evt ebSESEvent
	if err := json.Unmarshal([]byte(body), &evt); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if evt.Detail.Open.IPAddress != "5.6.7.8" {
		t.Errorf("Open.IPAddress: got %q, want %q", evt.Detail.Open.IPAddress, "5.6.7.8")
	}

	// publisher == nil: engagement path skips publish without error.
	savedPool := pool
	pool = nil
	defer func() { pool = savedPool }()

	rec := events.SQSMessage{MessageId: "test-sqs-id", Body: body}
	if err := processRecord(context.Background(), rec); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

// TestProcessRecordBounceSubObject verifies bounce type/subtype fields parse correctly.
func TestProcessRecordBounceSubObject(t *testing.T) {
	body := sampleEventBridgeSESEventFull("Email Bounced", "ses-bounce", "<bounce@example.com>", map[string]any{
		"bounce": map[string]any{
			"bounceType":    "Permanent",
			"bounceSubType": "General",
		},
	})

	var evt ebSESEvent
	if err := json.Unmarshal([]byte(body), &evt); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if evt.Detail.Bounce.BounceType != "Permanent" {
		t.Errorf("Bounce.BounceType: got %q, want Permanent", evt.Detail.Bounce.BounceType)
	}
	if evt.Detail.Bounce.BounceSubType != "General" {
		t.Errorf("Bounce.BounceSubType: got %q, want General", evt.Detail.Bounce.BounceSubType)
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
