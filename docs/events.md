# Event System

AgentMail uses Kafka as the event backbone. Every significant state change emits an event that downstream services consume asynchronously.

## Kafka Topics

| Topic | Producers | Consumers | Purpose |
|-------|-----------|-----------|---------|
| `email.inbound.raw` | email-pipeline (Enqueuer) | email-pipeline (InboundConsumer) | Raw SMTP jobs; Kafka acts as buffer between the SMTP fast path and async processing |
| `email.outbound.queue` | inbox-service, draft-service | email-pipeline (QueueConsumer) | Outbound send jobs |
| `events.fanout` | all services | event-dispatcher, embedder | All domain events (webhooks + WebSocket + semantic indexing) |
| `webhooks.delivery` | event-dispatcher | webhook-service | Per-webhook delivery tasks |

**Partition key**: `org_id` — events from the same org land on the same partition, preserving ordering within an org.

All topics are pre-created in the local `docker-compose.yml` with `1` partition and `1` replica. `AllowAutoTopicCreation: true` in the Go producer means topics are also created on first write if they do not exist.

## Event Envelope

All events share a common envelope (`BaseEvent`):

```json
{
  "id": "550e8400-e29b-41d4-a716-446655440001",
  "type": "message.received",
  "org_id": "550e8400-e29b-41d4-a716-446655440000",
  "occurred_at": "2024-01-15T10:30:00.123Z",
  "correlation_id": "req-abc123",
  "data": { ... }
}
```

| Field | Type | Description |
|-------|------|-------------|
| `id` | string (UUID) | Unique event identifier; use for deduplication |
| `type` | string | Event type (see below) |
| `org_id` | UUID string | Owning organization; used as Kafka partition key |
| `occurred_at` | RFC 3339 | When the event occurred (UTC) |
| `correlation_id` | string | Optional; links related events across service boundaries |
| `data` | object | Event-specific payload |

Note: the actual `BaseEvent` struct in `pkg/events/types.go` uses `occurred_at` (not `timestamp`) and `id` (UUID string, not ULID). There is no `pod_id` or `inbox_id` at the envelope level — those appear inside each event's `data` field.

## All Event Types

### Message Events

**`message.received`** — inbound email arrived and was stored
```json
{
  "data": {
    "message_id": "string",
    "inbox_id": "uuid",
    "thread_id": "uuid",
    "from": "sender@example.com",
    "subject": "Hello",
    "raw_s3_key": "inbound/raw/YYYY/MM/DD/{job_id}.eml"
  }
}
```

**`message.sent`** — outbound email successfully delivered
```json
{
  "data": {
    "message_id": "string",
    "inbox_id": "uuid",
    "thread_id": "uuid",
    "to": ["recipient@example.com"],
    "subject": "Hello"
  }
}
```

**`message.bounced`** — delivery attempt resulted in a bounce
```json
{
  "data": {
    "message_id": "string",
    "inbox_id": "uuid",
    "thread_id": "uuid",
    "bounce_code": "550",
    "bounce_reason": "5.1.1 User does not exist"
  }
}
```

### Thread Events

**`thread.created`**
```json
{
  "data": {
    "thread_id": "uuid",
    "inbox_id": "uuid",
    "subject": "Hello",
    "first_message_id": "string"
  }
}
```

**`thread.status_changed`**
```json
{
  "data": {
    "thread_id": "uuid",
    "inbox_id": "uuid",
    "old_status": "open",
    "new_status": "closed"
  }
}
```

### Draft Events

**`draft.created`** — agent queued a draft for human review
```json
{
  "data": {
    "draft_id": "uuid",
    "thread_id": "uuid",
    "inbox_id": "uuid"
  }
}
```

**`draft.approved`** — draft approved; will be sent
```json
{
  "data": {
    "draft_id": "uuid",
    "thread_id": "uuid",
    "inbox_id": "uuid"
  }
}
```

**`draft.rejected`** — draft rejected; will not be sent
```json
{
  "data": {
    "draft_id": "uuid",
    "thread_id": "uuid",
    "inbox_id": "uuid",
    "reason": "Tone is too aggressive"
  }
}
```

### Inbox Events

**`inbox.created`**
```json
{
  "data": {
    "inbox_id": "uuid",
    "email_address": "agent@acme.agentmail.io",
    "pod_id": "uuid"
  }
}
```

### Label Events

**`label.applied`** — label attached to a thread
```json
{
  "data": {
    "thread_id": "uuid",
    "label_id": "uuid",
    "label_name": "urgent"
  }
}
```

## Event Flow

```
Service mutation (e.g., inbound email processed)
         |
         v
Publish to events.fanout (Kafka, keyed by org_id)
         |
    +----+----------------------------+--------------------+
    |                                 |                    |
    v                                 v                    v
Event Dispatcher                Embedder Service     (future consumers)
consumes events.fanout          consumes events.fanout
    |                                 |
    +----+-------------------+        +-- download S3 body text
    |                        |        +-- call embedding server
    v                        v        +-- UPDATE messages.embedding
WebSocket Fanout        Webhook Fanout
PUBLISH to Redis        Query webhooks matching
ws:events:{org_id}      event type + org/pod/inbox
    |                        |
    v                        v
API Gateway Hub         Publish to webhooks.delivery
broadcasts to all WS    (one message per webhook)
clients for that org         |
                             v
                     Webhook Service consumes
                     HTTP POST to each endpoint
                     with HMAC signature
```

## Receiving Events via WebSocket

Connect with a valid API key as the token query parameter:

