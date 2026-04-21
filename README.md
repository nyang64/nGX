# nGX

**Self-hosted email infrastructure for AI agents.** Deploy on your own domain, your own AWS account. Your agents get real email addresses at `agent@yourdomain.com` — not `agent@somevendor.to`.

## Why nGX

Hosted email-for-agents services give your AI agents email addresses on a vendor's domain. You depend on their uptime, their pricing, and their data policies. nGX is the alternative: a complete email platform you deploy into your own AWS account, on your own domain.

- **You own the domain** — configure `MAIL_DOMAIN=mail.yourdomain.com` and every inbox provisions under your domain
- **You own the data** — email bodies, attachments, and thread history stay in your Aurora and S3
- **You own the keys** — DKIM signing uses SES, SPF and DMARC point to your DNS
- **No per-seat subscription** — deploy for as many agents as your AWS account supports
- **Regulatory compliance** — email content never leaves your AWS account; meet HIPAA, GDPR, SOC 2, and industry-specific data residency requirements without negotiating a vendor BAA or DPA

nGX is designed for enterprises that want the capabilities of a managed email platform without the vendor lock-in or the compliance risk of routing sensitive communications through a third-party SaaS.

## Features

- **Programmable inboxes** — create/manage email addresses via REST API; supply just a username and nGX appends your configured domain
- **Full email pipeline** — SES inbound reception with MIME parsing; SES outbound delivery with DKIM signing and retry
- **Thread management** — automatic conversation threading via In-Reply-To/References headers
- **Real-time events** — WebSocket stream and webhooks with HMAC-SHA256 signatures (`X-nGX-Signature`)
- **Draft / human-in-the-loop** — agents create drafts, humans approve/reject before sending
- **Custom domains** — provision per-org custom email domains with automatic SES receipt rules
- **Multi-tenancy** — Org → Pod → Inbox hierarchy with strict data isolation via PostgreSQL RLS
- **API key RBAC** — fine-grained scopes (inbox:read, inbox:write, draft:write, org:admin, ...)
- **Label system** — tag and filter threads
- **Full-text search** — PostgreSQL tsvector search across subjects and bodies

## Architecture

nGX runs entirely on AWS serverless infrastructure — no servers to manage, no Kafka or Redis clusters to operate.

```
                    ┌─────────────────────────────────────┐
                    │           CLIENT LAYER              │
                    │  REST API  │  WebSocket  │  Email   │
                    └──────┬──────────┬────────────┬──────┘
                           │          │            │
                    ┌──────▼──────────▼──┐    ┌────▼────────────┐
                    │  API Gateway       │    │  Amazon SES     │
                    │  REST + WebSocket  │    │  Inbound rules  │
                    └──────┬─────────────┘    └────┬────────────┘
                           │                       │
          ┌────────────────┼───────────────────────┤
          │                │                       │
   ┌──────▼──────┐  ┌──────▼──────┐   ┌────────────▼──────────┐
   │  Authorizer │  │  REST       │   │  SQS Queues           │
   │  Lambda     │  │  Lambdas    │   │  email_inbound        │
   │             │  │  inboxes    │   │  email_outbound       │
   └─────────────┘  │  messages   │   │  webhook_delivery     │
                    │  threads    │   │  ws_dispatch          │
                    │  drafts     │   │  ses_events           │
                    │  domains    │   │  embedder             │
                    │  orgs/keys  │   └────────────┬──────────┘
                    │  search     │                │
                    └─────────────┘    ┌───────────▼───────────┐
                                       │  Worker Lambdas       │
                                       │  email_inbound        │
                                       │  email_outbound       │
                                       │  event_dispatcher_ws  │
                                       │  event_dispatcher_wh  │
                                       │  scheduler_drafts     │
                                       │  ses_events           │
                                       └───────────────────────┘

  Data Stores (managed AWS services)
  ┌─────────────────┐  ┌──────────────┐  ┌─────────────────────┐
  │ Aurora          │  │  S3 Buckets  │  │  Secrets Manager    │
  │ PostgreSQL+RLS  │  │  emails      │  │  DB credentials     │
  │ (via RDS Proxy) │  │  attachments │  │  webhook keys       │
  └─────────────────┘  └──────────────┘  └─────────────────────┘
```

