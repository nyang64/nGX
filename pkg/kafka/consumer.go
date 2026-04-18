/*
 * Copyright (c) 2026 nyklabs.com. All rights reserved.
 *
 * Licensed under the nGX Commercial Source License v1.0.
 * See LICENSE file in the project root for full license information.
 */

package kafka

import (
	"context"
	"log/slog"

	kafkago "github.com/segmentio/kafka-go"
)

// Consumer wraps a kafka-go Reader for consuming messages from a single topic.
type Consumer struct {
	reader *kafkago.Reader
}

// NewConsumer creates a consumer for the given topic and consumer group.
func NewConsumer(brokers []string, topic, groupID string) *Consumer {
	return &Consumer{
		reader: kafkago.NewReader(kafkago.ReaderConfig{
			Brokers:  brokers,
			Topic:    topic,
			GroupID:  groupID,
			MinBytes: 1,
			MaxBytes: 10e6, // 10 MB
		}),
	}
}

// Consume reads messages in a loop and calls fn for each one.
// On fn success the offset is committed (at-least-once semantics).
// On fn error the error is logged and the loop continues.
// Returns nil when ctx is cancelled, or a non-context fetch error.
func (c *Consumer) Consume(ctx context.Context, fn func(ctx context.Context, msg kafkago.Message) error) error {
	for {
		msg, err := c.reader.FetchMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		if err := fn(ctx, msg); err != nil {
			slog.Error("error processing kafka message",
				"topic", msg.Topic,
				"offset", msg.Offset,
				"error", err,
			)
		} else {
			if err := c.reader.CommitMessages(ctx, msg); err != nil {
				slog.Error("error committing kafka offset", "error", err)
			}
		}
	}
}

// Close shuts down the underlying reader.
func (c *Consumer) Close() error {
	return c.reader.Close()
}
