-- Copyright (c) 2026 nyklabs.com. All rights reserved.
--
-- Licensed under the nGX Commercial Source License v1.0.
-- See LICENSE file in the project root for full license information.

ALTER TABLE webhooks
    DROP COLUMN IF EXISTS auth_header_name,
    DROP COLUMN IF EXISTS auth_header_value_enc;
