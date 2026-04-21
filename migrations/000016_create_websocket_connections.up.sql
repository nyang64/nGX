-- websocket_connections stores active API Gateway WebSocket connection IDs.
-- Replaces Redis pub/sub (ws:events:{org_id} channels).
--
-- Flow:
--   $connect  → Lambda inserts row (validates Bearer token first)
--   $disconnect → Lambda deletes row
--   Domain event → event_dispatcher_ws Lambda queries by org_id → PostToConnection
--   GoneException (stale connection) → Lambda deletes the row
--
-- No RLS needed: this table is queried only by internal Lambda functions
-- using the master DB role, never exposed to tenant API keys.

CREATE TABLE websocket_connections (
    connection_id TEXT        NOT NULL,
    org_id        UUID        NOT NULL,
    connected_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- ttl lets the scheduler_drafts job or a future cleanup job prune
    -- connections that somehow missed the $disconnect event (e.g. Lambda crash).
    ttl           TIMESTAMPTZ NOT NULL DEFAULT (NOW() + INTERVAL '24 hours'),

    CONSTRAINT websocket_connections_pkey PRIMARY KEY (connection_id)
);

-- Lookup all live connections for an org when dispatching a domain event
CREATE INDEX websocket_connections_org_id_idx
    ON websocket_connections (org_id);

-- Quickly prune stale connections (any periodic cleanup job)
-- No partial index predicate — NOW() is not IMMUTABLE in PostgreSQL.
-- Cleanup queries filter by ttl at runtime: WHERE ttl < NOW()
CREATE INDEX websocket_connections_ttl_idx
    ON websocket_connections (ttl);
