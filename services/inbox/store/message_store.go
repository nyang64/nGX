/*
 * Copyright (c) 2026 nyklabs.com. All rights reserved.
 *
 * Licensed under the nGX Commercial Source License v1.0.
 * See LICENSE file in the project root for full license information.
 */

package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"agentmail/pkg/models"
	"agentmail/pkg/pagination"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// MessageStore defines data access for messages.
type MessageStore interface {
	Create(ctx context.Context, tx pgx.Tx, msg *models.Message) error
	GetByID(ctx context.Context, tx pgx.Tx, orgID, messageID uuid.UUID) (*models.Message, error)
	List(ctx context.Context, tx pgx.Tx, orgID, threadID uuid.UUID, limit int, cursor string) ([]*models.Message, string, error)
	UpdateStatus(ctx context.Context, tx pgx.Tx, orgID, messageID uuid.UUID, status models.MessageStatus) error
	CreateAttachment(ctx context.Context, tx pgx.Tx, att *models.Attachment) error
	ListAttachments(ctx context.Context, tx pgx.Tx, orgID, messageID uuid.UUID) ([]*models.Attachment, error)
}

// PostgresMessageStore implements MessageStore using PostgreSQL.
type PostgresMessageStore struct {
	pool *pgxpool.Pool
}

// NewPostgresMessageStore creates a new PostgresMessageStore.
func NewPostgresMessageStore(pool *pgxpool.Pool) *PostgresMessageStore {
	return &PostgresMessageStore{pool: pool}
}

// Create inserts a new message record.
func (s *PostgresMessageStore) Create(ctx context.Context, tx pgx.Tx, msg *models.Message) error {
	toJSON, _ := json.Marshal(msg.To)
	ccJSON, _ := json.Marshal(msg.Cc)
	bccJSON, _ := json.Marshal(msg.Bcc)
	headersJSON, _ := json.Marshal(msg.Headers)
	metadataJSON, _ := json.Marshal(msg.Metadata)
	if msg.Metadata == nil {
		metadataJSON = []byte("{}")
	}

	q := `
		INSERT INTO messages (
			id, org_id, inbox_id, thread_id,
			message_id_header, in_reply_to, references_header,
			direction, status,
			from_address, from_name,
			to_addresses, cc_addresses, bcc_addresses,
			reply_to, subject,
			body_text_key, body_html_key, raw_key,
			size_bytes, has_attachments, headers, metadata,
			sent_at, received_at,
			created_at, updated_at
		) VALUES (
			$1, $2, $3, $4,
			$5, $6, $7,
			$8, $9,
			$10, $11,
			$12, $13, $14,
			$15, $16,
			$17, $18, $19,
			$20, $21, $22, $23,
			$24, $25,
			$26, $27
		)
	`
	_, err := tx.Exec(ctx, q,
		msg.ID, msg.OrgID, msg.InboxID, msg.ThreadID,
		msg.MessageID, msg.InReplyTo, msg.References,
		string(msg.Direction), string(msg.Status),
		msg.From.Email, msg.From.Name,
		toJSON, ccJSON, bccJSON,
		msg.ReplyTo, msg.Subject,
		msg.TextS3Key, msg.HtmlS3Key, msg.RawS3Key,
		msg.SizeBytes, len(msg.Attachments) > 0, headersJSON, metadataJSON,
		msg.SentAt, msg.ReceivedAt,
		msg.CreatedAt, msg.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert message: %w", err)
	}
	return nil
}

// GetByID retrieves a message by org and message ID.
func (s *PostgresMessageStore) GetByID(ctx context.Context, tx pgx.Tx, orgID, messageID uuid.UUID) (*models.Message, error) {
	q := `
		SELECT id, org_id, inbox_id, thread_id,
		       message_id_header, in_reply_to, references_header,
		       direction, status,
		       from_address, from_name,
		       to_addresses, cc_addresses, bcc_addresses,
		       reply_to, subject,
		       body_text_key, body_html_key, raw_key,
		       size_bytes, has_attachments, headers, metadata,
		       sent_at, received_at,
		       created_at, updated_at
		FROM messages
		WHERE org_id = $1 AND id = $2
	`
	row := tx.QueryRow(ctx, q, orgID, messageID)
	return scanMessage(row)
}

// List returns a cursor-paginated list of messages in a thread.
func (s *PostgresMessageStore) List(ctx context.Context, tx pgx.Tx, orgID, threadID uuid.UUID, limit int, cursor string) ([]*models.Message, string, error) {
	limit = pagination.ClampLimit(limit)

	args := []any{orgID, threadID}
	argIdx := 3
	where := "org_id = $1 AND thread_id = $2"

	if cursor != "" {
		parts, err := pagination.DecodeCursor(cursor)
		if err != nil || len(parts) < 2 {
			return nil, "", fmt.Errorf("invalid cursor")
		}
		where += fmt.Sprintf(" AND (created_at, id) > ($%d::timestamptz, $%d::uuid)", argIdx, argIdx+1)
		args = append(args, parts[0], parts[1])
		argIdx += 2
	}

	args = append(args, limit+1)
	q := fmt.Sprintf(`
		SELECT id, org_id, inbox_id, thread_id,
		       message_id_header, in_reply_to, references_header,
		       direction, status,
		       from_address, from_name,
		       to_addresses, cc_addresses, bcc_addresses,
		       reply_to, subject,
		       body_text_key, body_html_key, raw_key,
		       size_bytes, has_attachments, headers, metadata,
		       sent_at, received_at,
		       created_at, updated_at
		FROM messages
		WHERE %s
		ORDER BY created_at ASC, id ASC
		LIMIT $%d
	`, where, argIdx)

	rows, err := tx.Query(ctx, q, args...)
	if err != nil {
		return nil, "", fmt.Errorf("list messages: %w", err)
	}
	defer rows.Close()

	var msgs []*models.Message
	for rows.Next() {
		m, err := scanMessageRows(rows)
		if err != nil {
			return nil, "", err
		}
		msgs = append(msgs, m)
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("iterate messages: %w", err)
	}

	var nextCursor string
	if len(msgs) > limit {
		msgs = msgs[:limit]
		last := msgs[len(msgs)-1]
		nextCursor = pagination.EncodeCursor(last.CreatedAt.Format(time.RFC3339Nano), last.ID.String())
	}

	return msgs, nextCursor, nil
}