```javascript
const ws = new WebSocket('wss://api.agentmail.io/v1/ws?token=am_live_...');

ws.onopen = () => console.log('connected');
ws.onmessage = (e) => {
  const event = JSON.parse(e.data);

  switch (event.type) {
    case 'message.received':
      console.log(`New email in thread ${event.data.thread_id}`);
      console.log(`From: ${event.data.from}`);
      console.log(`Subject: ${event.data.subject}`);
      break;

    case 'draft.created':
      console.log(`Draft ${event.data.draft_id} needs review`);
      break;

    case 'thread.status_changed':
      console.log(`Thread ${event.data.thread_id}: ${event.data.old_status} -> ${event.data.new_status}`);
      break;
  }
};

ws.onclose = () => setTimeout(reconnect, 1000);  // auto-reconnect
```

The WebSocket connection receives all events for the authenticated org. There is currently no per-inbox or per-pod filter at the WebSocket level — filter in the client handler using `event.data.inbox_id` if needed.

The server sends WebSocket ping frames periodically. Most WebSocket libraries respond to pings automatically. If no pong is received, the server closes the connection.

## Receiving Events via Webhooks

### Registering a Webhook

```bash
curl -X POST https://api.agentmail.io/v1/webhooks \
  -H "Authorization: Bearer $KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "url": "https://your-server.com/webhooks/agentmail",
    "events": ["message.received", "draft.created"]
  }'
```

The response includes a `secret` field — this is shown **once** and used to verify webhook signatures. Store it securely.

Optional scope filters in the registration body:
- `"pod_id": "<uuid>"` — only deliver events from a specific pod
- `"inbox_id": "<uuid>"` — only deliver events from a specific inbox

### Verifying Signatures

Every webhook request includes `X-AgentMail-Signature: sha256=<hmac>`. The HMAC is computed over the raw request body using the webhook secret.

**Python**:
```python
import hmac
import hashlib

def verify_signature(secret: str, payload: bytes, header: str) -> bool:
    expected = "sha256=" + hmac.new(
        secret.encode("utf-8"),
        payload,
        hashlib.sha256
    ).hexdigest()
    return hmac.compare_digest(expected, header)

# Flask / FastAPI example
@app.post("/webhooks/agentmail")
async def handle_webhook(request: Request):
    payload = await request.body()
    sig = request.headers.get("X-AgentMail-Signature", "")
    if not verify_signature(WEBHOOK_SECRET, payload, sig):
        raise HTTPException(status_code=401, detail="invalid signature")
    event = json.loads(payload)
    # process event...
    return {"ok": True}
```

**Go**:
```go
func VerifySignature(secret string, payload []byte, header string) bool {
    mac := hmac.New(sha256.New, []byte(secret))
    mac.Write(payload)
    expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
    return hmac.Equal([]byte(expected), []byte(header))
}
```

### Responding to Webhooks

Return any `2xx` status within 30 seconds (hardcoded delivery timeout). AgentMail treats any non-2xx response as a failure and schedules a retry.

```python
@app.post("/webhooks/agentmail")
async def handle(request: Request):
    payload = await request.body()
    # ... verify signature ...
    event = json.loads(payload)

    # Process asynchronously — respond immediately to avoid timeout
    background_tasks.add_task(process_event, event)
    return {"ok": True}  # 200 OK
```

## Idempotency

Each event has a unique `id` (UUID). AgentMail guarantees **at-least-once delivery** — your endpoint may receive the same event more than once during retries. Store processed `event_id` values to deduplicate:

```python
# In production, use Redis or a database table instead of a set
processed_events = set()

def process_event(event: dict):
    if event["id"] in processed_events:
        return  # duplicate, skip
    processed_events.add(event["id"])
    # handle event...
```

```sql
-- Example deduplication table
CREATE TABLE processed_webhook_events (
    event_id TEXT PRIMARY KEY,
    processed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
-- INSERT ... ON CONFLICT DO NOTHING
```

## Retry Schedule

The webhook service uses exponential backoff between delivery attempts (`2^attempt` seconds, capped at 4096s). The number of retries is controlled by `WEBHOOK_MAX_RETRIES` (default: `8`). After all retries are exhausted, the delivery is marked `failed` and no further attempts are made.

| Attempt | Approximate delay |
|---------|------------------|
| 1 (initial) | immediate |
| 2 | 2s |
| 3 | 4s |
| 4 | 8s |
| 5 | 16s |
| 6 | 32s |
| 7 | 64s (~1 min) |
| 8 | 128s (~2 min) |
| 12+ | 4096s (~68 min, cap) |

The `webhook_deliveries` table stores `attempt_count`, `next_attempt_at`, `response_status`, `response_body`, and `error_message` for every attempt. Delivery history is accessible via:

```bash
curl http://localhost:8080/v1/webhooks/{webhookID}/deliveries \
  -H "Authorization: Bearer $KEY" | jq .
```

## Publishing Events from a Service

Services publish to `events.fanout` using `pkg/kafka`:

```go
producer := kafka.NewProducer(cfg.Kafka.Brokers, kafka.TopicEventsFanout)
defer producer.Close()

event := &events.MessageReceivedEvent{
    BaseEvent: events.NewBase(events.EventMessageReceived, orgID),
    Data: events.MessageReceivedData{
        MessageID: msg.ID.String(),
        InboxID:   msg.InboxID,
        ThreadID:  msg.ThreadID,
        From:      msg.FromAddress,
        Subject:   msg.Subject,
        RawS3Key:  rawKey,
    },
}

if err := producer.PublishEvent(ctx, event); err != nil {
    slog.Error("failed to publish event", "error", err)
}
```

`PublishEvent` marshals the event to JSON and uses `event.GetBase().OrgID` as the Kafka partition key, ensuring all events for an org are ordered within a partition.
