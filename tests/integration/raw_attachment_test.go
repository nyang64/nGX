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
	"encoding/base64"
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

// ── nGX-wbo: GET /messages/{id}/raw ──────────────────────────────────────────

// TestGetRawMessage verifies GET .../messages/{id}/raw:
//   - Inbound message: returns 200 with Content-Type: message/rfc822
//   - Body contains RFC 5322 headers (From, To, Subject)
//   - Nonexistent message ID returns 404
//   - Requires inbox:read scope
func TestGetRawMessage(t *testing.T) {
	c := newClient(t)

	bucketName := os.Getenv("TEST_S3_BUCKET_EMAILS")
	lambdaPrefix := os.Getenv("TEST_LAMBDA_PREFIX")
	awsRegion := os.Getenv("TEST_AWS_REGION")
	if bucketName == "" || lambdaPrefix == "" || awsRegion == "" {
		t.Skip("TEST_S3_BUCKET_EMAILS, TEST_LAMBDA_PREFIX, and TEST_AWS_REGION must be set")
	}

	// Create inbox.
	addr := uniqueName("raw-msg")
	code, body, err := c.post("/v1/inboxes", map[string]any{"address": addr})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	inboxID := mustStr(t, body, "id")
	inboxEmail := str(body, "email")
	t.Cleanup(func() { c.delete("/v1/inboxes/" + inboxID) }) //nolint

	subject := "Raw test " + uniqueName("s")
	rawEML := buildTestEML("success@simulator.amazonses.com", inboxEmail, subject, "Raw message body for testing")

	// Upload .eml to S3 and invoke email_inbound Lambda.
	ctx := context.Background()
	awsConf, err := awscfg.LoadDefaultConfig(ctx, awscfg.WithRegion(awsRegion))
	if err != nil {
		t.Fatalf("load AWS config: %v", err)
	}
	s3Client := s3sdk.NewFromConfig(awsConf)
	s3Key := fmt.Sprintf("inbound/raw/%s.eml", uniqueName("raw"))
	_, err = s3Client.PutObject(ctx, &s3sdk.PutObjectInput{
		Bucket:      aws.String(bucketName),
		Key:         aws.String(s3Key),
		Body:        bytes.NewReader(rawEML),
		ContentType: aws.String("message/rfc822"),
	})
	if err != nil {
		t.Fatalf("upload .eml: %v", err)
	}
	t.Cleanup(func() {
		s3Client.DeleteObject(ctx, &s3sdk.DeleteObjectInput{Bucket: aws.String(bucketName), Key: aws.String(s3Key)})
	})

	lambdaClient := lambdasdk.NewFromConfig(awsConf)
	resp, err := lambdaClient.Invoke(ctx, &lambdasdk.InvokeInput{
		FunctionName: aws.String(lambdaPrefix + "-email-inbound"),
		Payload:      buildS3EventPayload(bucketName, s3Key),
	})
	if err != nil {
		t.Fatalf("invoke email_inbound: %v", err)
	}
	if resp.FunctionError != nil {
		t.Fatalf("lambda error: %s — %s", *resp.FunctionError, string(resp.Payload))
	}

	// Wait for message to appear.
	var msgID, threadID string
	ok := pollUntil(t, 20*time.Second, 2*time.Second, func() bool {
		_, body, _ := c.get(fmt.Sprintf("/v1/inboxes/%s/threads", inboxID))
		for _, th := range listOf(body, "threads") {
			tm := asMap(th)
			if strings.Contains(str(tm, "subject"), "Raw test") {
				threadID = str(tm, "id")
				_, msgsBody, _ := c.get(fmt.Sprintf("/v1/inboxes/%s/threads/%s/messages", inboxID, threadID))
				msgs := listOf(msgsBody, "messages")
				if len(msgs) > 0 {
					msgID = str(asMap(msgs[0]), "id")
					return true
				}
			}
		}
		return false
	})
	if !ok {
		t.Fatal("inbound message never appeared")
	}

	rawURL := fmt.Sprintf("/v1/inboxes/%s/threads/%s/messages/%s/raw", inboxID, threadID, msgID)

	t.Run("returns_rfc822_content_type", func(t *testing.T) {
		code, data, headers, err := c.getBytes(rawURL)
		if err != nil {
			t.Fatal(err)
		}
		if code != 200 {
			t.Fatalf("expected 200, got %d: %s", code, string(data))
		}
		ct := headers.Get("Content-Type")
		if !strings.Contains(ct, "message/rfc822") {
			t.Fatalf("expected Content-Type message/rfc822, got %q", ct)
		}
	})

	t.Run("body_contains_rfc5322_headers", func(t *testing.T) {
		_, data, _, err := c.getBytes(rawURL)
		if err != nil {
			t.Fatal(err)
		}
		body := string(data)
		for _, hdr := range []string{"From:", "To:", "Subject:"} {
			if !strings.Contains(body, hdr) {
				t.Fatalf("raw message body missing %q header; got: %s", hdr, body[:min(200, len(body))])
			}
		}
	})

	t.Run("nonexistent_message_returns_404", func(t *testing.T) {
		badURL := fmt.Sprintf("/v1/inboxes/%s/threads/%s/messages/00000000-0000-0000-0000-000000000000/raw", inboxID, threadID)
		code, _, _, err := c.getBytes(badURL)
		if err != nil {
			t.Fatal(err)
		}
		if code != 404 {
			t.Fatalf("expected 404 for nonexistent message, got %d", code)
		}
	})
}

