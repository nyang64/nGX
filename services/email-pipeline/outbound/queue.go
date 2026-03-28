package outbound

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	kafkago "github.com/segmentio/kafka-go"

	"agentmail/pkg/db"
	"agentmail/pkg/events"
	"agentmail/pkg/kafka"
	"agentmail/pkg/models"
	"agentmail/services/email-pipeline/store"
)

// OutboundJob is the payload published onto the email.outbound.queue topic by
// the inbox service. Only a subset of fields is strictly required (MessageID,
// OrgID, InboxID) — the pipeline loads the authoritative message record from
// the DB. Inline body fields (BodyText, BodyHTML) are used as a fallback when
// the message was created without S3 body keys.
// NOTE: BCC is intentionally absent — the pipeline reads it from the DB message
// record to avoid persisting BCC recipients in a durable Kafka log.
type OutboundJob struct {
	MessageID  string `json:"message_id"`
	OrgID      string `json:"org_id"`
	InboxID    string `json:"inbox_id"`
	BodyText   string `json:"body_text"`
	BodyHTML   string `json:"body_html"`
}

// QueueConsumer reads outbound send jobs from Kafka and delivers them via Sender.
type QueueConsumer struct {
	consumer   *kafka.Consumer
	sender     *Sender
	emailStore *store.EmailStore
	pool       *pgxpool.Pool
	producer   *kafka.Producer
}

// NewQueueConsumer wires a QueueConsumer ready to consume from the outbound queue topic.
func NewQueueConsumer(
	brokers []string,
	groupID string,
	sender *Sender,
	emailStore *store.EmailStore,
	pool *pgxpool.Pool,
	producer *kafka.Producer,
) *QueueConsumer {
	return &QueueConsumer{
		consumer:   kafka.NewConsumer(brokers, kafka.TopicEmailOutboundQueue, groupID),
		sender:     sender,
		emailStore: emailStore,
		pool:       pool,
		producer:   producer,
	}
}

// Start begins consuming from Kafka. It blocks until ctx is cancelled.
func (q *QueueConsumer) Start(ctx context.Context) error {
	return q.consumer.Consume(ctx, func(ctx context.Context, msg kafkago.Message) error {
		var job OutboundJob
		if err := json.Unmarshal(msg.Value, &job); err != nil {
			slog.Error("failed to unmarshal outbound job", "error", err)
			return nil // skip malformed messages
		}
		if err := q.process(ctx, job); err != nil {
			slog.Error("failed to process outbound job",
				"message_id", job.MessageID,
				"error", err,
			)
			return err // re-deliver on transient failures
		}
		return nil
	})
}

// Close shuts down the underlying Kafka consumer.
func (q *QueueConsumer) Close() error {
	return q.consumer.Close()
}

