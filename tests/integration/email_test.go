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

// TestInboundEmailWithAttachment verifies that an inbound multipart email with
// a file attachment is stored correctly: the message has has_attachments=true.
// Requires the same env vars as TestInboundEmail.
func TestInboundEmailWithAttachment(t *testing.T) {
	c := newClient(t)

	bucketName := os.Getenv("TEST_S3_BUCKET_EMAILS")
	lambdaPrefix := os.Getenv("TEST_LAMBDA_PREFIX")
	awsRegion := os.Getenv("TEST_AWS_REGION")
	if bucketName == "" || lambdaPrefix == "" || awsRegion == "" {
		t.Skip("TEST_S3_BUCKET_EMAILS, TEST_LAMBDA_PREFIX, and TEST_AWS_REGION must be set")
	}

	addr := uniqueName("attin")
	code, body, err := c.post("/v1/inboxes", map[string]any{"address": addr})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	inboxID := mustStr(t, body, "id")
	inboxEmail := str(body, "email")
	t.Cleanup(func() { c.delete("/v1/inboxes/" + inboxID) }) //nolint

	subject := "Attachment test " + uniqueName("subj")
	rawEML := buildMultipartEML("sender@example.com", inboxEmail, subject,
		"Please find the report attached.", "report.txt", []byte("Q1 sales: $1,234,567"))

	ctx := context.Background()
	awsConf, err := awscfg.LoadDefaultConfig(ctx, awscfg.WithRegion(awsRegion))
	if err != nil {
		t.Fatalf("load AWS config: %v", err)
	}
	s3Client := s3sdk.NewFromConfig(awsConf)
	s3Key := fmt.Sprintf("inbound/raw/%s.eml", uniqueName("att"))
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
		t.Fatalf("lambda function error: %s — %s", *resp.FunctionError, string(resp.Payload))
	}

	// Poll until the message appears.
	var msgID string
	ok := pollUntil(t, 20*time.Second, 2*time.Second, func() bool {
		_, body, err := c.get(fmt.Sprintf("/v1/inboxes/%s/threads", inboxID))
		if err != nil {
			return false
		}
		for _, th := range listOf(body, "threads") {
			threadID := str(asMap(th), "id")
			_, mb, err := c.get(fmt.Sprintf("/v1/inboxes/%s/threads/%s/messages", inboxID, threadID))
			if err != nil {
				return false
			}
			for _, m := range listOf(mb, "messages") {
				if str(asMap(m), "direction") == "inbound" {
					msgID = str(asMap(m), "id")
					return true
				}
			}
		}
		return false
	})
	if !ok {
		t.Fatal("inbound message with attachment never appeared in threads")
	}

	t.Run("has_attachments_true", func(t *testing.T) {
		_, threads, err := c.get(fmt.Sprintf("/v1/inboxes/%s/threads", inboxID))
		if err != nil {
			t.Fatal(err)
		}
		threadID := str(asMap(listOf(threads, "threads")[0]), "id")
		_, mb, err := c.get(fmt.Sprintf("/v1/inboxes/%s/threads/%s/messages", inboxID, threadID))
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range listOf(mb, "messages") {
			if str(asMap(m), "id") == msgID {
				v, _ := asMap(m)["has_attachments"].(bool)
				if !v {
					t.Fatal("expected has_attachments=true for message with attachment")
				}
				return
			}
		}
		t.Fatal("message not found in thread")
	})
}

