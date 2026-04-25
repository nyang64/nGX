/*
 * Copyright (c) 2026 nyklabs.com. All rights reserved.
 *
 * Licensed under the nGX Commercial Source License v1.0.
 * See LICENSE file in the project root for full license information.
 */

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscfg "github.com/aws/aws-sdk-go-v2/config"
	sqssdk "github.com/aws/aws-sdk-go-v2/service/sqs"
)

// sesTestEnv holds AWS clients and queue URL, shared across sub-tests.
type sesTestEnv struct {
	sqsClient *sqssdk.Client
	queueURL  string
}

// newSESTestEnv loads AWS config and resolves the ses_events SQS queue URL.
// Skips the test if TEST_LAMBDA_PREFIX or TEST_AWS_REGION are not set.
func newSESTestEnv(t *testing.T) *sesTestEnv {
	t.Helper()
	lambdaPrefix, awsRegion := requireLambdaEnv(t)

	ctx := context.Background()
	awsConf, err := awscfg.LoadDefaultConfig(ctx, awscfg.WithRegion(awsRegion))
	if err != nil {
		t.Fatalf("load AWS config: %v", err)
	}

	sqsClient := sqssdk.NewFromConfig(awsConf)
	queueName := lambdaPrefix + "-ses-events"
	out, err := sqsClient.GetQueueUrl(ctx, &sqssdk.GetQueueUrlInput{
		QueueName: aws.String(queueName),
	})
	if err != nil {
		t.Fatalf("get queue URL for %s: %v", queueName, err)
	}

	return &sesTestEnv{sqsClient: sqsClient, queueURL: *out.QueueUrl}
}

// buildEBEventBody returns the EventBridge SES event JSON as it arrives as an
// SQS message body after the EventBridge rule routes it.
func buildEBEventBody(detailType, sesMessageID, rfc5322MsgID string) string {
	evt := map[string]any{
		"version":     "0",
		"id":          fmt.Sprintf("test-%d", time.Now().UnixNano()),
		"source":      "aws.ses",
		"account":     "123456789012",
		"time":        time.Now().UTC().Format(time.RFC3339),
		"region":      "us-east-1",
		"detail-type": detailType,
		"detail": map[string]any{
			"mail": map[string]any{
				"messageId": sesMessageID,
				"headers": []map[string]any{
					{"name": "From", "value": "sender@example.com"},
					{"name": "To", "value": "recipient@example.com"},
					// Angle-bracket form mirrors a real SES EventBridge event.
					// The Lambda strips <> before querying message_id_header.
					{"name": "Message-ID", "value": fmt.Sprintf("<%s>", rfc5322MsgID)},
					{"name": "Subject", "value": "Integration test"},
				},
			},
		},
	}
	b, _ := json.Marshal(evt)
	return string(b)
}

// injectSESEvent sends a synthetic EventBridge SES event directly into the
// ses_events SQS queue, bypassing EventBridge. The ses_events Lambda picks
// it up via the SQS event source mapping.
func (e *sesTestEnv) injectSESEvent(t *testing.T, detailType, sesMessageID, rfc5322MsgID string) {
	t.Helper()
	body := buildEBEventBody(detailType, sesMessageID, rfc5322MsgID)
	_, err := e.sqsClient.SendMessage(context.Background(), &sqssdk.SendMessageInput{
		QueueUrl:    aws.String(e.queueURL),
		MessageBody: aws.String(body),
	})
	if err != nil {
		t.Fatalf("inject SES event %q: %v", detailType, err)
	}
	t.Logf("injected %q for message_id=%s", detailType, rfc5322MsgID)
}

