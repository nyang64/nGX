/*
 * Copyright (c) 2026 nyklabs.com. All rights reserved.
 *
 * Licensed under the nGX Commercial Source License v1.0.
 * See LICENSE file in the project root for full license information.
 */

// Package sqs provides AWS SQS implementations of the events.EventPublisher
// and events.OutboundPublisher interfaces for use in Lambda functions.
package sqs

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	sqssdk "github.com/aws/aws-sdk-go-v2/service/sqs"

	"agentmail/pkg/events"
)

// Publisher implements events.EventPublisher and events.OutboundPublisher using
// AWS SQS. It fans out domain events to webhook_delivery and ws_dispatch queues,
// and additionally to the embedder queue for message.received events.
// Outbound email jobs are published to the email_outbound FIFO queue.
type Publisher struct {
	client               *sqssdk.Client
	webhookDeliveryURL   string
	wsDispatchURL        string
	embedderURL          string
	emailOutboundURL     string
}

// NewPublisher creates a Publisher reading queue URLs from environment variables:
//   - WEBHOOK_DELIVERY_QUEUE_URL
//   - WS_DISPATCH_QUEUE_URL
//   - EMBEDDER_QUEUE_URL
//   - EMAIL_OUTBOUND_QUEUE_URL
func NewPublisher(client *sqssdk.Client) *Publisher {
	return &Publisher{
		client:             client,
		webhookDeliveryURL: os.Getenv("WEBHOOK_DELIVERY_QUEUE_URL"),
		wsDispatchURL:      os.Getenv("WS_DISPATCH_QUEUE_URL"),
		embedderURL:        os.Getenv("EMBEDDER_QUEUE_URL"),
		emailOutboundURL:   os.Getenv("EMAIL_OUTBOUND_QUEUE_URL"),
	}
}

// PublishEvent implements events.EventPublisher. It fans the event out to the
// webhook_delivery and ws_dispatch queues, and additionally to the embedder
// queue for message.received events.
func (p *Publisher) PublishEvent(ctx context.Context, e events.Event) error {
	body, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("sqs: marshal event: %w", err)
	}
	bodyStr := string(body)

	queues := []string{p.webhookDeliveryURL, p.wsDispatchURL}
	if t := e.GetBase().Type; t == events.EventMessageReceived || t == events.EventMessageSent {
		queues = append(queues, p.embedderURL)
	}

	var errs []error
	for _, qURL := range queues {
		if qURL == "" {
			continue
		}
		if _, err := p.client.SendMessage(ctx, &sqssdk.SendMessageInput{
			QueueUrl:    &qURL,
			MessageBody: &bodyStr,
		}); err != nil {
			errs = append(errs, fmt.Errorf("sqs: send to %s: %w", qURL, err))
		}
	}

	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

// Publish implements events.OutboundPublisher. It sends the raw outbound job
// payload to the email_outbound FIFO queue, using key as both MessageGroupId
// and MessageDeduplicationId.
func (p *Publisher) Publish(ctx context.Context, key string, value []byte) error {
	if p.emailOutboundURL == "" {
		return fmt.Errorf("sqs: EMAIL_OUTBOUND_QUEUE_URL is not set")
	}
	bodyStr := string(value)
	if _, err := p.client.SendMessage(ctx, &sqssdk.SendMessageInput{
		QueueUrl:               &p.emailOutboundURL,
		MessageBody:            &bodyStr,
		MessageGroupId:         &key,
		MessageDeduplicationId: &key,
	}); err != nil {
		return fmt.Errorf("sqs: send outbound job: %w", err)
	}
	return nil
}