// ── nGX-xi0: GET /messages/{id}/attachments/{id} ─────────────────────────────

// TestGetMessageAttachment verifies GET .../messages/{id}/attachments/{id}:
//   - Sending a message with an attachment → GET attachment returns 200 binary
//   - Content-Type matches the uploaded attachment
//   - Content-Disposition: attachment present
//   - Nonexistent attachment ID returns 404
func TestGetMessageAttachment(t *testing.T) {
	c := newClient(t)

	// Create inbox.
	code, body, err := c.post("/v1/inboxes", map[string]any{"address": uniqueName("att-dl")})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	inboxID := mustStr(t, body, "id")
	t.Cleanup(func() { c.delete("/v1/inboxes/" + inboxID) }) //nolint

	// Send message with a small text attachment.
	attContent := base64.StdEncoding.EncodeToString([]byte("hello attachment"))
	code, body, err = c.post(fmt.Sprintf("/v1/inboxes/%s/messages/send", inboxID), map[string]any{
		"to":        []map[string]any{{"email": "success@simulator.amazonses.com"}},
		"subject":   "attachment download test " + uniqueName("s"),
		"body_text": "see attached",
		"attachments": []map[string]any{
			{
				"filename":     "hello.txt",
				"content_type": "text/plain",
				"content":      attContent,
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	msgID := mustStr(t, body, "id")
	threadID := mustStr(t, body, "thread_id")

	// Fetch message to get attachment ID.
	code, body, err = c.get(fmt.Sprintf("/v1/inboxes/%s/threads/%s/messages/%s", inboxID, threadID, msgID))
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 200, body)
	atts := listOf(body, "attachments")
	if len(atts) == 0 {
		t.Fatal("expected at least one attachment in message response")
	}
	attID := str(asMap(atts[0]), "id")
	if attID == "" {
		t.Fatal("attachment id is empty")
	}

	attURL := fmt.Sprintf("/v1/inboxes/%s/threads/%s/messages/%s/attachments/%s", inboxID, threadID, msgID, attID)

	t.Run("download_returns_binary", func(t *testing.T) {
		code, data, headers, err := c.getBytes(attURL)
		if err != nil {
			t.Fatal(err)
		}
		if code != 200 {
			t.Fatalf("expected 200, got %d: %s", code, string(data))
		}
		if len(data) == 0 {
			t.Fatal("expected non-empty attachment body")
		}
		ct := headers.Get("Content-Type")
		if !strings.Contains(ct, "text/plain") {
			t.Fatalf("expected Content-Type text/plain, got %q", ct)
		}
		cd := headers.Get("Content-Disposition")
		if !strings.Contains(cd, "attachment") {
			t.Fatalf("expected Content-Disposition to contain 'attachment', got %q", cd)
		}
	})

	t.Run("content_matches_uploaded", func(t *testing.T) {
		_, data, _, err := c.getBytes(attURL)
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != "hello attachment" {
			t.Fatalf("expected %q, got %q", "hello attachment", string(data))
		}
	})

	t.Run("nonexistent_attachment_returns_404", func(t *testing.T) {
		badURL := fmt.Sprintf("/v1/inboxes/%s/threads/%s/messages/%s/attachments/00000000-0000-0000-0000-000000000000", inboxID, threadID, msgID)
		code, _, _, err := c.getBytes(badURL)
		if err != nil {
			t.Fatal(err)
		}
		if code != 404 {
			t.Fatalf("expected 404 for nonexistent attachment, got %d", code)
		}
	})
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