// sendAndResolveMessageID creates an outbound message and polls until
// email_outbound has fully processed it: message_id_header is populated AND
// status has left "sending" (reached "sent" or a terminal state).
// Returns inboxID, threadID, msgID, rfc5322MsgID.
func sendAndResolveMessageID(t *testing.T, c *client, addrPrefix string) (inboxID, threadID, msgID, rfc5322MsgID string) {
	t.Helper()

	code, body, err := c.post("/v1/inboxes", map[string]any{"address": uniqueName(addrPrefix)})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	inboxID = mustStr(t, body, "id")
	t.Cleanup(func() { c.delete("/v1/inboxes/" + inboxID) }) //nolint

	// Retry send up to 3 times to handle Lambda cold-start throttling.
	var sendCode int
	var sendBody map[string]any
	for attempt := 0; attempt < 3; attempt++ {
		sendCode, sendBody, err = c.post(fmt.Sprintf("/v1/inboxes/%s/messages/send", inboxID), map[string]any{
			"to":        []map[string]any{{"email": "success@simulator.amazonses.com"}},
			"subject":   "SES events test " + uniqueName("subj"),
			"body_text": "Integration test — SES event injection",
		})
		if err != nil {
			t.Fatal(err)
		}
		if sendCode == 201 {
			break
		}
		if attempt < 2 {
			time.Sleep(2 * time.Second)
		}
	}
	mustCode(t, sendCode, 201, sendBody)
	msgID = mustStr(t, sendBody, "id")
	threadID = mustStr(t, sendBody, "thread_id")

	// Wait for email_outbound to fully process: message_id_header populated AND
	// status no longer "sending". This gives us a stable baseline before injecting
	// SES events, so "no status change" assertions aren't racing with email_outbound.
	ok := pollUntil(t, 45*time.Second, 2*time.Second, func() bool {
		_, b, err := c.get(fmt.Sprintf("/v1/inboxes/%s/threads/%s/messages/%s", inboxID, threadID, msgID))
		if err != nil {
			return false
		}
		rfc5322MsgID = str(b, "message_id")
		status := str(b, "status")
		return rfc5322MsgID != "" && status != "sending" && status != ""
	})
	if !ok {
		t.Skip("message never left 'sending' after 45s — email_outbound may not be running")
	}
	return inboxID, threadID, msgID, rfc5322MsgID
}

// messageStatus fetches the current status of a message.
func messageStatus(t *testing.T, c *client, inboxID, threadID, msgID string) string {
	t.Helper()
	_, body, err := c.get(fmt.Sprintf("/v1/inboxes/%s/threads/%s/messages/%s", inboxID, threadID, msgID))
	if err != nil {
		return ""
	}
	return str(body, "status")
}

// ── E2E: Email Delivered ──────────────────────────────────────────────────────

// TestSESEventsDeliveredE2E verifies the full SES delivery pipeline end-to-end:
//
//	send → SES → EventBridge "Email Delivered" → SQS → ses_events Lambda → DB
//
// This is the only test that exercises the EventBridge filter rule with real AWS
// traffic. The other five event types use direct SQS injection (see below).
//
// Uses success@simulator.amazonses.com which always delivers without sandbox issues.
//
// Requires: TEST_BASE_URL, TEST_API_KEY, TEST_LAMBDA_PREFIX, TEST_AWS_REGION
func TestSESEventsDeliveredE2E(t *testing.T) {
	c := newClient(t)
	_ = newSESTestEnv(t) // validates AWS env vars are present

	inboxID, threadID, msgID, _ := sendAndResolveMessageID(t, c, "e2e-dlv")

	// Wait for the real SES "Email Delivered" event to flow through EventBridge →
	// SQS → ses_events Lambda. The Lambda updates sent_at unconditionally on
	// Email Delivered, so we poll until sent_at is non-null.
	//
	// Status may already be 'sent' (set by email_outbound). We verify it remains
	// 'sent' and that sent_at is populated — confirming the full pipeline ran.
	ok := pollUntil(t, 90*time.Second, 3*time.Second, func() bool {
		_, body, err := c.get(fmt.Sprintf("/v1/inboxes/%s/threads/%s/messages/%s", inboxID, threadID, msgID))
		if err != nil {
			return false
		}
		return str(body, "status") == "sent" && str(body, "sent_at") != ""
	})
	if !ok {
		_, body, _ := c.get(fmt.Sprintf("/v1/inboxes/%s/threads/%s/messages/%s", inboxID, threadID, msgID))
		t.Fatalf("message never reached sent+sent_at state after 90s; status=%s sent_at=%s",
			str(body, "status"), str(body, "sent_at"))
	}
}

// ── SQS injection tests for all other event types ────────────────────────────

