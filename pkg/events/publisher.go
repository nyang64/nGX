/*
 * Copyright (c) 2026 nyklabs.com. All rights reserved.
 *
 * Licensed under the nGX Commercial Source License v1.0.
 * See LICENSE file in the project root for full license information.
 */

package events

import "context"

// EventPublisher publishes domain events. Implemented by kafka.Producer (long-running services)
// and sqs.Publisher (Lambda functions).
type EventPublisher interface {
	PublishEvent(ctx context.Context, e Event) error
}

// OutboundPublisher publishes raw outbound email jobs (key=message_id, value=JSON job).
type OutboundPublisher interface {
	Publish(ctx context.Context, key string, value []byte) error
}
