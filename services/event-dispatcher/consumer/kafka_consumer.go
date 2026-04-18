/*
 * Copyright (c) 2026 nyklabs.com. All rights reserved.
 *
 * Licensed under the nGX Commercial Source License v1.0.
 * See LICENSE file in the project root for full license information.
 */

package consumer

import (
	"context"
	"log/slog"

	"agentmail/pkg/events"
	"agentmail/pkg/kafka"
	"agentmail/services/event-dispatcher/fanout"

	kafkago "github.com/segmentio/kafka-go"
)

var _ kafkago.Message // keep import; used in Consume callback signature

// Consumer reads from the events.fanout topic and routes each event to all
// registered fanout handlers.
type Consumer struct {
	fanoutConsumer *kafka.Consumer
	webhookFanout  *fanout.WebhookFanout
	wsFanout       *fanout.WebSocketFanout
}

// New creates a Consumer subscribed to the events.fanout topic.
func New(brokers []string, groupID string, wf *fanout.WebhookFanout, ws *fanout.WebSocketFanout) *Consumer {
	return &Consumer{
		fanoutConsumer: kafka.NewConsumer(brokers, kafka.TopicEventsFanout, groupID+"-fanout"),
		webhookFanout:  wf,
		wsFanout:       ws,
	}
}

// Run starts the consumer and blocks until ctx is cancelled.
func (c *Consumer) Run(ctx context.Context) error {
	return c.fanoutConsumer.Consume(ctx, func(ctx context.Context, msg kafkago.Message) error {
		return c.handle(ctx, msg)
	})
}

// handle deserialises the raw Kafka message and dispatches to each fanout.
func (c *Consumer) handle(ctx context.Context, msg kafkago.Message) error {
	event, err := events.Unmarshal(msg.Value)
	if err != nil {
		slog.Warn("failed to unmarshal event, skipping",
			"topic", msg.Topic,
			"offset", msg.Offset,
			"error", err,
		)
		// Return nil so the offset is committed and we don't get stuck.
		return nil
	}

	base := event.GetBase()
	slog.Debug("dispatching event",
		"event_id", base.ID,
		"event_type", base.Type,
		"org_id", base.OrgID,
	)

	if err := c.webhookFanout.Dispatch(ctx, event); err != nil {
		slog.Error("webhook fanout error",
			"event_id", base.ID,
			"error", err,
		)
	}

	if err := c.wsFanout.Dispatch(ctx, event); err != nil {
		slog.Error("websocket fanout error",
			"event_id", base.ID,
			"error", err,
		)
	}

	return nil
}

// Close shuts down the underlying Kafka reader.
func (c *Consumer) Close() error {
	return c.fanoutConsumer.Close()
}
