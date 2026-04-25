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
	"fmt"
	"log/slog"
	"os"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/jackc/pgx/v5/pgxpool"

	"agentmail/lambdas/shared"
	"agentmail/pkg/models"
	emailstore "agentmail/services/email-pipeline/store"
)

var (
	pool    *pgxpool.Pool
	emailSt *emailstore.EmailStore
)

func init() {
	// Skip DB init when DATABASE_URL is not set (e.g. unit test environments).
	if os.Getenv("DATABASE_URL") == "" {
		return
	}
	pool = shared.InitDB()
	emailSt = emailstore.NewEmailStore(pool)
}

// sesMailHeader is a single header from the SES notification.
type sesMailHeader struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// sesMail contains the original email metadata in the SES notification.
type sesMail struct {
	MessageID string          `json:"messageId"`
	Headers   []sesMailHeader `json:"headers"`
}

// ebSESEvent is the EventBridge envelope for native SES events.
// SES publishes to the default event bus with source "aws.ses".
// Official detail-type values: https://docs.aws.amazon.com/ses/latest/dg/monitoring-eventbridge.html
type ebSESEvent struct {
	DetailType string   `json:"detail-type"`
	Detail     ebDetail `json:"detail"`
}

type ebDetail struct {
	Mail sesMail `json:"mail"`
}

func handler(ctx context.Context, sqsEvent events.SQSEvent) (events.SQSEventResponse, error) {
	var failures []events.SQSBatchItemFailure
	for _, record := range sqsEvent.Records {
		if err := processRecord(ctx, record); err != nil {
			slog.Error("ses_events: process record failed", "id", record.MessageId, "error", err)
			failures = append(failures, events.SQSBatchItemFailure{ItemIdentifier: record.MessageId})
		}
	}
	return events.SQSEventResponse{BatchItemFailures: failures}, nil
}

func processRecord(ctx context.Context, record events.SQSMessage) error {
	// SES → EventBridge → SQS: the SQS body is the EventBridge event JSON.
	var evt ebSESEvent
	if err := json.Unmarshal([]byte(record.Body), &evt); err != nil {
		slog.Warn("ses_events: unmarshal EventBridge event, skipping", "error", err)
		return nil
	}

	// Extract RFC 5322 Message-ID from the mail headers.
	var rfc5322MsgID string
	for _, h := range evt.Detail.Mail.Headers {
		if h.Name == "Message-ID" || h.Name == "Message-Id" {
			rfc5322MsgID = h.Value
			// Strip angle brackets.
			if len(rfc5322MsgID) > 2 && rfc5322MsgID[0] == '<' {
				rfc5322MsgID = rfc5322MsgID[1 : len(rfc5322MsgID)-1]
			}
			break
		}
	}

	if rfc5322MsgID == "" {
		slog.Debug("ses_events: no Message-ID header, skipping", "ses_msg_id", evt.Detail.Mail.MessageID)
		return nil
	}

	switch evt.DetailType {
	case "Email Bounced":
		if pool == nil {
			return fmt.Errorf("ses_events: database pool not initialised")
		}
		_, err := pool.Exec(ctx,
			`UPDATE messages SET status = $1, updated_at = NOW() WHERE message_id_header = $2`,
			string(models.MessageStatusBounced), rfc5322MsgID,
		)
		if err != nil {
			return err
		}
		slog.Info("ses_events: marked message bounced", "message_id", rfc5322MsgID)

	case "Email Complaint Received", "Email Rejected", "Email Rendering Failed":
		if pool == nil {
			return fmt.Errorf("ses_events: database pool not initialised")
		}
		_, err := pool.Exec(ctx,
			`UPDATE messages SET status = $1, updated_at = NOW() WHERE message_id_header = $2`,
			string(models.MessageStatusFailed), rfc5322MsgID,
		)
		if err != nil {
			return err
		}
		slog.Info("ses_events: marked message failed", "type", evt.DetailType, "message_id", rfc5322MsgID)

	case "Email Delivered":
		if pool == nil {
			return fmt.Errorf("ses_events: database pool not initialised")
		}
		// Always update sent_at to the SES delivery confirmation time.
		// Also advance status to 'sent' if still in 'sending' (e.g. if email_outbound
		// hasn't finished yet), but leave it unchanged if already at a terminal status.
		_, err := pool.Exec(ctx,
			`UPDATE messages
			 SET sent_at    = NOW(),
			     updated_at = NOW(),
			     status     = CASE WHEN status = 'sending' THEN 'sent' ELSE status END
			 WHERE message_id_header = $1`,
			rfc5322MsgID,
		)
		if err != nil {
			return err
		}
		slog.Info("ses_events: confirmed delivery", "message_id", rfc5322MsgID)

	case "Email Delivery Delayed":
		// Transient delay — SES will eventually send Email Delivered or Email Bounced.
		// No status change; log for observability.
		slog.Warn("ses_events: delivery delayed", "message_id", rfc5322MsgID)

	default:
		slog.Debug("ses_events: unhandled detail-type, skipping", "detail-type", evt.DetailType)
	}
	return nil
}

func main() {
	lambda.Start(handler)
}
