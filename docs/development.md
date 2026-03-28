# Development Guide

## Prerequisites

- **Go 1.23+** — `go version`
- **Docker + Docker Compose** — `docker compose version`
- **Make** — `make --version`

## Initial Setup

```bash
# 1. Copy environment config
cp .env.example .env
# Edit .env if needed (defaults work for local dev)

# 2. Start infrastructure
make up
# Starts: Postgres 16, Kafka + Zookeeper, Redis 7, MinIO, Mailhog, Nginx
# Waits for Postgres to pass its health check before returning.

# 3. Apply database migrations
make migrate-up
# Runs all migrations in order via tools/migrate

# 4. Verify infrastructure
docker compose ps  # all containers should be healthy
```

Alternatively, run `make setup` to do all of the above in one step (also installs `golangci-lint` and `air`).

## Infrastructure Services

| Service | Local URL | Purpose |
|---------|-----------|---------|
| PostgreSQL 16 + pgvector | `localhost:5432` | Primary database (`agentmail` / `agentmail_dev`); pgvector extension enabled |
| Kafka | `localhost:9092` | Event streaming (Confluent Platform 7.6.0) |
| Zookeeper | `localhost:2181` | Kafka coordination |
| Redis 7 | `localhost:6379` | Cache + WebSocket pub/sub (max 256 MB, allkeys-lru) |
| MinIO | `http://localhost:9000` | S3-compatible object storage |
| MinIO Console | `http://localhost:9001` | Web UI (user: `agentmail_minio` / pass: `agentmail_minio_dev_secret`) |
| Mailhog | `http://localhost:8025` | SMTP test server — catches all outbound email |
| Mailhog SMTP | `localhost:1025` | SMTP submission port |
| Infinity | `http://localhost:7997` | Local embedding server — serves `nomic-embed-text-v1.5` via OpenAI-compatible `/embeddings` API |
| Nginx | `http://localhost:80` | Local reverse proxy |

MinIO pre-creates two buckets on startup (`agentmail-emails`, `agentmail-attachments`). The `agentmail-attachments` bucket is set to public-read so presigned URLs are not required for attachment downloads in dev.

Infinity downloads the `nomic-embed-text-v1.5` model weights (~270 MB) on first startup and caches them in the `infinity_cache` Docker volume. Subsequent restarts load from cache instantly. The health check allows up to 120 s for the first-run download.

## Running Services

### With hot-reload (API service)

```bash
make dev-api
# Uses 'air' for hot-reload; installs air automatically if not found
# Listens on :8080
```

### Individual services (no hot-reload)

```bash
make dev-auth             # auth service      :8081
make dev-inbox            # inbox service     :8082
make dev-email-pipeline   # SMTP listener     :2525
make dev-webhook-service  # webhook service   :8083
make dev-search           # search service    :8084
make dev-embedder         # embedder service  (Kafka consumer, no HTTP port)
make dev-scheduler        # scheduler service  (cron jobs, no HTTP port)
```

Or run directly:
```bash
go run ./services/auth/cmd/auth
go run ./services/inbox/cmd/inbox
go run ./services/api/cmd/api
go run ./services/email-pipeline/cmd/email-pipeline
go run ./services/event-dispatcher/cmd/event-dispatcher
go run ./services/webhook-service/cmd/webhook-service
go run ./services/scheduler/cmd/scheduler
go run ./services/search/cmd/search
go run ./services/embedder/main.go
```

### Building binaries

```bash
make build              # builds all services to ./bin/
make build-api          # builds just the API service  → bin/api
make build-auth         # builds just the auth service → bin/auth
make build-inbox        # → bin/inbox
make build-email-pipeline   # → bin/email-pipeline
make build-event-dispatcher # → bin/event-dispatcher
make build-webhook-service  # → bin/webhook-service
make build-scheduler    # → bin/scheduler
make build-search       # → bin/search
make build-embedder     # → bin/embedder
```

All binaries are built with `-ldflags` that embed `gitCommit` and `buildTime` from the build environment.

## Database Migrations

