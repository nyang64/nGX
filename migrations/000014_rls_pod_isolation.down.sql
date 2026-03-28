DROP POLICY IF EXISTS inbox_isolation ON inboxes;

CREATE POLICY inbox_isolation ON inboxes
    USING (org_id = current_setting('app.current_org_id', TRUE)::uuid);
