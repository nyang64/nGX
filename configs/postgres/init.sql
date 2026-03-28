-- ============================================================
-- AgentMail PostgreSQL Initialization Script
-- Runs once when the postgres container first starts.
-- ============================================================

-- ------------------------------------------------------------
-- Extensions
-- ------------------------------------------------------------

-- uuid_generate_v4() helper (we also use gen_random_uuid() from pgcrypto)
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- Trigram index support for fast LIKE/ILIKE and full-text search
CREATE EXTENSION IF NOT EXISTS "pg_trgm";

-- GIN index support for composite types (JSONB, arrays, range types)
CREATE EXTENSION IF NOT EXISTS "btree_gin";

-- pgcrypto for gen_random_uuid() – preferred over uuid-ossp in PG 13+
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- ------------------------------------------------------------
-- Application Role
-- ------------------------------------------------------------
-- The agentmail_app role is used by the running application.
-- It owns no objects; objects are owned by the agentmail superuser
-- (the one created by POSTGRES_USER). Separating concerns makes
-- it straightforward to add RLS policies that distinguish between
-- the superuser (migrations) and the app role (runtime queries).

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'agentmail_app') THEN
        CREATE ROLE agentmail_app LOGIN PASSWORD 'agentmail_app_dev';
    END IF;
END;
$$;

-- Grant connect + schema usage to the app role
GRANT CONNECT ON DATABASE agentmail TO agentmail_app;
GRANT USAGE ON SCHEMA public TO agentmail_app;

-- Default privileges: any table/sequence created in future migrations is
-- automatically accessible to the app role.
ALTER DEFAULT PRIVILEGES IN SCHEMA public
    GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO agentmail_app;

ALTER DEFAULT PRIVILEGES IN SCHEMA public
    GRANT USAGE, SELECT ON SEQUENCES TO agentmail_app;

-- ------------------------------------------------------------
-- updated_at Trigger Helper
-- ------------------------------------------------------------
-- Usage: TRIGGER set_updated_at BEFORE UPDATE ON <table>
--        FOR EACH ROW EXECUTE FUNCTION set_updated_at();
--
-- Every table that has an `updated_at` column should attach this
-- trigger so the column stays accurate without application-layer
-- responsibility.

CREATE OR REPLACE FUNCTION set_updated_at()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$;

COMMENT ON FUNCTION set_updated_at IS
    'Automatically sets updated_at to the current timestamp on row update. '
    'Attach to tables with: BEFORE UPDATE FOR EACH ROW EXECUTE FUNCTION set_updated_at()';

-- ------------------------------------------------------------
-- Row-Level Security (RLS) Strategy
-- ------------------------------------------------------------
-- AgentMail is a multi-tenant SaaS. Each tenant is identified by
-- an `org_id` (UUID). RLS policies on tenant-scoped tables filter
-- rows using a session-local variable `app.current_org_id`.
--
-- Workflow:
--   1. Before running any query, the application calls:
--          SELECT set_rls_context('<org_uuid>');
--      This sets a local GUC for the duration of the transaction.
--   2. RLS policies on each table reference:
--          current_setting('app.current_org_id')::uuid
--   3. The superuser/migration role bypasses RLS (BYPASSRLS).
--      The agentmail_app role is subject to RLS policies.
--
-- Example policy (to be created in migrations, not here):
--   ALTER TABLE emails ENABLE ROW LEVEL SECURITY;
--   CREATE POLICY emails_org_isolation ON emails
--       USING (org_id = current_setting('app.current_org_id')::uuid);

CREATE OR REPLACE FUNCTION set_rls_context(p_org_id uuid)
RETURNS void
LANGUAGE plpgsql
SECURITY DEFINER
AS $$
BEGIN
    -- SET LOCAL scopes the GUC to the current transaction only.
    -- The application MUST call this inside a transaction block.
    PERFORM set_config('app.current_org_id', p_org_id::text, true);
END;
$$;

COMMENT ON FUNCTION set_rls_context(uuid) IS
    'Sets the RLS session context for multi-tenant row isolation. '
    'Call inside a transaction before executing tenant-scoped queries: '
    'SELECT set_rls_context(''<org_id>'')';

-- Grant execute to app role so it can set its own context
GRANT EXECUTE ON FUNCTION set_rls_context(uuid) TO agentmail_app;

-- ------------------------------------------------------------
-- Helpful search_path
-- ------------------------------------------------------------
-- Ensure the public schema is always searched first.
ALTER DATABASE agentmail SET search_path TO public;
