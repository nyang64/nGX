# Data Model

## Entity Relationships

```
organizations
  ├── pods (org_id FK)
  │     ├── inboxes (pod_id FK)
  │     │     ├── threads (inbox_id FK)
  │     │     │     ├── messages (thread_id FK)
  │     │     │     │     └── attachments (message_id FK)
  │     │     │     └── thread_labels (thread_id FK)
  │     │     └── drafts (inbox_id FK)
  │     └── domain_configs (pod_id FK)
  ├── labels (org_id FK)
  │     └── thread_labels (label_id FK)
  ├── api_keys (org_id FK, pod_id nullable FK)
  └── webhooks (org_id FK)
        └── webhook_deliveries (webhook_id FK)

messages → outbound_jobs (message_id FK)
```

## Table Reference

### organizations

Root tenant entity. Every other table traces back to an `org_id`.

| Column | Type | Notes |
|--------|------|-------|
| id | UUID | PK, `gen_random_uuid()` |
| name | TEXT | Human-readable org name |
| slug | TEXT | URL-safe unique identifier |
| plan | TEXT | `free` (default); billing tier |
| settings | JSONB | Extensible org-level config, defaults `{}` |
| created_at | TIMESTAMPTZ | Set on insert |
| updated_at | TIMESTAMPTZ | Maintained by `set_updated_at` trigger |

Indexes: PK on `id`, unique on `slug`.

RLS policy `org_isolation`: `id = current_setting('app.current_org_id', TRUE)::uuid`

---

### pods

A pod is a logical grouping of inboxes within an organization (e.g., "support", "sales"). Slug is unique per org, not globally.

| Column | Type | Notes |
|--------|------|-------|
| id | UUID | PK, `gen_random_uuid()` |
| org_id | UUID | FK → organizations(id) ON DELETE CASCADE |
| name | TEXT | Display name |
| slug | TEXT | URL-safe; unique within org via `pods_org_slug_unique` constraint |
| description | TEXT | Optional |
| settings | JSONB | Pod-level config, defaults `{}` |
| created_at | TIMESTAMPTZ | Set on insert |
| updated_at | TIMESTAMPTZ | Maintained by trigger |

Indexes: `pods_org_id_idx` on `(org_id)`.

RLS policy `pod_isolation`: `org_id = current_setting('app.current_org_id', TRUE)::uuid`

---

### inboxes

An inbox is a single email address that receives and sends messages. Belongs to an org and optionally to a pod.

| Column | Type | Notes |
|--------|------|-------|
| id | UUID | PK, `gen_random_uuid()` |
| org_id | UUID | FK → organizations(id) ON DELETE CASCADE |
| pod_id | UUID | Nullable FK → pods(id) ON DELETE SET NULL |
| address | TEXT | Full email address; globally unique |
| display_name | TEXT | Friendly "From" name |
| status | TEXT | `active` \| `suspended` \| `deleted`; CHECK constraint |
| settings | JSONB | Inbox-level config, defaults `{}` |
| created_at | TIMESTAMPTZ | Set on insert |
| updated_at | TIMESTAMPTZ | Maintained by trigger |

Indexes: `inboxes_org_id_idx`, `inboxes_pod_id_idx`, `inboxes_address_idx` (used for inbound routing without RLS — see bypass cases below).

RLS policy `inbox_isolation`: `org_id = current_setting('app.current_org_id', TRUE)::uuid`

---

### threads

A thread groups related messages by conversation (subject + References/In-Reply-To chain). Denormalized fields (`snippet`, `message_count`, `participants`, `last_message_at`) are updated by the application on each new message.

