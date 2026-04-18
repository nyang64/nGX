-- Copyright (c) 2026 nyklabs.com. All rights reserved.
--
-- Licensed under the nGX Commercial Source License v1.0.
-- See LICENSE file in the project root for full license information.

-- Tighten inbox RLS to enforce pod-level isolation when a pod-scoped API key is
-- in use. The transaction sets app.current_pod_id to either the pod UUID string
-- or an empty string (for org-wide keys). An empty value means "no restriction".

DROP POLICY IF EXISTS inbox_isolation ON inboxes;

CREATE POLICY inbox_isolation ON inboxes
    USING (
        org_id = current_setting('app.current_org_id', TRUE)::uuid
        AND (
            current_setting('app.current_pod_id', TRUE) = ''
            OR pod_id = current_setting('app.current_pod_id', TRUE)::uuid
        )
    );