### Lambda Functions

| Lambda | Trigger | Responsibility |
|--------|---------|----------------|
| `authorizer` | API GW | API key validation for all REST routes |
| `auth` | REST | API key lifecycle (create/list/delete) |
| `orgs` | REST | Org management |
| `inboxes` | REST | Inbox/pod CRUD, custom domain management |
| `messages` | REST | Message send, list, get |
| `threads` | REST | Thread list, get, label |
| `drafts` | REST | Draft create/list/approve/reject |
| `domains` | REST | Custom domain CRUD + SES verification |
| `search` | REST | Full-text search across messages |
| `webhooks` | REST | Webhook endpoint CRUD |
| `ws_connect` | WebSocket | WebSocket $connect handler |
| `ws_disconnect` | WebSocket | WebSocket $disconnect handler |
| `email_inbound` | SQS | Parse SES inbound → store message, update thread |
| `email_outbound` | SQS | Send via SES API v2, update message status |
| `event_dispatcher_ws` | SQS | Fan out events → WebSocket connections |
| `event_dispatcher_webhook` | SQS | Fan out events → webhook delivery queue |
| `ses_events` | SQS | Handle SES bounce/complaint notifications |
| `scheduler_drafts` | EventBridge | Process scheduled drafts |
| `embedder` | SQS | Generate vector embeddings for semantic search |

### Tenancy Model

```
Organization  (billing root, holds API keys)
  └── Pod     (isolated namespace — maps to a sub-customer or product)
        └── Inbox  (email address: agent@yourdomain.com)
              └── Thread  (conversation, grouped by In-Reply-To/References)
                    └── Message  (individual email, inbound or outbound)
                          └── Attachment
```

Every table carries `org_id` and PostgreSQL Row-Level Security enforces tenant isolation at the database layer — it is impossible for one org's query to read another's data.

## Deployment

### Prerequisites

- Go 1.23+
- AWS CLI with a profile that has IAM, Lambda, SES, API GW, Aurora, SQS, S3, and Secrets Manager permissions
- Terraform 1.5+
- `jq`
- Your own domain with DNS control (SES sandbox must be lifted for production use)

### 1. Configure environment

```bash
cp .env.example .env
```

Edit `.env` and fill in the `TF_VAR_*` values:

```bash
TF_VAR_app_name=ngx
TF_VAR_environment=prod
TF_VAR_aws_region=us-east-1
TF_VAR_mail_domain=mail.yourdomain.com   # subdomain you'll delegate to SES
TF_VAR_db_name=ngx
TF_VAR_db_username=ngxadmin
TF_VAR_webhook_encryption_key=$(openssl rand -hex 32)
```

### 2. Build Lambda binaries

```bash
make lambdas   # builds all lambdas to lambdas/bin/<name>/bootstrap (arm64, provided.al2023)
```

### 3. Deploy infrastructure

```bash
source loadenv.sh
AWS_PROFILE=<your-profile> terraform -chdir=terraform init
AWS_PROFILE=<your-profile> terraform -chdir=terraform apply
```

After apply, generate `.env.outputs` with all post-deploy values (endpoints, queue URLs, DB URL):

```bash
scripts/sync-env.sh --profile <your-profile>
source loadenv.sh   # picks up both .env and .env.outputs
```

### 4. Run database migrations

Connect via SSM tunnel to the Aurora cluster (bastion instance is in `.env`):

```bash
# Start SSM tunnel (see .env comment for exact command — tunnels Aurora to localhost:15432)
# Then run migrations:
DATABASE_URL="postgres://ngxadmin:<password>@127.0.0.1:15432/ngx?sslmode=verify-ca&sslrootcert=/usr/local/etc/openssl@3/cert.pem" \
  go run ./tools/migrate up
```