| Column | Type | Notes |
|--------|------|-------|
| id | UUID | PK, `gen_random_uuid()` |
| org_id | UUID | FK → organizations(id) ON DELETE CASCADE |
| inbox_id | UUID | FK → inboxes(id) ON DELETE CASCADE |
| subject | TEXT | Subject line; may be NULL for subject-less threads |
| snippet | TEXT | Preview of the latest message body |
| status | TEXT | `open` \| `closed` \| `spam` \| `trash`; CHECK constraint |
| is_read | BOOLEAN | Whether the thread has been read; defaults FALSE |
| is_starred | BOOLEAN | Starred/flagged state; defaults FALSE |
| message_count | INTEGER | Denormalized count; updated on insert/delete of messages |
| participants | JSONB | Array of address objects for all senders/recipients |
| last_message_at | TIMESTAMPTZ | Timestamp of most recent message; used for sort |
| created_at | TIMESTAMPTZ | Set on insert |
| updated_at | TIMESTAMPTZ | Maintained by trigger |

Indexes:
- `threads_org_id_idx` on `(org_id)`
- `threads_inbox_id_idx` on `(inbox_id)`
- `threads_status_idx` on `(org_id, status)` — efficient status-filtered list queries
- `threads_last_message_at_idx` on `(org_id, last_message_at DESC NULLS LAST)` — inbox feed ordering

RLS policy `thread_isolation`: `org_id = current_setting('app.current_org_id', TRUE)::uuid`

---

### messages

Individual email messages, both inbound and outbound. Bodies are stored in S3; only S3 object keys are kept in Postgres.

| Column | Type | Notes |
|--------|------|-------|
| id | UUID | PK, `gen_random_uuid()` |
| org_id | UUID | FK → organizations(id) ON DELETE CASCADE |
| inbox_id | UUID | FK → inboxes(id) ON DELETE CASCADE |
| thread_id | UUID | FK → threads(id) ON DELETE CASCADE |
| message_id_header | TEXT | RFC 5322 `Message-ID` header value; nullable |
| in_reply_to | TEXT | RFC 5322 `In-Reply-To` header; used for threading |
| references_header | TEXT[] | RFC 5322 `References` header split into array |
| direction | TEXT | `inbound` \| `outbound`; CHECK constraint |
| status | TEXT | `received` \| `sending` \| `sent` \| `failed` \| `draft`; CHECK constraint |
| from_address | TEXT | Sender email address |
| from_name | TEXT | Sender display name |
| to_addresses | JSONB | Array of `{email, name}` objects |
| cc_addresses | JSONB | Array of `{email, name}` objects |
| bcc_addresses | JSONB | Array of `{email, name}` objects |
| reply_to | TEXT | Reply-To address if different from From |
| subject | TEXT | Email subject |
| body_text_key | TEXT | S3 object key for plain-text body |
| body_html_key | TEXT | S3 object key for HTML body |
| raw_key | TEXT | S3 object key for original RFC 5322 `.eml` file |
| size_bytes | BIGINT | Total message size including attachments |
| has_attachments | BOOLEAN | Quick flag; avoids joining to attachment records |
| headers | JSONB | All RFC 5322 headers as `{name: [values]}` |
| metadata | JSONB | Application metadata (e.g., delivery receipts) |
| sent_at | TIMESTAMPTZ | When the outbound message was submitted to SMTP |
| received_at | TIMESTAMPTZ | When the inbound message arrived |
| created_at | TIMESTAMPTZ | Set on insert |
| updated_at | TIMESTAMPTZ | Maintained by trigger |
| search_vector | TSVECTOR | Auto-populated by trigger; see Full-Text Search below |
| embedding | vector(256) | 256-dim MRL-truncated embedding from `nomic-embed-text-v1.5`; NULL until the embedder service processes the message |
| embedded_at | TIMESTAMPTZ | When the embedding was last generated; NULL if not yet embedded |

Indexes:
- `messages_org_id_idx`, `messages_inbox_id_idx`, `messages_thread_id_idx`
- `messages_message_id_header_idx` on `(message_id_header) WHERE message_id_header IS NOT NULL` — partial index for de-duplication
- `messages_received_at_idx` on `(org_id, received_at DESC NULLS LAST)`
- `messages_search_vector_idx` — GIN index on `search_vector`
- `messages_embedding_hnsw` — HNSW index on `embedding` using `vector_cosine_ops` (`m=16, ef_construction=64`)

