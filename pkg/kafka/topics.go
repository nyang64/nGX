/*
 * Copyright (c) 2026 nyklabs.com. All rights reserved.
 *
 * Licensed under the nGX Commercial Source License v1.0.
 * See LICENSE file in the project root for full license information.
 */

package kafka

// Topic name constants used across all services.
const (
	TopicEmailInboundRaw    = "email.inbound.raw"
	TopicEmailOutboundQueue = "email.outbound.queue"
	TopicEventsFanout       = "events.fanout"
	TopicWebhooksDelivery   = "webhooks.delivery"
)
