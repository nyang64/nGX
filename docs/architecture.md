# AgentMail Architecture

## 1. System Overview

AgentMail is email infrastructure designed for AI agents. It provides:
- Real email addresses backed by actual SMTP delivery
- A REST API that abstracts away SMTP/IMAP complexity
- Event-driven notifications (webhooks + WebSocket) so agents can react to incoming mail
- Multi-tenancy with strict data isolation

Design principles:
- **API-first**: every operation is a REST call; no direct DB access for consumers
- **Event-driven**: services communicate via Kafka; no direct service-to-service calls except for auth validation
- **Tenant-isolated**: PostgreSQL Row-Level Security enforces data boundaries in every query
- **At-least-once delivery**: Kafka and the webhook retry system guarantee events are not lost

## 2. Tenancy Model

The data model has four levels of hierarchy:

- Organization: billing root, holds API keys
- Pod: isolated namespace, maps to a sub-customer or product
- Inbox: a real email address
- Thread: conversation grouped by In-Reply-To/References headers
- Message: individual email (inbound or outbound)
- Attachment: binary payloads on messages

Concrete example: Acme Corp (org) runs a support product with pod slug "support". The inbox address is support@acme.agentmail.io. The org creates an API key with pod:admin and inbox:write scopes scoped to that pod. That key can only see data inside the support pod.

An API key belongs to an Org. It can be org-wide (all pods visible) or scoped to a specific Pod. The pod_id field on the key's claims record determines which namespaces the key can access.

### Row-Level Security

Every data table carries an org_id column. PostgreSQL Row-Level Security policies enforce that a query can only see rows where org_id matches the current session setting:

```sql
CREATE POLICY org_isolation ON inboxes
    USING (org_id = current_setting('app.current_org_id', TRUE)::uuid);
```

The Go layer sets this session variable for every transaction via WithOrgTx in pkg/db:

```go
func WithOrgTx(ctx context.Context, pool *pgxpool.Pool, orgID uuid.UUID, fn func(pgx.Tx) error) error {
    return WithTx(ctx, pool, func(tx pgx.Tx) error {
        if err := SetOrgContext(ctx, tx, orgID); err != nil {
            return err
        }
        return fn(tx)
    })
}
```

No service can accidentally read another tenant's data. The database enforces the boundary even if application code has a bug.

## 3. Service Map

| Service | Port | Role | Communication | Data Stores |
|---------|------|------|---------------|-------------|
| api | 8080 | REST + WebSocket gateway; auth middleware; rate limiting | HTTP in; calls auth HTTP /validate; proxies to inbox + webhook services | PostgreSQL (org store), Redis (rate limits, WS pub/sub) |
| auth | 8081 | API key lifecycle; validates keys for all other services | HTTP in | PostgreSQL (api_keys table) |
| inbox | 8082 | Core business logic: inboxes, threads, messages, drafts, labels | HTTP in (from API gateway); Kafka out (events.fanout, email.outbound.queue) | PostgreSQL |
| email-pipeline | 2525 SMTP | Inbound SMTP reception + MIME parsing; outbound MX delivery | SMTP in; Kafka consumer (email.outbound.queue); Kafka out (events.fanout) | PostgreSQL, S3/MinIO |
| event-dispatcher | none | Fans out domain events to WebSocket subscribers and webhook consumers | Kafka consumer (events.fanout); Redis PUBLISH; Kafka out (webhooks.delivery) | PostgreSQL (webhook subscriptions), Redis |
| webhook-service | 8083 | HTTP delivery of webhook payloads with HMAC signatures and retry | Kafka consumer (webhooks.delivery); HTTP out to customer endpoints | PostgreSQL (delivery log) |
| scheduler | none | Background cron jobs: bounce detection, draft expiry | Internal cron; PostgreSQL reads/writes | PostgreSQL |
| search | 8084 | Keyword and semantic search across messages | HTTP in; calls auth service for validation; calls embedder for semantic queries | PostgreSQL (tsvector + pgvector) |
| embedder | none | Generates message embeddings for semantic search | Kafka consumer (events.fanout); HTTP out to embedding server; PostgreSQL writes | PostgreSQL, S3/MinIO |