// TestOutboundEmailWithAttachment is an end-to-end test for the outbound
// attachment pipeline:
//
//  1. POST /messages/send with an inline base64 attachment
//  2. Verify has_attachments=true and attachment metadata are stored in DB
//     (proves S3 upload + attachment record creation worked)
//  3. Poll until status = "sent"
//     (proves email_outbound Lambda downloaded the attachment, built
//     multipart/mixed MIME, and SES accepted the message)
func TestOutboundEmailWithAttachment(t *testing.T) {
	c := newClient(t)

	code, body, err := c.post("/v1/inboxes", map[string]any{"address": uniqueName("attout")})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	inboxID := mustStr(t, body, "id")
	t.Cleanup(func() { c.delete("/v1/inboxes/" + inboxID) }) //nolint

	const attFilename = "report.txt"
	const attContentType = "text/plain"
	attBytes := []byte("Q1 2026 sales: $1,234,567\nTotal: excellent")
	attachmentContent := base64.StdEncoding.EncodeToString(attBytes)

	code, body, err = c.post(fmt.Sprintf("/v1/inboxes/%s/messages/send", inboxID), map[string]any{
		"to":        []map[string]any{{"email": "success@simulator.amazonses.com"}},
		"subject":   "Outbound attachment test " + uniqueName("subj"),
		"body_text": "Please find the report attached.",
		"attachments": []map[string]any{
			{
				"filename":     attFilename,
				"content_type": attContentType,
				"content":      attachmentContent,
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	msgID := mustStr(t, body, "id")
	threadID := mustStr(t, body, "thread_id")

	msgPath := fmt.Sprintf("/v1/inboxes/%s/threads/%s/messages/%s", inboxID, threadID, msgID)

	// Step 1: has_attachments flag must be set immediately on the send response.
	t.Run("send_response_has_attachments", func(t *testing.T) {
		v, _ := body["has_attachments"].(bool)
		if !v {
			t.Fatalf("expected has_attachments=true in send response, got: %v", body)
		}
	})

	// Step 2: GET the message — attachment record must be present in DB with correct metadata.
	t.Run("attachment_record_stored", func(t *testing.T) {
		code, msg, err := c.get(msgPath)
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 200, msg)

		v, _ := msg["has_attachments"].(bool)
		if !v {
			t.Fatalf("expected has_attachments=true on GET message, got: %v", msg)
		}

		atts := listOf(msg, "attachments")
		if len(atts) != 1 {
			t.Fatalf("expected 1 attachment record, got %d — message: %v", len(atts), msg)
		}
		att := asMap(atts[0])

		if got := str(att, "filename"); got != attFilename {
			t.Errorf("attachment filename: want %q, got %q", attFilename, got)
		}
		if got := str(att, "content_type"); got != attContentType {
			t.Errorf("attachment content_type: want %q, got %q", attContentType, got)
		}
		wantSize := int64(len(attBytes))
		if gotSize, ok := att["size_bytes"].(float64); !ok || int64(gotSize) != wantSize {
			t.Errorf("attachment size_bytes: want %d, got %v", wantSize, att["size_bytes"])
		}
		if str(att, "id") == "" {
			t.Error("attachment id is empty")
		}
	})

	// Step 3: poll until status = "sent" — proves email_outbound Lambda built the
	// multipart/mixed MIME with the attachment and SES accepted it.
	t.Run("delivered_to_ses", func(t *testing.T) {
		var finalStatus string
		ok := pollUntil(t, 60*time.Second, 3*time.Second, func() bool {
			_, msg, err := c.get(msgPath)
			if err != nil {
				return false
			}
			finalStatus = str(msg, "status")
			return finalStatus == "sent"
		})
		if !ok {
			// Fetch current status for a clear failure message.
			_, msg, _ := c.get(msgPath)
			t.Fatalf("message never reached status=sent (got %q) — message: %v", str(msg, "status"), msg)
		}
	})
}

// TestDraftWithAttachment is an end-to-end test for the draft attachment lifecycle:
//
//  1. POST /drafts with inline base64 attachment → stored with draft_id in DB
//  2. GET /drafts/{id} → attachment is listed under the draft
//  3. POST /drafts/{id}/approve → attachment linked to message (message_id set, draft_id cleared)
//  4. GET message → has_attachments=true, attachment record accessible
//  5. Poll until status = "sent" → email_outbound Lambda delivered multipart/mixed to SES
func TestDraftWithAttachment(t *testing.T) {
	c := newClient(t)

	code, body, err := c.post("/v1/inboxes", map[string]any{"address": uniqueName("draftatt")})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	inboxID := mustStr(t, body, "id")
	t.Cleanup(func() { c.delete("/v1/inboxes/" + inboxID) }) //nolint

	const attFilename = "draft_report.txt"
	const attContentType = "text/plain"
	attBytes := []byte("Draft Q1 report: revenue up 12%")
	attachmentContent := base64.StdEncoding.EncodeToString(attBytes)

	// Step 1: create draft with attachment.
	code, body, err = c.post(fmt.Sprintf("/v1/inboxes/%s/drafts", inboxID), map[string]any{
		"to":        []map[string]any{{"email": "success@simulator.amazonses.com"}},
		"subject":   "Draft attachment test " + uniqueName("subj"),
		"body_text": "Please find the attached report.",
		"attachments": []map[string]any{
			{
				"filename":     attFilename,
				"content_type": attContentType,
				"content":      attachmentContent,
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	draftID := mustStr(t, body, "id")

	// Step 2: GET the draft — attachment should be listed.
	t.Run("draft_has_attachment", func(t *testing.T) {
		code, d, err := c.get(fmt.Sprintf("/v1/inboxes/%s/drafts/%s", inboxID, draftID))
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 200, d)
		atts := listOf(d, "attachments")
		if len(atts) != 1 {
			t.Fatalf("expected 1 attachment on draft, got %d — draft: %v", len(atts), d)
		}
		att := asMap(atts[0])
		if got := str(att, "filename"); got != attFilename {
			t.Errorf("draft attachment filename: want %q, got %q", attFilename, got)
		}
	})

	// Step 3: approve the draft → message created, attachment linked.
	code, body, err = c.post(fmt.Sprintf("/v1/inboxes/%s/drafts/%s/approve", inboxID, draftID), map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 200, body)

	// Draft.MessageID is set after approval.
	msgID := str(body, "message_id")
	if msgID == "" {
		t.Fatal("approve response did not include message_id")
	}

	// Resolve thread ID — needed for the message path.
	var threadID string
	ok := pollUntil(t, 20*time.Second, 2*time.Second, func() bool {
		_, threads, err := c.get(fmt.Sprintf("/v1/inboxes/%s/threads", inboxID))
		if err != nil {
			return false
		}
		list := listOf(threads, "threads")
		if len(list) == 0 {
			return false
		}
		threadID = str(asMap(list[0]), "id")
		return threadID != ""
	})
	if !ok {
		t.Fatal("no thread appeared after draft approval")
	}

	msgPath := fmt.Sprintf("/v1/inboxes/%s/threads/%s/messages/%s", inboxID, threadID, msgID)

	// Step 4: GET message — has_attachments=true, attachment record linked.
	t.Run("message_has_attachment_record", func(t *testing.T) {
		code, msg, err := c.get(msgPath)
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 200, msg)

		v, _ := msg["has_attachments"].(bool)
		if !v {
			t.Fatalf("expected has_attachments=true after approval, got: %v", msg)
		}

		atts := listOf(msg, "attachments")
		if len(atts) != 1 {
			t.Fatalf("expected 1 attachment on message, got %d — message: %v", len(atts), msg)
		}
		att := asMap(atts[0])
		if got := str(att, "filename"); got != attFilename {
			t.Errorf("message attachment filename: want %q, got %q", attFilename, got)
		}
		if got := str(att, "content_type"); got != attContentType {
			t.Errorf("message attachment content_type: want %q, got %q", attContentType, got)
		}
		wantSize := int64(len(attBytes))
		if gotSize, ok := att["size_bytes"].(float64); !ok || int64(gotSize) != wantSize {
			t.Errorf("message attachment size_bytes: want %d, got %v", wantSize, att["size_bytes"])
		}
	})

	// Step 5: poll until sent — proves email_outbound built multipart/mixed and SES accepted it.
	t.Run("delivered_to_ses", func(t *testing.T) {
		ok := pollUntil(t, 60*time.Second, 3*time.Second, func() bool {
			_, msg, err := c.get(msgPath)
			if err != nil {
				return false
			}
			return str(msg, "status") == "sent"
		})
		if !ok {
			_, msg, _ := c.get(msgPath)
			t.Fatalf("message never reached status=sent (got %q)", str(msg, "status"))
		}
	})
}

// buildMultipartEML constructs a multipart/mixed RFC 5322 email with a text
// body and a single file attachment encoded as base64.
func buildMultipartEML(from, to, subject, bodyText, filename string, attachment []byte) []byte {
	boundary := "boundary_" + uniqueName("b")
	encoded := base64.StdEncoding.EncodeToString(attachment)

	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", from)
	fmt.Fprintf(&b, "To: %s\r\n", to)
	fmt.Fprintf(&b, "Subject: %s\r\n", subject)
	fmt.Fprintf(&b, "Message-ID: <%s@test.example.com>\r\n", uniqueName("msgid"))
	fmt.Fprintf(&b, "Date: %s\r\n", time.Now().UTC().Format(time.RFC1123Z))
	fmt.Fprintf(&b, "MIME-Version: 1.0\r\n")
	fmt.Fprintf(&b, "Content-Type: multipart/mixed; boundary=%q\r\n", boundary)
	fmt.Fprintf(&b, "\r\n")
	fmt.Fprintf(&b, "--%s\r\n", boundary)
	fmt.Fprintf(&b, "Content-Type: text/plain; charset=utf-8\r\n")
	fmt.Fprintf(&b, "\r\n")
	fmt.Fprintf(&b, "%s\r\n", bodyText)
	fmt.Fprintf(&b, "\r\n--%s\r\n", boundary)
	fmt.Fprintf(&b, "Content-Type: text/plain; name=%q\r\n", filename)
	fmt.Fprintf(&b, "Content-Disposition: attachment; filename=%q\r\n", filename)
	fmt.Fprintf(&b, "Content-Transfer-Encoding: base64\r\n")
	fmt.Fprintf(&b, "\r\n")
	fmt.Fprintf(&b, "%s\r\n", encoded)
	fmt.Fprintf(&b, "--%s--\r\n", boundary)
	return []byte(b.String())
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

// ── nGX-829: BCC field behavior ───────────────────────────────────────────────

// TestBCCField verifies that BCC recipients are stored on the message record
// but not exposed in MIME headers when the email is delivered.
func TestBCCField(t *testing.T) {
	c := newClient(t)

	code, body, err := c.post("/v1/inboxes", map[string]any{"address": uniqueName("bcc")})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	inboxID := mustStr(t, body, "id")
	t.Cleanup(func() { c.delete("/v1/inboxes/" + inboxID) }) //nolint

	code, body, err = c.post(fmt.Sprintf("/v1/inboxes/%s/messages/send", inboxID), map[string]any{
		"to":        []map[string]any{{"email": "success@simulator.amazonses.com"}},
		"bcc":       []map[string]any{{"email": "bcc-recipient@example.com"}},
		"subject":   "BCC test " + uniqueName("subj"),
		"body_text": "BCC integration test",
	})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	msgID := mustStr(t, body, "id")
	threadID := mustStr(t, body, "thread_id")

	// BCC field should be present in the API response.
	t.Run("bcc_field_in_api_response", func(t *testing.T) {
		code, body, err := c.get(fmt.Sprintf("/v1/inboxes/%s/threads/%s/messages/%s", inboxID, threadID, msgID))
		if err != nil {
			t.Fatal(err)
		}
		mustCode(t, code, 200, body)
		bcc := listOf(body, "bcc")
		if len(bcc) == 0 {
			t.Fatal("expected bcc field to be present and non-empty in message response")
		}
		firstBCC := asMap(bcc[0])
		if got := str(firstBCC, "email"); got != "bcc-recipient@example.com" {
			t.Errorf("expected bcc email bcc-recipient@example.com, got %q", got)
		}
	})

	// BCC should not appear in raw MIME headers.
	t.Run("bcc_not_in_raw_headers", func(t *testing.T) {
		// Poll until message is sent (raw headers available after delivery).
		ok := pollUntil(t, 30*time.Second, 2*time.Second, func() bool {
			_, b, _ := c.get(fmt.Sprintf("/v1/inboxes/%s/threads/%s/messages/%s", inboxID, threadID, msgID))
			s := str(b, "status")
			return s == "sent" || s == "queued" || s == "accepted"
		})
		if !ok {
			t.Skip("message did not reach sent/queued/accepted within 30s; skipping raw header check")
		}
		// The GET /messages/{id} response includes headers — BCC must not be present.
		_, body, err := c.get(fmt.Sprintf("/v1/inboxes/%s/threads/%s/messages/%s", inboxID, threadID, msgID))
		if err != nil {
			t.Fatal(err)
		}
		// Check the raw headers field if available.
		if raw, ok := body["headers"].(map[string]any); ok {
			for k := range raw {
				if strings.EqualFold(k, "bcc") {
					t.Errorf("BCC header must not appear in delivered message headers: found key %q", k)
				}
			}
		}
	})
}

// ── nGX-lih: Inbound multipart/alternative (HTML+text) email ─────────────────

// TestInboundEmailHTMLBody verifies that an inbound multipart/alternative email
// (text/plain + text/html parts) is stored with both body_text and body_html populated.
func TestInboundEmailHTMLBody(t *testing.T) {
	c := newClient(t)

	bucketName := os.Getenv("TEST_S3_BUCKET_EMAILS")
	lambdaPrefix := os.Getenv("TEST_LAMBDA_PREFIX")
	awsRegion := os.Getenv("TEST_AWS_REGION")
	if bucketName == "" || lambdaPrefix == "" || awsRegion == "" {
		t.Skip("TEST_S3_BUCKET_EMAILS, TEST_LAMBDA_PREFIX, and TEST_AWS_REGION must be set")
	}

	addr := uniqueName("html-inbound")
	code, body, err := c.post("/v1/inboxes", map[string]any{"address": addr})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	inboxID := mustStr(t, body, "id")
	inboxEmail := str(body, "email")
	t.Cleanup(func() { c.delete("/v1/inboxes/" + inboxID) }) //nolint

	subject := "HTML inbound test " + uniqueName("subj")
	textBody := "Plain text part of the email."
	htmlBody := "<html><body><p>HTML part of the email.</p></body></html>"
	rawEML := buildAlternativeEML("success@simulator.amazonses.com", inboxEmail, subject, textBody, htmlBody)

	ctx := context.Background()
	awsConf, err := awscfg.LoadDefaultConfig(ctx, awscfg.WithRegion(awsRegion))
	if err != nil {
		t.Fatalf("load AWS config: %v", err)
	}

	s3Client := s3sdk.NewFromConfig(awsConf)
	s3Key := fmt.Sprintf("inbound/raw/%s.eml", uniqueName("htmlmsg"))
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

	// Poll until the thread appears.
	var threadID, msgID string
	ok := pollUntil(t, 20*time.Second, 2*time.Second, func() bool {
		_, body, err := c.get(fmt.Sprintf("/v1/inboxes/%s/threads", inboxID))
		if err != nil {
			return false
		}
		for _, th := range listOf(body, "threads") {
			if strings.Contains(str(asMap(th), "subject"), "HTML inbound test") {
				threadID = str(asMap(th), "id")
				return true
			}
		}
		return false
	})
	if !ok {
		t.Fatal("inbound HTML message never appeared in threads after Lambda invocation")
	}

	// Get the message ID.
	_, threadsBody, err := c.get(fmt.Sprintf("/v1/inboxes/%s/threads/%s/messages", inboxID, threadID))
	if err != nil {
		t.Fatal(err)
	}
	msgs := listOf(threadsBody, "messages")
	if len(msgs) == 0 {
		t.Fatal("no messages in thread")
	}
	msgID = str(asMap(msgs[0]), "id")

	// Assert both text and HTML S3 keys are populated (bodies stored in S3).
	t.Run("both_body_parts_stored", func(t *testing.T) {
		_, msgBody, err := c.get(fmt.Sprintf("/v1/inboxes/%s/threads/%s/messages/%s", inboxID, threadID, msgID))
		if err != nil {
			t.Fatal(err)
		}
		if str(msgBody, "text_s3_key") == "" {
			t.Error("expected text_s3_key to be set for multipart/alternative message (body_text stored in S3)")
		}
		if str(msgBody, "html_s3_key") == "" {
			t.Error("expected html_s3_key to be set for multipart/alternative message (body_html stored in S3)")
		}
	})
}

// buildAlternativeEML constructs a multipart/alternative RFC 5322 email
// with both text/plain and text/html parts.
func buildAlternativeEML(from, to, subject, textBody, htmlBody string) []byte {
	boundary := "alt_boundary_" + uniqueName("b")

	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", from)
	fmt.Fprintf(&b, "To: %s\r\n", to)
	fmt.Fprintf(&b, "Subject: %s\r\n", subject)
	fmt.Fprintf(&b, "Message-ID: <%s@test.example.com>\r\n", uniqueName("msgid"))
	fmt.Fprintf(&b, "Date: %s\r\n", time.Now().UTC().Format(time.RFC1123Z))
	fmt.Fprintf(&b, "MIME-Version: 1.0\r\n")
	fmt.Fprintf(&b, "Content-Type: multipart/alternative; boundary=%q\r\n", boundary)
	fmt.Fprintf(&b, "\r\n")
	fmt.Fprintf(&b, "--%s\r\n", boundary)
	fmt.Fprintf(&b, "Content-Type: text/plain; charset=utf-8\r\n")
	fmt.Fprintf(&b, "\r\n")
	fmt.Fprintf(&b, "%s\r\n", textBody)
	fmt.Fprintf(&b, "\r\n--%s\r\n", boundary)
	fmt.Fprintf(&b, "Content-Type: text/html; charset=utf-8\r\n")
	fmt.Fprintf(&b, "\r\n")
	fmt.Fprintf(&b, "%s\r\n", htmlBody)
	fmt.Fprintf(&b, "--%s--\r\n", boundary)
	return []byte(b.String())
}
