/*
 * Copyright (c) 2026 nyklabs.com. All rights reserved.
 *
 * Licensed under the nGX Commercial Source License v1.0.
 * See LICENSE file in the project root for full license information.
 */

package main

import (
	"context"
	"log/slog"
	"os"
	"strconv"
	"strings"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"agentmail/lambdas/shared"
	embedderpkg "agentmail/pkg/embedder"
	domainevents "agentmail/pkg/events"
	s3pkg "agentmail/pkg/s3"
	embstore "agentmail/services/embedder/store"
)

// maxTextBytes caps the body text sent to the embedder.
const maxTextBytes = 8192

var (
	pool     *pgxpool.Pool
	store    *embstore.Store
	s3Client *s3pkg.Client
	emb      *embedderpkg.Client
)

func init() {
	ctx := context.Background()
	pool = shared.InitDB()
	store = embstore.New(pool)

	bucket := os.Getenv("S3_BUCKET_EMAILS")
	var err error
	s3Client, err = s3pkg.NewFromAWS(ctx, bucket)
	if err != nil {
		slog.Error("embedder: init S3 client", "error", err)
		os.Exit(1)
	}

	embedderURL := os.Getenv("EMBEDDER_URL")
	embedderModel := os.Getenv("EMBEDDER_MODEL")
	embedderAPIKey := os.Getenv("EMBEDDER_API_KEY")
	embedderDims := 0
	if s := os.Getenv("EMBEDDER_DIMS"); s != "" {
		if d, err := strconv.Atoi(s); err == nil && d > 0 {
			embedderDims = d
		}
	}
	emb = embedderpkg.New(embedderURL, embedderModel, embedderAPIKey, embedderDims)
}

func handler(ctx context.Context, sqsEvent events.SQSEvent) error {
	for _, record := range sqsEvent.Records {
		if err := processRecord(ctx, record); err != nil {
			slog.Error("embedder: process record", "message_id", record.MessageId, "error", err)
			// Return error to trigger SQS retry / DLQ routing.
			return err
		}
	}
	return nil
}

func processRecord(ctx context.Context, record events.SQSMessage) error {
	evt, err := domainevents.Unmarshal([]byte(record.Body))
	if err != nil {
		slog.Warn("embedder: unmarshal event, skipping", "error", err)
		return nil
	}

	base := evt.GetBase()
	switch base.Type {
	case domainevents.EventMessageReceived, domainevents.EventMessageSent:
	default:
		return nil
	}

	var msgIDStr string
	switch e := evt.(type) {
	case *domainevents.MessageReceivedEvent:
		msgIDStr = e.Data.MessageID
	case *domainevents.MessageSentEvent:
		msgIDStr = e.Data.MessageID
	default:
		return nil
	}

	msgID, err := uuid.Parse(msgIDStr)
	if err != nil {
		slog.Warn("embedder: invalid message_id in event", "raw", msgIDStr)
		return nil
	}

	textKey, err := store.GetTextKey(ctx, msgID)
	if err != nil {
		slog.Error("embedder: get text key", "message_id", msgID, "error", err)
		return nil
	}
	if textKey == "" {
		slog.Debug("embedder: no text body, skipping", "message_id", msgID)
		return nil
	}

	raw, err := s3Client.Download(ctx, textKey)
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

	vec, err := emb.Embed(ctx, text)
	if err != nil {
		slog.Error("embedder: generate embedding", "message_id", msgID, "error", err)
		return nil
	}

	if err := store.SetEmbedding(ctx, msgID, vec); err != nil {
		slog.Error("embedder: set embedding", "message_id", msgID, "error", err)
		return nil
	}

	slog.Debug("embedder: embedded message", "message_id", msgID, "dims", len(vec))
	return nil
}

func main() {
	lambda.Start(handler)
}
