-- Copyright (c) 2026 nyklabs.com. All rights reserved.
--
-- Licensed under the nGX Commercial Source License v1.0.
-- See LICENSE file in the project root for full license information.

DROP INDEX IF EXISTS messages_embedding_hnsw;

ALTER TABLE messages
    DROP COLUMN IF EXISTS embedded_at,
    DROP COLUMN IF EXISTS embedding;
