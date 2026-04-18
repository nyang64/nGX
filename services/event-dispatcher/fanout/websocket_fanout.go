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

	"agentmail/pkg/events"
	pkgredis "agentmail/pkg/redis"

	"github.com/redis/go-redis/v9"
)

// WebSocketFanout publishes events to the Redis pub/sub channel that the API
// service's WebSocket hub subscribes to, keyed by org_id.
type WebSocketFanout struct {
	client *redis.Client
}

// NewWebSocketFanout creates a WebSocketFanout backed by the given Redis client.
func NewWebSocketFanout(client *redis.Client) *WebSocketFanout {
	return &WebSocketFanout{client: client}
}

// Dispatch serialises the event and publishes it to the org-scoped channel.
func (f *WebSocketFanout) Dispatch(ctx context.Context, event events.Event) error {
	base := event.GetBase()

	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal event for websocket: %w", err)
	}

	channel := pkgredis.WebSocketChannel(base.OrgID)
	if err := f.client.Publish(ctx, channel, data).Err(); err != nil {
		return fmt.Errorf("redis publish to %s: %w", channel, err)
	}
	return nil
}
