# Inbox Service

**Module**: `nGX/services/inbox`
**Port**: `8082`
**Role**: Core business logic for inboxes, threads, messages, drafts, and labels. Internal service — not exposed directly to external clients.

## Internal Authentication

The Inbox Service is not publicly accessible. The API Gateway calls it after authenticating the external request. Auth context is passed via HTTP headers set by `InboxClient.proxy()`:

| Header | Value |
|--------|-------|
| `X-Org-ID` | UUID of the authenticated org |
| `X-Key-ID` | UUID of the API key |
| `X-Scopes` | Comma-separated scope list |
| `X-Pod-ID` | UUID if key is pod-scoped (header omitted otherwise) |

The `internalAuthMiddleware` in `server/server.go` reads these headers and constructs a `*auth.Claims` for every request. If `X-Org-ID` or `X-Key-ID` are missing, the request is passed through without a claims context (this only applies to the `/health` endpoint).

## Internal Route Layout

The internal routes differ slightly from the external API paths. Notably, thread and message lookups are by resource ID directly (not always nested under inbox):

| Method | Path | Description |
|--------|------|-------------|
| POST/GET | /inboxes | Create / list inboxes |
| GET/PATCH/DELETE | /inboxes/{inboxID} | Get / update / delete inbox |
| GET | /inboxes/{inboxID}/threads | List threads |
| GET/PATCH | /threads/{threadID} | Get / update thread |
| POST/DELETE | /threads/{threadID}/labels/{labelID} | Apply / remove label |
| POST | /inboxes/{inboxID}/messages | Send message |
| GET | /threads/{threadID}/messages | List messages in thread |
| GET | /messages/{messageID} | Get message |
| POST/GET | /inboxes/{inboxID}/drafts | Create / list drafts |
| GET/PATCH/DELETE | /drafts/{draftID} | Get / update / delete draft |
| POST | /drafts/{draftID}/approve | Approve draft |
| POST | /drafts/{draftID}/reject | Reject draft |
| POST/GET/PATCH/DELETE | /labels | Org-scoped label management |

## Inboxes

**Address uniqueness**: enforced at DB level (`UNIQUE` on the `address` column). A duplicate create returns 409.

**GetByAddress** (used by the email-pipeline): bypasses RLS — the org is unknown when routing inbound email by recipient address.

## Threads

### Thread Deduplication

When an inbound email arrives, the processor calls `FindByMessageIDHeaders` to locate an existing thread. The query:

```sql
SELECT DISTINCT t.*
FROM threads t
JOIN messages m ON m.thread_id = t.id
WHERE t.org_id = $1
  AND t.inbox_id = $2
  AND m.message_id_header = ANY($3)
LIMIT 1
```

`$3` is the array of Message-IDs from the email's `In-Reply-To` and `References` headers. If any existing message shares a `message_id_header`, the new message joins that thread.

This runs inside a `WithOrgTx` transaction, serializing concurrent dedup lookups for the same thread.

### Thread List Filtering

`GET /inboxes/{id}/threads` supports query params:

- `status` — open / closed / spam / trash
- `label_id` — filter by label UUID (uses an EXISTS subquery on `thread_labels`)
- `is_read` — boolean
- `limit` + `cursor` — pagination

### Thread Cursor

The list cursor is keyed on `(last_message_at, id)`:

```sql
WHERE (t.last_message_at, t.id) < ($cursor_time::timestamptz, $cursor_id::uuid)
ORDER BY t.last_message_at DESC NULLS LAST, t.id DESC
LIMIT $limit + 1
```

### Label Operations

```
PUT  /threads/{threadID}/labels/{labelID}
  → INSERT INTO thread_labels (thread_id, label_id, applied_at) VALUES (...) ON CONFLICT DO NOTHING

DELETE /threads/{threadID}/labels/{labelID}
  → DELETE FROM thread_labels WHERE thread_id = $1 AND label_id = $2
```

After each label operation the service publishes a `label.applied` event to `events.fanout`.

### Thread Fields

`ThreadPatch` supports updating `status`, `is_read`, and `is_starred`. Only provided fields are SET; if the patch is empty, a plain SELECT is returned.

## Messages

### Sending (Outbound)

`POST /inboxes/{inboxID}/messages` flow (inside a single `WithOrgTx` transaction):

