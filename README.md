# nGX

**Self-hosted email infrastructure for AI agents.** Deploy on your own domain, your own servers. Your agents get real email addresses at `agent@mail.yourdomain.com` — not `agent@somevendor.to`.

## Why nGX

Hosted email-for-agents services give your AI agents email addresses on a vendor's domain. You depend on their uptime, their pricing, and their data policies. nGX is the alternative: a complete email platform you install in your own infrastructure, on your own domain.

- **You own the domain** — configure `MAIL_DOMAIN=mail.yourdomain.com` and every inbox provisions under your domain
- **You own the data** — email bodies, attachments, and thread history stay in your PostgreSQL and S3
- **You own the keys** — DKIM signing uses your private keys; SPF and DMARC point to your DNS
- **No per-seat subscription** — deploy for as many agents as your hardware supports
- **Regulatory compliance** — email content never leaves your environment; meet HIPAA, GDPR, SOC 2, and industry-specific data residency requirements without negotiating a vendor BAA or DPA

nGX is designed for enterprises that want the capabilities of a managed email platform without the vendor lock-in or the compliance risk of routing sensitive communications through a third-party SaaS.

## Features

- **Programmable inboxes** — create/manage email addresses via REST API; supply just a username and nGX appends your configured domain
- **Full SMTP pipeline** — inbound reception with MIME parsing; outbound MX delivery with DKIM signing and retry
- **Thread management** — automatic conversation threading via In-Reply-To/References headers
- **Real-time events** — WebSocket stream and webhooks with HMAC-SHA256 signatures (`X-nGX-Signature`)
- **Draft / human-in-the-loop** — agents create drafts, humans approve/reject before sending
- **Multi-tenancy** — Org → Pod → Inbox hierarchy with strict data isolation via PostgreSQL RLS
- **API key RBAC** — fine-grained scopes (inbox:read, inbox:write, draft:write, org:admin, ...)
- **Label system** — tag and filter threads
- **Full-text search** — PostgreSQL tsvector search across subjects and bodies
- **Event-driven** — Kafka backbone connects all services; emit events to your own consumers

## Architecture

```
                    ┌─────────────────────────────────────┐
                    │           CLIENT LAYER              │
                    │  REST API  │  WebSocket  │  SMTP    │
                    └─────────────────┬───────────────────┘
                                      │
                    ┌────────-─────────▼───────────────────┐
                    │         API GATEWAY :8080            │
                    │  Auth · Rate Limit · CORS · Routing  │
                    └──┬──────────┬──────────┬────-────────┘
                       │          │          │
          ┌──────────-─▼─-┐  ┌────▼───┐  ┌──▼────────-──┐
          │  Auth :8081   │  │ Inbox  │  │  Search      │
          │  API Key CRUD │  │ :8082  │  │  :8084       │
          └───────────────┘  └────┬───┘  └────────────-─┘
                                  │
          ┌───────────────────────▼───────────-───────────┐
          │                  KAFKA                        │
          │  email.inbound.raw · email.outbound.queue     │
          │  events.fanout · webhooks.delivery            │
          └──────┬────────────────────┬─────────────────-─┘
                 │                    │
    ┌────────────▼──────┐   ┌─────────▼──────────┐
    │  Email Pipeline   │   │  Event Dispatcher  │
    │  SMTP :2525       │   │  (Kafka consumer)  │
    │  Inbound + MX out │   └───────┬────────────┘
    └───────────────────┘           │
                            ┌───────┴──────────────┐
                            │                      │
               ┌────────────▼──────┐  ┌────────────▼─────-─┐
               │ Webhook Service   │  │  Redis Pub/Sub     │
               │ HTTP delivery     │  │  (WS hub)          │
               └───────────────────┘  └────────────────-───┘

  Data Stores (all self-hosted)
  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐
  │ Postgres │  │  Redis   │  │   S3/    │  │  Kafka   │
  │ +RLS     │  │ Cache    │  │  MinIO   │  │  Topics  │
  └──────────┘  └──────────┘  └──────────┘  └──────────┘
```

## Tenancy Model

```
Organization  (billing root, holds API keys)
  └── Pod     (isolated namespace — maps to a sub-customer or product)
        └── Inbox  (email address: agent@mail.yourdomain.com)
              └── Thread  (conversation, grouped by In-Reply-To/References)
                    └── Message  (individual email, inbound or outbound)
                          └── Attachment
```

Every table carries `org_id` and PostgreSQL Row-Level Security enforces tenant isolation at the database layer — it is impossible for one org's query to read another's data.

## Services

| Service | Port | Responsibility |
|---------|------|----------------|
| api | 8080 | REST + WebSocket gateway, auth, rate limiting |
| auth | 8081 | API key lifecycle, RBAC validation |
| inbox | 8082 | Inbox/thread/message/draft/label business logic |
| email-pipeline | :2525 (SMTP) | Inbound SMTP reception; outbound MX delivery |
| event-dispatcher | — | Kafka -> Redis pub/sub + webhook fanout |
| webhook-service | 8083 | HTTP delivery with HMAC signatures + retry |
| scheduler | — | Bounce check, draft expiry cron jobs |
| embedder | — | Kafka consumer; generates vector embeddings for messages via OpenAI-compatible server |
| search | 8084 | PostgreSQL full-text search |

