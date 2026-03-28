package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"agentmail/pkg/models"
)

// EmailStore provides DB operations needed by the email pipeline.
// GetInboxByAddress bypasses RLS (called before the org is known).
// All other methods use the RLS-scoped transaction provided by the caller.
type EmailStore struct {
	pool *pgxpool.Pool
}

// NewEmailStore creates a new EmailStore backed by pool.
func NewEmailStore(pool *pgxpool.Pool) *EmailStore {
	return &EmailStore{pool: pool}
}

// GetInboxByAddress looks up an inbox by its email address without RLS,
// because the org is not yet known when an inbound message arrives.
func (s *EmailStore) GetInboxByAddress(ctx context.Context, address string) (*models.Inbox, error) {
	const query = `
		SELECT id, org_id, pod_id, address, display_name, status, settings, created_at, updated_at
		FROM inboxes
		WHERE address = $1
		  AND status = 'active'`

	inbox := &models.Inbox{}
	row := s.pool.QueryRow(ctx, query, address)
	if err := row.Scan(
		&inbox.ID,
		&inbox.OrgID,
		&inbox.PodID,
		&inbox.Email,
		&inbox.DisplayName,
		&inbox.Status,
		&inbox.Settings,
		&inbox.CreatedAt,
		&inbox.UpdatedAt,
	); err != nil {
		return nil, err // caller checks db.IsNotFound
	}
	return inbox, nil
}

// FindThreadByMessageIDs returns the first thread whose messages contain any of
// the given Message-ID header values. Uses the RLS-scoped transaction.
func (s *EmailStore) FindThreadByMessageIDs(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, inboxID uuid.UUID, msgIDs []string) (*models.Thread, error) {
	const query = `
		SELECT t.id, t.org_id, t.inbox_id, t.subject, t.snippet, t.status,
		       t.is_read, t.is_starred, t.message_count, t.participants,
		       t.last_message_at, t.created_at, t.updated_at
		FROM threads t
		JOIN messages m ON m.thread_id = t.id
		WHERE t.org_id  = $1
		  AND t.inbox_id = $2
		  AND m.message_id_header = ANY($3)
		LIMIT 1`

	thread := &models.Thread{}
	var participantsJSON []byte
	row := tx.QueryRow(ctx, query, orgID, inboxID, msgIDs)
	if err := row.Scan(
		&thread.ID,
		&thread.OrgID,
		&thread.InboxID,
		&thread.Subject,
		&thread.Snippet,
		&thread.Status,
		&thread.IsRead,
		&thread.IsStarred,
		&thread.MessageCount,
		&participantsJSON,
		&thread.LastMessageAt,
		&thread.CreatedAt,
		&thread.UpdatedAt,
	); err != nil {
		return nil, err // caller checks db.IsNotFound
	}
	if err := json.Unmarshal(participantsJSON, &thread.Participants); err != nil {
		thread.Participants = nil
	}
	return thread, nil
}

// CreateThread inserts a new thread record within the provided transaction.
func (s *EmailStore) CreateThread(ctx context.Context, tx pgx.Tx, thread *models.Thread) error {
	participantsJSON, err := json.Marshal(thread.Participants)
	if err != nil {
		participantsJSON = []byte("[]")
	}

	const query = `
		INSERT INTO threads
		    (id, org_id, inbox_id, subject, snippet, status,
		     is_read, is_starred, message_count, participants,
		     last_message_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6,
		        $7, $8, $9, $10,
		        $11, $12, $13)`

	_, err = tx.Exec(ctx, query,
		thread.ID,
		thread.OrgID,
		thread.InboxID,
		thread.Subject,
		thread.Snippet,
		string(thread.Status),
		thread.IsRead,
		thread.IsStarred,
		thread.MessageCount,
		participantsJSON,
		thread.LastMessageAt,
		thread.CreatedAt,
		thread.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert thread: %w", err)
	}
	return nil
}