### Service Interaction Diagram

```
External Clients (REST / WebSocket)
         |
         v
+------------------------------------------+
|           API Gateway :8080              |
|  RequestID, Logger, CORS, Recover        |
|  authMiddleware, requireScope, RateLimit |
+------+----------------------------+------+
       |                            |
       | POST /validate             | Proxy + X-Org-ID headers
       v                            v
+-------------+           +-----------------+   +-------------+
| Auth :8081  |           |  Inbox :8082    |   | Search:8084 |
| key lookup  |           |  business logic |   | full-text   |
+-------------+           +--------+--------+   +-------------+
                                   |
                                   | Kafka publish
         +-------------------------+---------------------------+
         |                      KAFKA                         |
         |  email.inbound.raw                                 |
         |  email.outbound.queue                              |
         |  events.fanout                                     |
         |  webhooks.delivery                                 |
         +---+----------------------------+-------------------+
             |                            |
    +--------+---------+        +---------+----------+
    | Email Pipeline   |        | Event Dispatcher   |
    | SMTP :2525       |        | Kafka consumer     |
    | inbound + MX out |        +----+----------+----+
    +------------------+             |          |
                              Redis  |          | Kafka
                              PUBLISH|          | webhooks.delivery
                                     v          v
                             +-------+----+   +-----------------+
                             | Redis      |   | Webhook Service |
                             | ws:events: |   | HTTP POST + HMAC|
                             | {org_id}   |   +-----------------+
                             +------+-----+
                                    | SUBSCRIBE
                             +------+-----+
                             | API Gateway|
                             | WS Hub     |
                             | broadcasts |
                             | to clients |
                             +------------+
```

## 4. Request Lifecycle

### Authenticated REST Request

Step by step from HTTP arrival to database response:

1. Request arrives at API Gateway (:8080)
2. RequestID middleware assigns or propagates X-Request-ID
3. Logger middleware starts timing
4. CORS middleware sets response headers
5. Recover middleware wraps the handler in a panic handler
6. authMiddleware extracts the Bearer token from the Authorization header
7. API Gateway POSTs to Auth Service POST /validate with the raw key
8. Auth Service computes SHA-256 of the key, queries api_keys WHERE key_hash = $hash AND revoked_at IS NULL, returns Claims JSON
9. Claims (OrgID, KeyID, Scopes, PodID) are injected into the request context
10. requireScope middleware checks Claims.HasScope(required) and returns 403 if missing
11. Rate limit is checked via Redis (sliding window, key = rl:{org_id}:{path})
12. Handler calls InboxClient.proxy(), forwarding the request with X-Org-ID, X-Key-ID, and X-Scopes headers added
13. Inbox Service reads those headers, reconstructs Claims, calls WithOrgTx which sets app.current_org_id on the transaction
14. SQL executes with RLS filtering automatically applied
15. JSON response is proxied verbatim back to the client

### Inbound Email Flow

Inbound processing is deliberately split into two stages separated by the `email.inbound.raw` Kafka topic. The reason: SMTP is a synchronous protocol — the sending MTA waits for `250 OK` before considering delivery done. If `250 OK` is delayed by DB writes or MIME parsing (which can take hundreds of milliseconds), the MTA may time out and retry, causing duplicate delivery. The two-stage design keeps the SMTP session under ~50 ms regardless of downstream load.

