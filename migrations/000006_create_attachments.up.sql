CREATE TABLE attachments (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    message_id      UUID NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    filename        TEXT NOT NULL,
    content_type    TEXT NOT NULL,
    size_bytes      BIGINT NOT NULL,
    s3_key          TEXT NOT NULL,
    content_id      TEXT,
    is_inline       BOOLEAN NOT NULL DEFAULT FALSE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX attachments_org_id_idx     ON attachments (org_id);
CREATE INDEX attachments_message_id_idx ON attachments (message_id);

ALTER TABLE attachments ENABLE ROW LEVEL SECURITY;
CREATE POLICY attachment_isolation ON attachments
    USING (org_id = current_setting('app.current_org_id', TRUE)::uuid);
