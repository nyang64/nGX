/*
 * Copyright (c) 2026 nyklabs.com. All rights reserved.
 *
 * Licensed under the nGX Commercial Source License v1.0.
 * See LICENSE file in the project root for full license information.
 */

package integration

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscfg "github.com/aws/aws-sdk-go-v2/config"
	lambdasdk "github.com/aws/aws-sdk-go-v2/service/lambda"
)

// TestSchedulerDrafts verifies the scheduler_drafts Lambda end-to-end:
//  1. Create a draft with scheduled_at set to a time in the past.
//  2. Invoke the scheduler_drafts Lambda directly (simulating a cron trigger).
//  3. Verify the draft is no longer in pending status (it was approved/sent).
//
// Requires:
//
//	TEST_LAMBDA_PREFIX — Lambda name prefix (e.g. ngx-prod)
//	TEST_AWS_REGION    — AWS region (e.g. us-east-1)
func TestSchedulerDrafts(t *testing.T) {
	c := newClient(t)

	lambdaPrefix := os.Getenv("TEST_LAMBDA_PREFIX")
	awsRegion := os.Getenv("TEST_AWS_REGION")
	if lambdaPrefix == "" || awsRegion == "" {
		t.Skip("TEST_LAMBDA_PREFIX and TEST_AWS_REGION must be set")
	}

	// Create a dedicated inbox.
	code, body, err := c.post("/v1/inboxes", map[string]any{"address": uniqueName("sched")})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	inboxID := mustStr(t, body, "id")
	t.Cleanup(func() { c.delete("/v1/inboxes/" + inboxID) }) //nolint

	// Create a draft with scheduled_at in the past (1 minute ago).
	pastTime := time.Now().UTC().Add(-1 * time.Minute).Format(time.RFC3339)
	code, body, err = c.post(fmt.Sprintf("/v1/inboxes/%s/drafts", inboxID), map[string]any{
		"to":           []map[string]any{{"email": "sched-test@example.com"}},
		"subject":      "Scheduler test " + uniqueName("subj"),
		"body_text":    "Integration test scheduled send",
		"scheduled_at": pastTime,
	})
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 201, body)
	draftID := mustStr(t, body, "id")
	t.Logf("Created draft %s with scheduled_at=%s", draftID, pastTime)

	// Verify the draft exists with review_status=pending.
	code, body, err = c.get(fmt.Sprintf("/v1/inboxes/%s/drafts/%s", inboxID, draftID))
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 200, body)
	if str(body, "review_status") != "pending" {
		t.Fatalf("expected review_status=pending before scheduler, got %s", str(body, "review_status"))
	}

	// Invoke the scheduler_drafts Lambda with an empty event (it scans by itself).
	ctx := context.Background()
	awsConf, err := awscfg.LoadDefaultConfig(ctx, awscfg.WithRegion(awsRegion))
	if err != nil {
		t.Fatalf("load AWS config: %v", err)
	}
	lambdaClient := lambdasdk.NewFromConfig(awsConf)
	lambdaName := lambdaPrefix + "-scheduler-drafts"

	resp, err := lambdaClient.Invoke(ctx, &lambdasdk.InvokeInput{
		FunctionName: aws.String(lambdaName),
		Payload:      []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("invoke %s: %v", lambdaName, err)
	}
	if resp.FunctionError != nil {
		t.Fatalf("scheduler lambda error: %s — %s", *resp.FunctionError, string(resp.Payload))
	}
	t.Logf("Scheduler Lambda invoked successfully (status %d)", resp.StatusCode)

	// Poll until the draft disappears from the drafts list (approved = removed).
	ok := pollUntil(t, 15*time.Second, 2*time.Second, func() bool {
		_, listBody, err := c.get(fmt.Sprintf("/v1/inboxes/%s/drafts", inboxID))
		if err != nil {
			return false
		}
		for _, d := range listOf(listBody, "drafts") {
			if str(asMap(d), "id") == draftID {
				return false // still present
			}
		}
		return true // gone
	})
	if !ok {
		t.Fatal("draft still present after scheduler invocation (expected it to be sent and removed)")
	}
	t.Logf("Draft %s was processed and removed by the scheduler", draftID)

	// The draft should have produced a sent message — verify the thread exists.
	t.Run("sent_message_exists", func(t *testing.T) {
		ok := pollUntil(t, 10*time.Second, 2*time.Second, func() bool {
			_, body, err := c.get(fmt.Sprintf("/v1/inboxes/%s/threads", inboxID))
			if err != nil {
				return false
			}
			return len(listOf(body, "threads")) > 0
		})
		if !ok {
			t.Fatal("no thread created after scheduled draft was sent")
		}
	})
}
