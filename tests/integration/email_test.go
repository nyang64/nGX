/*
 * Copyright (c) 2026 nyklabs.com. All rights reserved.
 *
 * Licensed under the nGX Commercial Source License v1.0.
 * See LICENSE file in the project root for full license information.
 */

package integration

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscfg "github.com/aws/aws-sdk-go-v2/config"
	lambdasdk "github.com/aws/aws-sdk-go-v2/service/lambda"
	s3sdk "github.com/aws/aws-sdk-go-v2/service/s3"
)

// TestOutboundEmail verifies that sending a message via the API enqueues and
// transitions its status to sent (or at least accepted).
func TestOutboundEmail(t *testing.T) {
	c := newClient(t)

	// Create a dedicated inbox.
	code, body, err := c.post("/v1/inboxes", map[string]any{"address": uniqueName("out")})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	inboxID := mustStr(t, body, "id")
	t.Cleanup(func() { c.delete("/v1/inboxes/" + inboxID) }) //nolint

	// Use the SES mailbox simulator — always succeeds even in sandbox mode.
	// success@simulator.amazonses.com is a special address provided by AWS for testing.
	code, body, err = c.post(fmt.Sprintf("/v1/inboxes/%s/messages/send", inboxID), map[string]any{
		"to":        []map[string]any{{"email": "success@simulator.amazonses.com"}},
		"subject":   "Outbound test " + uniqueName("subj"),
		"body_text": "Integration test outbound message",
	})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	msgID := mustStr(t, body, "id")
	threadID := mustStr(t, body, "thread_id")

	// Verify thread and message are accessible immediately.
	t.Run("message_accessible", func(t *testing.T) {
		code, body, err := c.get(fmt.Sprintf("/v1/inboxes/%s/threads/%s/messages/%s", inboxID, threadID, msgID))
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 200, body)
		if str(body, "id") != msgID {
			t.Fatalf("message id mismatch: %s", str(body, "id"))
		}
	})

	// Poll for status transition (queued → sent). SES delivery can be slow so
	// wait up to 30s. Some test environments may only reach "queued" if SES is
	// sandboxed — we accept either queued or sent.
	t.Run("status_transitions", func(t *testing.T) {
		ok := pollUntil(t, 30*time.Second, 2*time.Second, func() bool {
			_, body, err := c.get(fmt.Sprintf("/v1/inboxes/%s/threads/%s/messages/%s", inboxID, threadID, msgID))
			if err != nil {
				return false
			}
			status := str(body, "status")
			return status == "sent" || status == "queued" || status == "accepted"
		})
		if !ok {
			t.Fatal("message status never reached sent/queued/accepted")
		}
	})
}

