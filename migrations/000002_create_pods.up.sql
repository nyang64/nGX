CREATE TABLE pods (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id      UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    slug        TEXT NOT NULL,
    description TEXT,
    settings    JSONB NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT pods_org_slug_unique UNIQUE (org_id, slug)
);

CREATE INDEX pods_org_id_idx ON pods (org_id);

CREATE TRIGGER set_pods_updated_at
    BEFORE UPDATE ON pods
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE pods ENABLE ROW LEVEL SECURITY;
CREATE POLICY pod_isolation ON pods
    USING (org_id = current_setting('app.current_org_id', TRUE)::uuid);