1. Load inbox (to get the From address and display name).
2. If `reply_to_id` is provided: load the parent message to get its `message_id` header (used as `In-Reply-To`), then load the parent's thread.
3. Otherwise: create a new thread.
4. Insert a `messages` row: `direction=outbound, status=sending`.
5. Call `IncrMessageCount` on the thread: increments `message_count`, sets `last_message_at`, updates `snippet`.
6. Publish `OutboundJob` to `email.outbound.queue` (Kafka). Publish failure is logged but does not roll back the transaction — the message is recorded and the pipeline can retry.
7. Return the message record.

The `OutboundJob` payload sent to Kafka:

```json
{
  "message_id": "uuid",
  "org_id": "uuid",
  "inbox_id": "uuid",
  "thread_id": "uuid",
  "from": {"email": "...", "name": "..."},
  "to": [...],
  "cc": [...],
  "bcc": [...],
  "subject": "...",
  "body_text": "...",
  "body_html": "...",
  "in_reply_to": "..."
}
```

### Body Storage

Message bodies are not stored in PostgreSQL. For inbound messages, `text_s3_key`, `html_s3_key`, and `raw_s3_key` reference S3 object keys. Callers retrieve bodies via presigned URLs. For outbound messages sent via draft approval, body content is passed directly in the job payload (no S3 keys on the draft itself).

### Get Message

`Get` loads the message record and then calls `ListAttachments` — both within the same `WithOrgTx` transaction. Attachments are appended to `msg.Attachments` before returning.

## Draft State Machine

```
       POST /drafts
            │
            ▼
        PENDING ────────────────────────────────┐
            │                                    │
  POST /approve                        POST /reject
            │                                    │
            ▼                                    ▼
       APPROVED                             REJECTED
            │
   (message created +
    outbound job queued +
    event published)
```

A draft can only be approved or rejected while `review_status = 'pending'`. Both operations return `ErrInvalidReviewStatus` (409) otherwise.

**Update** also checks `review_status = 'pending'` — you cannot modify an approved or rejected draft.

### Approve Logic (`DraftService.Approve`)

All steps run inside a single `WithOrgTx` transaction:

1. Load draft, verify `review_status == "pending"`.
2. Load inbox for the From address.
3. If `draft.ThreadID != nil`: load the existing thread. Otherwise: create a new thread.
4. Create a `messages` row from the draft content (`direction=outbound, status=sending`).
5. Call `IncrMessageCount` on the thread.
6. Update the draft: `review_status = "approved"`, `reviewed_at = NOW()`, `reviewed_by = claims.KeyID`.
7. Publish `OutboundJob` to `email.outbound.queue`.
8. Publish `EventDraftApproved` to `events.fanout`.

### Reject Logic (`DraftService.Reject`)

Inside a single `WithOrgTx` transaction:

1. Load draft, verify `review_status == "pending"`.
2. Update draft: `review_status = "rejected"`, `review_note = reason`, `reviewed_at = NOW()`, `reviewed_by = claims.KeyID`.
3. Publish `EventDraftRejected` to `events.fanout`.

## Cursor-Based Pagination

All list endpoints use cursor pagination. The cursor is `base64url(JSON([timestamp, id]))` encoded by `pagination.EncodeCursor` and decoded by `pagination.DecodeCursor`.

SQL keyset pattern (drafts/messages use `created_at`; threads use `last_message_at`):

```sql
WHERE (created_at, id) < ($cursor_time::timestamptz, $cursor_id::uuid)
ORDER BY created_at DESC, id DESC
LIMIT $limit + 1  -- fetch one extra to determine has_more
```

## Event Publishing

| Action | Event Type | Topic |
|--------|-----------|-------|
| Create draft | `draft.created` | events.fanout |
| Approve draft | `draft.approved` | events.fanout |
| Reject draft | `draft.rejected` | events.fanout |
| Thread status change | `thread.status_changed` | events.fanout |
| Apply label | `label.applied` | events.fanout |
| Send message (queue) | — | email.outbound.queue |

Note: `message.received` and `message.sent` events are published by the email-pipeline service, not the inbox service.

## Configuration

| Env Var | Default | Description |
|---------|---------|-------------|
| DATABASE_URL | postgres://... | Postgres connection |
| KAFKA_BROKERS | localhost:9092 | Comma-separated brokers |
| INBOX_SERVICE_PORT | 8082 | HTTP port (hardcoded; not read from environment) |
