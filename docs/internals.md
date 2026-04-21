# nGX Internals: Data and Processing Flows

This document describes how nGX processes email internally — from wire to
database to event fan-out. All details are derived from source code, not
from design intent.

---

## Table of Contents

1. [Inbound Email Flow](#1-inbound-email-flow)
2. [Outbound Email Flow](#2-outbound-email-flow)
3. [Thread Mechanics](#3-thread-mechanics)
4. [Multiple Recipients and S3 Storage](#4-multiple-recipients-and-s3-storage)
5. [Event Fan-out](#5-event-fan-out)
6. [S3 Path Reference](#6-s3-path-reference)
7. [Database Schema Quick Reference](#7-database-schema-quick-reference)
8. [Key Implementation Details](#8-key-implementation-details)

---

## 1. Inbound Email Flow

```
External sender
      │  SMTP
      ▼
Amazon SES (receipt rule)
      │  Stores raw RFC 5322 message
      ▼
S3 bucket  (ngx-prod-emails)
      │  S3 event notification
      ▼
SQS queue  (email_inbound)
      │  SQS trigger
      ▼
email_inbound Lambda
      ├─── Parse MIME ──────────────────────────────────────┐
      ├─── Match recipients → inboxes (DB lookup)           │
      ├─── Find or create thread                            │
      ├─── Write message row per inbox (DB, within tx)      │
      ├─── Upload text/html body parts to S3                │
      ├─── Upload attachments to S3                         │
      └─── Publish events ──────────────────────────────────┘
                │
        ┌───────┼────────────┐
        ▼       ▼            ▼
  webhook_   ws_dispatch  embedder
  delivery    queue        queue
```

### 1.1 How SES delivers the email

SES applies a receipt rule that writes the full RFC 5322 message as a single
S3 object. An S3 event notification fires, SES converts it to an SQS message
containing the bucket name and S3 key, and that triggers the `email_inbound`
Lambda.

The Lambda receives an `aws.S3Event`. The raw email is fetched from S3:

```go
s3Key, _ := url.QueryUnescape(record.S3.Object.Key)
rawData, _ := emailsS3.Download(ctx, s3Key)
```

This `s3Key` is the canonical location of the original message and is stored
as `messages.raw_key` in the database.

### 1.2 MIME parsing

The full RFC 5322 byte stream is parsed by the internal `pkg/mime` package:

```go
parsed, err := mimepkg.Parse(bytes.NewReader(rawData))
```

If parsing fails the Lambda constructs an empty `ParsedEmail{}` and continues
— ingest is not aborted. Fields extracted:

| Field | Source |
|-------|--------|
| `MessageID` | `Message-ID` header |
| `InReplyTo` | `In-Reply-To` header |
| `References` | `References` header (split into slice) |
| `From` | `From` header |
| `To` | `To` header (slice of addresses) |
| `CC` | `Cc` header (slice of addresses) |
| `ReplyTo` | `Reply-To` header |
| `Subject` | `Subject` header |
| `Date` | `Date` header → `time.Time` |
| `BodyText` | `text/plain` MIME part |
| `BodyHTML` | `text/html` MIME part |
| `Parts` | All other MIME parts (attachments, inline) |
| `Headers` | All RFC headers preserved as `map[string][]string` |

### 1.3 Recipient-to-inbox matching

Recipients are collected from both `To` and `CC` headers (BCC is not present
in received email):

```go
recipients = append(recipients, parsed.To...)
recipients = append(recipients, parsed.CC...)
```

For each address, the Lambda queries:

```sql
SELECT id, org_id, pod_id, address, display_name, status, settings, ...
FROM inboxes
WHERE address = $1 AND status = 'active'
```

If the address is not found the recipient is silently skipped (debug log). If
the query fails the Lambda returns an error and the SQS message is retried.

### 1.4 Per-inbox processing (one transaction per inbox)

Everything from here runs inside `dbpkg.WithOrgTx(ctx, pool, inbox.OrgID, …)`,
which sets the PostgreSQL `app.current_org_id` session variable so RLS
policies apply. The same email received by two inboxes runs through this
block twice independently.

**Order of DB writes:**

1. **Find or create thread** — see §3 for full thread-matching logic
2. **Insert message row** — `direction = 'inbound'`, `status = 'received'`
3. **Insert attachment rows** — one per MIME part that is an attachment or
   inline resource (errors logged but do not abort the transaction)
4. **Increment thread message count** and update `last_message_at`, `snippet`
5. **Merge thread participants** — `buildParticipants()` collects From + To +
   CC, deduplicated in PostgreSQL via `jsonb_agg DISTINCT` on the email field

### 1.5 S3 uploads (outside the DB transaction)

After the transaction commits, body parts are uploaded to S3:

```
text/plain body → {org_id}/{pod_id|"no-pod"}/{inbox_id}/text/{message_id}.txt
text/html  body → {org_id}/{pod_id|"no-pod"}/{inbox_id}/html/{message_id}.html
attachments     → {org_id}/{pod_id|"no-pod"}/{inbox_id}/attachments/{message_id}/{filename}
```

The S3 keys are written back to `messages.body_text_key` / `messages.body_html_key`.

### 1.6 Events published after ingest

```go
queues := []string{webhookDeliveryURL, wsDispatchURL, embedderURL}
```

| Event | Destination queues |
|-------|--------------------|
| `message.received` | webhook_delivery, ws_dispatch, **embedder** |
| `thread.created` (new thread only) | webhook_delivery, ws_dispatch |

---

## 2. Outbound Email Flow

```
API caller
    │  POST /v1/inboxes/{inboxId}/messages/send
    ▼
API Gateway (REST)
    │  Lambda proxy
    ▼
messages Lambda
    ├─── Validate request + load inbox
    ├─── Resolve thread (new or reply)
    ├─── Insert message row  (status = 'sending')
    └─── Enqueue job ──────────────────────────────────────┐
                                                           │
                                             SQS FIFO queue (email_outbound)
                                                           │  SQS trigger
                                                           ▼
                                             email_outbound Lambda
                                                 ├─── Load message from DB
                                                 ├─── Build RFC 5322 MIME
                                                 ├─── Call SES SendEmail API
                                                 ├─── Update message status
                                                 └─── Publish event
```

### 2.1 API request

`POST /v1/inboxes/{inboxId}/messages/send`

```json
{
  "to":         [{"email": "alice@example.com", "name": "Alice"}],
  "cc":         [{"email": "bob@example.com"}],
  "bcc":        [{"email": "hidden@example.com"}],
  "subject":    "Hello",
  "body_text":  "Plain text body",
  "body_html":  "<p>HTML body</p>",
  "reply_to_id": "<uuid of parent message — optional>",
  "metadata":   {}
}
```

### 2.2 Messages Lambda processing

1. Load the inbox to get the `From` address.
2. If `reply_to_id` is set: load the parent message, extract its
   `message_id_header` as the new `In-Reply-To`, and build `References` by
   appending the parent's message ID to its existing References list (RFC 5322
   threading chain).
3. If no `reply_to_id`: create a new thread.
4. Insert message row with `direction = 'outbound'`, `status = 'sending'`.
5. Publish a job to the FIFO outbound queue.

The FIFO queue uses the message UUID as both `MessageGroupId` and
`MessageDeduplicationId`, preventing duplicate sends.

**Job payload:**

```json
{
  "message_id": "uuid",
  "org_id":     "uuid",
  "inbox_id":   "uuid",
  "thread_id":  "uuid",
  "from":       {"email": "...", "name": "..."},
  "to":         [...],
  "cc":         [...],
  "subject":    "...",
  "body_text":  "...",
  "body_html":  "...",
  "in_reply_to": "...",
  "references":  [...]
}
```

> **Note:** BCC is intentionally excluded from the job payload to avoid
> leaking BCC recipients. The `email_outbound` Lambda reads BCC from the DB
> message record directly.

### 2.3 Email outbound Lambda

1. Unmarshal the SQS job.
2. Load the full message record from DB (to get BCC and any S3-stored bodies).
3. Collect recipients:
   - `To` and `CC` from the DB record's JSONB arrays.
   - `BCC` from the DB record (not in the job payload).
4. Build the RFC 5322 MIME message:
   - `From`, `To`, `Cc`, `Subject`, `In-Reply-To`, `References`, `Reply-To`
     headers set from job / message record.
   - Message-ID generated as `<{uuid}@nGX>`.
   - Body: `multipart/alternative` (text + HTML), `text/html` only, or
     `text/plain` only — whichever parts are non-empty.
   - All body parts encoded as `quoted-printable`.
5. Call `sesv2.SendEmail` with the raw MIME bytes.
   - BCC recipients are placed in `Destination.ToAddresses` — they receive the
     email but the `Bcc:` header is not present in the MIME (standard BCC
     behaviour).
   - `SES_CONFIGURATION_SET` env var is attached when set.

### 2.4 Status updates and events

| Outcome | DB update | Event published |
|---------|-----------|-----------------|
| SES returns 2xx | `status = 'sent'`, `sent_at = now()` | `message.sent` |
| SES returns error | `status = 'failed'` | `message.bounced` |

### 2.5 Bounce and complaint handling (ses_events Lambda)

SES publishes bounce, complaint, and delivery notifications to SNS. SNS
delivers them to the `ses_events` SQS queue, where the `ses_events` Lambda
processes them.

The SQS message body is a JSON-encoded SNS wrapper:

```json
{
  "Type":    "Notification",
  "Message": "{\"notificationType\":\"Bounce\", \"mail\":{\"messageId\":\"...\",\"headers\":[...]}}"
}
```

The Lambda:
1. Unwraps the SNS envelope.
2. Parses the inner SES notification.
3. Extracts the RFC 5322 `Message-ID` header from `mail.headers`.
4. Strips angle brackets (`<id>` → `id`).
5. Updates the matching message row by `message_id_header`:

| `notificationType` | DB action |
|--------------------|-----------|
| `Bounce` | `status = 'failed'` |
| `Complaint` | `status = 'failed'` |
| `Delivery` | `status = 'sent'`, `sent_at = now()` |

The `ses_events` Lambda bypasses RLS because it does not have an org context
— it matches messages by the global `message_id_header` column.

---

## 3. Thread Mechanics

### 3.1 Thread matching algorithm

When a new message arrives (inbound or outbound reply), nGX checks whether it
belongs to an existing thread using the RFC 5322 threading headers:

```
lookupIDs = []
if In-Reply-To present:
    lookupIDs += [In-Reply-To]
lookupIDs += References (in order)
```

Then:

```sql
SELECT DISTINCT t.*
FROM   threads t
JOIN   messages m ON m.thread_id = t.id
WHERE  t.org_id   = $1
AND    t.inbox_id = $2
AND    m.message_id_header = ANY($3)   -- $3 is lookupIDs array
LIMIT  1
```

If a match is found the incoming message is appended to that thread. If no
match is found a new thread is created.

### 3.2 New thread creation

```
thread.id            = new UUID
thread.org_id        = inbox.org_id
thread.inbox_id      = inbox.id
thread.subject       = parsed.Subject     ← set once, never updated
thread.snippet       = first 200 chars of body text
thread.status        = 'open'
thread.is_read       = false
thread.is_starred    = false
thread.message_count = 0
thread.participants  = buildParticipants(From + To + CC)
```

The subject is frozen at thread creation. Subsequent messages in the same
thread do not update it.

### 3.3 Thread updates on each new message

```
thread.message_count  += 1
thread.last_message_at = message.received_at (or sent_at for outbound)
thread.snippet         = new message snippet
thread.participants    = merged (PostgreSQL jsonb_agg DISTINCT on email field)
```

### 3.4 Snippet generation

```go
snippet = strings.ReplaceAll(bodyText, "\n", " ")
snippet = strings.TrimSpace(snippet)
if len(snippet) > 200 {
    snippet = snippet[:200]
}
```

---

## 4. Multiple Recipients and S3 Storage

### 4.1 One message row per inbox

A single inbound email addressed to multiple nGX inboxes is processed once
per matched inbox. Each creates an independent message row, an independent
thread (or attaches to an existing thread within that inbox), and independent
S3 body objects.

```
Inbound: To: alice@mail.example.com, bob@mail.example.com

Inbox A (alice)                    Inbox B (bob)
│                                  │
├── thread T-A (new or existing)   ├── thread T-B (new or existing)
├── message M-A                    ├── message M-B
│   raw_key  → same S3 object  ←──┘   raw_key  → same S3 object
│   text_key → A/…/text/M-A.txt       text_key → B/…/text/M-B.txt
└── attachments → A/…/attachments/M-A/{filename}
                                   └── attachments → B/…/attachments/M-B/{filename}
```

The raw S3 key (the original RFC 5322 message written by SES) is shared
across all inbox rows. Body parts (text, HTML, attachments) are written once
per inbox so that access control and per-inbox isolation are maintained.

### 4.2 S3 path construction

```go
podSegment := "no-pod"
if inbox.PodID != nil {
    podSegment = inbox.PodID.String()
}
prefix := fmt.Sprintf("%s/%s/%s", inbox.OrgID, podSegment, inbox.ID)

textKey = fmt.Sprintf("%s/text/%s.txt",  prefix, messageID)
htmlKey = fmt.Sprintf("%s/html/%s.html", prefix, messageID)
attKey  = fmt.Sprintf("%s/attachments/%s/%s", prefix, messageID, filename)
```

Concrete example for org `aaa-…`, pod `bbb-…`, inbox `ccc-…`, message `ddd-…`:

```
aaa-.../bbb-.../ccc-.../text/ddd-....txt
aaa-.../bbb-.../ccc-.../html/ddd-....html
aaa-.../bbb-.../ccc-.../attachments/ddd-..../invoice.pdf
```

If the inbox has no pod, the second segment is the literal string `no-pod`:

```
aaa-.../no-pod/ccc-.../text/ddd-....txt
```

### 4.3 CC vs BCC handling

| Header | Inbound: stored in messages? | Inbound: triggers inbox match? | Outbound: in MIME headers? |
|--------|------------------------------|--------------------------------|---------------------------|
| `To`   | `to_addresses` JSONB | Yes | Yes |
| `Cc`   | `cc_addresses` JSONB | Yes | Yes |
| `Bcc`  | `bcc_addresses` JSONB | No (not present in received email) | No (address delivered via SES `Destination` only) |

---

## 5. Event Fan-out

### 5.1 Event envelope

Every event published by a Lambda shares a common base:

```json
{
  "id":             "uuid",
  "type":           "message.received",
  "org_id":         "uuid",
  "occurred_at":    "2026-04-21T12:00:00Z",
  "correlation_id": "optional-uuid"
}
```

Event-specific data is carried in a `data` field alongside the base.

### 5.2 Which events go where

| Event | webhook_delivery queue | ws_dispatch queue | embedder queue |
|-------|:---:|:---:|:---:|
| `message.received` | ✓ | ✓ | ✓ |
| `message.sent` | ✓ | ✓ | |
| `message.bounced` | ✓ | ✓ | |
| `thread.created` | ✓ | ✓ | |
| `thread.status_changed` | ✓ | ✓ | |
| `draft.created` | ✓ | ✓ | |
| `draft.approved` | ✓ | ✓ | |
| `draft.rejected` | ✓ | ✓ | |
| `inbox.created` | ✓ | ✓ | |
| `label.applied` | ✓ | ✓ | |

### 5.3 Webhook delivery (event_dispatcher_webhook Lambda)

```
webhook_delivery queue
        │  SQS trigger
        ▼
event_dispatcher_webhook Lambda
        │
        ├── Unmarshal event envelope
        ├── Load all active webhooks for org_id  (DB query)
        └── For each webhook that subscribes to this event type:
                ├── Decrypt auth header value (AES-256, Secrets Manager key)
                ├── Create delivery record (DB)
                ├── POST event JSON to webhook URL
                │     Headers:
                │       Content-Type: application/json
                │       X-nGX-Signature: sha256={HMAC-SHA256(payload, secret)}
                │       X-nGX-Event: webhook.delivery
                │       User-Agent: nGX-Webhook/1.0
                │       {custom auth header}: {decrypted value}
                └── Update delivery record (success / failure)
```

A webhook with an empty `events` list receives all event types. A webhook
with a populated `events` list receives only the listed types.

### 5.4 WebSocket dispatch (event_dispatcher_ws Lambda)

```
ws_dispatch queue
        │  SQS trigger
        ▼
event_dispatcher_ws Lambda
        │
        ├── Unmarshal event envelope
        ├── Query active WebSocket connections for org_id:
        │     SELECT connection_id FROM websocket_connections
        │     WHERE org_id = $1 AND (ttl IS NULL OR ttl > NOW())
        └── For each connection:
                ├── POST event JSON to API Gateway Management API
                │     (connection-specific endpoint)
                └── If GoneException (stale connection):
                        DELETE FROM websocket_connections
                        WHERE connection_id = $1
```

---

## 6. S3 Path Reference

| Content | S3 key pattern |
|---------|---------------|
| Raw inbound RFC 5322 | Assigned by SES receipt rule; stored as `messages.raw_key` |
| Inbound text body | `{org_id}/{pod_id\|no-pod}/{inbox_id}/text/{message_id}.txt` |
| Inbound HTML body | `{org_id}/{pod_id\|no-pod}/{inbox_id}/html/{message_id}.html` |
| Inbound attachment | `{org_id}/{pod_id\|no-pod}/{inbox_id}/attachments/{message_id}/{filename}` |
| Outbound bodies | Not stored to S3 by default; bodies in DB / job payload |

All inbound objects are in the `S3_BUCKET_EMAILS` bucket. All keys use
UUID strings with no additional encoding.

---

## 7. Database Schema Quick Reference

### messages

| Column | Type | Notes |
|--------|------|-------|
| `id` | UUID | Primary key |
| `org_id` | UUID | RLS tenant key |
| `inbox_id` | UUID | FK → inboxes |
| `thread_id` | UUID | FK → threads |
| `message_id_header` | text | RFC 5322 `Message-ID` value |
| `in_reply_to` | text | RFC 5322 `In-Reply-To` value |
| `references_header` | text[] | RFC 5322 `References` split into array |
| `direction` | enum | `inbound` \| `outbound` |
| `status` | enum | `received` \| `sending` \| `sent` \| `failed` |
| `from_address` | text | |
| `from_name` | text | |
| `to_addresses` | JSONB | Array of `{email, name}` |
| `cc_addresses` | JSONB | Array of `{email, name}` |
| `bcc_addresses` | JSONB | Array of `{email, name}` |
| `reply_to` | text | |
| `subject` | text | |
| `body_text_key` | text | S3 key, nullable |
| `body_html_key` | text | S3 key, nullable |
| `raw_key` | text | S3 key to original RFC 5322 object |
| `size_bytes` | bigint | |
| `has_attachments` | boolean | |
| `headers` | JSONB | All RFC headers |
| `metadata` | JSONB | Caller-supplied metadata |
| `sent_at` | timestamptz | Nullable |
| `received_at` | timestamptz | Nullable |

### threads

| Column | Type | Notes |
|--------|------|-------|
| `id` | UUID | Primary key |
| `org_id` | UUID | RLS tenant key |
| `inbox_id` | UUID | FK → inboxes |
| `subject` | text | Set at creation, immutable |
| `snippet` | text | Last 200 chars of most recent body |
| `status` | enum | `open` \| `archived` \| `deleted` |
| `is_read` | boolean | |
| `is_starred` | boolean | |
| `message_count` | integer | Incremented on each message |
| `participants` | JSONB | Deduped array of `{email, name}` |
| `last_message_at` | timestamptz | Nullable |

### attachments

| Column | Type | Notes |
|--------|------|-------|
| `id` | UUID | Primary key |
| `org_id` | UUID | RLS tenant key |
| `message_id` | UUID | FK → messages |
| `filename` | text | |
| `content_type` | text | |
| `size_bytes` | bigint | |
| `s3_key` | text | |
| `content_id` | text | Nullable; for `cid:` inline references |
| `is_inline` | boolean | |

---

## 8. Key Implementation Details

### Row-Level Security bypass for inbox lookup

The `email_inbound` Lambda must find an inbox by email address without
knowing the `org_id` in advance. This initial lookup (`GetInboxByAddress`)
runs outside RLS. Once the inbox (and its `org_id`) is resolved, all
subsequent writes run inside `dbpkg.WithOrgTx` which sets
`app.current_org_id` and activates RLS for the transaction.

### FIFO queue prevents duplicate sends

The outbound SQS queue is a FIFO queue. Both `MessageGroupId` and
`MessageDeduplicationId` are set to the message UUID. If the `messages`
Lambda is retried (e.g. Lambda timeout after the message is already
enqueued), SQS deduplicates the second enqueue within the 5-minute
deduplication window.

### BCC privacy preservation

BCC addresses are stored in `messages.bcc_addresses` (JSONB) in the
database. They are deliberately omitted from the SQS outbound job payload.
The `email_outbound` Lambda re-reads BCC from the DB so that BCC recipients
are never exposed in the message queue. In the MIME message, BCC addresses
appear only in the SES `Destination` envelope — not in any MIME header.

### MIME graceful degradation

If `mimepkg.Parse()` returns an error on an inbound message, the Lambda
constructs an empty `ParsedEmail{}` and continues. The raw S3 key is still
stored and the message row is written with whatever fields could be
extracted. This prevents a malformed email from blocking the SQS queue.

### Outbound Message-ID generation

```go
func generateMessageID() string {
    return fmt.Sprintf("<%s@nGX>", uuid.New().String())
}
```

This ID is stored as `messages.message_id_header` and used by recipients'
mail clients to thread replies back to nGX.

### Thread subject immutability

`threads.subject` is written once at creation and never updated, even if
subsequent messages arrive with a different subject. This matches the
behaviour of most email clients and prevents subject changes (e.g.
`Re: Re: Fwd: …`) from fragmenting thread display.