RLS policy `message_isolation`: `org_id = current_setting('app.current_org_id', TRUE)::uuid`

---

### attachments

Not a standalone migration in the current set — attachment metadata is tracked via `has_attachments` on `messages` and the files are stored in S3 under a predictable key path. See S3 Object Key Structure below.

---

### labels + thread_labels

`labels` defines org-scoped tags. `thread_labels` is the junction table that applies labels to threads (many-to-many).

**labels**

| Column | Type | Notes |
|--------|------|-------|
| id | UUID | PK, `gen_random_uuid()` |
| org_id | UUID | FK → organizations(id) ON DELETE CASCADE |
| name | TEXT | Label name; unique within org via `labels_org_name_unique` |
| color | TEXT | Optional hex color for UI display |
| description | TEXT | Optional description |
| created_at | TIMESTAMPTZ | Set on insert |

Indexes: `labels_org_id_idx` on `(org_id)`.

RLS policy `label_isolation`: `org_id = current_setting('app.current_org_id', TRUE)::uuid`

**thread_labels**

| Column | Type | Notes |
|--------|------|-------|
| thread_id | UUID | FK → threads(id) ON DELETE CASCADE; part of composite PK |
| label_id | UUID | FK → labels(id) ON DELETE CASCADE; part of composite PK |
| applied_at | TIMESTAMPTZ | When the label was attached |

Indexes: `thread_labels_label_id_idx` on `(label_id)` — for "find all threads with label X".

RLS policy `thread_label_isolation`: sub-selects `threads` to resolve org ownership (no direct `org_id` column):
```sql
EXISTS (
    SELECT 1 FROM threads t
    WHERE t.id = thread_id
      AND t.org_id = current_setting('app.current_org_id', TRUE)::uuid
)
```

---

### api_keys

Stores hashed API keys. The plaintext key is shown once at creation and never stored.

| Column | Type | Notes |
|--------|------|-------|
| id | UUID | PK, `gen_random_uuid()` |
| org_id | UUID | FK → organizations(id) ON DELETE CASCADE |
| name | TEXT | Human-readable label |
| key_prefix | TEXT | First 16 chars of plaintext, shown in UI for identification |
| key_hash | TEXT | SHA-256 hex of plaintext; globally unique; used for lookup |
| scopes | TEXT[] | Array of scope strings; see `pkg/auth/scopes.go` |
| pod_id | UUID | Nullable FK → pods(id) ON DELETE SET NULL; if set, key is pod-scoped |
| last_used_at | TIMESTAMPTZ | Updated on each successful authentication |
| expires_at | TIMESTAMPTZ | NULL = never expires |
| revoked_at | TIMESTAMPTZ | NULL = active; set to revoke |
| created_at | TIMESTAMPTZ | Set on insert |

Indexes:
- `api_keys_org_id_idx` on `(org_id)`
- `api_keys_key_hash_idx` on `(key_hash)` — fast lookup during auth; bypasses RLS

RLS policy `api_key_isolation`: `org_id = current_setting('app.current_org_id', TRUE)::uuid`

---

### drafts

Agent-authored messages queued for human review before sending. Body content is stored inline (TEXT columns), not in S3, because drafts are typically short-lived and frequently edited.

