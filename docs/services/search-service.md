# Search Service

**Module**: `agentmail/services/search`
**Port**: `8084`
**Role**: Keyword and semantic search across email messages using PostgreSQL `tsvector` (keyword) and `pgvector` HNSW (semantic).

## Endpoint

```
GET /search?q={query}&inbox_id={uuid}&limit={n}&cursor={cursor}&mode={keyword|semantic}
```

The search service is called via the API Gateway. The gateway applies `requireScope(search:read)` and proxies with `X-Org-ID` / `X-Scopes` headers. The search handler reads `OrgID` from the context set by `internalAuthMiddleware`.

**Parameters**:

| Param | Required | Description |
|-------|----------|-------------|
| `q` | Yes | Search query string |
| `inbox_id` | No | Restrict results to one inbox |
| `limit` | No | Results per page (clamped by `pagination.ClampLimit`, default 20, max 100) |
| `cursor` | No | Pagination cursor from previous response |
| `mode` | No | `keyword` (default) or `semantic` |

## Keyword Search

Uses PostgreSQL's native `tsvector`/`tsquery` for BM25-style ranked full-text search.

### tsvector Column

Each `messages` row has a `search_vector TSVECTOR` column auto-populated by a database trigger on INSERT/UPDATE using subject, from_address, and from_name:

```sql
NEW.search_vector :=
    setweight(to_tsvector('english', COALESCE(NEW.subject, '')), 'A') ||
    setweight(to_tsvector('english', COALESCE(NEW.from_address, '')), 'B') ||
    setweight(to_tsvector('english', COALESCE(NEW.from_name, '')), 'B');
```

Weight `A` (subject) ranks higher than weight `B` (addresses). A GIN index on `search_vector` makes `@@` queries efficient on large tables.

### Keyword Query

```sql
SELECT
    m.id, m.thread_id, m.inbox_id, m.subject,
    COALESCE(t.snippet, ''),
    m.from_address, m.from_name,
    m.received_at, m.sent_at, m.direction,
    ts_rank(m.search_vector, plainto_tsquery('english', $1)) AS rank
FROM messages m
LEFT JOIN threads t ON t.id = m.thread_id
WHERE m.org_id = $2
  AND m.search_vector @@ plainto_tsquery('english', $1)
  AND ($3::uuid IS NULL OR m.inbox_id = $3::uuid)
ORDER BY rank DESC, m.received_at DESC
LIMIT $N
```

`plainto_tsquery` handles natural language input — stop words are filtered, terms are stemmed, multi-word queries become AND of all terms.

## Semantic Search

Uses `pgvector` HNSW approximate nearest-neighbour search on 256-dim message embeddings.

### How Embeddings Are Generated

The `embedder` service runs as a separate background process:

1. Consumes `message.received` and `message.sent` events from `events.fanout`
2. Downloads the message's plain-text body from S3 (`body_text_key`)
3. Sends the text (truncated to 8 KB) to the embedding server (`EMBEDDER_URL/v1/embeddings`)
4. Receives a 768-dim vector from `nomic-embed-text-v1.5`, truncates to 256 dims (MRL)
5. Writes the vector to `messages.embedding` and stamps `messages.embedded_at`

Messages without a text body are skipped. The `embedded_at` timestamp allows a reconciliation job to find and re-embed missed messages.

### Semantic Query

The search service embeds the query string using the same model, then runs:

```sql
SELECT
    m.id, m.thread_id, m.inbox_id, m.subject,
    COALESCE(t.snippet, ''),
    m.from_address, m.from_name,
    m.received_at, m.sent_at, m.direction,
    1 - (m.embedding <=> $1::vector) AS rank
FROM messages m
LEFT JOIN threads t ON t.id = m.thread_id
WHERE m.org_id = $2
  AND m.embedding IS NOT NULL
  AND ($3::uuid IS NULL OR m.inbox_id = $3::uuid)
ORDER BY m.embedding <=> $1::vector ASC
LIMIT $N
```

`<=>` is the pgvector cosine distance operator. Results are ordered by ascending distance (= descending similarity). `rank` in the response is the cosine similarity (0–1).

### Graceful Degradation

If the embedding server is unreachable or returns an error, the search service automatically falls back to keyword search for that request. No error is returned to the client.

## Pagination

Keyset pagination on `(received_at, id)`. The cursor encodes the last row's `received_at` and `message_id` as `pagination.EncodeCursor(receivedAt, messageID)`. On the next page:

```sql
AND (m.received_at, m.id) < ($cursor_time::timestamptz, $cursor_id::uuid)
```

## Response

```json
{
  "items": [
    {
      "message_id": "uuid",
      "thread_id": "uuid",
      "inbox_id": "uuid",
      "subject": "Invoice overdue",
      "snippet": "Hi, your invoice for...",
      "from": {"email": "billing@acme.com", "name": "Acme Billing"},
      "received_at": "2024-01-15T10:30:00Z",
      "sent_at": null,
      "direction": "inbound",
      "rank": 0.912
    }
  ],
  "next_cursor": "base64...",
  "has_more": true
}
```

`rank` is `ts_rank` score for keyword mode and cosine similarity (0–1) for semantic mode. `has_more` is `true` when `next_cursor` is non-empty.

## Embedding Model

| Property | Value |
|----------|-------|
| Model | `nomic-embed-text-v1.5` |
| Full dimensions | 768 |
| Stored dimensions | 256 (MRL truncation) |
| Context window | 8 192 tokens |
| Inference server | Infinity (`michaelf34/infinity`) or Ollama |
| API format | OpenAI-compatible (`POST /v1/embeddings`) |

256 dims was chosen to balance recall quality and index memory. `nomic-embed-text-v1.5` is trained with Matryoshka Representation Learning (MRL), meaning the first 256 dimensions carry most of the semantic information. At 1M messages the HNSW index is approximately 1 GB in memory.

## Configuration

| Env Var | Default | Description |
|---------|---------|-------------|
| `DATABASE_URL` | `postgres://...` | PostgreSQL connection string (must have pgvector extension) |
| `AUTH_SERVICE_URL` | `http://localhost:8081` | Auth service for API key validation |
| `EMBEDDER_URL` | `http://localhost:7997` | Base URL of the OpenAI-compatible embedding server |
| `EMBEDDER_MODEL` | `nomic-embed-text-v1.5` | Model name passed to the embedding server |
| `API_HOST` | `0.0.0.0` | HTTP bind host |
| `API_PORT` | `8084` | HTTP port |

When `EMBEDDER_URL` is set to an empty string, semantic search is disabled and `?mode=semantic` requests fall back to keyword search silently.
