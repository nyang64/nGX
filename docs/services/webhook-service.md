# Webhook Service

**Module**: `nGX/services/webhook-service`
**Role**: Reliable HTTP delivery of events to customer-registered endpoints with exponential backoff retry.

## Delivery Flow

```
Kafka (webhooks.delivery topic)
    │
    ▼
Consumer reads webhookDeliveryMessage{WebhookID, EventID, EventType, OrgID, Payload}
    │
    ▼
INSERT webhook_deliveries (status='pending')
    │
    ▼
Deliverer.Deliver(ctx, webhook, payloadBytes)
    ├── HTTP POST with HMAC-SHA256 signature
    ├── Success (2xx): MarkSuccess → status='success'
    └── Failure:       MarkFailed  → status='retrying', next_attempt_at=NOW()+backoff
```

## Request Format

```http
POST https://your-endpoint.com/webhooks/nGX
Content-Type: application/json
X-nGX-Signature: sha256=<hmac-hex>
X-nGX-Event: webhook.delivery
User-Agent: nGX-Webhook/1.0

{ ...event JSON... }
```

The HTTP client has a 30-second timeout per delivery attempt.

## Signature Verification

`X-nGX-Signature` contains `sha256=` followed by the HMAC-SHA256 hex digest of the raw request body, keyed by the webhook's secret.

**Verification in Go**:
```go
func Verify(secret string, payload []byte, signature string) bool {
    mac := hmac.New(sha256.New, []byte(secret))
    mac.Write(payload)
    expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
    return hmac.Equal([]byte(expected), []byte(signature))
}
```

**Verification in Python**:
```python
import hmac, hashlib

def verify(secret: str, payload: bytes, signature: str) -> bool:
    expected = "sha256=" + hmac.new(
        secret.encode(), payload, hashlib.sha256
    ).hexdigest()
    return hmac.compare_digest(expected, signature)
```

Always use a constant-time comparison (`hmac.Equal` / `hmac.compare_digest`) to prevent timing attacks.

## Retry Policy

The `RetryScheduler` uses exponential backoff: `2^attempt` seconds, capped at attempt=12.

| Attempt | Delay |
|---------|-------|
| 1 | 2s |
| 2 | 4s |
| 3 | 8s |
| 4 | 16s |
| 5 | 32s |
| 6 | 64s |
| 7 | 128s (~2 min) |
| 8 | 256s (~4 min) |
| ... | 2^n seconds |
| 12+ | 4096s (~68 min, cap) |

After `maxRetries` failed attempts (configurable, default 8), `MarkFailed` is called with no further reschedule — the delivery becomes terminal.

The retry scheduler polls every **30 seconds**:

```sql
SELECT wd.*, w.url, w.secret
FROM webhook_deliveries wd
JOIN webhooks w ON w.id = wd.webhook_id
WHERE wd.status = 'retrying'
  AND wd.next_attempt_at <= NOW()
LIMIT 100
```

`AttemptCount` is incremented on every attempt (initial + retries).

## Idempotency

Each event has a stable `event_id` (ULID). Consumer endpoints should store processed event IDs and skip duplicates. nGX guarantees at-least-once delivery — the same event may be delivered more than once if a retry occurs after a transient failure on either side.

## Delivery Statuses

| Status | Meaning |
|--------|---------|
| `pending` | Created, first attempt in progress |
| `success` | Delivered with 2xx response |
| `retrying` | Failed attempt, scheduled for retry |
| `failed` | Max retries exhausted (terminal) |

## Response Capture

Up to 1024 bytes of the HTTP response body are captured and stored in `webhook_deliveries.response_body` for debugging. The HTTP status code is stored in `response_status`.

## Configuration

| Env Var | Default | Description |
|---------|---------|-------------|
| DATABASE_URL | postgres://... | Postgres connection |
| KAFKA_BROKERS | localhost:9092 | webhooks.delivery topic |
| WEBHOOK_CONCURRENCY | 10 | Parallel delivery goroutines |
| WEBHOOK_MAX_RETRIES | 8 | Max delivery attempts before terminal failure |