// CreateMessage inserts a new message record within the provided transaction.
// The Message model uses EmailAddress structs for From/To/Cc/Bcc — these are
// stored as JSONB arrays / a JSONB object in the DB.
func (s *EmailStore) CreateMessage(ctx context.Context, tx pgx.Tx, msg *models.Message) error {
	toJSON, err := json.Marshal(msg.To)
	if err != nil {
		toJSON = []byte("[]")
	}
	ccJSON, err := json.Marshal(msg.Cc)
	if err != nil {
		ccJSON = []byte("[]")
	}
	bccJSON, err := json.Marshal(msg.Bcc)
	if err != nil {
		bccJSON = []byte("[]")
	}
	headersJSON, err := json.Marshal(msg.Headers)
	if err != nil {
		headersJSON = []byte("{}")
	}
	metadataJSON, err := json.Marshal(msg.Metadata)
	if err != nil || msg.Metadata == nil {
		metadataJSON = []byte("{}")
	}

	const query = `
		INSERT INTO messages
		    (id, org_id, inbox_id, thread_id,
		     message_id_header, in_reply_to, references_header,
		     direction, status,
		     from_address, from_name,
		     to_addresses, cc_addresses, bcc_addresses,
		     reply_to, subject,
		     body_text_key, body_html_key, raw_key,
		     size_bytes, has_attachments,
		     headers, metadata,
		     sent_at, received_at,
		     created_at, updated_at)
		VALUES
		    ($1,  $2,  $3,  $4,
		     $5,  $6,  $7,
		     $8,  $9,
		     $10, $11,
		     $12, $13, $14,
		     $15, $16,
		     $17, $18, $19,
		     $20, $21,
		     $22, $23,
		     $24, $25,
		     $26, $27)`

	_, err = tx.Exec(ctx, query,
		msg.ID,
		msg.OrgID,
		msg.InboxID,
		msg.ThreadID,
		msg.MessageID,
		msg.InReplyTo,
		msg.References,
		string(msg.Direction),
		string(msg.Status),
		msg.From.Email,
		msg.From.Name,
		toJSON,
		ccJSON,
		bccJSON,
		msg.ReplyTo,
		msg.Subject,
		msg.TextS3Key,
		msg.HtmlS3Key,
		msg.RawS3Key,
		msg.SizeBytes,
		len(msg.Attachments) > 0,
		headersJSON,
		metadataJSON,
		msg.SentAt,
		msg.ReceivedAt,
		msg.CreatedAt,
		msg.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert message: %w", err)
	}
	return nil
}

// CreateAttachment inserts an attachment record within the provided transaction.
func (s *EmailStore) CreateAttachment(ctx context.Context, tx pgx.Tx, att *models.Attachment) error {
	const query = `
		INSERT INTO attachments
		    (id, org_id, message_id, filename, content_type,
		     size_bytes, s3_key, content_id, is_inline, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`

	_, err := tx.Exec(ctx, query,
		att.ID,
		att.OrgID,
		att.MessageID,
		att.Filename,
		att.ContentType,
		att.SizeBytes,
		att.S3Key,
		att.ContentID,
		att.Inline,
		att.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert attachment: %w", err)
	}
	return nil
}

// MergeThreadParticipants appends any new participants to the thread's
// participants JSONB array, deduplicating by email address.
func (s *EmailStore) MergeThreadParticipants(ctx context.Context, tx pgx.Tx, threadID uuid.UUID, newParticipants []models.EmailAddress) error {
	if len(newParticipants) == 0 {
		return nil
	}
	newJSON, err := json.Marshal(newParticipants)
	if err != nil {
		return fmt.Errorf("marshal participants: %w", err)
	}
	// Use PostgreSQL to merge the arrays, deduplicating on the email field.
	const query = `
		UPDATE threads
		SET participants = (
			SELECT jsonb_agg(DISTINCT elem ORDER BY elem->>'email')
			FROM (
				SELECT jsonb_array_elements(COALESCE(participants, '[]'::jsonb)) AS elem
				UNION
				SELECT jsonb_array_elements($2::jsonb)
			) sub
		),
		    updated_at   = NOW()
		WHERE id = $1`
	_, err = tx.Exec(ctx, query, threadID, newJSON)
	if err != nil {
		return fmt.Errorf("merge thread participants: %w", err)
	}
	return nil
}

