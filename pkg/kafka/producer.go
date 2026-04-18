/*
 * Copyright (c) 2026 nyklabs.com. All rights reserved.
 *
 * Licensed under the nGX Commercial Source License v1.0.
 * See LICENSE file in the project root for full license information.
 */

package kafka

import (
	"context"
	"encoding/json"

	"agentmail/pkg/events"

	kafkago "github.com/segmentio/kafka-go"
)

// Producer wraps a kafka-go Writer for publishing messages to a single topic.
type Producer struct {
	writer *kafkago.Writer
}

// NewProducer creates a synchronous producer for the given topic.
func NewProducer(brokers []string, topic string) *Producer {
	return &Producer{
		writer: &kafkago.Writer{
			Addr:                   kafkago.TCP(brokers...),
			Topic:                  topic,
			AllowAutoTopicCreation: true,
			Async:                  false,
		},
	}
}

// Publish sends a raw key/value message to the producer's topic.
func (p *Producer) Publish(ctx context.Context, key string, value []byte) error {
	return p.writer.WriteMessages(ctx, kafkago.Message{
		Key:   []byte(key),
		Value: value,
	})
}

// PublishEvent marshals e to JSON and publishes it, keyed by the event's OrgID.
func (p *Producer) PublishEvent(ctx context.Context, e events.Event) error {
	b := e.GetBase()
	data, err := json.Marshal(e)
	if err != nil {
		return err
	}
	return p.Publish(ctx, b.OrgID, data)
}

// Close shuts down the underlying writer, flushing any buffered messages.
func (p *Producer) Close() error {
	return p.writer.Close()
}