## Quick Start

### Prerequisites
- Go 1.23+
- Docker + Docker Compose
- Make
- Your own domain with DNS control (for production deployments)

### 1. Configure your domain

Before starting, set your mail domain. This is what all inboxes will be provisioned under:

```bash
cp .env.example .env
# Edit .env and set:
#   MAIL_DOMAIN=mail.yourdomain.com
#   DKIM_DOMAIN=mail.yourdomain.com
```

For local development you can leave `MAIL_DOMAIN` empty and supply full addresses (e.g. `agent@localhost`) when creating inboxes.

### 2. Bootstrap local environment

```bash
make setup       # installs tools, copies .env.example, starts infra, runs migrations
```

Or step by step:

```bash
make up          # starts Postgres, Kafka, Redis, MinIO, Mailhog
make migrate-up  # applies all database migrations
```

### 3. Start services

```bash
# Terminal 1 — core services
go run ./services/auth/cmd/auth       &  # :8081
go run ./services/inbox/cmd/inbox     &  # :8082
go run ./services/api/cmd/api            # :8080

# Terminal 2 — pipeline + events
go run ./services/email-pipeline/cmd/email-pipeline    &
go run ./services/event-dispatcher/cmd/event-dispatcher &
go run ./services/webhook-service/cmd/webhook-service  &
go run ./services/scheduler/cmd/scheduler              &
go run ./services/embedder                             &
go run ./services/search/cmd/search                      # :8084
```

Or use the Makefile hot-reload target for the API:

```bash
make dev-api
```

### 4. Create an org and API key

```bash
# Create organization
curl -X POST http://localhost:8080/v1/org \
  -H "Content-Type: application/json" \
  -d '{"name":"Acme Corp","slug":"acme"}'

# Create API key (save the returned "key" value — shown once)
curl -X POST http://localhost:8080/v1/keys \
  -H "Authorization: Bearer am_live_BOOTSTRAP_KEY" \
  -H "Content-Type: application/json" \
  -d '{"name":"dev key","scopes":["org:admin","inbox:read","inbox:write","draft:write"]}'

export KEY=am_live_xxxx   # from response

# Create a pod and inbox
curl -X POST http://localhost:8080/v1/pods \
  -H "Authorization: Bearer $KEY" \
  -d '{"name":"My Product","slug":"my-product"}'

# Provision an inbox — with MAIL_DOMAIN set, just supply the username
curl -X POST http://localhost:8080/v1/inboxes \
  -H "Authorization: Bearer $KEY" \
  -d '{"pod_id":"<pod-id>","address":"agent"}'
# → inbox.email will be "agent@mail.yourdomain.com"
```

### 5. Send and receive email

```bash
# Send outbound
curl -X POST http://localhost:8080/v1/inboxes/<inbox-id>/messages/send \
  -H "Authorization: Bearer $KEY" \
  -d '{"to":[{"email":"test@example.com"}],"subject":"Hello","body_text":"Hi there"}'

# List threads
curl http://localhost:8080/v1/inboxes/<inbox-id>/threads \
  -H "Authorization: Bearer $KEY"
```

## Production Deployment

For a production self-hosted deployment you need:

1. **DNS records on your domain**
   - MX record: `mail.yourdomain.com` → your server IP
   - SPF TXT: `v=spf1 ip4:<your-ip> -all`
   - DKIM TXT: publish the public key matching your `DKIM_PRIVATE_KEY`
   - DMARC TXT: `v=DMARC1; p=quarantine; rua=mailto:dmarc@yourdomain.com`

2. **Environment variables** (see `.env.example` for full reference)
   ```
   MAIL_DOMAIN=mail.yourdomain.com
   DKIM_DOMAIN=mail.yourdomain.com
   DKIM_SELECTOR=mail
   DKIM_PRIVATE_KEY=<base64-encoded RSA private key>
   DATABASE_URL=postgres://...
   ```

3. **Port exposure**: open port 25 (or 2525) for inbound SMTP; expose port 8080 behind your load balancer for the REST API.

## Project Structure

