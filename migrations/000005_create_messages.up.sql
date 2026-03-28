CREATE TABLE messages (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id              UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    inbox_id            UUID NOT NULL REFERENCES inboxes(id) ON DELETE CASCADE,
    thread_id           UUID NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
    message_id_header   TEXT,
    in_reply_to         TEXT,
    references_header   TEXT[],
    direction           TEXT NOT NULL CHECK (direction IN ('inbound', 'outbound')),
    status              TEXT NOT NULL DEFAULT 'received'
                            CHECK (status IN ('received', 'sending', 'sent', 'failed', 'draft')),
    from_address        TEXT NOT NULL,
    from_name           TEXT,
    to_addresses        JSONB NOT NULL DEFAULT '[]',
    cc_addresses        JSONB NOT NULL DEFAULT '[]',
    bcc_addresses       JSONB NOT NULL DEFAULT '[]',
    reply_to            TEXT,
    subject             TEXT,
    body_text_key       TEXT,
    body_html_key       TEXT,
    raw_key             TEXT,
    size_bytes          BIGINT,
    has_attachments     BOOLEAN NOT NULL DEFAULT FALSE,
    headers             JSONB NOT NULL DEFAULT '{}',
    metadata            JSONB NOT NULL DEFAULT '{}',
    sent_at             TIMESTAMPTZ,
    received_at         TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    search_vector       TSVECTOR
);

CREATE INDEX messages_org_id_idx        ON messages (org_id);
CREATE INDEX messages_inbox_id_idx      ON messages (inbox_id);
CREATE INDEX messages_thread_id_idx     ON messages (thread_id);
CREATE INDEX messages_message_id_header_idx ON messages (message_id_header) WHERE message_id_header IS NOT NULL;
CREATE INDEX messages_received_at_idx   ON messages (org_id, received_at DESC NULLS LAST);
CREATE INDEX messages_search_vector_idx ON messages USING GIN (search_vector);

-- Trigger function to auto-update search_vector from subject and body content
CREATE OR REPLACE FUNCTION messages_search_vector_update() RETURNS TRIGGER AS $$
BEGIN
    NEW.search_vector :=
        setweight(to_tsvector('english', COALESCE(NEW.subject, '')), 'A') ||
        setweight(to_tsvector('english', COALESCE(NEW.from_address, '')), 'B') ||
        setweight(to_tsvector('english', COALESCE(NEW.from_name, '')), 'B');
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER messages_search_vector_trigger
    BEFORE INSERT OR UPDATE OF subject, from_address, from_name ON messages
    FOR EACH ROW EXECUTE FUNCTION messages_search_vector_update();

CREATE TRIGGER set_messages_updated_at
    BEFORE UPDATE ON messages
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE messages ENABLE ROW LEVEL SECURITY;
CREATE POLICY message_isolation ON messages
    USING (org_id = current_setting('app.current_org_id', TRUE)::uuid);