```bash
# Apply all pending migrations
make migrate-up

# Roll back the most recent migration
make migrate-down

# Check current migration status
make migrate-status

# Create a new migration pair
make migrate-create name=add_webhook_retries
# Creates:
#   migrations/NNNNNN_add_webhook_retries.up.sql
#   migrations/NNNNNN_add_webhook_retries.down.sql

# Roll back ALL migrations (destructive — drops all tables)
make migrate-reset
```

The migration runner is at `tools/migrate` and uses `golang-migrate/migrate/v4`. Migration files live in `migrations/` and are numbered sequentially.

## Make Targets

| Target | Description |
|--------|-------------|
| **Infrastructure** | |
| `make up` | Start all infrastructure services (detached); waits for Postgres health |
| `make down` | Stop all infrastructure services |
| `make down-volumes` | Stop all services and remove volumes (destructive) |
| `make logs` | Tail logs from all infrastructure services |
| `make logs-<service>` | Tail logs for a specific service (e.g. `make logs-postgres`) |
| `make ps` | Show status of all Docker Compose services |
| **Migrations** | |
| `make migrate-up` | Apply all pending migrations |
| `make migrate-down` | Roll back the most recent migration |
| `make migrate-status` | Show current migration version |
| `make migrate-create name=<n>` | Create a new migration pair |
| `make migrate-reset` | Roll back ALL migrations (destructive) |
| **Build** | |
| `make build` | Build all service binaries to `./bin/` |
| `make build-<service>` | Build a single service binary |
| **Tests** | |
| `make test` | Run all tests with race detector |
| `make test-coverage` | Run tests with coverage; writes `coverage.html` |
| `make test-api` | Run tests for the `api` service only |
| `make test-auth` | Run tests for the `auth` service only |
| `make test-inbox` | Run tests for the `inbox` service only |
| `make test-email-pipeline` | Run tests for `email-pipeline` |
| `make test-event-dispatcher` | Run tests for `event-dispatcher` |
| `make test-webhook-service` | Run tests for `webhook-service` |
| `make test-scheduler` | Run tests for `scheduler` |
| `make test-search` | Run tests for `search` |
| `make test-pkg` | Run tests for shared `pkg/` only |
| **Code Quality** | |
| `make lint` | Run `golangci-lint` across all modules |
| `make lint-fix` | Run `golangci-lint` with auto-fix |
| `make fmt` | Format all `.go` files with `gofmt` |
| `make vet` | Run `go vet` across all modules |
| `make generate` | Run `go generate ./...` |
| **Module Management** | |
| `make tidy` | `go work sync` + `go mod tidy` for every module |
| **Docker Images** | |
| `make docker-build` | Build Docker images for all services |
| `make docker-build-<service>` | Build Docker image for one service |
| `make docker-push` | Push all images to `IMAGE_REGISTRY` |
| **Local Dev** | |
| `make dev-api` | Run API with hot-reload via `air` |
| `make dev-auth` | Run auth service locally |
| `make dev-inbox` | Run inbox service locally |
| `make dev-email-pipeline` | Run email-pipeline locally |
| `make dev-webhook-service` | Run webhook-service locally |
| `make dev-scheduler` | Run scheduler locally |
| `make dev-search` | Run search service locally |
| **Setup & Clean** | |
| `make setup` | Full bootstrap: install tools, copy `.env`, start infra, run migrations |
| `make clean` | Remove `bin/`, `dist/`, `coverage.*`, `*.test` files |
| `make clean-docker` | Remove local Docker images for this project |
| `make help` | Show all available targets |

## Go Workspace

The project uses a Go workspace (`go.work`) to link all modules for local development:

```
go.work
├── pkg/                      <- agentmail/pkg (shared)
├── services/api/             <- agentmail/services/api
├── services/auth/            <- agentmail/services/auth
├── services/inbox/           <- agentmail/services/inbox
├── services/email-pipeline/
├── services/event-dispatcher/
├── services/webhook-service/
├── services/scheduler/
├── services/search/
├── services/embedder/
└── tools/migrate/
```

Each service has its own `go.mod`. The workspace lets you edit `pkg/` and immediately see changes in all services without publishing or using `replace` directives.

```bash
# Sync workspace after adding dependencies
go work sync

# Tidy a specific module
cd services/api && go mod tidy

# Tidy all modules at once
make tidy
```

