/*
 * Copyright (c) 2026 nyklabs.com. All rights reserved.
 *
 * Licensed under the nGX Commercial Source License v1.0.
 * See LICENSE file in the project root for full license information.
 */

package fanout

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"agentmail/pkg/events"
	"agentmail/pkg/kafka"
	"agentmail/pkg/models"
	"agentmail/services/event-dispatcher/store"

	"github.com/google/uuid"
)

// webhookDeliveryMessage is the payload published to the webhooks.delivery topic.
type webhookDeliveryMessage struct {
	WebhookID string         `json:"webhook_id"`
	EventID   string         `json:"event_id"`
	EventType string         `json:"event_type"`
	OrgID     string         `json:"org_id"`
	Payload   map[string]any `json:"payload"`
}

// WebhookFanout queries active webhook subscriptions and publishes delivery
// tasks for each matching webhook to the webhooks.delivery Kafka topic.
type WebhookFanout struct {
	producer *kafka.Producer
	store    *store.WebhookSubscriptionStore
}

// NewWebhookFanout creates a WebhookFanout.
func NewWebhookFanout(brokers []string, s *store.WebhookSubscriptionStore) *WebhookFanout {
	return &WebhookFanout{
		producer: kafka.NewProducer(brokers, kafka.TopicWebhooksDelivery),
		store:    s,
	}
}

// Dispatch looks up matching webhooks for the event's org and event type, then
// publishes a delivery message for each one.
func (f *WebhookFanout) Dispatch(ctx context.Context, event events.Event) error {
	base := event.GetBase()

	orgID, err := uuid.Parse(base.OrgID)
	if err != nil {
		return fmt.Errorf("parse org_id %q: %w", base.OrgID, err)
	}

	webhooks, err := f.store.FindMatchingWebhooks(ctx, orgID, string(base.Type))
	if err != nil {
		return fmt.Errorf("find matching webhooks: %w", err)
	}

	if len(webhooks) == 0 {
		return nil
	}

	// Marshal the event as a generic map to use as the delivery payload.
	rawBytes, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal event payload: %w", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(rawBytes, &payload); err != nil {
		return fmt.Errorf("unmarshal event to map: %w", err)
	}

	for _, wh := range webhooks {
		if err := f.publishDelivery(ctx, wh, base, payload); err != nil {
			// Log and continue so one bad webhook doesn't block others.
			slog.Error("publish webhook delivery",
				"webhook_id", wh.ID,
				"event_id", base.ID,
				"error", err,
			)
		}
	}
	return nil
}

func (f *WebhookFanout) publishDelivery(ctx context.Context, wh *models.Webhook, base events.BaseEvent, payload map[string]any) error {
	msg := webhookDeliveryMessage{
		WebhookID: wh.ID.String(),
		EventID:   base.ID,
		EventType: string(base.Type),
		OrgID:     base.OrgID,
		Payload:   payload,
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal delivery message: %w", err)
	}
	// Key by webhook ID so deliveries for the same webhook are ordered.
	return f.producer.Publish(ctx, wh.ID.String(), data)
}

// Close shuts down the underlying Kafka producer.
func (f *WebhookFanout) Close() error {
	return f.producer.Close()
}
