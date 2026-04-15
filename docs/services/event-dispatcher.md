# Event Dispatcher

**Module**: `nGX/services/event-dispatcher`
**Role**: Consumes all domain events from Kafka and fans out to the webhook delivery queue and to WebSocket clients via Redis pub/sub.

## Data Flow

```
Kafka (events.fanout topic)
          │
          ▼
  Kafka Consumer (group: nGX)
          │
          ├─────────────────────────────┐
          ▼                             ▼
  WebSocketFanout              WebhookFanout
  Redis PUBLISH                DB query: find matching webhooks
  ws:events:{org_id}           Kafka PUBLISH to webhooks.delivery
          │                             │
          ▼                             ▼
  API Gateway Hub          Webhook Service (HTTP delivery)
  → connected WS clients
```

## Webhook Fanout (`WebhookFanout`)

For every event consumed, `Dispatch` is called:

1. Parses `base.OrgID` and `base.Type` from the event.
2. Queries `WebhookSubscriptionStore.FindMatchingWebhooks(ctx, orgID, eventType)`:
   ```sql
   SELECT * FROM webhooks
   WHERE org_id = $1
     AND is_active = TRUE
     AND (events @> ARRAY[$2::text] OR events @> ARRAY['*'])
   ```
   The `@>` operator checks if the webhook's `events` array contains the specific event type or the wildcard `'*'`.
3. If no matching webhooks, returns immediately (no-op).
4. Marshals the full event to JSON, then unmarshals to `map[string]any` to use as the delivery payload.
5. For each matching webhook, publishes to the `webhooks.delivery` Kafka topic:
   ```json
   {
     "webhook_id": "uuid",
     "event_id": "01HX...",
     "event_type": "message.received",
     "org_id": "uuid",
     "payload": { ...full event JSON... }
   }
   ```
   Deliveries for the same webhook are **keyed by `webhook_id`** so they are processed in order within a Kafka partition.
6. Per-webhook publish errors are logged and skipped — one bad webhook does not block others.

## WebSocket Fanout (`WebSocketFanout`)

```go
channel := pkgredis.WebSocketChannel(base.OrgID)  // "ws:events:{org_id}"
redisClient.Publish(ctx, channel, eventJSON)
```

The API Gateway Hub is subscribed to this channel. Multiple API Gateway replicas each independently subscribe via Redis pub/sub and push to their locally connected clients. This decouples the dispatcher from specific gateway instances.

## At-Least-Once Processing

The Kafka consumer commits offsets only after successful processing. If the dispatcher crashes mid-fanout, the event is reprocessed on restart. Downstream systems should handle duplicate events via `event_id`.

Within a single dispatch, webhook publish errors per-webhook are logged but do not cause offset retry — only a total failure to dispatch (e.g. a database error in `FindMatchingWebhooks`) would result in reprocessing.

## Consumer Group

The dispatcher uses consumer group ID `nGX` (configurable via `KAFKA_GROUP_ID`). Only one instance in the group processes each partition. If you scale to multiple dispatcher instances, increase the topic partition count accordingly.

## Configuration

| Env Var | Default | Description |
|---------|---------|-------------|
| DATABASE_URL | postgres://... | For webhook subscription queries |
| KAFKA_BROKERS | localhost:9092 | Source topics + webhooks.delivery |
| KAFKA_GROUP_ID | nGX | Consumer group ID |
| REDIS_URL | redis://localhost:6379 | WebSocket pub/sub |
