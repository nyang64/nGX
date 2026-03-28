package store

import (
	"context"
	"fmt"

	"agentmail/pkg/embedder"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store handles DB reads/writes for the embedder pipeline.
// It intentionally bypasses RLS: the embedder is a trusted background service
// that operates on messages it already received via Kafka events.
type Store struct {
	pool *pgxpool.Pool
}

// New creates a Store.
func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// GetTextKey returns the S3 body_text_key for a message, or "" if none.
func (s *Store) GetTextKey(ctx context.Context, msgID uuid.UUID) (string, error) {
	var key string
	err := s.pool.QueryRow(ctx,
		`SELECT COALESCE(body_text_key, '') FROM messages WHERE id = $1`,
		msgID,
	).Scan(&key)
	if err != nil {
		return "", fmt.Errorf("get text key for message %s: %w", msgID, err)
	}
	return key, nil
}

// SetEmbedding writes the embedding vector and stamps embedded_at for a message.
func (s *Store) SetEmbedding(ctx context.Context, msgID uuid.UUID, vec []float32) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE messages SET embedding = $1::vector, embedded_at = NOW() WHERE id = $2`,
		embedder.VectorLiteral(vec), msgID,
	)
	if err != nil {
		return fmt.Errorf("set embedding for message %s: %w", msgID, err)
	}
	return nil
}
