-- Add message_id to drafts so the approved draft can reference the created message.
ALTER TABLE drafts
  ADD COLUMN message_id UUID REFERENCES messages(id) ON DELETE SET NULL;
