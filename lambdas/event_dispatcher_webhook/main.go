/*
 * Copyright (c) 2026 nyklabs.com. All rights reserved.
 *
 * Licensed under the nGX Commercial Source License v1.0.
 * See LICENSE file in the project root for full license information.
 */

package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"time"

	awsevents "github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"agentmail/lambdas/shared"
	"agentmail/pkg/crypto"
	domainevents "agentmail/pkg/events"
	"agentmail/pkg/models"
	whstore "agentmail/services/webhook-service/store"
)

var (
	pool   *pgxpool.Pool
	whs    *whstore.DeliveryStore
	encKey []byte
)

func init() {
	pool = shared.InitDB()
	whs = whstore.NewDeliveryStore(pool)

	keyHex := os.Getenv("WEBHOOK_ENCRYPTION_KEY")
	if keyHex != "" {
		var err error
		encKey, err = crypto.KeyFromHex(keyHex)
		if err != nil {
			slog.Error("event_dispatcher_webhook: invalid WEBHOOK_ENCRYPTION_KEY", "error", err)
			os.Exit(1)
		}
	}
}

var httpClient = &http.Client{Timeout: 30 * time.Second}

func handler(ctx context.Context, sqsEvent awsevents.SQSEvent) (awsevents.SQSEventResponse, error) {
	var failures []awsevents.SQSBatchItemFailure
	for _, record := range sqsEvent.Records {
		if err := processRecord(ctx, record); err != nil {
			slog.Error("event_dispatcher_webhook: process record failed", "id", record.MessageId, "error", err)
			failures = append(failures, awsevents.SQSBatchItemFailure{ItemIdentifier: record.MessageId})
		}
	}
	return awsevents.SQSEventResponse{BatchItemFailures: failures}, nil
}

func processRecord(ctx context.Context, record awsevents.SQSMessage) error {
	evt, err := domainevents.Unmarshal([]byte(record.Body))
	if err != nil {
		slog.Warn("event_dispatcher_webhook: unmarshal event, skipping", "error", err)
		return nil
	}

	base := evt.GetBase()

	orgID, err := uuid.Parse(base.OrgID)
	if err != nil {
		slog.Warn("event_dispatcher_webhook: invalid org_id in event", "org_id", base.OrgID)
		return nil
	}

	// Load all webhooks for the org.
	hooks, err := whs.ListWebhooks(ctx, orgID)
	if err != nil {
		return err
	}

	// Marshal event to JSON payload once.
	payload, err := json.Marshal(evt)
	if err != nil {
		return err
	}

	// Build the payload map for the delivery record.
	var payloadMap map[string]any
	_ = json.Unmarshal(payload, &payloadMap)

	for _, wh := range hooks {
		if !wh.IsActive {
			continue
		}
		// Check event subscription (empty = all events).
		if !matchesEvent(wh.Events, string(base.Type)) {
			continue
		}

		// Decrypt auth header if present.
		if len(wh.AuthHeaderValueEnc) > 0 && encKey != nil {
			dec, err := crypto.Decrypt(encKey, wh.AuthHeaderValueEnc)
			if err != nil {
				slog.Warn("event_dispatcher_webhook: decrypt auth header", "webhook_id", wh.ID, "error", err)
			} else {
				wh.AuthHeaderValue = string(dec)
			}
		}

		delivery := &models.WebhookDelivery{
			ID:        uuid.New(),
			WebhookID: wh.ID,
			EventID:   base.ID,
			EventType: string(base.Type),
			Payload:   payloadMap,
			Status:    models.DeliveryStatusPending,
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		}
		if err := whs.CreateDelivery(ctx, delivery); err != nil {
			slog.Warn("event_dispatcher_webhook: create delivery record", "webhook_id", wh.ID, "error", err)
		}

		statusCode, body, delivErr := deliver(ctx, wh, payload)

		now := time.Now().UTC()
		delivery.AttemptCount = 1
		delivery.LastAttemptAt = &now

		if delivErr == nil && statusCode >= 200 && statusCode < 300 {
			delivery.ResponseStatus = &statusCode
			delivery.ResponseBody = body
			if err := whs.MarkSuccess(ctx, delivery); err != nil {
				slog.Warn("event_dispatcher_webhook: mark success", "delivery_id", delivery.ID, "error", err)
			}
		} else {
			errMsg := ""
			if delivErr != nil {
				errMsg = delivErr.Error()
			}
			delivery.ResponseStatus = &statusCode
			delivery.ResponseBody = body
			delivery.ErrorMessage = errMsg
			if err := whs.MarkFailed(ctx, delivery, 1); err != nil {
				slog.Warn("event_dispatcher_webhook: mark failed", "delivery_id", delivery.ID, "error", err)
			}
		}
	}
	return nil
}

func matchesEvent(subscribed []string, eventType string) bool {
	if len(subscribed) == 0 {
		return true
	}
	for _, s := range subscribed {
		if s == eventType {
			return true
		}
	}
	return false
}

func deliver(ctx context.Context, wh *models.Webhook, payload []byte) (int, string, error) {
	mac := hmac.New(sha256.New, []byte(wh.Secret))
	mac.Write(payload)
	sig := hex.EncodeToString(mac.Sum(nil))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, wh.URL, bytes.NewReader(payload))
	if err != nil {
		return 0, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-nGX-Signature", "sha256="+sig)
	req.Header.Set("X-nGX-Event", "webhook.delivery")
	req.Header.Set("User-Agent", "nGX-Webhook/1.0")
	if wh.AuthHeaderName != nil && wh.AuthHeaderValue != "" {
		req.Header.Set(*wh.AuthHeaderName, wh.AuthHeaderValue)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	return resp.StatusCode, string(bodyBytes), nil
}

func main() {
	lambda.Start(handler)
}