// IncrThreadMessageCount atomically increments the thread's message_count,
// sets last_message_at, and updates the snippet.
func (s *EmailStore) IncrThreadMessageCount(ctx context.Context, tx pgx.Tx, threadID uuid.UUID, at time.Time, snippet string) error {
	const query = `
		UPDATE threads
		SET message_count  = message_count + 1,
		    last_message_at = $2,
		    snippet         = $3,
		    updated_at      = NOW()
		WHERE id = $1`

	tag, err := tx.Exec(ctx, query, threadID, at, snippet)
	if err != nil {
		return fmt.Errorf("update thread message count: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("thread not found: %s", threadID)
	}
	return nil
}

// GetMessageByID fetches a message by its ID using the RLS-scoped transaction.
func (s *EmailStore) GetMessageByID(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, messageID uuid.UUID) (*models.Message, error) {
	const query = `
		SELECT id, org_id, inbox_id, thread_id,
		       message_id_header, in_reply_to, references_header,
		       direction, status,
		       from_address, from_name,
		       to_addresses, cc_addresses, bcc_addresses,
		       reply_to, subject,
		       body_text_key, body_html_key, raw_key,
		       size_bytes,
		       headers, metadata,
		       sent_at, received_at,
		       created_at, updated_at
		FROM messages
		WHERE id = $1
		  AND org_id = $2`

	msg := &models.Message{}
	var toJSON, ccJSON, bccJSON, headersJSON, metadataJSON []byte
	var fromAddress, fromName string

	row := tx.QueryRow(ctx, query, messageID, orgID)
	if err := row.Scan(
		&msg.ID,
		&msg.OrgID,
		&msg.InboxID,
		&msg.ThreadID,
		&msg.MessageID,
		&msg.InReplyTo,
		&msg.References,
		&msg.Direction,
		&msg.Status,
		&fromAddress,
		&fromName,
		&toJSON,
		&ccJSON,
		&bccJSON,
		&msg.ReplyTo,
		&msg.Subject,
		&msg.TextS3Key,
		&msg.HtmlS3Key,
		&msg.RawS3Key,
		&msg.SizeBytes,
		&headersJSON,
		&metadataJSON,
		&msg.SentAt,
		&msg.ReceivedAt,
		&msg.CreatedAt,
		&msg.UpdatedAt,
	); err != nil {
		return nil, err
	}

	msg.From = models.EmailAddress{Email: fromAddress, Name: fromName}

	if err := json.Unmarshal(toJSON, &msg.To); err != nil {
		msg.To = nil
	}
	if err := json.Unmarshal(ccJSON, &msg.Cc); err != nil {
		msg.Cc = nil
	}
	if err := json.Unmarshal(bccJSON, &msg.Bcc); err != nil {
		msg.Bcc = nil
	}
	if err := json.Unmarshal(headersJSON, &msg.Headers); err != nil {
		msg.Headers = nil
	}
	if err := json.Unmarshal(metadataJSON, &msg.Metadata); err != nil {
		msg.Metadata = nil
	}

	return msg, nil
}

// UpdateMessageStatus updates a message's status within an RLS-scoped transaction.
func (s *EmailStore) UpdateMessageStatus(ctx context.Context, tx pgx.Tx, messageID uuid.UUID, status models.MessageStatus) error {
	const query = `
		UPDATE messages
		SET status     = $2,
		    updated_at = NOW()
		WHERE id = $1`

	tag, err := tx.Exec(ctx, query, messageID, string(status))
	if err != nil {
		return fmt.Errorf("update message status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("message not found: %s", messageID)
	}
	return nil
}

// GetAttachmentsByMessageID returns all attachments for a message without RLS
// (called from the outbound pipeline where orgID is already known and the
// message record was loaded under RLS; a direct pool query is safe here).
func (s *EmailStore) GetAttachmentsByMessageID(ctx context.Context, orgID uuid.UUID, messageID uuid.UUID) ([]*models.Attachment, error) {
	const query = `
		SELECT id, org_id, message_id, filename, content_type,
		       size_bytes, s3_key, content_id, is_inline, created_at
		FROM attachments
		WHERE org_id = $1 AND message_id = $2
		ORDER BY created_at ASC`

	rows, err := s.pool.Query(ctx, query, orgID, messageID)
	if err != nil {
		return nil, fmt.Errorf("query attachments: %w", err)
	}
	defer rows.Close()

	var atts []*models.Attachment
	for rows.Next() {
		a := &models.Attachment{}
		if err := rows.Scan(
			&a.ID, &a.OrgID, &a.MessageID,
			&a.Filename, &a.ContentType,
			&a.SizeBytes, &a.S3Key,
			&a.ContentID, &a.Inline,
			&a.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan attachment: %w", err)
		}
		atts = append(atts, a)
	}
	return atts, rows.Err()
}

// UpdateMessageSentAt sets sent_at and status = 'sent' in one query.
func (s *EmailStore) UpdateMessageSentAt(ctx context.Context, tx pgx.Tx, messageID uuid.UUID, sentAt time.Time) error {
	const query = `
		UPDATE messages
		SET status     = 'sent',
		    sent_at    = $2,
		    updated_at = NOW()
		WHERE id = $1`

	tag, err := tx.Exec(ctx, query, messageID, sentAt)
	if err != nil {
		return fmt.Errorf("update message sent_at: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("message not found: %s", messageID)
	}
	return nil
}