```
nGX/
├── api/                  # OpenAPI 3.1 specification
├── configs/              # Postgres init, Kafka topics, Nginx
├── docs/                 # This documentation
├── migrations/           # SQL migration pairs (up + down)
├── pkg/                  # Shared Go packages
│   ├── auth/             #   API key generation, Claims, RBAC
│   ├── config/           #   Env-based config loading
│   ├── db/               #   pgxpool, transactions, RLS injection
│   ├── events/           #   Event types and Kafka envelope
│   ├── kafka/            #   Producer and consumer wrappers
│   ├── middleware/        #   HTTP middleware (auth, rate limit, ...)
│   ├── mime/             #   RFC 5322 MIME parser
│   ├── models/           #   Domain structs
│   ├── redis/            #   Client + key helpers
│   ├── s3/               #   S3/MinIO client
│   └── telemetry/        #   slog + OpenTelemetry setup
├── services/
│   ├── api/              # REST + WebSocket gateway
│   ├── auth/             # API key management
│   ├── email-pipeline/   # SMTP inbound + outbound
│   ├── event-dispatcher/ # Event fan-out
│   ├── inbox/            # Core inbox business logic
│   ├── scheduler/        # Background jobs
│   ├── search/           # Full-text search
│   └── webhook-service/  # Webhook HTTP delivery
└── tools/
    └── migrate/          # Migration runner CLI
```

## Development

Common make targets:

| Target | Description |
|--------|-------------|
| `make setup` | Bootstrap local dev environment (tools + infra + migrations) |
| `make up` | Start all Docker infrastructure |
| `make down` | Stop infrastructure |
| `make down-volumes` | Stop infrastructure and remove volumes (destructive) |
| `make migrate-up` | Apply pending migrations |
| `make migrate-down` | Rollback one migration |
| `make migrate-create name=X` | Create a new migration file pair |
| `make migrate-reset` | Rollback all migrations (destructive) |
| `make build` | Build all service binaries into bin/ |
| `make test` | Run all tests with race detector |
| `make test-coverage` | Run tests and generate HTML coverage report |
| `make lint` | Run golangci-lint |
| `make fmt` | Format all Go source files |
| `make tidy` | Sync go.work and tidy all modules |
| `make dev-api` | Run API service with hot-reload (air) |

## Documentation

| Document | Description |
|----------|-------------|
| [Architecture](docs/architecture.md) | System design, data flows, design decisions |
| [Data Model](docs/data-model.md) | Database schema, RLS, pagination |
| [Shared Packages](docs/shared-packages.md) | pkg/ reference |
| [Events](docs/events.md) | Kafka topics, event types, webhooks/WebSocket |
| [Development](docs/development.md) | Local dev guide, env vars |
| [Services](docs/services/) | Per-service deep dives |

## Built With

**Infrastructure**

| Component | Project |
|-----------|---------|
| Relational database + RLS + vector search | [PostgreSQL](https://www.postgresql.org) + [pgvector](https://github.com/pgvector/pgvector) |
| Message streaming | [Apache Kafka](https://kafka.apache.org) (Confluent Platform) |
| Cache + pub/sub | [Redis](https://redis.io) |
| Object storage (S3-compatible) | [MinIO](https://min.io) |
| Local SMTP sink + web UI | [MailHog](https://github.com/mailhog/MailHog) |
| Local embedding server | [Infinity](https://github.com/michaelfeil/infinity) (nomic-embed-text-v1.5) |
| Reverse proxy | [Nginx](https://nginx.org) |

**Go Libraries**

| Library | Purpose |
|---------|---------|
| [go-chi/chi](https://github.com/go-chi/chi) | HTTP router |
| [go-chi/cors](https://github.com/go-chi/cors) | CORS middleware |
| [gorilla/websocket](https://github.com/gorilla/websocket) | WebSocket support |
| [jackc/pgx](https://github.com/jackc/pgx) | PostgreSQL driver |
| [redis/go-redis](https://github.com/redis/go-redis) | Redis client |
| [segmentio/kafka-go](https://github.com/segmentio/kafka-go) | Kafka producer/consumer |
| [emersion/go-smtp](https://github.com/emersion/go-smtp) | SMTP server library |
| [emersion/go-msgauth](https://github.com/emersion/go-msgauth) | DKIM/SPF authentication |
| [aws/aws-sdk-go-v2](https://github.com/aws/aws-sdk-go-v2) | S3/MinIO client |
| [golang-migrate/migrate](https://github.com/golang-migrate/migrate) | Database migrations |
| [oklog/ulid](https://github.com/oklog/ulid) | ULID generation |
| [google/uuid](https://github.com/google/uuid) | UUID generation |
| [go-playground/validator](https://github.com/go-playground/validator) | Request validation |
| [go.opentelemetry.io/otel](https://opentelemetry.io) | Distributed tracing + metrics |
| [blitiri.com.ar/go/spf](https://pkg.go.dev/blitiri.com.ar/go/spf) | SPF record validation |

**Tooling**

| Tool | Purpose |
|------|---------|
| [air](https://github.com/air-verse/air) | Hot-reload for local development |
| [golangci-lint](https://golangci-lint.run) | Go linter |

---

## License

nGX is source-available software licensed under the [nGX Commercial Source License](LICENSE).

- **Free** for non-commercial use, evaluation, and internal assessment (up to 90 days)
- **Commercial license required** for production or business use — contact [licensing@nyklabs.com](mailto:licensing@nyklabs.com)
- **Managed service rights reserved** — only nyklabs.com may offer nGX as a hosted or cloud service

Copyright (c) 2026 [nyklabs.com](https://nyklabs.com). All rights reserved.