// process loads the message from the DB, delivers it via Sender, and updates status.
func (q *QueueConsumer) process(ctx context.Context, job OutboundJob) error {
	orgID, err := uuid.Parse(job.OrgID)
	if err != nil {
		return fmt.Errorf("parse org_id: %w", err)
	}
	messageID, err := uuid.Parse(job.MessageID)
	if err != nil {
		return fmt.Errorf("parse message_id: %w", err)
	}

	// Load message within an RLS-scoped transaction.
	var msg *models.Message
	if err := db.WithOrgTx(ctx, q.pool, orgID, func(tx pgx.Tx) error {
		m, err := q.emailStore.GetMessageByID(ctx, tx, orgID, messageID)
		if err != nil {
			return fmt.Errorf("get message: %w", err)
		}
		msg = m
		// Mark as sending immediately to prevent duplicate sends.
		return q.emailStore.UpdateMessageStatus(ctx, tx, messageID, models.MessageStatusSending)
	}); err != nil {
		return fmt.Errorf("load message: %w", err)
	}

	// Build per-role address lists for MIME headers and SMTP RCPT TO.
	toAddrs := emailAddrsToStrings(msg.To)
	ccAddrs := emailAddrsToStrings(msg.Cc)
	bccAddrs := emailAddrsToStrings(msg.Bcc) // for RCPT TO only; not in MIME

	// Load attachments for this message.
	var attRefs []AttachmentRef
	atts, err := q.emailStore.GetAttachmentsByMessageID(ctx, msg.OrgID, msg.ID)
	if err != nil {
		slog.Warn("could not load attachments for outbound message",
			"message_id", messageID, "error", err)
	}
	for _, a := range atts {
		attRefs = append(attRefs, AttachmentRef{
			S3Key:       a.S3Key,
			Filename:    a.Filename,
			ContentType: a.ContentType,
			ContentID:   a.ContentID,
			Inline:      a.Inline,
		})
	}

	// Use DB S3 keys when available; fall back to inline body from the Kafka job.
	textS3Key := msg.TextS3Key
	htmlS3Key := msg.HtmlS3Key
	bodyText := ""
	bodyHTML := ""
	if textS3Key == "" {
		bodyText = job.BodyText
	}
	if htmlS3Key == "" {
		bodyHTML = job.BodyHTML
	}

	sendJob := SendJob{
		MessageID:   msg.ID,
		OrgID:       msg.OrgID,
		InboxID:     msg.InboxID,
		From:        msg.From.Email,
		To:          toAddrs,
		Cc:          ccAddrs,
		Bcc:         bccAddrs,
		Subject:     msg.Subject,
		ReplyTo:     msg.ReplyTo,
		InReplyTo:   msg.InReplyTo,
		References:  msg.References,
		TextS3Key:   textS3Key,
		HtmlS3Key:   htmlS3Key,
		BodyText:    bodyText,
		BodyHTML:    bodyHTML,
		Headers:     msg.Headers,
		Attachments: attRefs,
	}

	// Attempt delivery.
	sendErr := q.sender.Send(ctx, sendJob)
	now := time.Now().UTC()

	if sendErr != nil {
		slog.Error("outbound delivery failed",
			"message_id", messageID,
			"error", sendErr,
		)
		// Mark as failed in the DB.
		_ = db.WithOrgTx(ctx, q.pool, orgID, func(tx pgx.Tx) error {
			return q.emailStore.UpdateMessageStatus(ctx, tx, messageID, models.MessageStatusFailed)
		})
		// Publish bounce event for observability.
		q.publishBounceEvent(ctx, msg, sendErr.Error())
		return sendErr
	}

	// Mark as sent.
	if err := db.WithOrgTx(ctx, q.pool, orgID, func(tx pgx.Tx) error {
		return q.emailStore.UpdateMessageSentAt(ctx, tx, messageID, now)
	}); err != nil {
		slog.Error("failed to update message sent_at", "message_id", messageID, "error", err)
	}

	// Publish message.sent event.
	toStrs := make([]string, len(msg.To))
	for i, a := range msg.To {
		toStrs[i] = a.Email
	}
	evt := &events.MessageSentEvent{
		BaseEvent: events.NewBase(events.EventMessageSent, msg.OrgID),
		Data: events.MessageSentData{
			MessageID: msg.ID.String(),
			InboxID:   msg.InboxID,
			ThreadID:  msg.ThreadID,
			To:        toStrs,
			Subject:   msg.Subject,
		},
	}
	if err := q.producer.PublishEvent(ctx, evt); err != nil {
		slog.Error("failed to publish message.sent event", "message_id", messageID, "error", err)
	}

	slog.Info("outbound message delivered",
		"message_id", messageID,
		"to", toAddrs,
	)
	return nil
}

// emailAddrsToStrings extracts the email address string from each EmailAddress.
func emailAddrsToStrings(addrs []models.EmailAddress) []string {
	out := make([]string, 0, len(addrs))
	for _, a := range addrs {
		if a.Email != "" {
			out = append(out, a.Email)
		}
	}
	return out
}

func (q *QueueConsumer) publishBounceEvent(ctx context.Context, msg *models.Message, reason string) {
	evt := &events.MessageBouncedEvent{
		BaseEvent: events.NewBase(events.EventMessageBounced, msg.OrgID),
		Data: events.MessageBouncedData{
			MessageID:    msg.ID.String(),
			InboxID:      msg.InboxID,
			ThreadID:     msg.ThreadID,
			BounceCode:   "500",
			BounceReason: reason,
		},
	}
	if err := q.producer.PublishEvent(ctx, evt); err != nil {
		slog.Error("failed to publish message.bounced event", "message_id", msg.ID, "error", err)
	}
}
