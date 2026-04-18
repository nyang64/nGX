-- Copyright (c) 2026 nyklabs.com. All rights reserved.
--
-- Licensed under the nGX Commercial Source License v1.0.
-- See LICENSE file in the project root for full license information.

CREATE TABLE domain_configs (
    id                          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id                      UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    pod_id                      UUID NOT NULL REFERENCES pods(id) ON DELETE CASCADE,
    domain                      TEXT NOT NULL UNIQUE,
    status                      TEXT NOT NULL DEFAULT 'pending'
                                    CHECK (status IN ('pending', 'verifying', 'active', 'failed')),
    spf_record                  TEXT,
    dkim_selector               TEXT,
    dkim_public_key             TEXT,
    dkim_private_key_encrypted  TEXT,
    dmarc_policy                TEXT,
    verified_at                 TIMESTAMPTZ,
    created_at                  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at                  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX domain_configs_org_id_idx  ON domain_configs (org_id);
CREATE INDEX domain_configs_pod_id_idx  ON domain_configs (pod_id);
CREATE INDEX domain_configs_status_idx  ON domain_configs (status);

CREATE TRIGGER set_domain_configs_updated_at
    BEFORE UPDATE ON domain_configs
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE domain_configs ENABLE ROW LEVEL SECURITY;
CREATE POLICY domain_config_isolation ON domain_configs
    USING (org_id = current_setting('app.current_org_id', TRUE)::uuid);
