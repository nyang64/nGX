-- Allow attachments to be associated with a draft (before approval creates a message).
-- message_id becomes nullable; exactly one of message_id or draft_id must be set.

ALTER TABLE attachments
  ALTER COLUMN message_id DROP NOT NULL,
  ADD COLUMN draft_id UUID REFERENCES drafts(id) ON DELETE CASCADE;

ALTER TABLE attachments
  ADD CONSTRAINT attachments_has_parent
    CHECK (message_id IS NOT NULL OR draft_id IS NOT NULL);

CREATE INDEX attachments_draft_id_idx ON attachments (draft_id);
