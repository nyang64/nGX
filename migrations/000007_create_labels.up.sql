CREATE TABLE labels (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id      UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    color       TEXT,
    description TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT labels_org_name_unique UNIQUE (org_id, name)
);

CREATE INDEX labels_org_id_idx ON labels (org_id);

ALTER TABLE labels ENABLE ROW LEVEL SECURITY;
CREATE POLICY label_isolation ON labels
    USING (org_id = current_setting('app.current_org_id', TRUE)::uuid);

-- Junction table linking threads to labels
CREATE TABLE thread_labels (
    thread_id   UUID NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
    label_id    UUID NOT NULL REFERENCES labels(id) ON DELETE CASCADE,
    applied_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (thread_id, label_id)
);

CREATE INDEX thread_labels_label_id_idx ON thread_labels (label_id);

ALTER TABLE thread_labels ENABLE ROW LEVEL SECURITY;
CREATE POLICY thread_label_isolation ON thread_labels
    USING (
        EXISTS (
            SELECT 1 FROM threads t
            WHERE t.id = thread_id
              AND t.org_id = current_setting('app.current_org_id', TRUE)::uuid
        )
    );
