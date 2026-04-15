# Shared Packages (`pkg/`)

The `pkg/` directory is a single Go module (`nGX/pkg`) shared across all services. The rule: if two or more services need it, it goes in `pkg/`. Services never import each other.

## pkg/config

Central configuration loaded from environment variables via `os.Getenv` with typed parsing helpers (`getEnv`, `getEnvInt`, `getEnvBool`). No external dependency.

### Config Struct

```go
type Config struct {
    Environment string   // development | production
    LogLevel    string   // debug | info | warn | error
    LogFormat   string   // json | text
    Database    DatabaseConfig
    Kafka       KafkaConfig
    Redis       RedisConfig
    S3          S3Config
    API         APIConfig
    Auth        AuthConfig
    SMTP        SMTPConfig
    Webhook     WebhookConfig
    OTEL        OTELConfig
    // Service URLs for inter-service calls
    AuthServiceURL    string
    InboxServiceURL   string
    WebhookServiceURL string
    // Embedding server
    EmbedderURL   string  // OpenAI-compatible embedding server base URL
    EmbedderModel string  // model name, e.g. "nomic-embed-text-v1.5"
}
```

Sub-structs of note:

- `DatabaseConfig` — `URL`, pool sizing (`MaxConns`, `MinConns`, `MaxConnLifetime`, `MaxConnIdleTime`)
- `KafkaConfig` — `Brokers []string`, `GroupID`
- `S3Config` — `Endpoint`, `Bucket`, `Region`, credentials, `UsePathStyle` (set `true` for MinIO)
- `AuthConfig` — `InternalSecret` for inter-service signing
- `SMTPConfig` — `ListenAddr`, `Hostname` for the inbound SMTP pipeline
- `WebhookConfig` — `Concurrency`, `MaxRetries`
- `OTELConfig` — `Endpoint`, `ServiceName`

### Usage

```go
cfg := config.Load()  // reads env vars, applies defaults
```

All defaults are tuned for local development and match the values in `.env.example`. In production, override via environment variables.

---

## pkg/db

PostgreSQL connection and transaction management using `pgx/v5`.

### Connect

```go
pool, err := db.Connect(ctx, cfg.Database)
// pgxpool with MaxConns, MinConns, MaxConnLifetime, MaxConnIdleTime
```

### WithTx

```go
err := db.WithTx(ctx, pool, func(tx pgx.Tx) error {
    // queries here
    return nil  // commit; return error → rollback
})
```

Begins a transaction, runs `fn`, and commits on nil return or rolls back on error.

### WithOrgTx

```go
err := db.WithOrgTx(ctx, pool, orgID, func(tx pgx.Tx) error {
    // app.current_org_id is set; RLS is active
    return nil
})
```

Combines `WithTx` + `SetOrgContext`. Use this for **all** queries on tenant-isolated tables. The org context is local to the transaction and resets automatically on commit or rollback.

### SetOrgContext

```go
func SetOrgContext(ctx context.Context, tx pgx.Tx, orgID uuid.UUID) error {
    _, err := tx.Exec(ctx, "SELECT set_config('app.current_org_id', $1, TRUE)", orgID.String())
    return err
}
```

The `TRUE` flag in `set_config` scopes the GUC to the current transaction.

### Error Helpers

```go
db.IsNotFound(err)              // pgx.ErrNoRows
db.IsDuplicateKey(err)          // Postgres error code 23505
db.IsForeignKeyViolation(err)   // Postgres error code 23503
```

---

## pkg/auth

API key generation, hashing, claims, and RBAC.

### Key Generation

```go
plaintext, hash, prefix, err := auth.GenerateAPIKey()
// plaintext: "am_live_<base64url-32-random-bytes>" (shown to user once)
// hash: SHA-256 hex string (stored in DB)
// prefix: first 16 chars (displayed in UI for identification)
```

Keys use the `am_live_` prefix. The hash is computed with `sha256.Sum256` and hex-encoded. `VerifyAPIKey(plaintext, hash)` re-hashes and compares.

### Claims

`Claims` carries the resolved identity after a successful key lookup:

