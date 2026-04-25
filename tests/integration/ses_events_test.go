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
	lambdasdk "github.com/aws/aws-sdk-go-v2/service/lambda"
)

// buildSQSEventWithEBPayload constructs the JSON payload for a direct Lambda
// invocation that simulates EventBridge → SQS → ses_events Lambda.
// The SQS body is the raw EventBridge event envelope (no SNS wrapper).
func buildSQSEventWithEBPayload(detailType, sesMessageID, rfc5322MsgID string) []byte {
	// EventBridge SES event envelope as it arrives in the SQS message body.
	ebEvent := map[string]any{
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
					// rfc5322MsgID is stored in the DB without angle brackets.
				// The Lambda strips <> from the header value before querying,
				// so we include them here to mirror a real SES EventBridge event.
				{"name": "Message-ID", "value": fmt.Sprintf("<%s>", rfc5322MsgID)},
					{"name": "Subject", "value": "Integration test"},
				},
			},
		},
	}
	ebBody, _ := json.Marshal(ebEvent)

	// Wrap in an SQS event as the Lambda runtime would deliver it.
	sqsEvent := map[string]any{
		"Records": []map[string]any{
			{
				"messageId":     fmt.Sprintf("test-sqs-%d", time.Now().UnixNano()),
				"receiptHandle": "test-receipt",
				"body":          string(ebBody),
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

// TestSESEventsBounce verifies that a bounce event (delivered via EventBridge →
// SQS → ses_events Lambda) sets the message status to "failed".
//
// Requires: TEST_BASE_URL, TEST_API_KEY, TEST_LAMBDA_PREFIX, TEST_AWS_REGION
func TestSESEventsBounce(t *testing.T) {
	c := newClient(t)
	lambdaPrefix, awsRegion := requireLambdaEnv(t)

	// Create an inbox and send a message to get a real message_id_header.
	code, body, err := c.post("/v1/inboxes", map[string]any{"address": uniqueName("evtbounce")})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	inboxID := mustStr(t, body, "id")
	t.Cleanup(func() { c.delete("/v1/inboxes/" + inboxID) }) //nolint

	// Send a message — use bounce@simulator.amazonses.com so SES accepts it.
	// We don't wait for real SES delivery; we'll inject the bounce event ourselves.
	code, body, err = c.post(fmt.Sprintf("/v1/inboxes/%s/messages/send", inboxID), map[string]any{
		"to":        []map[string]any{{"email": "bounce@simulator.amazonses.com"}},
		"subject":   "Bounce test " + uniqueName("subj"),
		"body_text": "Integration test bounce event",
	})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	msgID := mustStr(t, body, "id")
	threadID := mustStr(t, body, "thread_id")

	// Read back the message to get its RFC 5322 Message-ID (message_id_header).
	code, body, err = c.get(fmt.Sprintf("/v1/inboxes/%s/threads/%s/messages/%s", inboxID, threadID, msgID))
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 200, body)
	rfc5322MsgID := str(body, "message_id") // model field mapped from message_id_header
	if rfc5322MsgID == "" {
		t.Skip("message has no message_id (message_id_header) — cannot inject bounce event")
	}

	// Invoke ses_events Lambda directly with a synthetic EventBridge Bounce event.
	ctx := context.Background()
	awsConf, err := awscfg.LoadDefaultConfig(ctx, awscfg.WithRegion(awsRegion))
	if err != nil {
		t.Fatalf("load AWS config: %v", err)
	}
	lambdaClient := lambdasdk.NewFromConfig(awsConf)
	lambdaName := lambdaPrefix + "-ses-events"

	payload := buildSQSEventWithEBPayload("Email Bounced", "ses-injected-"+uniqueName("id"), rfc5322MsgID)
	resp, err := lambdaClient.Invoke(ctx, &lambdasdk.InvokeInput{
		FunctionName: aws.String(lambdaName),
		Payload:      payload,
	})
	if err != nil {
		t.Fatalf("invoke %s: %v", lambdaName, err)
	}
	if resp.FunctionError != nil {
		t.Fatalf("lambda function error: %s — %s", *resp.FunctionError, string(resp.Payload))
	}

	// Poll until message status transitions to "bounced".
	ok := pollUntil(t, 15*time.Second, 1*time.Second, func() bool {
		_, body, err := c.get(fmt.Sprintf("/v1/inboxes/%s/threads/%s/messages/%s", inboxID, threadID, msgID))
		if err != nil {
			return false
		}
		return str(body, "status") == "bounced"
	})
	if !ok {
		_, body, _ := c.get(fmt.Sprintf("/v1/inboxes/%s/threads/%s/messages/%s", inboxID, threadID, msgID))
		t.Fatalf("message status never reached 'bounced' after bounce event; current status: %s", str(body, "status"))
	}
}

// TestSESEventsDelivery verifies that a delivery event sets the message status
// to "sent". Uses the success simulator address so SES accepts the send.
//
// Requires: TEST_BASE_URL, TEST_API_KEY, TEST_LAMBDA_PREFIX, TEST_AWS_REGION
func TestSESEventsDelivery(t *testing.T) {
	c := newClient(t)
	lambdaPrefix, awsRegion := requireLambdaEnv(t)

	code, body, err := c.post("/v1/inboxes", map[string]any{"address": uniqueName("evtdelivery")})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	inboxID := mustStr(t, body, "id")
	t.Cleanup(func() { c.delete("/v1/inboxes/" + inboxID) }) //nolint

	code, body, err = c.post(fmt.Sprintf("/v1/inboxes/%s/messages/send", inboxID), map[string]any{
		"to":        []map[string]any{{"email": "success@simulator.amazonses.com"}},
		"subject":   "Delivery test " + uniqueName("subj"),
		"body_text": "Integration test delivery event",
	})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	msgID := mustStr(t, body, "id")
	threadID := mustStr(t, body, "thread_id")

	code, body, err = c.get(fmt.Sprintf("/v1/inboxes/%s/threads/%s/messages/%s", inboxID, threadID, msgID))
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 200, body)
	rfc5322MsgID := str(body, "message_id")
	if rfc5322MsgID == "" {
		t.Skip("message has no message_id — cannot inject delivery event")
	}

	ctx := context.Background()
	awsConf, err := awscfg.LoadDefaultConfig(ctx, awscfg.WithRegion(awsRegion))
	if err != nil {
		t.Fatalf("load AWS config: %v", err)
	}
	lambdaClient := lambdasdk.NewFromConfig(awsConf)
	lambdaName := lambdaPrefix + "-ses-events"

	payload := buildSQSEventWithEBPayload("Email Delivered", "ses-injected-"+uniqueName("id"), rfc5322MsgID)
	resp, err := lambdaClient.Invoke(ctx, &lambdasdk.InvokeInput{
		FunctionName: aws.String(lambdaName),
		Payload:      payload,
	})
	if err != nil {
		t.Fatalf("invoke %s: %v", lambdaName, err)
	}
	if resp.FunctionError != nil {
		t.Fatalf("lambda function error: %s — %s", *resp.FunctionError, string(resp.Payload))
	}

	ok := pollUntil(t, 15*time.Second, 1*time.Second, func() bool {
		_, body, err := c.get(fmt.Sprintf("/v1/inboxes/%s/threads/%s/messages/%s", inboxID, threadID, msgID))
		if err != nil {
			return false
		}
		return str(body, "status") == "sent"
	})
	if !ok {
		_, body, _ := c.get(fmt.Sprintf("/v1/inboxes/%s/threads/%s/messages/%s", inboxID, threadID, msgID))
		t.Fatalf("message status never reached 'sent' after delivery event; current status: %s", str(body, "status"))
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
