-- Copyright (c) 2026 nyklabs.com. All rights reserved.
--
-- Licensed under the nGX Commercial Source License v1.0.
-- See LICENSE file in the project root for full license information.

-- Caller-supplied auth header for outbound webhook delivery.
-- auth_header_name  : the HTTP header name to inject (e.g. "Authorization")
-- auth_header_value_enc : AES-256-GCM encrypted header value; NULL when not set
ALTER TABLE webhooks
    ADD COLUMN auth_header_name      TEXT,
    ADD COLUMN auth_header_value_enc BYTEA;
