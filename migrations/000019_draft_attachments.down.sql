DROP INDEX IF EXISTS attachments_draft_id_idx;

ALTER TABLE attachments
  DROP CONSTRAINT IF EXISTS attachments_has_parent,
  DROP COLUMN IF EXISTS draft_id,
  ALTER COLUMN message_id SET NOT NULL;