| Column | Type | Notes |
|--------|------|-------|
| id | UUID | PK, `gen_random_uuid()` |
| org_id | UUID | FK → organizations(id) ON DELETE CASCADE |
| inbox_id | UUID | FK → inboxes(id) ON DELETE CASCADE |
| thread_id | UUID | Nullable FK → threads(id) ON DELETE SET NULL; NULL for new threads |
| to_addresses | JSONB | Array of `{email, name}` objects |
| cc_addresses | JSONB | Array of `{email, name}` objects |
| bcc_addresses | JSONB | Array of `{email, name}` objects |
| subject | TEXT | Draft subject |
| body_text | TEXT | Plain-text draft body (inline, not S3) |
| body_html | TEXT | HTML draft body (inline, not S3) |
| metadata | JSONB | Agent-supplied context (e.g., reasoning, citations) |
| review_status | TEXT | `pending` \| `approved` \| `rejected` \| `sent`; CHECK constraint |
| review_note | TEXT | Human reviewer's note on approval or rejection |
| reviewed_by | TEXT | Identifier of the reviewer |
| reviewed_at | TIMESTAMPTZ | When review action was taken |
| expires_at | TIMESTAMPTZ | Optional auto-expiry for time-sensitive drafts |
| created_at | TIMESTAMPTZ | Set on insert |
| updated_at | TIMESTAMPTZ | Maintained by trigger |

Indexes:
- `drafts_org_id_idx`, `drafts_inbox_id_idx`
- `drafts_thread_id_idx` on `(thread_id) WHERE thread_id IS NOT NULL` — partial index
- `drafts_review_status_idx` on `(org_id, review_status)` — pending review queue

RLS policy `draft_isolation`: `org_id = current_setting('app.current_org_id', TRUE)::uuid`

---

### webhooks + webhook_deliveries

`webhooks` defines registered HTTP endpoints. `webhook_deliveries` is the per-attempt delivery log.

**webhooks**

| Column | Type | Notes |
|--------|------|-------|
| id | UUID | PK, `gen_random_uuid()` |
| org_id | UUID | FK → organizations(id) ON DELETE CASCADE |
| url | TEXT | Target endpoint URL |
| secret | TEXT | HMAC signing secret; provided to caller at creation |
| events | TEXT[] | Event types to deliver (e.g., `{"message.received"}`) |
| pod_id | UUID | Nullable FK — scope deliveries to a specific pod |
| inbox_id | UUID | Nullable FK — scope deliveries to a specific inbox |
| is_active | BOOLEAN | Whether deliveries are attempted; defaults TRUE |
| failure_count | INTEGER | Consecutive failure count; used to auto-disable |
| last_success_at | TIMESTAMPTZ | Last successful delivery timestamp |
| last_failure_at | TIMESTAMPTZ | Last failed delivery timestamp |
| created_at | TIMESTAMPTZ | Set on insert |
| updated_at | TIMESTAMPTZ | Maintained by trigger |

**webhook_deliveries**

| Column | Type | Notes |
|--------|------|-------|
| id | UUID | PK, `gen_random_uuid()` |
| webhook_id | UUID | FK → webhooks(id) ON DELETE CASCADE |
| event_id | TEXT | The `BaseEvent.ID` being delivered |
| event_type | TEXT | The `BaseEvent.Type` string |
| payload | JSONB | Full event JSON |
| status | TEXT | `pending` \| `success` \| `failed` \| `retrying`; CHECK constraint |
| attempt_count | INTEGER | How many delivery attempts have been made |
| next_attempt_at | TIMESTAMPTZ | When to retry next |
| last_attempt_at | TIMESTAMPTZ | Timestamp of most recent attempt |
| response_status | INTEGER | HTTP status code from last attempt |
| response_body | TEXT | Response body from last attempt (truncated) |
| error_message | TEXT | Error description if delivery failed |
| created_at | TIMESTAMPTZ | Set on insert |
| updated_at | TIMESTAMPTZ | Maintained by trigger |

Indexes:
- `webhook_deliveries_webhook_id_idx` on `(webhook_id)`
- `webhook_deliveries_status_next_attempt_idx` on `(status, next_attempt_at) WHERE status IN ('pending', 'retrying')` — partial index used by the scheduler

RLS policy `webhook_delivery_isolation`: sub-selects `webhooks` to resolve org:
```sql
EXISTS (
    SELECT 1 FROM webhooks w
    WHERE w.id = webhook_id
      AND w.org_id = current_setting('app.current_org_id', TRUE)::uuid
)
```

