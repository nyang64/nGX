package consumer

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"agentmail/pkg/crypto"
	"agentmail/pkg/kafka"
	"agentmail/pkg/models"
	"agentmail/services/webhook-service/delivery"
	"agentmail/services/webhook-service/store"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	kafkago "github.com/segmentio/kafka-go"
)

// deliveryTask is the message format published by the event-dispatcher to the
// webhooks.delivery topic.
type deliveryTask struct {
	WebhookID string         `json:"webhook_id"`
	EventID   string         `json:"event_id"`
	EventType string         `json:"event_type"`
	OrgID     string         `json:"org_id"`
	Payload   map[string]any `json:"payload"`
}

// Consumer reads webhook delivery tasks from Kafka, persists them, and
// attempts immediate delivery.
type Consumer struct {
	reader        *kafka.Consumer
	deliveryStore *store.DeliveryStore
	deliverer     *delivery.Deliverer
	pool          *pgxpool.Pool
	encKey        []byte // may be nil if encryption is not configured
}

// New creates a Consumer.
func New(brokers []string, groupID string, ds *store.DeliveryStore, d *delivery.Deliverer, pool *pgxpool.Pool, encKey []byte) *Consumer {
	return &Consumer{
		reader:        kafka.NewConsumer(brokers, kafka.TopicWebhooksDelivery, groupID+"-webhook-delivery"),
		deliveryStore: ds,
		deliverer:     d,
		pool:          pool,
		encKey:        encKey,
	}
}

// webhookSubscribesTo reports whether the webhook's Events list includes eventType.
// An empty Events list is treated as "all events" for backwards compatibility.
func webhookSubscribesTo(wh *models.Webhook, eventType string) bool {
	if len(wh.Events) == 0 {
		return true
	}
	for _, e := range wh.Events {
		if e == eventType {
			return true
		}
	}
	return false
}

// Run starts consuming messages until ctx is cancelled.
func (c *Consumer) Run(ctx context.Context) error {
	return c.reader.Consume(ctx, func(ctx context.Context, msg kafkago.Message) error {
		return c.handle(ctx, msg)
	})
}

func (c *Consumer) handle(ctx context.Context, msg kafkago.Message) error {
	var task deliveryTask
	if err := json.Unmarshal(msg.Value, &task); err != nil {
		slog.Warn("failed to unmarshal delivery task, skipping", "error", err)
		return nil
	}

	webhookID, err := uuid.Parse(task.WebhookID)
	if err != nil {
		slog.Warn("invalid webhook_id in delivery task", "webhook_id", task.WebhookID)
		return nil
	}

	// Fetch the webhook record so we have the URL and secret.
	webhook, err := c.deliveryStore.GetWebhook(ctx, webhookID)
	if err != nil {
		return fmt.Errorf("get webhook %s: %w", webhookID, err)
	}

	// Decrypt the caller-supplied auth header value if present.
	if webhook.AuthHeaderValueEnc != nil {
		if len(c.encKey) == 0 {
			slog.Warn("webhook has encrypted auth header but WEBHOOK_ENCRYPTION_KEY is not set; delivering without auth header",
				"webhook_id", webhookID)
		} else {
			plain, err := crypto.Decrypt(c.encKey, webhook.AuthHeaderValueEnc)
			if err != nil {
				slog.Error("failed to decrypt webhook auth header; delivering without auth header",
					"webhook_id", webhookID, "error", err)
			} else {
				webhook.AuthHeaderValue = string(plain)
			}
		}
	}

	// Check whether this webhook subscribes to the incoming event type.
	if !webhookSubscribesTo(webhook, task.EventType) {
		slog.Debug("webhook does not subscribe to event type, skipping",
			"webhook_id", webhookID, "event_type", task.EventType)
		return nil
	}

	// Serialize the payload for delivery.
	payloadBytes, err := json.Marshal(task.Payload)
	if err != nil {
		return fmt.Errorf("marshal delivery payload: %w", err)
	}

	// Create the delivery record as pending.
	d := &models.WebhookDelivery{
		ID:        uuid.New(),
		WebhookID: webhookID,
		EventID:   task.EventID,
		EventType: task.EventType,
		Payload:   task.Payload,
		Status:    models.DeliveryStatusPending,
	}
	if err := c.deliveryStore.CreateDelivery(ctx, d); err != nil {
		return fmt.Errorf("create delivery record: %w", err)
	}

	// Attempt delivery immediately.
	result := c.deliverer.Deliver(ctx, webhook, payloadBytes)
	now := time.Now()
	d.LastAttemptAt = &now
	d.AttemptCount = 1

	if result.Success {
		d.Status = models.DeliveryStatusSuccess
		d.ResponseStatus = &result.StatusCode
		d.ResponseBody = result.ResponseBody
		if err := c.deliveryStore.MarkSuccess(ctx, d); err != nil {
			slog.Error("mark delivery success", "delivery_id", d.ID, "error", err)
		}
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
		if err := c.deliveryStore.MarkFailed(ctx, d, 1); err != nil {
			slog.Error("mark delivery failed", "delivery_id", d.ID, "error", err)
		}
	}

	slog.Info("webhook delivery attempt",
		"delivery_id", d.ID,
		"webhook_id", webhookID,
		"event_type", task.EventType,
		"success", result.Success,
	)
	return nil
}
