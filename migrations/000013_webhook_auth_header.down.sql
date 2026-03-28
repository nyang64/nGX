ALTER TABLE webhooks
    DROP COLUMN IF EXISTS auth_header_name,
    DROP COLUMN IF EXISTS auth_header_value_enc;
