-- Copyright (c) 2026 nyklabs.com. All rights reserved.
--
-- Licensed under the nGX Commercial Source License v1.0.
-- See LICENSE file in the project root for full license information.

-- Add 'bounced' to the messages.status CHECK constraint.
-- 'bounced' is already defined in pkg/models/types.go (MessageStatusBounced) and
-- the OpenAPI MessageStatus enum, but was missing from the DB constraint.
-- Without this, SES "Email Bounced" events would cause a constraint violation.

ALTER TABLE messages DROP CONSTRAINT messages_status_check;
ALTER TABLE messages ADD CONSTRAINT messages_status_check
  CHECK (status IN ('received', 'sending', 'sent', 'failed', 'bounced', 'draft'));
