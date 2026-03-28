CREATE TABLE drafts (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    inbox_id        UUID NOT NULL REFERENCES inboxes(id) ON DELETE CASCADE,
    thread_id       UUID REFERENCES threads(id) ON DELETE SET NULL,
    to_addresses    JSONB NOT NULL DEFAULT '[]',
    cc_addresses    JSONB NOT NULL DEFAULT '[]',
    bcc_addresses   JSONB NOT NULL DEFAULT '[]',
    subject         TEXT,
    body_text       TEXT,
    body_html       TEXT,
    metadata        JSONB NOT NULL DEFAULT '{}',
    review_status   TEXT NOT NULL DEFAULT 'pending'
                        CHECK (review_status IN ('pending', 'approved', 'rejected', 'sent')),
    review_note     TEXT,
    reviewed_by     TEXT,
    reviewed_at     TIMESTAMPTZ,
    expires_at      TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX drafts_org_id_idx      ON drafts (org_id);
CREATE INDEX drafts_inbox_id_idx    ON drafts (inbox_id);
CREATE INDEX drafts_thread_id_idx   ON drafts (thread_id) WHERE thread_id IS NOT NULL;
CREATE INDEX drafts_review_status_idx ON drafts (org_id, review_status);

CREATE TRIGGER set_drafts_updated_at
    BEFORE UPDATE ON drafts
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE drafts ENABLE ROW LEVEL SECURITY;
CREATE POLICY draft_isolation ON drafts
    USING (org_id = current_setting('app.current_org_id', TRUE)::uuid);
