package consumer

import (
	"context"
	"log/slog"
	"strings"

	"agentmail/pkg/embedder"
	"agentmail/pkg/events"
	"agentmail/pkg/kafka"
	"agentmail/pkg/s3"
	"agentmail/services/embedder/store"

	"github.com/google/uuid"
	kafkago "github.com/segmentio/kafka-go"
)

// maxTextBytes caps the body text sent to the embedder (~2 000 tokens for
// nomic-embed-text-v1.5 which supports up to 8 192 tokens; 8 KB covers most
// emails without blowing past the context window).
const maxTextBytes = 8192

// Consumer reads domain events from events.fanout, fetches message body text
// from S3, generates embeddings, and stores them back in the messages table.
type Consumer struct {
	consumer *kafka.Consumer
	store    *store.Store
	s3       *s3.Client
	embedder *embedder.Client
}

// New creates a Consumer.
func New(brokers []string, groupID string, st *store.Store, s3c *s3.Client, emb *embedder.Client) *Consumer {
	return &Consumer{
		consumer: kafka.NewConsumer(brokers, kafka.TopicEventsFanout, groupID+"-embedder"),
		store:    st,
		s3:       s3c,
		embedder: emb,
	}
}

// Run consumes events until ctx is cancelled.
func (c *Consumer) Run(ctx context.Context) error {
	return c.consumer.Consume(ctx, func(ctx context.Context, msg kafkago.Message) error {
		return c.handle(ctx, msg)
	})
}

// Close shuts down the underlying Kafka reader.
func (c *Consumer) Close() error {
	return c.consumer.Close()
}

func (c *Consumer) handle(ctx context.Context, msg kafkago.Message) error {
	evt, err := events.Unmarshal(msg.Value)
	if err != nil {
		slog.Warn("embedder: failed to unmarshal event, skipping", "error", err)
		return nil
	}

	base := evt.GetBase()
	switch base.Type {
	case events.EventMessageReceived, events.EventMessageSent:
	default:
		return nil // not a message event — ignore
	}

	var msgIDStr string
	switch e := evt.(type) {
	case *events.MessageReceivedEvent:
		msgIDStr = e.Data.MessageID
	case *events.MessageSentEvent:
		msgIDStr = e.Data.MessageID
	default:
		return nil
	}

	msgID, err := uuid.Parse(msgIDStr)
	if err != nil {
		slog.Warn("embedder: invalid message_id in event", "raw", msgIDStr)
		return nil
	}

	// Look up S3 key for the plain-text body.
	textKey, err := c.store.GetTextKey(ctx, msgID)
	if err != nil {
		slog.Error("embedder: get text key", "message_id", msgID, "error", err)
		return nil // non-fatal: don't block the Kafka partition
	}
	if textKey == "" {
		slog.Debug("embedder: no text body, skipping", "message_id", msgID)
		return nil
	}

	// Download body text from S3.
	raw, err := c.s3.Download(ctx, textKey)
	if err != nil {
		slog.Error("embedder: download body text", "message_id", msgID, "s3_key", textKey, "error", err)
		return nil
	}

	text := strings.TrimSpace(string(raw))
	if text == "" {
		return nil
	}
	if len(text) > maxTextBytes {
		text = text[:maxTextBytes]
	}

	// Generate embedding (truncated to configured dims by client).
	vec, err := c.embedder.Embed(ctx, text)
	if err != nil {
		slog.Error("embedder: generate embedding", "message_id", msgID, "error", err)
		return nil // embedding server may be temporarily unavailable
	}

	// Persist to DB.
	if err := c.store.SetEmbedding(ctx, msgID, vec); err != nil {
		slog.Error("embedder: set embedding", "message_id", msgID, "error", err)
		return nil
	}

	slog.Debug("embedder: embedded message", "message_id", msgID, "dims", len(vec))
	return nil
}
