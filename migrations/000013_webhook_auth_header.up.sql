-- Caller-supplied auth header for outbound webhook delivery.
-- auth_header_name  : the HTTP header name to inject (e.g. "Authorization")
-- auth_header_value_enc : AES-256-GCM encrypted header value; NULL when not set
ALTER TABLE webhooks
    ADD COLUMN auth_header_name      TEXT,
    ADD COLUMN auth_header_value_enc BYTEA;
