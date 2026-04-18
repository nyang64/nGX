-- Copyright (c) 2026 nyklabs.com. All rights reserved.
--
-- Licensed under the nGX Commercial Source License v1.0.
-- See LICENSE file in the project root for full license information.

CREATE TABLE threads (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    inbox_id        UUID NOT NULL REFERENCES inboxes(id) ON DELETE CASCADE,
    subject         TEXT,
    snippet         TEXT,
    status          TEXT NOT NULL DEFAULT 'open'
                        CHECK (status IN ('open', 'closed', 'spam', 'trash')),
    is_read         BOOLEAN NOT NULL DEFAULT FALSE,
    is_starred      BOOLEAN NOT NULL DEFAULT FALSE,
    message_count   INTEGER NOT NULL DEFAULT 0,
    participants    JSONB NOT NULL DEFAULT '[]',
    last_message_at TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX threads_org_id_idx         ON threads (org_id);
CREATE INDEX threads_inbox_id_idx       ON threads (inbox_id);
CREATE INDEX threads_status_idx         ON threads (org_id, status);
CREATE INDEX threads_last_message_at_idx ON threads (org_id, last_message_at DESC NULLS LAST);

CREATE TRIGGER set_threads_updated_at
    BEFORE UPDATE ON threads
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE threads ENABLE ROW LEVEL SECURITY;
CREATE POLICY thread_isolation ON threads
    USING (org_id = current_setting('app.current_org_id', TRUE)::uuid);