## End-to-End Flow Walkthrough

A complete example: create an org, create an inbox, send an email, and list the thread.

### 1. Bootstrap — Create an Org and API Key

```bash
# Create organization
curl -s -X POST http://localhost:8080/v1/org \
  -H "Content-Type: application/json" \
  -d '{"name":"Test Org","slug":"test-org"}' | jq .

# Create API key with full access
curl -s -X POST http://localhost:8080/v1/keys \
  -H "Authorization: Bearer BOOTSTRAP_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "dev key",
    "scopes": ["org:admin","pod:admin","inbox:read","inbox:write","draft:read","draft:write","search:read","webhook:read","webhook:write"]
  }' | jq .

export KEY="am_live_..."  # copy from the "key" field in the response (shown once)
```

### 2. Create Pod and Inbox

```bash
POD=$(curl -s -X POST http://localhost:8080/v1/pods \
  -H "Authorization: Bearer $KEY" \
  -H "Content-Type: application/json" \
  -d '{"name":"Test Pod","slug":"test-pod"}' | jq -r .id)

INBOX=$(curl -s -X POST http://localhost:8080/v1/inboxes \
  -H "Authorization: Bearer $KEY" \
  -H "Content-Type: application/json" \
  -d "{\"pod_id\":\"$POD\",\"address\":\"agent@localhost\",\"display_name\":\"Test Agent\"}" | jq -r .id)

echo "Inbox ID: $INBOX"
```

### 3. Send Outbound Email

```bash
curl -s -X POST http://localhost:8080/v1/inboxes/$INBOX/messages/send \
  -H "Authorization: Bearer $KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "to": [{"email":"test@example.com","name":"Test"}],
    "subject": "Hello from AgentMail",
    "body_text": "This is a test email.",
    "body_html": "<p>This is a test email.</p>"
  }' | jq .
# Email is caught by Mailhog: http://localhost:8025
```

### 4. Simulate Inbound Email

```bash
# Requires swaks: brew install swaks
swaks --to agent@localhost --from sender@example.com \
  --server localhost:2525 \
  --header "Subject: Inbound test" \
  --body "Hello agent!"

# Alternatively, use telnet or any SMTP client pointing at localhost:2525
```

### 5. List Threads and Messages

```bash
# List threads in the inbox
curl http://localhost:8080/v1/inboxes/$INBOX/threads \
  -H "Authorization: Bearer $KEY" | jq .

# Get messages in a specific thread
THREAD=<thread-id-from-above>
curl http://localhost:8080/v1/inboxes/$INBOX/threads/$THREAD/messages \
  -H "Authorization: Bearer $KEY" | jq .
```

### 6. Create and Approve a Draft

```bash
# Agent creates a draft reply
DRAFT=$(curl -s -X POST http://localhost:8080/v1/inboxes/$INBOX/drafts \
  -H "Authorization: Bearer $KEY" \
  -H "Content-Type: application/json" \
  -d "{
    \"thread_id\": \"$THREAD\",
    \"to\": [{\"email\":\"sender@example.com\"}],
    \"subject\": \"Re: Inbound test\",
    \"body_text\": \"Thanks for reaching out!\"
  }" | jq -r .id)

# Human reviewer approves
curl -s -X POST http://localhost:8080/v1/inboxes/$INBOX/drafts/$DRAFT/approve \
  -H "Authorization: Bearer $KEY" | jq .
```

### 7. WebSocket Real-Time Events

```javascript
// Connect in browser console or Node.js
const ws = new WebSocket(`ws://localhost:8080/v1/ws?token=${KEY}`);
ws.onmessage = (e) => console.log(JSON.parse(e.data));
// Send an email to the inbox — you'll see the message.received event instantly
```

## Smoke Test

A quick end-to-end smoke test that verifies every layer of the stack: auth, send, outbound delivery, inbound SMTP pipeline, storage, embedding, and search.

### 1. Seed the Database

Run the seed script to create a test org, pod, inbox, and API key:

```bash
psql $DATABASE_URL -f /tmp/agentmail_seed.sql
# Or inline:
psql $DATABASE_URL <<'EOF'
DO $$
DECLARE
  v_org_id  UUID := 'aaaaaaaa-0000-0000-0000-000000000001';
  v_pod_id  UUID := 'bbbbbbbb-0000-0000-0000-000000000001';
  v_inbox_id UUID := 'cccccccc-0000-0000-0000-000000000001';
  v_key_id  UUID := 'dddddddd-0000-0000-0000-000000000001';
  v_key_hash TEXT;
