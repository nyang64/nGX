# API Gateway

**Module**: `nGX/services/api`
**Port**: `8080`
**Role**: External-facing REST API and WebSocket hub. Authenticates all requests, enforces RBAC, and proxies to internal services.

## Responsibilities

- API key validation (delegates to Auth Service via POST /validate)
- RBAC scope enforcement per route
- HTTP proxy to Inbox Service and Auth Service
- WebSocket hub with Redis pub/sub backing
- Direct Org/Pod management (OrgStore talks to Postgres directly)

## Middleware Chain

Every request passes through this chain in order:

```
Incoming request
  → RealIP         (chi middleware: sets RemoteAddr from X-Forwarded-For)
  → RequestID      (assigns/propagates X-Request-ID)
  → Logger         (structured slog: method, path, status, duration_ms)
  → Recover        (panic → 500, logs panic value and path)
  → authMiddleware (Bearer token → POST /validate on Auth Service → Claims in ctx)
  → requireScope   (per-route scope check via Claims.HasScope)
  → Handler
```

`authMiddleware` is applied at the `/v1` router level. `requireScope` is applied per-route or per-sub-router. Public routes (`/health`, `/readyz`) are outside `/v1` and skip auth entirely.

### extractBearerToken

Reads the `Authorization` header, splits on the first space, and requires the prefix to equal `bearer` (case-insensitive). Returns empty string if missing or malformed — the caller returns 401.

## Route Table

All routes are under `/v1` and require a valid API key unless noted.

### Public
| Method | Path | Description |
|--------|------|-------------|
| GET | /health | Liveness check |
| GET | /readyz | Readiness check |

### Organization & Pods
| Method | Path | Scope | Description |
|--------|------|-------|-------------|
| GET | /v1/org | org:admin | Get current organization |
| PATCH | /v1/org | org:admin | Update organization settings |
| GET | /v1/pods | pod:admin | List pods |
| POST | /v1/pods | pod:admin | Create pod |
| GET | /v1/pods/{podID} | pod:admin | Get pod |
| PATCH | /v1/pods/{podID} | pod:admin | Update pod |
| DELETE | /v1/pods/{podID} | pod:admin | Delete pod |

### API Keys
| Method | Path | Scope | Description |
|--------|------|-------|-------------|
| GET | /v1/keys | org:admin | List API keys |
| POST | /v1/keys | org:admin | Create API key |
| GET | /v1/keys/{keyID} | org:admin | Get API key |
| DELETE | /v1/keys/{keyID} | org:admin | Revoke API key |

### Inboxes
| Method | Path | Scope | Description |
|--------|------|-------|-------------|
| GET | /v1/inboxes | inbox:read | List inboxes |
| POST | /v1/inboxes | inbox:write | Create inbox |
| GET | /v1/inboxes/{inboxID} | inbox:read | Get inbox |
| PATCH | /v1/inboxes/{inboxID} | inbox:write | Update inbox |
| DELETE | /v1/inboxes/{inboxID} | inbox:write | Delete inbox |

### Threads (nested under inbox)
| Method | Path | Scope | Description |
|--------|------|-------|-------------|
| GET | /v1/inboxes/{inboxID}/threads | inbox:read | List threads |
| GET | /v1/inboxes/{inboxID}/threads/{threadID} | inbox:read | Get thread |
| PATCH | /v1/inboxes/{inboxID}/threads/{threadID} | inbox:write | Update thread |
| PUT | /v1/inboxes/{inboxID}/threads/{threadID}/labels/{labelID} | inbox:write | Apply label |
| DELETE | /v1/inboxes/{inboxID}/threads/{threadID}/labels/{labelID} | inbox:write | Remove label |

### Messages
| Method | Path | Scope | Description |
|--------|------|-------|-------------|
| GET | /v1/inboxes/{inboxID}/threads/{threadID}/messages | inbox:read | List messages in thread |
| GET | /v1/inboxes/{inboxID}/threads/{threadID}/messages/{messageID} | inbox:read | Get message |
| POST | /v1/inboxes/{inboxID}/messages/send | inbox:write | Send a new message |

### Drafts (nested under inbox)
| Method | Path | Scope | Description |
|--------|------|-------|-------------|
| GET | /v1/inboxes/{inboxID}/drafts | draft:read | List drafts |
| POST | /v1/inboxes/{inboxID}/drafts | draft:write | Create draft |
| GET | /v1/inboxes/{inboxID}/drafts/{draftID} | draft:read | Get draft |
| PATCH | /v1/inboxes/{inboxID}/drafts/{draftID} | draft:write | Update draft |
| DELETE | /v1/inboxes/{inboxID}/drafts/{draftID} | draft:write | Delete draft |
| POST | /v1/inboxes/{inboxID}/drafts/{draftID}/approve | draft:write | Approve draft |
| POST | /v1/inboxes/{inboxID}/drafts/{draftID}/reject | draft:write | Reject draft |

