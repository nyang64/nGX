package inbound

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	kafkago "github.com/segmentio/kafka-go"

	"agentmail/pkg/db"
	"agentmail/pkg/events"
	"agentmail/pkg/kafka"
	mimepkg "agentmail/pkg/mime"
	"agentmail/pkg/models"
	"agentmail/pkg/s3"
	"agentmail/services/email-pipeline/emailauth"
	"agentmail/services/email-pipeline/store"
)

// InboundConsumer reads RawEmailJob messages from email.inbound.raw,
// parses the MIME structure, persists to Postgres, and publishes domain events.
type InboundConsumer struct {
	consumer   *kafka.Consumer
	pool       *pgxpool.Pool
	emailStore *store.EmailStore
	s3Client   *s3.Client
	producer   *kafka.Producer
}

// NewInboundConsumer wires an InboundConsumer ready to consume from email.inbound.raw.
func NewInboundConsumer(
	brokers []string,
	groupID string,
	pool *pgxpool.Pool,
	emailStore *store.EmailStore,
	s3Client *s3.Client,
	producer *kafka.Producer,
) *InboundConsumer {
	return &InboundConsumer{
		consumer:   kafka.NewConsumer(brokers, kafka.TopicEmailInboundRaw, groupID),
		pool:       pool,
		emailStore: emailStore,
		s3Client:   s3Client,
		producer:   producer,
	}
}

// Start begins consuming from email.inbound.raw. Blocks until ctx is cancelled.
func (c *InboundConsumer) Start(ctx context.Context) error {
	return c.consumer.Consume(ctx, func(ctx context.Context, msg kafkago.Message) error {
		var job RawEmailJob
		if err := json.Unmarshal(msg.Value, &job); err != nil {
			slog.Error("failed to unmarshal inbound job", "error", err)
			return nil // skip malformed messages — don't block the partition
		}
		if err := c.process(ctx, job); err != nil {
			slog.Error("failed to process inbound job",
				"job_id", job.JobID,
				"s3_key", job.S3Key,
				"error", err,
			)
			return err // return error to trigger Kafka re-delivery on transient failures
		}
		return nil
	})
}

// Close shuts down the underlying Kafka consumer.
func (c *InboundConsumer) Close() error {
	return c.consumer.Close()
}

// process handles one inbound job: download raw EML from S3, run DKIM+DMARC,
// parse, store, and publish domain events.
func (c *InboundConsumer) process(ctx context.Context, job RawEmailJob) error {
	rawData, err := c.s3Client.Download(ctx, job.S3Key)
	if err != nil {
		return fmt.Errorf("download raw email from S3: %w", err)
	}

	// --- DKIM verification ---
	dkimResult := emailauth.VerifyDKIM(rawData)
	slog.Info("DKIM check", "job_id", job.JobID, "result", dkimResult)

	parsed, err := mimepkg.Parse(bytes.NewReader(rawData))
	if err != nil {
		slog.Error("failed to parse MIME", "job_id", job.JobID, "error", err)
		parsed = &mimepkg.ParsedEmail{
			From: mimepkg.EmailAddress{Email: job.From},
		}
	}

	// --- DMARC enforcement (per From domain) ---
	fromDomain := emailauth.ExtractFromDomain(parsed.From.Email)
	spfResult := emailauth.SPFResult(job.SPFResult)

	authResults := emailauth.AuthResults{
		SPF:  spfResult,
		DKIM: dkimResult,
	}

	if fromDomain != "" {
		disposition := emailauth.CheckDMARC(fromDomain, spfResult, dkimResult)
		authResults.DMARC = disposition
		slog.Info("DMARC check", "job_id", job.JobID, "domain", fromDomain, "disposition", disposition)

		if disposition == emailauth.DMARCReject {
			slog.Warn("message rejected by DMARC policy",
				"job_id", job.JobID,
				"from", parsed.From.Email,
				"spf", spfResult,
				"dkim", dkimResult,
			)
			return nil // discard — commit the Kafka offset
		}
	}

	for _, rcpt := range job.To {
		if err := c.processForRecipient(ctx, job, rcpt, parsed, rawData, authResults); err != nil {
			slog.Error("failed to process for recipient",
				"job_id", job.JobID,
				"recipient", rcpt,
				"error", err,
			)
		}
	}
	return nil
}

