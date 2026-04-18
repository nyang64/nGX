-- Copyright (c) 2026 nyklabs.com. All rights reserved.
--
-- Licensed under the nGX Commercial Source License v1.0.
-- See LICENSE file in the project root for full license information.

DROP POLICY IF EXISTS inbox_isolation ON inboxes;

CREATE POLICY inbox_isolation ON inboxes
    USING (org_id = current_setting('app.current_org_id', TRUE)::uuid);
