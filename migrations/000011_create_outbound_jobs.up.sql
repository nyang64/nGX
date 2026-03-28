CREATE TABLE outbound_jobs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    message_id      UUID NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    status          TEXT NOT NULL DEFAULT 'pending'
                        CHECK (status IN ('pending', 'processing', 'sent', 'failed', 'cancelled')),
    attempt_count   INTEGER NOT NULL DEFAULT 0,
    max_attempts    INTEGER NOT NULL DEFAULT 3,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_error      TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX outbound_jobs_org_id_idx               ON outbound_jobs (org_id);
CREATE INDEX outbound_jobs_status_next_attempt_idx  ON outbound_jobs (status, next_attempt_at)
    WHERE status IN ('pending', 'processing');

CREATE TRIGGER set_outbound_jobs_updated_at
    BEFORE UPDATE ON outbound_jobs
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

ALTER TABLE outbound_jobs ENABLE ROW LEVEL SECURITY;
CREATE POLICY outbound_job_isolation ON outbound_jobs
    USING (org_id = current_setting('app.current_org_id', TRUE)::uuid);
