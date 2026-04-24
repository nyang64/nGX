-- Copyright (c) 2026 nyklabs.com. All rights reserved.
--
-- Licensed under the nGX Commercial Source License v1.0.
-- See LICENSE file in the project root for full license information.

-- Resize embedding column from 256 to 768 dims to match bge-base-en-v1.5
-- (Cloudflare Workers AI). BGE models do not support MRL truncation so we
-- store the full 768-dim output.
--
-- The HNSW index must be dropped and recreated because pgvector does not
-- allow ALTER COLUMN on indexed vector columns.

DROP INDEX IF EXISTS messages_embedding_hnsw;

ALTER TABLE messages
    ALTER COLUMN embedding TYPE vector(768);

CREATE INDEX messages_embedding_hnsw
    ON messages
    USING hnsw (embedding vector_cosine_ops)
    WITH (m = 16, ef_construction = 64);