### Labels, Webhooks, Search, WebSocket
| Method | Path | Scope | Description |
|--------|------|-------|-------------|
| GET | /v1/labels | inbox:read | List org-scoped labels |
| POST | /v1/labels | inbox:write | Create label |
| PATCH | /v1/labels/{labelID} | inbox:write | Update label |
| DELETE | /v1/labels/{labelID} | inbox:write | Delete label |
| GET | /v1/webhooks | webhook:read | List webhooks |
| POST | /v1/webhooks | webhook:write | Create webhook |
| GET | /v1/webhooks/{webhookID} | webhook:read | Get webhook |
| PATCH | /v1/webhooks/{webhookID} | webhook:write | Update webhook |
| DELETE | /v1/webhooks/{webhookID} | webhook:write | Delete webhook |
| GET | /v1/webhooks/{webhookID}/deliveries | webhook:read | Delivery history |
| GET | /v1/search | search:read | Full-text search |
| GET | /v1/ws | (any valid key) | WebSocket upgrade |

## WebSocket

**Endpoint**: `GET /v1/ws`
**Auth**: Bearer token in the `Authorization` header (same as all other `/v1` routes — the `auth` middleware is applied to the whole `/v1` router).

### Connection Lifecycle

1. Client connects with a valid API key in the Authorization header.
2. `authMiddleware` validates → Claims placed in context.
3. `Hub.ServeWS()` upgrades HTTP → WebSocket (gorilla/websocket).
4. A `Client` struct is created and sent to the Hub's `register` channel.
5. The Hub (`Run` goroutine) registers the client. If it is the **first** client for that org, it spawns a goroutine that subscribes to the Redis channel `ws:events:{org_id}`.
6. Events published to that Redis channel are received by `subscribeOrg` and broadcast to all clients for the org via buffered `send` channels (size 256). Slow clients that cannot keep up have the message dropped silently.
7. When the last client for an org disconnects, the org map entry is deleted (the Redis subscription goroutine exits via context cancellation).

### Keepalive

- Server sends a WebSocket Ping every `54s` (`pongWait * 9 / 10`, where `pongWait = 60s`).
- Client must respond with a Pong within `60s` or the read deadline expires and the connection is closed.
- Write deadline is `10s` per frame.
- Client-to-server messages are ignored (max size 512 bytes); the read pump only handles pong frames and disconnect detection.

### Event Format

WebSocket messages are the raw event JSON published by services — the same `BaseEvent` envelope used everywhere:

```json
{
  "id": "550e8400-e29b-41d4-a716-446655440001",
  "type": "message.received",
  "org_id": "uuid",
  "occurred_at": "2024-01-15T10:30:00.123Z",
  "correlation_id": "req-abc123",
  "data": { ... }
}
```

`inbox_id` and other resource IDs appear inside `data`, not at the envelope level.

## Proxy Pattern

Inbox/thread/message/draft/label/webhook operations are proxied to internal services via `InboxClient.proxy()`. The proxy:

1. Extracts `*auth.Claims` from the request context (set by `authMiddleware`).
2. Builds a new HTTP request to the target service with:
   - `X-Org-ID: {org_id}`
   - `X-Key-ID: {key_id}`
   - `X-Scopes: inbox:read,inbox:write,...` (comma-separated)
   - `X-Pod-ID: {pod_id}` (only if the key is pod-scoped)
3. Returns the response body and status code verbatim to the caller via `writeProxied`.

The gateway does not re-validate or re-parse the proxied response.

## Org/Pod: Direct DB Access

Organization and Pod management is handled directly by the API service via `OrgStore` — no internal service hop. These are low-frequency admin operations that do not require the inbox service's threading/messaging logic.

## Configuration

| Env Var | Default | Description |
|---------|---------|-------------|
| API_HOST | 0.0.0.0 | Listen address |
| API_PORT | 8080 | HTTP port |
| AUTH_SERVICE_URL | http://localhost:8081 | Auth service for key validation |
| INBOX_SERVICE_URL | http://localhost:8082 | Inbox service proxy target |
| WEBHOOK_SERVICE_URL | http://localhost:8083 | Webhook service proxy target |
| REDIS_URL | redis://localhost:6379 | For WebSocket pub/sub |
| DATABASE_URL | postgres://... | For OrgStore (pod/org CRUD) |