```
External MTA
    │  SMTP session (must complete fast)
    ▼
email-pipeline SMTP server :2525
    │
    │  Stage 1: fast path — no parsing, no DB
    │  Only two operations:
    │    1. Upload raw .eml to S3
    │    2. Publish tiny job to Kafka
    │
    ├─► S3: inbound/raw/YYYY/MM/DD/{job_id}.eml
    │
    └─► Kafka: email.inbound.raw
              RawEmailJob { job_id, s3_key, from, to[], size_bytes, received_at }
              (no message content — just a pointer to the S3 object)
              │
              │  250 OK returned to MTA ← SMTP session ends here
              │
              │  Stage 2: async — full processing
              ▼
         InboundConsumer (email-pipeline)
              │
              ├─ Download raw .eml from S3  (shared across all recipients)
              ├─ mime.Parse() → headers, text body, HTML body, attachment parts
              │                 (parsed once, reused for every recipient)
              │
              └─ for each recipient in job.To[]:   ← SMTP RCPT TO envelope list
                    │  (per-recipient errors are logged and skipped — one bad inbox
                    │   does not block delivery to the others)
                    │
                    ├─ Resolve inbox: emailStore.GetInboxByAddress(recipient)
                    │    └─ bypasses RLS (org unknown until inbox is found)
                    │    └─ unknown address → silently discard, continue loop
                    │
                    ├─ Generate new msg_id (UUID) for this recipient's copy
                    │
                    ├─ Upload per-inbox S3 files using this inbox's prefix:
                    │    {org}/{pod}/{inbox}/text/{msg_id}.txt       (if text body present)
                    │    {org}/{pod}/{inbox}/html/{msg_id}.html      (if HTML body present)
                    │    {org}/{pod}/{inbox}/attachments/{msg_id}/{filename}  (per attachment)
                    │    raw_s3_key → points to shared inbound/raw/.../job_id.eml (not copied)
                    │
                    ├─ WithOrgTx transaction (scoped to this inbox's org):
                    │    ├─ Thread dedup: match In-Reply-To + References
                    │    │  → join existing thread or create new one
                    │    ├─ INSERT messages row (this inbox, this msg_id, S3 key refs)
                    │    ├─ INSERT attachment records
                    │    └─ IncrThreadMessageCount (snippet, last_message_at)
                    │
                    └─► Kafka: events.fanout
                          EventMessageReceived { message_id, inbox_id, thread_id, ... }
                          EventThreadCreated   { ... }  ← if new thread for this inbox
```

**Per-recipient isolation**: each inbox in `job.To` gets its own `msg_id`, its own S3 files, its own `messages` row, and its own thread dedup. The only shared object is the raw `.eml` at `inbound/raw/...`, which all recipients' message rows reference via `raw_s3_key`.

**SMTP envelope vs. headers**: `job.To` contains the SMTP `RCPT TO` envelope recipients collected by the Enqueuer — not the `To:`/`Cc:`/`Bcc:` email headers. A BCC recipient receives their inbox copy only if the sending MTA included them in the `RCPT TO` list. The `Cc` header values are stored on each message row but do not independently trigger inbox delivery.

**Why consumer lag is safe here**: if the InboundConsumer is slow or temporarily down, jobs accumulate in `email.inbound.raw`. MTAs have already received `250 OK` and will not retry. When the consumer catches up, it processes all jobs in order. The raw `.eml` in S3 is the durable source of truth.

