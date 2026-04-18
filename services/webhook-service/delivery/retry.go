/*
 * Copyright (c) 2026 nyklabs.com. All rights reserved.
 *
 * Licensed under the nGX Commercial Source License v1.0.
 * See LICENSE file in the project root for full license information.
 */

package delivery

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"agentmail/pkg/crypto"
	"agentmail/services/webhook-service/store"

	"github.com/jackc/pgx/v5/pgxpool"
)

// RetryScheduler polls for failed webhook deliveries and retries them with
// exponential backoff.
type RetryScheduler struct {
	pool          *pgxpool.Pool
	deliverer     *Deliverer
	deliveryStore *store.DeliveryStore
	maxRetries    int
	encKey        []byte // may be nil if encryption is not configured
}

// NewRetryScheduler creates a RetryScheduler.
func NewRetryScheduler(pool *pgxpool.Pool, d *Deliverer, ds *store.DeliveryStore, maxRetries int, encKey []byte) *RetryScheduler {
	return &RetryScheduler{
		pool:          pool,
		deliverer:     d,
		deliveryStore: ds,
		maxRetries:    maxRetries,
		encKey:        encKey,
	}
}

// nextBackoff returns 2^attempt seconds, capped at 1 hour.
func nextBackoff(attempt int) time.Duration {
	if attempt > 12 {
		attempt = 12
	}
	return time.Duration(1<<uint(attempt)) * time.Second
}

// Run polls for pending retries every 30 seconds until ctx is cancelled.
func (s *RetryScheduler) Run(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.processRetries(ctx)
		}
	}
}

func (s *RetryScheduler) processRetries(ctx context.Context) {
	deliveries, err := s.deliveryStore.GetPendingRetries(ctx)
	if err != nil {
		slog.Error("get pending retries", "error", err)
		return
	}

	for _, d := range deliveries {
		webhook, err := s.deliveryStore.GetWebhook(ctx, d.WebhookID)
		if err != nil {
			slog.Error("get webhook for retry", "webhook_id", d.WebhookID, "error", err)
			continue
		}

		// Decrypt the caller-supplied auth header value if present.
		if webhook.AuthHeaderValueEnc != nil {
			if len(s.encKey) == 0 {
				slog.Warn("webhook has encrypted auth header but WEBHOOK_ENCRYPTION_KEY is not set; retrying without auth header",
					"webhook_id", d.WebhookID)
			} else {
				plain, decErr := crypto.Decrypt(s.encKey, webhook.AuthHeaderValueEnc)
				if decErr != nil {
					slog.Error("failed to decrypt webhook auth header; retrying without auth header",
						"webhook_id", d.WebhookID, "error", decErr)
				} else {
					webhook.AuthHeaderValue = string(plain)
				}
			}
		}

		payloadBytes, err := json.Marshal(d.Payload)
		if err != nil {
			slog.Error("marshal retry payload", "delivery_id", d.ID, "error", err)
			continue
		}

		result := s.deliverer.Deliver(ctx, webhook, payloadBytes)
		now := time.Now()
		d.LastAttemptAt = &now
		d.AttemptCount++

		if result.Success {
			d.ResponseBody = result.ResponseBody
			d.ResponseStatus = &result.StatusCode
			if err := s.deliveryStore.MarkSuccess(ctx, d); err != nil {
				slog.Error("mark retry success", "delivery_id", d.ID, "error", err)
			}
			slog.Info("webhook retry succeeded",
				"delivery_id", d.ID,
				"attempt", d.AttemptCount,
			)
		} else {
			errMsg := result.Error
			if errMsg == "" {
				errMsg = fmt.Sprintf("HTTP %d", result.StatusCode)
			}
			d.ErrorMessage = errMsg
			d.ResponseBody = result.ResponseBody
			if result.StatusCode != 0 {
				d.ResponseStatus = &result.StatusCode
			}

			if d.AttemptCount >= s.maxRetries {
				if err := s.deliveryStore.MarkFailed(ctx, d, d.AttemptCount); err != nil {
					slog.Error("mark retry permanently failed", "delivery_id", d.ID, "error", err)
				}
				slog.Warn("webhook delivery permanently failed",
					"delivery_id", d.ID,
					"attempts", d.AttemptCount,
				)
			} else {
				if err := s.deliveryStore.MarkFailed(ctx, d, d.AttemptCount); err != nil {
					slog.Error("schedule retry", "delivery_id", d.ID, "error", err)
				}
				slog.Info("webhook retry failed, rescheduled",
					"delivery_id", d.ID,
					"attempt", d.AttemptCount,
					"next_backoff", nextBackoff(d.AttemptCount),
				)
			}
		}
	}
}