```go
type Claims struct {
    OrgID  uuid.UUID
    KeyID  uuid.UUID
    Scopes []Scope
    PodID  *uuid.UUID  // nil = org-wide key; non-nil = pod-scoped key
}
```

Helper methods:

```go
claims.HasScope(auth.ScopeInboxRead)  // true if has the scope OR has org:admin
claims.CanAccessPod(podID)            // false if key is pod-scoped to a different pod
```

### Context Helpers

```go
ctx = auth.WithClaims(ctx, claims)   // store claims in context
claims = auth.ClaimsFromCtx(ctx)     // retrieve (nil if not present)
orgID = auth.OrgIDFromCtx(ctx)       // shortcut for claims.OrgID
```

### All Scopes

| Scope | What it allows |
|-------|----------------|
| `org:admin` | Full org management: read/write org settings, pods, API keys; implies all other scopes |
| `pod:admin` | Create, update, delete pods |
| `inbox:read` | List and read inboxes, threads, messages, labels |
| `inbox:write` | Create/update/delete inboxes; update thread status; apply/remove labels; send messages |
| `draft:read` | List and read drafts |
| `draft:write` | Create, update, delete, approve, and reject drafts |
| `webhook:read` | List webhooks and delivery logs |
| `webhook:write` | Create, update, delete webhooks |
| `search:read` | Full-text search across messages |

---

## pkg/events

All event type definitions and the Kafka event envelope. Consumers import this package for type-safe event handling.

### Topics (from `pkg/kafka/topics.go`)

```go
const (
    TopicEmailInboundRaw    = "email.inbound.raw"
    TopicEmailOutboundQueue = "email.outbound.queue"
    TopicEventsFanout       = "events.fanout"
    TopicWebhooksDelivery   = "webhooks.delivery"
)
```

### BaseEvent

```go
type BaseEvent struct {
    ID            string    `json:"id"`           // UUID string
    Type          EventType `json:"type"`
    OrgID         string    `json:"org_id"`       // used as Kafka partition key
    OccurredAt    time.Time `json:"occurred_at"`
    CorrelationID string    `json:"correlation_id,omitempty"`
}
```

`NewBase(t EventType, orgID uuid.UUID)` constructs a `BaseEvent` with a fresh UUID and current UTC time.

### Event Types

**Message events**

| Type | Struct | When emitted |
|------|--------|--------------|
| `message.received` | `MessageReceivedEvent` | Inbound email stored; data includes `MessageID`, `InboxID`, `ThreadID`, `From`, `Subject`, `RawS3Key` |
| `message.sent` | `MessageSentEvent` | Outbound message delivered; data includes `MessageID`, `InboxID`, `ThreadID`, `To`, `Subject` |
| `message.bounced` | `MessageBouncedEvent` | Delivery bounce received; data includes `MessageID`, `InboxID`, `ThreadID`, `BounceCode`, `BounceReason` |

**Thread events**

| Type | Struct | When emitted |
|------|--------|--------------|
| `thread.created` | `ThreadCreatedEvent` | New thread opened; data includes `ThreadID`, `InboxID`, `Subject`, `first_message_id` |
| `thread.status_changed` | `ThreadStatusChangedEvent` | Status transition; data includes `ThreadID`, `InboxID`, `OldStatus`, `NewStatus` |

**Draft events**

| Type | Struct | When emitted |
|------|--------|--------------|
| `draft.created` | `DraftCreatedEvent` | Agent queued a draft for review; data includes `DraftID`, `ThreadID`, `InboxID` |
| `draft.approved` | `DraftApprovedEvent` | Draft approved for sending; data includes `DraftID`, `ThreadID`, `InboxID` |
| `draft.rejected` | `DraftRejectedEvent` | Draft rejected; data includes `DraftID`, `ThreadID`, `InboxID`, `Reason` |

**Inbox events**

| Type | Struct | When emitted |
|------|--------|--------------|
| `inbox.created` | `InboxCreatedEvent` | New inbox provisioned; data includes `InboxID`, `EmailAddress`, `PodID` |

**Label events**

