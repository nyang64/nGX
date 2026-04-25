-- Copyright (c) 2026 nyklabs.com. All rights reserved.
--
-- Licensed under the nGX Commercial Source License v1.0.
-- See LICENSE file in the project root for full license information.

-- Revert: remove 'bounced' from the messages.status CHECK constraint.
-- Note: if any rows have status='bounced' this will fail — migrate those rows first.

ALTER TABLE messages DROP CONSTRAINT messages_status_check;
ALTER TABLE messages ADD CONSTRAINT messages_status_check
  CHECK (status IN ('received', 'sending', 'sent', 'failed', 'draft'));