### 5. Configure SES DNS

After first deploy, Terraform prints `post_deploy_instructions` with the exact DNS records required:

- **MX record**: `mail.yourdomain.com` → SES inbound SMTP endpoint
- **SPF TXT**: `v=spf1 include:amazonses.com ~all`
- **DKIM CNAME records**: 3 CNAME records provided by SES domain verification
- **DMARC TXT**: `v=DMARC1; p=quarantine; rua=mailto:...`

Check SES verification status:

```bash
aws ses get-identity-verification-attributes \
  --profile <your-profile> --region us-east-1 \
  --identities mail.yourdomain.com
```

### 6. Bootstrap initial org

```bash
# DATABASE_URL must point at Aurora (via SSM tunnel or Lambda invoke)
go run ./tools/bootstrap org="My Org" slug="my-org"
export API_KEY=am_live_xxxx
```

### 7. Verify the deployment

```bash
source loadenv.sh
# REST_API_ENDPOINT is populated in .env.outputs by scripts/sync-env.sh

curl ${REST_API_ENDPOINT}/v1/org \
  -H "Authorization: Bearer ${API_KEY}"
```

## Usage

### Create a pod and inbox

```bash
# Create a pod
curl -X POST ${REST_API_ENDPOINT}/v1/pods \
  -H "Authorization: Bearer ${API_KEY}" \
  -H "Content-Type: application/json" \
  -d '{"name":"My Product","slug":"my-product"}'

# Provision an inbox — with MAIL_DOMAIN set, just supply the username
curl -X POST ${REST_API_ENDPOINT}/v1/inboxes \
  -H "Authorization: Bearer ${API_KEY}" \
  -H "Content-Type: application/json" \
  -d '{"pod_id":"<pod-id>","address":"agent"}'
# → inbox.email will be "agent@mail.yourdomain.com"
```

### Send and receive email

```bash
# Send outbound
curl -X POST ${REST_API_ENDPOINT}/v1/inboxes/<inbox-id>/messages/send \
  -H "Authorization: Bearer ${API_KEY}" \
  -H "Content-Type: application/json" \
  -d '{"to":[{"email":"test@example.com"}],"subject":"Hello","body_text":"Hi there"}'

# List threads
curl ${REST_API_ENDPOINT}/v1/inboxes/<inbox-id>/threads \
  -H "Authorization: Bearer ${API_KEY}"

# Get messages in a thread
curl ${REST_API_ENDPOINT}/v1/inboxes/<inbox-id>/threads/<thread-id>/messages \
  -H "Authorization: Bearer ${API_KEY}"
```

### Custom domains

```bash
# Add a custom domain for an org
curl -X POST ${REST_API_ENDPOINT}/v1/domains \
  -H "Authorization: Bearer ${API_KEY}" \
  -H "Content-Type: application/json" \
  -d '{"domain":"mail.mycustomer.com"}'

# Get verification DNS records to add
curl ${REST_API_ENDPOINT}/v1/domains/<domain-id> \
  -H "Authorization: Bearer ${API_KEY}"

# Trigger SES verification check after DNS records are added
curl -X POST ${REST_API_ENDPOINT}/v1/domains/<domain-id>/verify \
  -H "Authorization: Bearer ${API_KEY}"
```

## Environment Variables

nGX uses a two-file environment model:

| File | Contents | Managed by |
|------|----------|-----------|
| `.env` | Pre-deploy inputs: `TF_VAR_*`, app settings | You (copy from `.env.example`) |
| `.env.outputs` | Post-deploy outputs: endpoints, queue URLs, DB URL | `scripts/sync-env.sh` (auto-generated after `terraform apply`) |

Run `source loadenv.sh` to load both files. Re-run `scripts/sync-env.sh` after every `terraform apply`.

See `.env.example` for the full variable reference.

## Integration Tests