BEGIN
  v_key_hash := encode(sha256('am_live_smoketest000000000000000000000000000'::bytea), 'hex');
  INSERT INTO organizations (id, name, slug, plan, settings, created_at, updated_at)
    VALUES (v_org_id, 'Smoke Test Org', 'smoke-test', 'free', '{}', NOW(), NOW())
    ON CONFLICT (id) DO NOTHING;
  INSERT INTO pods (id, org_id, name, slug, settings, created_at, updated_at)
    VALUES (v_pod_id, v_org_id, 'Test Pod', 'test-pod', '{}', NOW(), NOW())
    ON CONFLICT (id) DO NOTHING;
  INSERT INTO inboxes (id, org_id, pod_id, address, display_name, status, settings, created_at, updated_at)
    VALUES (v_inbox_id, v_org_id, v_pod_id, 'agent@localhost', 'Test Agent', 'active', '{}', NOW(), NOW())
    ON CONFLICT (id) DO NOTHING;
  INSERT INTO api_keys (id, org_id, name, key_prefix, key_hash, scopes, created_at)
    VALUES (v_key_id, v_org_id, 'smoke-test-key', 'am_live_smoket', v_key_hash,
      ARRAY['org:admin','pod:admin','inbox:read','inbox:write','draft:read','draft:write','search:read','webhook:read','webhook:write'],
      NOW())
    ON CONFLICT (id) DO NOTHING;
END $$;
EOF
```

Plaintext key: `am_live_smoketest000000000000000000000000000`
Inbox ID: `cccccccc-0000-0000-0000-000000000001`

### 2. Verify Auth

```bash
curl -sf -X POST http://localhost:8081/validate \
  -H "Authorization: Bearer am_live_smoketest000000000000000000000000000" | jq .
# → {"org_id":"aaaaaaaa-...","scopes":["org:admin",...]}
```

### 3. Send Outbound Email

```bash
curl -s -X POST http://localhost:8080/v1/inboxes/cccccccc-0000-0000-0000-000000000001/messages/send \
  -H "Authorization: Bearer am_live_smoketest000000000000000000000000000" \
  -H "Content-Type: application/json" \
  -d '{"to":[{"email":"test@example.com"}],"subject":"Smoke test","body_text":"Hello."}' | jq .status
# → "sending"

# Verify delivery in Mailhog (SMTP_RELAY_HOST=localhost:1025 routes it here)
sleep 3 && curl -s http://localhost:8025/api/v2/messages | jq '.total, .items[0].Content.Headers.Subject'
# → 1, ["Smoke test"]
```

### 4. Inbound SMTP Pipeline

Send an email directly to the inbound SMTP listener. Use a sender domain without a restrictive SPF record (e.g. `.test`, `.local`) — `example.com` has a hard SPF fail that rejects `localhost`.

```bash
python3 -c "
import smtplib
from email.mime.text import MIMEText
msg = MIMEText('Your invoice #1042 for \$450 is overdue by 14 days.')
msg['Subject'] = 'Invoice #1042 overdue'
msg['From'] = 'billing@vendor.test'
msg['To'] = 'agent@localhost'
s = smtplib.SMTP('localhost', 2525)
s.sendmail('billing@vendor.test', ['agent@localhost'], msg.as_string())
s.quit()
"
```

### 5. Verify Storage and Embedding

```bash
sleep 5  # allow pipeline + embedder to process

psql $DATABASE_URL -c "
  SELECT direction, status, subject,
         (body_text_key != '') AS body_in_s3,
         vector_dims(embedding) AS embed_dims,
         embedded_at
  FROM messages ORDER BY created_at DESC LIMIT 4;"
