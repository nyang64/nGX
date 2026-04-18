-- Copyright (c) 2026 nyklabs.com. All rights reserved.
--
-- Licensed under the nGX Commercial Source License v1.0.
-- See LICENSE file in the project root for full license information.

CREATE TABLE webhooks (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    url             TEXT NOT NULL,
    secret          TEXT NOT NULL,
    events          TEXT[] NOT NULL DEFAULT '{}',
    pod_id          UUID REFERENCES pods(id) ON DELETE SET NULL,
    inbox_id        UUID REFERENCES inboxes(id) ON DELETE SET NULL,
    is_active       BOOLEAN NOT NULL DEFAULT TRUE,
    failure_count   INTEGER NOT NULL DEFAULT 0,
    last_success_at TIMESTAMPTZ,
    last_failure_at TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX webhooks_org_id_idx ON webhooks (org_id);

CREATE TRIGGER set_webhooks_updated_at
    BEFORE UPDATE ON webhooks
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE webhooks ENABLE ROW LEVEL SECURITY;
CREATE POLICY webhook_isolation ON webhooks
    USING (org_id = current_setting('app.current_org_id', TRUE)::uuid);

-- Delivery log for webhook dispatch attempts
CREATE TABLE webhook_deliveries (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    webhook_id          UUID NOT NULL REFERENCES webhooks(id) ON DELETE CASCADE,
    event_id            TEXT NOT NULL,
    event_type          TEXT NOT NULL,
    payload             JSONB NOT NULL,
    status              TEXT NOT NULL DEFAULT 'pending'
                            CHECK (status IN ('pending', 'success', 'failed', 'retrying')),
    attempt_count       INTEGER NOT NULL DEFAULT 0,
    next_attempt_at     TIMESTAMPTZ,
    last_attempt_at     TIMESTAMPTZ,
    response_status     INTEGER,
    response_body       TEXT,
    error_message       TEXT,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX webhook_deliveries_webhook_id_idx          ON webhook_deliveries (webhook_id);
CREATE INDEX webhook_deliveries_status_next_attempt_idx ON webhook_deliveries (status, next_attempt_at)
    WHERE status IN ('pending', 'retrying');

CREATE TRIGGER set_webhook_deliveries_updated_at
    BEFORE UPDATE ON webhook_deliveries
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE webhook_deliveries ENABLE ROW LEVEL SECURITY;
CREATE POLICY webhook_delivery_isolation ON webhook_deliveries
    USING (
        EXISTS (
            SELECT 1 FROM webhooks w
            WHERE w.id = webhook_id
              AND w.org_id = current_setting('app.current_org_id', TRUE)::uuid
        )
    );