Tests run against the live deployed AWS stack:

```bash
source loadenv.sh
go test ./tests/integration/... -v -timeout 120s
```

> **SES sandbox**: Tests use `success@simulator.amazonses.com` for outbound mail so they work in SES sandbox mode.

## Project Structure

```
nGX/
├── lambdas/              # Lambda function source code
│   ├── <function>/       #   One directory per Lambda
│   └── shared/           #   Shared helpers (auth, response, DB, ...)
├── migrations/           # SQL migration pairs (up + down)
├── pkg/                  # Shared Go packages
│   ├── auth/             #   API key generation, Claims, RBAC
│   ├── config/           #   Env-based config loading
│   ├── db/               #   pgxpool, transactions, RLS injection
│   ├── models/           #   Domain structs
│   ├── s3/               #   S3 client
│   └── telemetry/        #   slog + OpenTelemetry setup
├── scripts/
│   └── sync-env.sh       # Generate .env.outputs from terraform + Secrets Manager
├── terraform/            # AWS infrastructure (IaC)
│   ├── main.tf           #   Core resources (VPC, Aurora, SES, ...)
│   ├── lambdas.tf        #   Lambda function definitions
│   ├── api_gateway.tf    #   REST + WebSocket API GW
│   ├── sqs.tf            #   SQS queues
│   ├── iam.tf            #   Lambda execution role + policies
│   └── outputs.tf        #   Exported values
├── tests/
│   └── integration/      # Integration tests (run against deployed AWS)
├── tools/
│   ├── bootstrap/        # Org + API key bootstrapper
│   └── migrate/          # Migration runner CLI
├── .env.example          # Environment variable reference
├── loadenv.sh            # Sources .env + .env.outputs
└── scripts/sync-env.sh   # Generates .env.outputs from terraform outputs
```

## Built With

**AWS Services**

| Service | Role |
|---------|------|
| [API Gateway](https://aws.amazon.com/api-gateway/) | REST + WebSocket entry point |
| [Lambda](https://aws.amazon.com/lambda/) | All business logic (Go, arm64, provided.al2023) |
| [SES v2](https://aws.amazon.com/ses/) | Inbound receipt + outbound delivery + DKIM |
| [Aurora PostgreSQL Serverless v2](https://aws.amazon.com/rds/aurora/serverless/) | Primary data store with RLS |
| [RDS Proxy](https://aws.amazon.com/rds/proxy/) | Connection pooling for Lambdas |
| [SQS](https://aws.amazon.com/sqs/) | Async job queues (inbound, outbound, webhooks, WS, embedder) |
| [S3](https://aws.amazon.com/s3/) | Email + attachment storage |
| [Secrets Manager](https://aws.amazon.com/secrets-manager/) | DB credentials |
| [SSM](https://aws.amazon.com/systems-manager/) | Bastion access for DB tunneling |

**Go Libraries**

| Library | Purpose |
|---------|---------|
| [aws/aws-sdk-go-v2](https://github.com/aws/aws-sdk-go-v2) | AWS service clients |
| [jackc/pgx](https://github.com/jackc/pgx) | PostgreSQL driver |
| [golang-migrate/migrate](https://github.com/golang-migrate/migrate) | Database migrations |
| [oklog/ulid](https://github.com/oklog/ulid) | ULID generation |
| [google/uuid](https://github.com/google/uuid) | UUID generation |
| [go.opentelemetry.io/otel](https://opentelemetry.io) | Distributed tracing + metrics |

---

## License

nGX is source-available software licensed under the [nGX Commercial Source License](LICENSE).

- **Free** for non-commercial use, evaluation, and internal assessment (up to 90 days)
- **Commercial license required** for production or business use — contact [licensing@nyklabs.com](mailto:licensing@nyklabs.com)
- **Managed service rights reserved** — only nyklabs.com may offer nGX as a hosted or cloud service

Copyright (c) 2026 [nyklabs.com](https://nyklabs.com). All rights reserved.
