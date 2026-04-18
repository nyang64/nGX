-- Copyright (c) 2026 nyklabs.com. All rights reserved.
--
-- Licensed under the nGX Commercial Source License v1.0.
-- See LICENSE file in the project root for full license information.

CREATE TABLE inboxes (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    pod_id          UUID REFERENCES pods(id) ON DELETE SET NULL,
    address         TEXT NOT NULL UNIQUE,
    display_name    TEXT,
    status          TEXT NOT NULL DEFAULT 'active'
                        CHECK (status IN ('active', 'suspended', 'deleted')),
    settings        JSONB NOT NULL DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX inboxes_org_id_idx    ON inboxes (org_id);
CREATE INDEX inboxes_pod_id_idx    ON inboxes (pod_id);
CREATE INDEX inboxes_address_idx   ON inboxes (address);

CREATE TRIGGER set_inboxes_updated_at
    BEFORE UPDATE ON inboxes
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE inboxes ENABLE ROW LEVEL SECURITY;
CREATE POLICY inbox_isolation ON inboxes
    USING (org_id = current_setting('app.current_org_id', TRUE)::uuid);