| Type | Struct | When emitted |
|------|--------|--------------|
| `label.applied` | `LabelAppliedEvent` | Label attached to thread; data includes `ThreadID`, `LabelID`, `LabelName` |

---

## pkg/kafka

### Producer

```go
p := kafka.NewProducer(brokers, kafka.TopicEventsFanout)
defer p.Close()

// Publish raw bytes with an explicit key
p.Publish(ctx, key, valueBytes)

// Publish a typed event (auto-marshals to JSON, uses OrgID as partition key)
p.PublishEvent(ctx, &events.MessageReceivedEvent{...})
```

The underlying `kafka-go` writer is **synchronous** (`Async: false`) — `Publish` blocks until the broker acknowledges the write. `AllowAutoTopicCreation: true` means topics are created on first write in dev without manual setup.

### Consumer

```go
c := kafka.NewConsumer(brokers, kafka.TopicEventsFanout, "my-group")
defer c.Close()

c.Consume(ctx, func(ctx context.Context, msg kafkago.Message) error {
    // process msg.Value
    return nil  // nil → commit offset; error → log and skip
})
```

**At-least-once semantics**: the offset is committed only on `nil` return. On error, the message is logged (`slog.Error`) and the consumer moves on to the next message — there is no inline retry. Persistent failures must be detected via monitoring. The loop exits cleanly when `ctx` is cancelled.

Max message size is 10 MB (`MaxBytes: 10e6`).

---

## pkg/mime

RFC 5322 MIME email parser. Uses only the Go standard library (`net/mail`, `mime/multipart`, `mime/quotedprintable`).

```go
parsed, err := mime.Parse(r io.Reader)
```

`ParsedEmail` fields:

```go
parsed.From        // EmailAddress{Email, Name}
parsed.To          // []EmailAddress
parsed.CC          // []EmailAddress
parsed.ReplyTo     // string
parsed.Subject     // string (RFC 2047 decoded)
parsed.Date        // time.Time
parsed.MessageID   // string (angle brackets stripped)
parsed.InReplyTo   // string
parsed.References  // []string
parsed.BodyText    // []byte
parsed.BodyHTML    // []byte
parsed.Parts       // []Part — attachments and inline images
parsed.Headers     // map[string][]string — all raw headers
```

`Part` fields: `ContentType`, `Filename` (RFC 2047 decoded), `ContentID`, `IsInline`, `Data []byte`.

Content-Transfer-Encoding handled: `quoted-printable`, `base64` (with whitespace stripping for line-wrapped base64), and raw (8-bit/7-bit).

MIME structures handled: `multipart/mixed`, `multipart/alternative`, `multipart/related`, and nested multipart recursively.

---

## pkg/redis

```go
client, err := redis.NewClient(cfg.Redis.URL)  // connects and pings
```

### Key Helpers

All Redis keys are defined in `pkg/redis/keys.go` to enforce consistent namespacing:

```go
redis.RateLimitKey(orgID, endpoint)  // "rl:{orgID}:{endpoint}"
redis.WebSocketChannel(orgID)         // "ws:events:{orgID}"
redis.InboxCacheKey(inboxID)          // "inbox:{inboxID}"
redis.SessionKey(keyID)               // "session:{keyID}"
redis.APIKeyHashKey(hash)             // "apikey:{hash}"
```

`WebSocketChannel` is the pub/sub channel used by the event-dispatcher to fan out events to all WebSocket connections for an org. `APIKeyHashKey` is used to cache resolved key lookups, avoiding a DB round-trip on every request.

---

## pkg/s3

```go
client, err := s3.NewClient(ctx, cfg.S3)

// Upload
client.Upload(ctx, key, data, contentType)
client.UploadStream(ctx, key, reader, contentType)

// Download
data, err := client.Download(ctx, key)

// Presigned URL (valid 1 hour)
url, err := client.PresignedURL(ctx, key, time.Hour)
```

MinIO-compatible via `UsePathStyle: true` in local dev (set `S3_USE_PATH_STYLE=true`). For AWS S3 in production, set `UsePathStyle=false` and leave `S3_ENDPOINT` empty.

---

## pkg/embedder

