DROP INDEX IF EXISTS messages_embedding_hnsw;

ALTER TABLE messages
    DROP COLUMN IF EXISTS embedded_at,
    DROP COLUMN IF EXISTS embedding;