// UpdateStatus updates the delivery status of a message.
func (s *PostgresMessageStore) UpdateStatus(ctx context.Context, tx pgx.Tx, orgID, messageID uuid.UUID, status models.MessageStatus) error {
	q := `UPDATE messages SET status = $3, updated_at = NOW() WHERE org_id = $1 AND id = $2`
	_, err := tx.Exec(ctx, q, orgID, messageID, string(status))
	if err != nil {
		return fmt.Errorf("update message status: %w", err)
	}
	return nil
}

// CreateAttachment inserts a new attachment record.
func (s *PostgresMessageStore) CreateAttachment(ctx context.Context, tx pgx.Tx, att *models.Attachment) error {
	q := `
		INSERT INTO attachments (id, org_id, message_id, filename, content_type, size_bytes, s3_key, content_id, is_inline, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`
	_, err := tx.Exec(ctx, q,
		att.ID, att.OrgID, att.MessageID,
		att.Filename, att.ContentType,
		att.SizeBytes, att.S3Key,
		att.ContentID, att.Inline,
		att.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert attachment: %w", err)
	}
	return nil
}

// ListAttachments returns all attachments for a message.
func (s *PostgresMessageStore) ListAttachments(ctx context.Context, tx pgx.Tx, orgID, messageID uuid.UUID) ([]*models.Attachment, error) {
	q := `
		SELECT id, org_id, message_id, filename, content_type, size_bytes, s3_key, content_id, is_inline, created_at
		FROM attachments
		WHERE org_id = $1 AND message_id = $2
		ORDER BY created_at ASC
	`
	rows, err := tx.Query(ctx, q, orgID, messageID)
	if err != nil {
		return nil, fmt.Errorf("list attachments: %w", err)
	}
	defer rows.Close()

	var atts []*models.Attachment
	for rows.Next() {
		var a models.Attachment
		if err := rows.Scan(&a.ID, &a.OrgID, &a.MessageID, &a.Filename, &a.ContentType, &a.SizeBytes, &a.S3Key, &a.ContentID, &a.Inline, &a.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan attachment: %w", err)
		}
		atts = append(atts, &a)
	}
	return atts, rows.Err()
}

func scanMessage(row pgx.Row) (*models.Message, error) {
	var m models.Message
	var toJSON, ccJSON, bccJSON, headersJSON, metadataJSON []byte
	err := row.Scan(
		&m.ID, &m.OrgID, &m.InboxID, &m.ThreadID,
		&m.MessageID, &m.InReplyTo, &m.References,
		&m.Direction, &m.Status,
		&m.From.Email, &m.From.Name,
		&toJSON, &ccJSON, &bccJSON,
		&m.ReplyTo, &m.Subject,
		&m.TextS3Key, &m.HtmlS3Key, &m.RawS3Key,
		&m.SizeBytes, &m.HasAttachments, &headersJSON, &metadataJSON,
		&m.SentAt, &m.ReceivedAt,
		&m.CreatedAt, &m.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scan message: %w", err)
	}
	_ = json.Unmarshal(toJSON, &m.To)
	_ = json.Unmarshal(ccJSON, &m.Cc)
	_ = json.Unmarshal(bccJSON, &m.Bcc)
	_ = json.Unmarshal(headersJSON, &m.Headers)
	_ = json.Unmarshal(metadataJSON, &m.Metadata)
	return &m, nil
}

func scanMessageRows(rows pgx.Rows) (*models.Message, error) {
	var m models.Message
	var toJSON, ccJSON, bccJSON, headersJSON, metadataJSON []byte
	err := rows.Scan(
		&m.ID, &m.OrgID, &m.InboxID, &m.ThreadID,
		&m.MessageID, &m.InReplyTo, &m.References,
		&m.Direction, &m.Status,
		&m.From.Email, &m.From.Name,
		&toJSON, &ccJSON, &bccJSON,
		&m.ReplyTo, &m.Subject,
		&m.TextS3Key, &m.HtmlS3Key, &m.RawS3Key,
		&m.SizeBytes, &m.HasAttachments, &headersJSON, &metadataJSON,
		&m.SentAt, &m.ReceivedAt,
		&m.CreatedAt, &m.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scan message rows: %w", err)
	}
	_ = json.Unmarshal(toJSON, &m.To)
	_ = json.Unmarshal(ccJSON, &m.Cc)
	_ = json.Unmarshal(bccJSON, &m.Bcc)
	_ = json.Unmarshal(headersJSON, &m.Headers)
	_ = json.Unmarshal(metadataJSON, &m.Metadata)
	return &m, nil
}