---

### outbound_jobs

A job queue for outbound email delivery. Created when a message transitions to `sending` status; consumed by the email-pipeline service.

| Column | Type | Notes |
|--------|------|-------|
| id | UUID | PK, `gen_random_uuid()` |
| org_id | UUID | FK → organizations(id) ON DELETE CASCADE |
| message_id | UUID | FK → messages(id) ON DELETE CASCADE |
| status | TEXT | `pending` \| `processing` \| `sent` \| `failed` \| `cancelled`; CHECK constraint |
| attempt_count | INTEGER | Number of delivery attempts made |
| max_attempts | INTEGER | Maximum attempts before marking `failed`; defaults 3 |
| next_attempt_at | TIMESTAMPTZ | When to process next; defaults NOW() |
| last_error | TEXT | Error from last failed attempt |
| created_at | TIMESTAMPTZ | Set on insert |
| updated_at | TIMESTAMPTZ | Maintained by trigger |

Indexes:
- `outbound_jobs_org_id_idx` on `(org_id)`
- `outbound_jobs_status_next_attempt_idx` on `(status, next_attempt_at) WHERE status IN ('pending', 'processing')` — partial index used by the scheduler to claim work

RLS policy `outbound_job_isolation`: `org_id = current_setting('app.current_org_id', TRUE)::uuid`

---

### domain_configs

Not present in the current migration set. Domain verification records (SPF, DKIM, MX) for custom sending domains are a planned addition under `pods`.

---

## Row-Level Security

### Overview

Every table has RLS enabled. The current org is injected at the start of each database transaction using a PostgreSQL session variable. The `nGX_app` role (used by all running services) is subject to RLS policies. The superuser/migration role bypasses RLS automatically.

### Setting the Context

From `pkg/db/rls.go`:
```go
func SetOrgContext(ctx context.Context, tx pgx.Tx, orgID uuid.UUID) error {
    _, err := tx.Exec(ctx,
        "SELECT set_config('app.current_org_id', $1, TRUE)",
        orgID.String(),
    )
    return err
}
```

`TRUE` in `set_config` means the setting is LOCAL to the transaction — it resets automatically when the transaction ends. An alternative `set_rls_context(uuid)` SQL function (defined in `configs/postgres/init.sql`) wraps the same call with `SECURITY DEFINER` for contexts that call it from SQL directly.

### Policy Examples

```sql
-- Direct org_id column (most tables)
CREATE POLICY org_isolation ON inboxes
    USING (org_id = current_setting('app.current_org_id', TRUE)::uuid);

-- Tables without direct org_id (thread_labels, webhook_deliveries)
CREATE POLICY org_isolation ON thread_labels
    USING (EXISTS (
        SELECT 1 FROM threads t
        WHERE t.id = thread_labels.thread_id
          AND t.org_id = current_setting('app.current_org_id', TRUE)::uuid
    ));
```

### WithOrgTx Helper

```go
// WithOrgTx starts a transaction, injects RLS context, runs fn, commits.
func WithOrgTx(ctx context.Context, pool *pgxpool.Pool, orgID uuid.UUID, fn func(pgx.Tx) error) error {
    return WithTx(ctx, pool, func(tx pgx.Tx) error {
        if err := SetOrgContext(ctx, tx, orgID); err != nil {
            return err
        }
        return fn(tx)
    })
}
```

### RLS Bypass Cases

Two operations intentionally bypass RLS:

1. **API key validation** (`Auth.Validate`): org is unknown before key lookup; safe because `key_hash` is globally unique and the query only reads the `api_keys` table using the hash as the lookup predicate.
2. **Inbox address lookup** (`GetInboxByAddress`): the email-pipeline looks up an inbox by address before knowing which org owns it. This is read-only on a non-sensitive column and required for inbound routing to function.

---

## Cursor-Based Pagination

### Why Not OFFSET?