HTTP client for any OpenAI-compatible embedding server (Infinity, Ollama, etc.). Used by both the `embedder` service (to generate message embeddings) and the `search` service (to embed query strings for semantic search).

```go
client := embedder.New(
    "http://infinity:7997",    // EMBEDDER_URL
    "nomic-embed-text-v1.5",   // EMBEDDER_MODEL
    256,                        // MRL truncation dims (0 = keep all)
)

vec, err := client.Embed(ctx, "invoice payment overdue")
// vec: []float32 of length 256
```

`New` returns a `*Client` that calls `POST {baseURL}/v1/embeddings` with a JSON body `{"model": "...", "input": "..."}` and parses the OpenAI-format response. The HTTP timeout is 30 seconds.

### VectorLiteral

```go
lit := embedder.VectorLiteral(vec)
// "[0.12345,0.67890,...]"

// Use in SQL:
pool.Exec(ctx,
    "UPDATE messages SET embedding = $1::vector, embedded_at = NOW() WHERE id = $2",
    lit, msgID,
)
```

`VectorLiteral` formats a `[]float32` as a PostgreSQL vector literal string suitable for use with a `::vector` cast. This avoids importing a pgvector Go library.

---

## pkg/middleware

HTTP middleware built on the standard `net/http` interface, compatible with `chi`.

### RequestID

```go
r.Use(middleware.RequestID)
```

Reads `X-Request-ID` from the incoming request (pass-through for upstream proxies) or generates a new UUID. Sets the header on the response and stores the value in context. Retrieve with `middleware.RequestIDFromCtx(ctx)`.

### Logger

```go
r.Use(middleware.Logger(logger))
```

Logs method, path, status code, `duration_ms`, and `request_id` at INFO level after each request using `slog`.

### Recover

Catches panics, logs a stack trace, and returns a `500` response. Prevents a single panicking handler from crashing the server process.

### Authenticator

```go
validateFn := func(ctx context.Context, key string) (*auth.Claims, error) { ... }
r.Use(middleware.Authenticator(validateFn))
```

Extracts the `Bearer` token from `Authorization`, calls `validateFn`, and on success injects the returned `*auth.Claims` into context via `auth.WithClaims`. Returns `401` with a JSON error body on missing or invalid tokens.

### RateLimit

```go
r.Use(middleware.RateLimit(redisClient, 100, time.Minute))
```

Sliding-window counter per `(org_id, path)` using a Redis INCR + EXPIRE pipeline. Sets `X-RateLimit-Limit` and `X-RateLimit-Remaining` headers. Returns `429` with `Retry-After` when exceeded. **Fails open** on Redis errors — the request is allowed through rather than rejected.

---

## pkg/pagination

```go
// Encode the last row's position as an opaque cursor
cursor := pagination.EncodeCursor(lastRow.CreatedAt.Format(time.RFC3339Nano), lastRow.ID.String())

// Decode to use in SQL WHERE clause
parts, err := pagination.DecodeCursor(cursorStr)
// parts[0] = timestamp, parts[1] = UUID

// Normalize limit (1–100, default 20)
limit := pagination.ClampLimit(requestedLimit)
```

`EncodeCursor` accepts variadic string parts joined by `|` and base64-encoded. `DecodeCursor` reverses this. An empty cursor string returns `nil, nil` — the caller should treat this as "start from the beginning".

Defaults: `defaultLimit = 20`, `maxLimit = 100`.

---

## pkg/telemetry

```go
// Structured logger (JSON in production, text in development)
logger := telemetry.SetupLogger(cfg.LogLevel, cfg.LogFormat)
slog.SetDefault(logger)

// OpenTelemetry (no-op if OTEL_ENDPOINT is empty)
shutdown, err := telemetry.Setup(ctx, "nGX-api", cfg.OTEL.Endpoint)
defer shutdown(ctx)
```

When `OTEL_ENDPOINT` is empty, `telemetry.Setup` returns a no-op shutdown function — no spans or metrics are exported. Set `OTEL_ENDPOINT` to an OTLP HTTP endpoint (Jaeger, Grafana Tempo, etc.) to enable tracing.