```

Expected output for the inbound message:

```
 direction |  status  |        subject        | body_in_s3 | embed_dims |          embedded_at
-----------+----------+-----------------------+------------+------------+-------------------------------
 inbound   | received | Invoice #1042 overdue | t          |        256 | 2026-03-28 16:49:23.810921+00
```

- `body_in_s3 = t` — plain-text body stored in MinIO under `{org_id}/{pod_id}/{inbox_id}/text/`
- `embed_dims = 256` — MRL-truncated vector from `nomic-embed-text-v1.5` stored in pgvector column
- `embedded_at` — timestamp set by the embedder service after writing the vector

### 6. Keyword and Semantic Search

```bash
# Keyword search
curl -s "http://localhost:8080/v1/search?q=invoice+overdue&inbox_id=cccccccc-0000-0000-0000-000000000001" \
  -H "Authorization: Bearer am_live_smoketest000000000000000000000000000" | jq '[.items[] | {subject, rank}]'

# Semantic search — finds the invoice email even with different wording
curl -s "http://localhost:8080/v1/search?q=payment+past+due&inbox_id=cccccccc-0000-0000-0000-000000000001&mode=semantic" \
  -H "Authorization: Bearer am_live_smoketest000000000000000000000000000" | jq '[.items[] | {subject, rank}]'
# → [{"subject":"Invoice #1042 overdue","rank":0.664...}]
```

The semantic search query "payment past due" retrieves the "Invoice #1042 overdue" email despite having no word overlap — demonstrating that the embedding pipeline is working correctly.

### Verified Pipeline (2026-03-28)

| Layer | Status | Notes |
|-------|--------|-------|
| Auth (`POST /validate`) | ✅ | Returns claims JSON |
| Send (`POST /v1/inboxes/{id}/messages/send`) | ✅ | 201, message stored with `status=sending` |
| Outbound Kafka consumer → Mailhog | ✅ | Delivered via `SMTP_RELAY_HOST=localhost:1025` |
| Inbound SMTP → Kafka → processor | ✅ | SPF/DKIM/DMARC checked; stored in DB |
| Body text stored in S3 (MinIO) | ✅ | `body_text_key` present |
| Raw `.eml` stored in S3 | ✅ | `raw_key` present |
| `events.fanout` → embedder service | ✅ | `message.received` event consumed |
| Infinity `/embeddings` API | ✅ | 768 dims returned, truncated to 256 |
| `messages.embedding` + `embedded_at` | ✅ | `vector_dims = 256` confirmed in pgvector |
| Threads and messages API | ✅ | List, get, nested messages all return correct data |
| Keyword search | ✅ | BM25 rank scores returned |
| Semantic search (`?mode=semantic`) | ✅ | Finds invoice email from "payment past due" query |

## Environment Variables Reference

### Application

| Variable | Default | Description |
|----------|---------|-------------|
| `ENVIRONMENT` | `development` | Runtime environment: `development` \| `staging` \| `production` |
| `LOG_LEVEL` | `debug` | Log verbosity: `debug` \| `info` \| `warn` \| `error` |
| `LOG_FORMAT` | `text` | Log format: `text` (dev) \| `json` (production) |

### Database

| Variable | Default | Description |
|----------|---------|-------------|
| `DATABASE_URL` | `postgres://agentmail:agentmail_dev@localhost:5432/agentmail?sslmode=disable` | PostgreSQL connection string |
| `DB_MAX_CONNS` | `25` | Maximum open connections in pool |
| `DB_MIN_CONNS` | `5` | Minimum idle connections in pool |
| `DB_MAX_CONN_LIFETIME_SEC` | `3600` | Max lifetime of a pooled connection (seconds) |
| `DB_MAX_CONN_IDLE_SEC` | `300` | Max idle time before connection is closed (seconds) |

### Kafka

| Variable | Default | Description |
|----------|---------|-------------|
| `KAFKA_BROKERS` | `localhost:9092` | Comma-separated broker list |
| `KAFKA_GROUP_ID` | `agentmail` | Consumer group ID |

Topic names are hardcoded in `pkg/kafka/topics.go` and are not configurable via environment variables.

### Redis

