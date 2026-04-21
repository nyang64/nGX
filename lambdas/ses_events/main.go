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
	"log/slog"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/jackc/pgx/v5/pgxpool"

	"agentmail/lambdas/shared"
	"agentmail/pkg/models"
	emailstore "agentmail/services/email-pipeline/store"
)

var (
	pool      *pgxpool.Pool
	emailSt   *emailstore.EmailStore
)

func init() {
	pool = shared.InitDB()
	emailSt = emailstore.NewEmailStore(pool)
}

// sesNotification is the SNS message body wrapping the SES event.
type snsWrapper struct {
	Type    string `json:"Type"`
	Message string `json:"Message"`
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

// sesNotificationPayload is the inner JSON in the SNS Message field.
type sesNotificationPayload struct {
	NotificationType string  `json:"notificationType"`
	Mail             sesMail `json:"mail"`
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
	// SES → SNS → SQS: the SQS body is the SNS notification JSON.
	var sns snsWrapper
	if err := json.Unmarshal([]byte(record.Body), &sns); err != nil {
		slog.Warn("ses_events: unmarshal SNS wrapper, skipping", "error", err)
		return nil
	}

	var payload sesNotificationPayload
	if err := json.Unmarshal([]byte(sns.Message), &payload); err != nil {
		slog.Warn("ses_events: unmarshal SES payload, skipping", "error", err)
		return nil
	}

	// Extract RFC 5322 Message-ID from the mail headers.
	var rfc5322MsgID string
	for _, h := range payload.Mail.Headers {
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
		slog.Debug("ses_events: no Message-ID header, skipping", "ses_msg_id", payload.Mail.MessageID)
		return nil
	}

	switch payload.NotificationType {
	case "Bounce", "Complaint":
		_, err := pool.Exec(ctx,
			`UPDATE messages SET status = $1, updated_at = NOW() WHERE message_id = $2`,
			string(models.MessageStatusFailed), rfc5322MsgID,
		)
		if err != nil {
			return err
		}
		slog.Info("ses_events: marked message failed", "type", payload.NotificationType, "message_id", rfc5322MsgID)
	case "Delivery":
		_, err := pool.Exec(ctx,
			`UPDATE messages SET status = $1, sent_at = NOW(), updated_at = NOW()
			 WHERE message_id = $2 AND status != $1`,
			string(models.MessageStatusSent), rfc5322MsgID,
		)
		if err != nil {
			return err
		}
		slog.Info("ses_events: confirmed delivery", "message_id", rfc5322MsgID)
	default:
		slog.Debug("ses_events: unknown notification type, skipping", "type", payload.NotificationType)
	}
	return nil
}

func main() {
	lambda.Start(handler)
}
