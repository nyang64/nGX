-- Copyright (c) 2026 nyklabs.com. All rights reserved.
--
-- Licensed under the nGX Commercial Source License v1.0.
-- See LICENSE file in the project root for full license information.

-- Enable pgvector extension (idempotent).
CREATE EXTENSION IF NOT EXISTS vector;

-- Add embedding column (256-dim, MRL-truncated nomic-embed-text-v1.5)
-- and a timestamp to track when each message was last embedded.
ALTER TABLE messages
    ADD COLUMN IF NOT EXISTS embedding   vector(256),
    ADD COLUMN IF NOT EXISTS embedded_at TIMESTAMPTZ;

-- HNSW index for approximate nearest-neighbour cosine search.
-- m=16, ef_construction=64 is a reasonable default: good recall,
-- moderate build time and memory footprint.
CREATE INDEX IF NOT EXISTS messages_embedding_hnsw
    ON messages
    USING hnsw (embedding vector_cosine_ops)
    WITH (m = 16, ef_construction = 64);
