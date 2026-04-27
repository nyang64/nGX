-- Copyright (c) 2026 nyklabs.com. All rights reserved.
--
-- Licensed under the nGX Commercial Source License v1.0.
-- See LICENSE file in the project root for full license information.

ALTER TABLE messages
    ADD COLUMN is_read    BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN is_starred BOOLEAN NOT NULL DEFAULT FALSE;

CREATE INDEX messages_is_read_idx    ON messages (org_id, inbox_id, is_read);
CREATE INDEX messages_is_starred_idx ON messages (org_id, inbox_id, is_starred);