| Variable | Default | Description |
|----------|---------|-------------|
| `REDIS_URL` | `redis://localhost:6379/0` | Redis connection URL |

### S3 / MinIO

| Variable | Default | Description |
|----------|---------|-------------|
| `S3_ENDPOINT` | `http://localhost:9000` | Leave empty for AWS S3; set for MinIO |
| `S3_REGION` | `us-east-1` | S3 region |
| `S3_BUCKET` | `agentmail` | Bucket for email bodies and attachments |
| `S3_ACCESS_KEY_ID` | `agentmail_minio` | Access key (MinIO root user in dev) |
| `S3_SECRET_ACCESS_KEY` | `agentmail_minio_dev_secret` | Secret key |
| `S3_USE_PATH_STYLE` | `true` | Set `true` for MinIO; `false` for AWS S3 |

### Service Ports & URLs

| Variable | Default | Description |
|----------|---------|---------|
| `API_HOST` | `0.0.0.0` | API service bind host |
| `API_PORT` | `8080` | API service port |
| `AUTH_SERVICE_URL` | `http://localhost:8081` | Auth service base URL (used by API gateway) |
| `INBOX_SERVICE_URL` | `http://localhost:8082` | Inbox service base URL (used by API gateway) |
| `WEBHOOK_SERVICE_URL` | `http://localhost:8083` | Webhook service base URL (used by API gateway) |
| `WEBHOOK_PORT` | `8083` | Webhook service HTTP listen port |
| `SEARCH_SERVICE_URL` | `http://localhost:8084` | Search service base URL (used by API gateway) |
| `AUTH_SERVICE_PORT` | `8081` | Auth service listen port (hardcoded; env var is informational) |

Auth (port 8081), inbox (port 8082), and search (port 8084) services listen on hardcoded ports — their port is not configurable via environment variables.

### SMTP

| Variable | Default | Description |
|----------|---------|-------------|
| `SMTP_LISTEN_ADDR` | `:2525` | Inbound SMTP listener address |
| `SMTP_HOSTNAME` | `localhost` | Domain claimed in SMTP EHLO |
| `SMTP_RELAY_HOST` | _(empty)_ | When set, all outbound mail is delivered to this `host:port` instead of doing live MX lookups. Set to `localhost:1025` in dev to route through Mailhog. Leave empty in production for real MX delivery. |
| `DKIM_PRIVATE_KEY_PEM` | _(empty)_ | PEM-encoded RSA/Ed25519 private key for DKIM signing (leave empty to disable) |
| `DKIM_SELECTOR` | `agentmail1` | DNS selector label for DKIM |
| `DKIM_DOMAIN` | _(empty)_ | Signing domain; must match the `From` domain |

### Auth

| Variable | Default | Description |
|----------|---------|-------------|
| `INTERNAL_SECRET` | `dev-internal-secret-change-in-prod` | HMAC key for inter-service calls |

### Webhook Service

| Variable | Default | Description |
|----------|---------|-------------|
| `WEBHOOK_PORT` | `8083` | Webhook service HTTP port |
| `WEBHOOK_CONCURRENCY` | `10` | Concurrent delivery worker goroutines |
| `WEBHOOK_MAX_RETRIES` | `8` | Maximum delivery retry attempts (backoff: `2^attempt` seconds, capped at 4096s) |
| `WEBHOOK_ENCRYPTION_KEY` | _(empty)_ | 64-char hex AES-256 key for encrypting caller-supplied auth header values at rest — required in production |

### Embedder Service

| Variable | Default | Description |
|----------|---------|-------------|
| `EMBEDDER_URL` | `http://localhost:7997` | Base URL of the OpenAI-compatible embedding server (Infinity or Ollama) |
| `EMBEDDER_MODEL` | `nomic-embed-text-v1.5` | Model name passed to the embedding server |

### Observability

Tracing is enabled when `OTEL_ENDPOINT` is non-empty.

| Variable | Default | Description |
|----------|---------|-------------|
| `OTEL_ENDPOINT` | _(empty)_ | OTLP HTTP endpoint (Jaeger, Tempo, etc.); tracing disabled when empty |
| `OTEL_SERVICE_NAME` | `agentmail` | Service name reported to the collector |