**Error handling**: a malformed job is logged and the offset is committed (skip — don't block the partition). A transient error (DB down, S3 unreachable) returns an error to Kafka, which re-delivers the job without committing the offset.

---

### Outbound Email Flow

Outbound email is also decoupled via Kafka (`email.outbound.queue`). The inbox service records the intent synchronously — so the API returns immediately with the message record — and the actual SMTP delivery happens asynchronously.

```
Client: POST /v1/inboxes/{id}/messages/send
    │
    ▼
API Gateway :8080
    ├─ authMiddleware → POST /validate to Auth :8081 → Claims
    ├─ requireScope(inbox:write)
    └─ InboxClient.proxy() → Inbox Service :8082
              │
              │  WithOrgTx transaction (all or nothing):
              ├─ Load inbox (get From address and display name)
              ├─ If reply_to_id: load parent message for In-Reply-To header + load its thread
              │  Else: create new thread
              ├─ INSERT messages row: direction=outbound, status=sending
              ├─ IncrThreadMessageCount (snippet, last_message_at)
              │
              └─► Kafka: email.outbound.queue
                    OutboundJob { message_id, org_id, inbox_id }
                    (only IDs — content is loaded from DB by the consumer)
              │
              └─ Return message record to client ← HTTP response ends here
                 (status=sending; client can poll or watch for message.sent event)

              │  Async: email-pipeline QueueConsumer
              ▼
         QueueConsumer reads email.outbound.queue
              │
              ├─ WithOrgTx: load full message record by message_id
              ├─ UPDATE messages SET status='sending'  ← prevents duplicate sends on re-delivery
              ├─ Build SendJob from message fields (To, Cc, Bcc, subject, body S3 keys, headers)
              │
              └─ outbound.Sender.Send(ctx, sendJob):
                    ├─ Download text/HTML body from S3 (if S3 keys present)
                    ├─ buildMIMEMessage → assemble RFC 5322 bytes
                    ├─ DKIM-sign (if DKIM_PRIVATE_KEY_PEM configured; failure is non-fatal)
                    ├─ If SMTP_RELAY_HOST set → smtp.SendMail to relay (dev/staging)
                    └─ Else → net.LookupMX(recipient domain)
                              → try TLS on mxHost:465
                              → fallback: STARTTLS on mxHost:25

              On success:
              ├─ UPDATE messages SET sent_at=NOW()
              └─► Kafka: events.fanout
                    EventMessageSent { message_id, inbox_id, thread_id, to[], subject }

              On failure:
              ├─ UPDATE messages SET status='failed'
              └─► Kafka: events.fanout
                    EventMessageBounced { message_id, inbox_id, thread_id, bounce_code, bounce_reason }
              (Kafka re-delivers the job on error — consumer returns non-nil error without committing offset)
```

**Why OutboundJob carries only IDs**: the consumer always loads the authoritative message record from the database. This means even if the job is re-delivered after a crash, it picks up the latest state. It also keeps the Kafka message small.

---

### Draft Approval Flow

The draft system adds a human-in-the-loop gate before sending. A draft follows the same outbound path as a direct send, but only after explicit approval.

```
Agent: POST /v1/inboxes/{id}/drafts
    └─ INSERT drafts row: review_status='pending'
    └─► events.fanout: EventDraftCreated { draft_id, thread_id, inbox_id }
         └─ Event Dispatcher → webhook/WebSocket notification to human reviewer

Human approves: POST /v1/inboxes/{id}/drafts/{draftID}/approve
    └─ WithOrgTx:
        ├─ Verify review_status='pending' (else 409)
        ├─ INSERT messages row: direction=outbound, status=sending
        ├─ UPDATE drafts: review_status='approved'
        ├─► email.outbound.queue: OutboundJob { message_id, org_id, inbox_id }
        └─► events.fanout: EventDraftApproved { draft_id, thread_id, inbox_id }
    └─ Email Pipeline delivers the message (same path as direct send above)

Human rejects: POST /v1/inboxes/{id}/drafts/{draftID}/reject
    └─ WithOrgTx:
        ├─ Verify review_status='pending' (else 409)
        ├─ UPDATE drafts: review_status='rejected', review_note=reason
        └─► events.fanout: EventDraftRejected { draft_id, thread_id, inbox_id, reason }

Scheduler (draft_expiry, every 5 minutes):
    └─ UPDATE drafts SET review_status='rejected', review_note='expired: scheduled send time has passed'
       WHERE review_status='pending' AND scheduled_at < NOW()
       (no event published — maintenance operation)
```

## 5. Event System

### Kafka Topics

| Topic | Producer(s) | Consumer(s) | Purpose |
|-------|------------|-------------|---------|
| `email.inbound.raw` | email-pipeline (Enqueuer) | email-pipeline (InboundConsumer) | Decouples SMTP fast path from async processing; carries `RawEmailJob` with S3 pointer |
| `email.outbound.queue` | inbox-service (send + draft approve) | email-pipeline (QueueConsumer) | Queues outbound send jobs; carries only message IDs — content loaded from DB |
| `events.fanout` | email-pipeline, inbox-service | event-dispatcher, embedder | All domain events keyed by `org_id`; drives webhooks, WebSocket, and semantic indexing |
| `webhooks.delivery` | event-dispatcher | webhook-service | One message per matching webhook subscription; drives HTTP delivery with retry |

These constants are defined in `pkg/kafka/topics.go`. All topics use `org_id` as the partition key (via `PublishEvent`), which guarantees ordering of events within an org.

### events.fanout → notifications and indexing

`events.fanout` is the single bus that all domain mutations publish to. It has three independent consumers, each doing a completely different job:

```
Service publishes event to events.fanout (keyed by org_id)
    │
    ├─► Event Dispatcher
    │       │
    │       ├─ WebSocket fanout:
    │       │     Redis PUBLISH ws:events:{org_id} ← raw event JSON
    │       │         └─ API Gateway Hub (SUBSCRIBE on that channel)
    │       │               └─ broadcasts to all connected WS clients for that org
    │       │
    │       └─ Webhook fanout:
    │             SELECT webhooks WHERE org_id=$1 AND events @> ARRAY[$2] AND is_active=TRUE
    │             └─ for each matching webhook:
    │                   Kafka PUBLISH to webhooks.delivery
    │                       WebhookDeliveryMsg { webhook_id, event_id, event_type, org_id, payload }
    │                       keyed by webhook_id (orders deliveries per webhook)
    │                           └─ Webhook Service consumer:
    │                                 INSERT webhook_deliveries (status='pending')
    │                                 HTTP POST to webhook.url with X-AgentMail-Signature header
    │                                 On 2xx: mark success
    │                                 On failure: mark retrying, schedule retry (2^attempt seconds)
    │                                 After MaxRetries: mark failed (terminal)
    │
    └─► Embedder Service
            └─ On message.received / message.sent:
                  Download text body from S3
                  POST /v1/embeddings to embedding server (Infinity / Ollama)
                  UPDATE messages SET embedding=$vector, embedded_at=NOW()
                  (used by search service for semantic queries)
```

**Key design point**: `events.fanout` consumers are fully independent. Adding a new consumer (analytics, alerting, etc.) requires no changes to the producing services.

### Event Envelope

The BaseEvent struct (pkg/events/types.go) is embedded in all event types:

```go
type BaseEvent struct {
    ID            string    `json:"id"`
    Type          EventType `json:"type"`
    OrgID         string    `json:"org_id"`
    OccurredAt    time.Time `json:"occurred_at"`
    CorrelationID string    `json:"correlation_id,omitempty"`
}
```

Example JSON payload for a message.received event:

```json
{
  "id": "a3f2c1d4-...",
  "type": "message.received",
  "org_id": "550e8400-e29b-41d4-a716-446655440000",
  "occurred_at": "2024-01-15T10:30:00Z",
  "correlation_id": "req-abc123",
  "data": {
    "message_id": "...",
    "thread_id": "...",
    "inbox_id": "...",
    "from": "sender@example.com",
    "subject": "Hello",
    "raw_s3_key": "inbound/raw/YYYY/MM/DD/{job_id}.eml"
  }
}
```

### Domain Event Types

Defined in pkg/events/types.go:

| Event Type | When Published |
|-----------|---------------|
| message.received | Inbound email arrives and is processed |
| message.sent | Outbound email delivered successfully |
| message.bounced | Outbound delivery fails permanently |
| thread.created | New thread created (first message in a conversation) |
| thread.status_changed | Thread status updated (e.g., open -> resolved) |
| draft.created | Draft created with pending review_status |
| draft.approved | Draft approved and queued for sending |
| draft.rejected | Draft rejected by reviewer |
| inbox.created | New inbox provisioned |
| label.applied | Label applied to a thread |

## 6. Authentication and Authorization

### API Key Format

Keys use the prefix am_live_ followed by base64url-encoded random bytes. Example: am_live_abc123xyz...

The prefix allows easy identification and revocation scanning. The plaintext key is returned exactly once at creation time. Only the SHA-256 hash is stored in the database.

### Validation Flow

1. Client sends Authorization: Bearer am_live_...
2. API Gateway calls POST /validate on Auth Service with the raw key value
3. Auth Service computes SHA-256 of the key, queries api_keys WHERE key_hash = $hash AND revoked_at IS NULL
4. On success, returns Claims JSON: OrgID, KeyID, Scopes slice, optional PodID
5. Claims are injected into the request context for downstream middleware and handlers

The auth service also exposes a validation endpoint that the search service calls directly, using the same RemoteValidator pattern.

### RBAC Scopes

Defined in pkg/auth/scopes.go:

| Scope | What It Allows |
|-------|---------------|
| org:admin | All operations; effectively includes all other scopes |
| pod:admin | Create, update, and delete pods |
| inbox:read | Read inboxes, threads, and messages |
| inbox:write | Send messages, update thread state, create and manage inboxes |
| draft:read | View draft contents and status |
| draft:write | Create drafts; approve or reject pending drafts |
| webhook:read | View webhook configurations and delivery history |
| webhook:write | Create, update, and delete webhooks |
| search:read | Execute full-text search queries |

Routes in services/api/server/routes.go wire each endpoint to its required scope using the requireScope middleware. For example, POST /v1/inboxes requires inbox:write, while GET /v1/inboxes requires inbox:read.

## 7. Real-Time: WebSocket

### Connection

Clients connect to:

```
ws://localhost:8080/v1/ws
Authorization: Bearer am_live_...
```

The auth middleware on the /v1 route group validates the token before the WebSocket upgrade occurs.

### Hub Architecture

The API Gateway runs a Hub that manages all active WebSocket connections and their Redis subscriptions:

```
Client connects
  -> authMiddleware validates Bearer token
  -> Hub.register(client) called
  -> Hub subscribes to Redis channel ws:events:{org_id} if this is the first client for that org
  -> client receives all events published to that channel

Event Dispatcher publishes event
  -> Redis PUBLISH ws:events:{org_id} <event JSON>
  -> Hub receives message on its Redis SUBSCRIBE goroutine
  -> Hub.broadcast() sends JSON to all registered WS clients for that org_id

Last client for an org disconnects
  -> Hub.unregister(client)
  -> Hub unsubscribes from ws:events:{org_id} channel
```

The Hub starts in a background goroutine when the API service starts (services/api/main.go calls hub.Run(ctx)) and is stopped on graceful shutdown.

## 8. Storage Architecture

| Store | What Lives There | Why |
|-------|-----------------|-----|
| PostgreSQL 16 + pgvector | All metadata: orgs, pods, inboxes, threads, messages, drafts, labels, api_keys, webhooks, delivery logs; message embedding vectors | ACID guarantees; RLS tenant isolation; tsvector full-text search; HNSW vector similarity search via pgvector |
| S3 / MinIO | Raw .eml files, extracted text bodies, HTML bodies, attachments | Blobs do not belong in relational rows; keeps the messages table lean and fast to scan |
| Redis 7 | Rate limit counters; WebSocket pub/sub channel per org | Sub-millisecond counter increments; native pub/sub for fan-out |
| Kafka (Confluent 7.6) | Inbound/outbound email queues; domain event fan-out; webhook delivery tasks | Durable, replayable, supports multiple independent consumers |

### S3 Key Structure

```
inbound/raw/YYYY/MM/DD/{job_id}.eml              ← written by Enqueuer; stays here permanently

{org_id}/{pod_id}/{inbox_id}/text/{msg_id}.txt           ← extracted by InboundConsumer (if text body present)
{org_id}/{pod_id}/{inbox_id}/html/{msg_id}.html          ← extracted by InboundConsumer (if HTML body present)
{org_id}/{pod_id}/{inbox_id}/attachments/{msg_id}/{filename}  ← per attachment (filename or ContentID)
```

`{pod_id}` is the literal string `no-pod` when the inbox has no pod. The raw `.eml` is never copied — the `messages.raw_s3_key` column points directly to the `inbound/raw/...` key written by the Enqueuer.

A single bucket (`S3_BUCKET`, default `agentmail`) is used for all objects. In local development, MinIO provides S3-compatible storage.

### PostgreSQL Connection Pool

Configured via pkg/config/config.go and environment variables:

| Variable | Default | Description |
|----------|---------|-------------|
| DB_MAX_CONNS | 25 | Maximum open connections |
| DB_MIN_CONNS | 5 | Minimum idle connections |
| DB_MAX_CONN_LIFETIME_SEC | 3600 | Max connection lifetime (seconds) |
| DB_MAX_CONN_IDLE_SEC | 300 | Max idle time before closing (seconds) |

## 9. Background Jobs (Scheduler)

The scheduler service uses a cron runner to execute periodic maintenance jobs against PostgreSQL.

| Job | Schedule | What It Does |
|-----|----------|--------------|
| bounce_check | Daily at midnight (00:00) | Scans outbound messages that have been in "sending" state too long and marks them as bounced |
| draft_expiry | Every 5 minutes | Marks pending drafts whose expiry timestamp has passed as expired |

Both jobs bypass RLS and use the raw `pgxpool.Pool` directly, as they operate across all orgs and cannot set a per-org RLS context.

## 10. Key Design Decisions

**Thread deduplication via email headers**
When an inbound message arrives, its In-Reply-To and References headers are matched against messages.message_id_header to find an existing thread. If found, the message joins that thread. If not, a new thread is created. This runs inside a serializable transaction to prevent race conditions when concurrent replies arrive simultaneously.

**Shared DB with RLS vs. per-tenant database**
Chosen for cost efficiency. A single PostgreSQL cluster with RLS gives strong isolation without the operational overhead of thousands of separate databases. High-value enterprise tenants can be migrated to dedicated clusters independently if needed.

**S3 for email bodies**
Storing large blobs in PostgreSQL degrades table scans, bloats WAL, and complicates vacuuming. S3 keeps the messages table lean — only metadata and S3 keys are stored in the database row. Email bodies are fetched on demand.

**Kafka as event backbone**
Durable, replayable, and supports multiple independent consumers. The same inbound email event simultaneously drives WebSocket push, webhook delivery, and the embedder service — all without coupling between consumers.

**At-least-once webhook delivery**
The webhook-service uses a RetryScheduler that scans for failed deliveries and re-queues them. The event_id field on each event allows consumer endpoints to deduplicate. MaxRetries defaults to 8 with exponential backoff (configurable via WEBHOOK_MAX_RETRIES).

**Proxy pattern in the API Gateway**
The gateway is intentionally thin: it validates auth, checks scopes, then proxies requests verbatim to internal services with org identity headers injected. Internal services evolve independently; the gateway does not duplicate business logic. This also means the gateway can be scaled independently of the services it fronts.

**Inter-service trust via header injection**
The API Gateway injects X-Org-ID, X-Key-ID, and X-Scopes headers after validating the Bearer token. Internal services trust these headers (they are not exposed externally). The INTERNAL_SECRET environment variable provides an additional signing layer for service-to-service calls that bypass the public auth flow.