func (c *InboundConsumer) processForRecipient(
	ctx context.Context,
	job RawEmailJob,
	recipient string,
	parsed *mimepkg.ParsedEmail,
	rawData []byte,
	auth emailauth.AuthResults,
) error {
	// Look up inbox by recipient address (no RLS — org unknown at this point).
	inbox, err := c.emailStore.GetInboxByAddress(ctx, recipient)
	if err != nil {
		if db.IsNotFound(err) {
			slog.Warn("no inbox found for recipient, discarding", "recipient", recipient)
			return nil
		}
		return fmt.Errorf("lookup inbox: %w", err)
	}

	msgID := uuid.New()

	podSegment := "no-pod"
	if inbox.PodID != nil {
		podSegment = inbox.PodID.String()
	}
	prefix := fmt.Sprintf("%s/%s/%s", inbox.OrgID, podSegment, inbox.ID)

	// The raw .eml is already in S3 from the enqueuer; record its key for the
	// message record but point at the per-inbox location.
	rawKey := job.S3Key

	// Store plain-text body.
	var textKey string
	if len(parsed.BodyText) > 0 {
		textKey = fmt.Sprintf("%s/text/%s.txt", prefix, msgID)
		if err := c.s3Client.Upload(ctx, textKey, parsed.BodyText, "text/plain; charset=utf-8"); err != nil {
			slog.Warn("failed to store text body", "error", err)
		}
	}

	// Store HTML body.
	var htmlKey string
	if len(parsed.BodyHTML) > 0 {
		htmlKey = fmt.Sprintf("%s/html/%s.html", prefix, msgID)
		if err := c.s3Client.Upload(ctx, htmlKey, parsed.BodyHTML, "text/html; charset=utf-8"); err != nil {
			slog.Warn("failed to store html body", "error", err)
		}
	}

	snippet := buildSnippet(parsed.BodyText, 200)
	now := time.Now().UTC()

	// Upload attachments before the transaction so we can reference keys inside it.
	type attUpload struct {
		model *models.Attachment
		s3Key string
	}
	var attUploads []attUpload
	for _, part := range parsed.Parts {
		if part.Filename == "" && !part.IsInline {
			continue
		}
		filename := part.Filename
		if filename == "" {
			filename = part.ContentID
		}
		attKey := fmt.Sprintf("%s/attachments/%s/%s", prefix, msgID, filename)
		if err := c.s3Client.Upload(ctx, attKey, part.Data, part.ContentType); err != nil {
			slog.Warn("failed to store attachment", "filename", filename, "error", err)
			continue
		}
		att := &models.Attachment{
			ID:          uuid.New(),
			OrgID:       inbox.OrgID,
			MessageID:   msgID,
			Filename:    filename,
			ContentType: part.ContentType,
			SizeBytes:   int64(len(part.Data)),
			S3Key:       attKey,
			ContentID:   part.ContentID,
			Inline:      part.IsInline,
			CreatedAt:   now,
		}
		attUploads = append(attUploads, attUpload{model: att, s3Key: attKey})
	}

	// Persist everything in one RLS-scoped transaction.
	var message *models.Message
	var isNewThread bool

	err = db.WithOrgTx(ctx, c.pool, inbox.OrgID, func(tx pgx.Tx) error {
		// Thread deduplication via In-Reply-To / References headers.
		var thread *models.Thread

		lookupIDs := make([]string, 0, 1+len(parsed.References))
		if parsed.InReplyTo != "" {
			lookupIDs = append(lookupIDs, parsed.InReplyTo)
		}
		lookupIDs = append(lookupIDs, parsed.References...)

		if len(lookupIDs) > 0 {
			found, findErr := c.emailStore.FindThreadByMessageIDs(ctx, tx, inbox.OrgID, inbox.ID, lookupIDs)
			if findErr != nil && !db.IsNotFound(findErr) {
				return fmt.Errorf("find thread: %w", findErr)
			}
			thread = found
		}

		if thread == nil {
			isNewThread = true
			thread = &models.Thread{
				ID:           uuid.New(),
				OrgID:        inbox.OrgID,
				InboxID:      inbox.ID,
				Subject:      parsed.Subject,
				Snippet:      snippet,
				Status:       models.ThreadStatusOpen,
				IsRead:       false,
				IsStarred:    false,
				MessageCount: 0,
				Participants: buildParticipants(parsed),
				CreatedAt:    now,
				UpdatedAt:    now,
			}
			if err := c.emailStore.CreateThread(ctx, tx, thread); err != nil {
				return fmt.Errorf("create thread: %w", err)
			}
		}

		// Merge auth results into the stored headers so they are visible via
		// the API without requiring a schema change.
		hdrs := parsed.Headers
		if hdrs == nil {
			hdrs = make(map[string][]string)
		}
		hdrs["X-nGX-SPF"] = []string{string(auth.SPF)}
		hdrs["X-nGX-DKIM"] = []string{string(auth.DKIM)}
		if auth.DMARC != "" {
			hdrs["X-nGX-DMARC"] = []string{string(auth.DMARC)}
		}
		if auth.DMARC == emailauth.DMARCQuarantine {
			hdrs["X-nGX-Spam"] = []string{"true"}
		}

		message = &models.Message{
			ID:         msgID,
			OrgID:      inbox.OrgID,
			InboxID:    inbox.ID,
			ThreadID:   thread.ID,
			MessageID:  parsed.MessageID,
			InReplyTo:  parsed.InReplyTo,
			References: parsed.References,
			Direction:  models.DirectionInbound,
			Status:     models.MessageStatusReceived,
			From:       models.EmailAddress{Email: parsed.From.Email, Name: parsed.From.Name},
			To:         convertAddresses(parsed.To),
			Cc:         convertAddresses(parsed.CC),
			ReplyTo:    parsed.ReplyTo,
			Subject:    parsed.Subject,
			Snippet:    snippet,
			TextS3Key:  textKey,
			HtmlS3Key:  htmlKey,
			RawS3Key:   rawKey,
			SizeBytes:  int64(len(rawData)),
			Headers:    hdrs,
			ReceivedAt: &now,
			CreatedAt:  now,
			UpdatedAt:  now,
		}
		if len(attUploads) > 0 {
			message.Attachments = make([]models.Attachment, len(attUploads))
		}

		if err := c.emailStore.CreateMessage(ctx, tx, message); err != nil {
			return fmt.Errorf("create message: %w", err)
		}

		for _, a := range attUploads {
			if err := c.emailStore.CreateAttachment(ctx, tx, a.model); err != nil {
				slog.Warn("failed to insert attachment record", "filename", a.model.Filename, "error", err)
			}
		}

		if err := c.emailStore.IncrThreadMessageCount(ctx, tx, thread.ID, now, snippet); err != nil {
			return err
		}

		// For replies to existing threads, merge any new participants in.
		if !isNewThread {
			newParts := buildParticipants(parsed)
			if err := c.emailStore.MergeThreadParticipants(ctx, tx, thread.ID, newParts); err != nil {
				slog.Warn("failed to merge thread participants", "thread_id", thread.ID, "error", err)
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("db transaction: %w", err)
	}

	// Publish domain events — non-fatal if Kafka is temporarily unavailable.
	evt := &events.MessageReceivedEvent{
		BaseEvent: events.NewBase(events.EventMessageReceived, inbox.OrgID),
		Data: events.MessageReceivedData{
			MessageID: message.ID.String(),
			InboxID:   inbox.ID,
			ThreadID:  message.ThreadID,
			From:      parsed.From.Email,
			Subject:   parsed.Subject,
			RawS3Key:  rawKey,
		},
	}
	if err := c.producer.PublishEvent(ctx, evt); err != nil {
		slog.Error("failed to publish message.received event", "message_id", msgID, "error", err)
	}

	if isNewThread {
		threadEvt := &events.ThreadCreatedEvent{
			BaseEvent: events.NewBase(events.EventThreadCreated, inbox.OrgID),
			Data: events.ThreadCreatedData{
				ThreadID:  message.ThreadID,
				InboxID:   inbox.ID,
				Subject:   parsed.Subject,
				MessageID: message.ID.String(),
			},
		}
		if err := c.producer.PublishEvent(ctx, threadEvt); err != nil {
			slog.Error("failed to publish thread.created event", "thread_id", message.ThreadID, "error", err)
		}
	}

	return nil
}

func buildSnippet(text []byte, maxLen int) string {
	s := strings.TrimSpace(string(text))
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", "")
	if len(s) > maxLen {
		return s[:maxLen]
	}
	return s
}

func convertAddresses(addrs []mimepkg.EmailAddress) []models.EmailAddress {
	result := make([]models.EmailAddress, len(addrs))
	for i, a := range addrs {
		result[i] = models.EmailAddress{Email: a.Email, Name: a.Name}
	}
	return result
}

func buildParticipants(parsed *mimepkg.ParsedEmail) []models.EmailAddress {
	seen := make(map[string]bool)
	var out []models.EmailAddress
	addAddr := func(a mimepkg.EmailAddress) {
		if a.Email != "" && !seen[a.Email] {
			seen[a.Email] = true
			out = append(out, models.EmailAddress{Email: a.Email, Name: a.Name})
		}
	}
	addAddr(parsed.From)
	for _, a := range parsed.To {
		addAddr(a)
	}
	for _, a := range parsed.CC {
		addAddr(a)
	}
	return out
}