`OFFSET N` requires the database to scan and discard N rows on every page. With concurrent inserts, rows shift — a row can appear on two pages or be skipped entirely.

### Cursor Design

The cursor encodes the last row's position as `base64(part1|part2|...)`. For time-ordered lists the parts are `created_at` (RFC 3339) and `id` (UUID string):

```go
// Encode the last row's position
cursor := pagination.EncodeCursor(lastRow.CreatedAt.Format(time.RFC3339Nano), lastRow.ID.String())

// Decode to use in SQL WHERE clause
parts, err := pagination.DecodeCursor(cursorStr)
// parts[0] = timestamp string, parts[1] = UUID string
```

### SQL Pattern

```sql
-- Descending by time, with UUID as tiebreaker
WHERE (created_at, id) < ($cursor_time::timestamptz, $cursor_id::uuid)
ORDER BY created_at DESC, id DESC
LIMIT $limit + 1  -- fetch extra to determine has_more
```

If `len(results) > limit`, set `has_more = true` and trim the last item. The next cursor is encoded from the last returned row.

`ClampLimit` normalizes caller-supplied limits: values `<= 0` default to 20, values `> 100` are capped at 100.

---

## Search

### Keyword Search (tsvector)

The `messages` table has a `search_vector TSVECTOR` column populated by a trigger on INSERT and on UPDATE of `subject`, `from_address`, or `from_name`:

```sql
NEW.search_vector :=
    setweight(to_tsvector('english', COALESCE(NEW.subject, '')), 'A') ||
    setweight(to_tsvector('english', COALESCE(NEW.from_address, '')), 'B') ||
    setweight(to_tsvector('english', COALESCE(NEW.from_name, '')), 'B');
```

Weight `A` (subject) ranks higher than weight `B` (addresses) in `ts_rank` scoring. A GIN index on `search_vector` makes `@@` queries efficient.

`plainto_tsquery` converts natural language ("invoice overdue") to `invoice & overdue` automatically.

### Semantic Search (pgvector)

The `messages` table also has an `embedding vector(256)` column. Embeddings are generated asynchronously by the `embedder` service using the `nomic-embed-text-v1.5` model, with MRL truncation to 256 dimensions.

An HNSW index enables approximate nearest-neighbour cosine similarity queries:

```sql
-- Find messages semantically similar to a query embedding
SELECT m.id, m.subject,
       1 - (m.embedding <=> $1::vector) AS similarity
FROM messages m
WHERE m.org_id = $2
  AND m.embedding IS NOT NULL
ORDER BY m.embedding <=> $1::vector ASC
LIMIT 20;
```

The `embedder` service populates `embedding` and `embedded_at` after receiving `message.received` or `message.sent` events from Kafka. Messages without a body text S3 key are skipped. The `embedded_at` column is used by a reconciliation job to detect and re-queue messages that were missed due to transient embedder failures.

The search service exposes both modes via `GET /search?mode=keyword` (default) and `GET /search?mode=semantic`. Semantic mode gracefully degrades to keyword search if the embedding server is unavailable.

---

## S3 Object Key Structure

Email bodies and attachments are stored in S3 (or MinIO locally), not in PostgreSQL. The `messages` table stores only the S3 key strings.

```
{org_id}/
  {pod_id}/
    {inbox_id}/
      raw/{msg_id}.eml           <- original RFC 5322 message
      text/{msg_id}.txt          <- extracted plain-text body
      html/{msg_id}.html         <- extracted HTML body
      attachments/{msg_id}/
        {filename}               <- each attachment by original filename
```

Two S3 buckets are provisioned by `docker-compose.yml` via `minio-init`:
- `nGX-emails` — raw and parsed message bodies
- `nGX-attachments` — attachment files (public-read in dev via `mc anonymous set download`)

**Why S3?**
- Email bodies can be megabytes; storing in Postgres bloats the `messages` table and degrades index scans.
- S3 is cheaper per GB for cold object storage.
- Presigned URLs let API clients download bodies directly without proxying through the application.