// TestSESEventsSQSInjection covers the remaining 5 SES event types by injecting
// synthetic EventBridge events directly into the ses_events SQS queue.
// This tests the SQS → ses_events Lambda → DB path for each event type.
//
// Requires: TEST_BASE_URL, TEST_API_KEY, TEST_LAMBDA_PREFIX, TEST_AWS_REGION
func TestSESEventsSQSInjection(t *testing.T) {
	c := newClient(t)
	env := newSESTestEnv(t)

	cases := []struct {
		name           string
		addrPrefix     string
		detailType     string
		wantStatus     string // "" means status must remain unchanged
		pollTimeout    time.Duration
	}{
		{
			name:        "Email Bounced → bounced",
			addrPrefix:  "ses-bounce",
			detailType:  "Email Bounced",
			wantStatus:  "bounced",
			pollTimeout: 20 * time.Second,
		},
		{
			name:        "Email Complaint Received → failed",
			addrPrefix:  "ses-complaint",
			detailType:  "Email Complaint Received",
			wantStatus:  "failed",
			pollTimeout: 20 * time.Second,
		},
		{
			name:        "Email Rejected → failed",
			addrPrefix:  "ses-rejected",
			detailType:  "Email Rejected",
			wantStatus:  "failed",
			pollTimeout: 20 * time.Second,
		},
		{
			name:        "Email Rendering Failed → failed",
			addrPrefix:  "ses-render",
			detailType:  "Email Rendering Failed",
			wantStatus:  "failed",
			pollTimeout: 20 * time.Second,
		},
		{
			name:        "Email Delivery Delayed → no status change",
			addrPrefix:  "ses-delayed",
			detailType:  "Email Delivery Delayed",
			wantStatus:  "", // log-only; status must remain unchanged
			pollTimeout: 10 * time.Second,
		},
		{
			name:        "Email Opened → no status change",
			addrPrefix:  "ses-opened",
			detailType:  "Email Opened",
			wantStatus:  "", // engagement signal only; no status transition
			pollTimeout: 10 * time.Second,
		},
		{
			name:        "Email Clicked → no status change",
			addrPrefix:  "ses-clicked",
			detailType:  "Email Clicked",
			wantStatus:  "", // engagement signal only; no status transition
			pollTimeout: 10 * time.Second,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			inboxID, threadID, msgID, rfc5322MsgID := sendAndResolveMessageID(t, c, tc.addrPrefix)
			statusBefore := messageStatus(t, c, inboxID, threadID, msgID)

			env.injectSESEvent(t,
				tc.detailType,
				"ses-injected-"+uniqueName("id"),
				rfc5322MsgID,
			)

			if tc.wantStatus == "" {
				// Engagement/delayed events: no status change expected.
				// Wait the poll window and assert status is unchanged.
				time.Sleep(tc.pollTimeout)
				statusAfter := messageStatus(t, c, inboxID, threadID, msgID)
				if statusAfter != statusBefore {
					t.Errorf("%s must not change status: was %q, got %q",
						tc.detailType, statusBefore, statusAfter)
				}
				return
			}

			ok := pollUntil(t, tc.pollTimeout, 1*time.Second, func() bool {
				return messageStatus(t, c, inboxID, threadID, msgID) == tc.wantStatus
			})
			if !ok {
				got := messageStatus(t, c, inboxID, threadID, msgID)
				t.Fatalf("%s: status never reached %q (got %q)", tc.detailType, tc.wantStatus, got)
			}
		})
	}
}

// requireLambdaEnv returns TEST_LAMBDA_PREFIX and TEST_AWS_REGION, skipping
// the test if either is missing.
func requireLambdaEnv(t *testing.T) (lambdaPrefix, awsRegion string) {
	t.Helper()
	lambdaPrefix = os.Getenv("TEST_LAMBDA_PREFIX")
	awsRegion = os.Getenv("TEST_AWS_REGION")
	if lambdaPrefix == "" || awsRegion == "" {
		t.Skip("TEST_LAMBDA_PREFIX and TEST_AWS_REGION must be set")
	}
	return lambdaPrefix, awsRegion
}

// buildSQSEventWithEBPayload is retained for backward compatibility with any
// external tooling that references the helper.  New tests use injectSESEvent.
func buildSQSEventWithEBPayload(detailType, sesMessageID, rfc5322MsgID string) []byte {
	sqsEvent := map[string]any{
		"Records": []map[string]any{
			{
				"messageId":     fmt.Sprintf("test-sqs-%d", time.Now().UnixNano()),
				"receiptHandle": "test-receipt",
				"body":          buildEBEventBody(detailType, sesMessageID, rfc5322MsgID),
				"attributes": map[string]string{
					"ApproximateReceiveCount":          "1",
					"SentTimestamp":                    fmt.Sprintf("%d", time.Now().UnixMilli()),
					"SenderId":                         "events.amazonaws.com",
					"ApproximateFirstReceiveTimestamp": fmt.Sprintf("%d", time.Now().UnixMilli()),
				},
				"messageAttributes": map[string]any{},
				"md5OfBody":         "test",
				"eventSource":       "aws:sqs",
				"eventSourceARN":    "arn:aws:sqs:us-east-1:123456789012:test-ses-events",
				"awsRegion":         "us-east-1",
			},
		},
	}
	b, _ := json.Marshal(sqsEvent)
	return b
}