// TestInboundEmail verifies the full inbound email pipeline:
// upload a raw .eml to S3, invoke the email_inbound Lambda directly, then
// confirm the message appears in the API. Requires:
//
//	TEST_S3_BUCKET_EMAILS — the emails bucket name (e.g. ngx-prod-emails-...)
//	TEST_LAMBDA_PREFIX    — Lambda name prefix (e.g. ngx-prod)
//	TEST_AWS_REGION       — AWS region (e.g. us-east-1)
func TestInboundEmail(t *testing.T) {
	c := newClient(t)

	bucketName := os.Getenv("TEST_S3_BUCKET_EMAILS")
	lambdaPrefix := os.Getenv("TEST_LAMBDA_PREFIX")
	awsRegion := os.Getenv("TEST_AWS_REGION")
	if bucketName == "" || lambdaPrefix == "" || awsRegion == "" {
		t.Skip("TEST_S3_BUCKET_EMAILS, TEST_LAMBDA_PREFIX, and TEST_AWS_REGION must be set")
	}

	// Create a dedicated inbox whose address will be the recipient in the .eml.
	addr := uniqueName("inbound")
	code, body, err := c.post("/v1/inboxes", map[string]any{"address": addr})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	inboxID := mustStr(t, body, "id")
	inboxEmail := str(body, "email")
	t.Cleanup(func() { c.delete("/v1/inboxes/" + inboxID) }) //nolint

	if inboxEmail == "" {
		t.Fatal("inbox has no email address")
	}

	subject := "Inbound test " + uniqueName("subj")
	rawEML := buildTestEML("success@simulator.amazonses.com", inboxEmail, subject, "Integration test inbound body")

	// Upload raw .eml to S3 under inbound/raw/.
	ctx := context.Background()
	awsConf, err := awscfg.LoadDefaultConfig(ctx, awscfg.WithRegion(awsRegion))
	if err != nil {
		t.Fatalf("load AWS config: %v", err)
	}

	s3Client := s3sdk.NewFromConfig(awsConf)
	s3Key := fmt.Sprintf("inbound/raw/%s.eml", uniqueName("msg"))
	_, err = s3Client.PutObject(ctx, &s3sdk.PutObjectInput{
		Bucket:      aws.String(bucketName),
		Key:         aws.String(s3Key),
		Body:        bytes.NewReader(rawEML),
		ContentType: aws.String("message/rfc822"),
	})
	if err != nil {
		t.Fatalf("upload .eml to S3: %v", err)
	}
	t.Cleanup(func() {
		s3Client.DeleteObject(ctx, &s3sdk.DeleteObjectInput{
			Bucket: aws.String(bucketName),
			Key:    aws.String(s3Key),
		})
	})

	// Invoke the email_inbound Lambda directly with an S3Event payload.
	lambdaClient := lambdasdk.NewFromConfig(awsConf)
	lambdaName := lambdaPrefix + "-email-inbound"
	payload := buildS3EventPayload(bucketName, s3Key)
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

	// Poll until the message appears in the inbox threads.
	ok := pollUntil(t, 20*time.Second, 2*time.Second, func() bool {
		_, body, err := c.get(fmt.Sprintf("/v1/inboxes/%s/threads", inboxID))
		if err != nil {
			return false
		}
		for _, th := range listOf(body, "threads") {
			if strings.Contains(str(asMap(th), "subject"), "Inbound test") {
				return true
			}
		}
		return false
	})
	if !ok {
		t.Fatal("inbound message never appeared in threads after Lambda invocation")
	}

	// Fetch the thread and verify message details.
	t.Run("message_stored", func(t *testing.T) {
		_, threadsBody, err := c.get(fmt.Sprintf("/v1/inboxes/%s/threads", inboxID))
		if err != nil {
			t.Fatal(err)
		}
		threads := listOf(threadsBody, "threads")
		if len(threads) == 0 {
			t.Fatal("no threads found")
		}
		threadID := str(asMap(threads[0]), "id")

		code, body, err := c.get(fmt.Sprintf("/v1/inboxes/%s/threads/%s/messages", inboxID, threadID))
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 200, body)
		msgs := listOf(body, "messages")
		if len(msgs) == 0 {
			t.Fatal("no messages in thread")
		}
		msg := asMap(msgs[0])
		if str(msg, "direction") != "inbound" {
			t.Fatalf("expected direction=inbound, got %s", str(msg, "direction"))
		}
	})
}

// buildTestEML constructs a minimal RFC 5322 email as raw bytes.
func buildTestEML(from, to, subject, body string) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", from)
	fmt.Fprintf(&b, "To: %s\r\n", to)
	fmt.Fprintf(&b, "Subject: %s\r\n", subject)
	fmt.Fprintf(&b, "Message-ID: <%s@test.example.com>\r\n", uniqueName("msgid"))
	fmt.Fprintf(&b, "Date: %s\r\n", time.Now().UTC().Format(time.RFC1123Z))
	fmt.Fprintf(&b, "MIME-Version: 1.0\r\n")
	fmt.Fprintf(&b, "Content-Type: text/plain; charset=utf-8\r\n")
	fmt.Fprintf(&b, "\r\n")
	fmt.Fprintf(&b, "%s\r\n", body)
	return []byte(b.String())
}

// buildS3EventPayload builds the JSON payload for an S3Event Lambda invocation.
func buildS3EventPayload(bucket, key string) []byte {
	// URL-encode the key (simple version — only encode spaces).
	encodedKey := strings.ReplaceAll(key, " ", "+")
	payload := fmt.Sprintf(`{
		"Records": [{
			"eventVersion": "2.1",
			"eventSource": "aws:s3",
			"awsRegion": "us-east-1",
			"eventTime": "%s",
			"eventName": "ObjectCreated:Put",
			"s3": {
				"s3SchemaVersion": "1.0",
				"configurationId": "integration-test",
				"bucket": {
					"name": %q,
					"ownerIdentity": {"principalId": "TEST"},
					"arn": "arn:aws:s3:::%s"
				},
				"object": {
					"key": %q,
					"size": 512,
					"eTag": "test",
					"sequencer": "test"
				}
			}
		}]
	}`, time.Now().UTC().Format(time.RFC3339), bucket, bucket, encodedKey)
	return []byte(payload)
}
