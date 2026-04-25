/*
 * Copyright (c) 2026 nyklabs.com. All rights reserved.
 *
 * Licensed under the nGX Commercial Source License v1.0.
 * See LICENSE file in the project root for full license information.
 */

package main

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"time"

	awsevents "github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	awscfg "github.com/aws/aws-sdk-go-v2/config"
	sqssdk "github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"agentmail/lambdas/shared"
	dbpkg "agentmail/pkg/db"
	domainevents "agentmail/pkg/events"
	mimepkg "agentmail/pkg/mime"
	"agentmail/pkg/models"
	s3pkg "agentmail/pkg/s3"
	sqspkg "agentmail/pkg/sqs"
	emailstore "agentmail/services/email-pipeline/store"
)

var (
	pool      *pgxpool.Pool
	emailSt   *emailstore.EmailStore
	emailsS3  *s3pkg.Client
	publisher *sqspkg.Publisher
)

func init() {
	ctx := context.Background()
	pool = shared.InitDB()
	emailSt = emailstore.NewEmailStore(pool)

	var err error
	emailsS3, err = s3pkg.NewFromAWS(ctx, os.Getenv("S3_BUCKET_EMAILS"))
	if err != nil {
		slog.Error("email_inbound: init S3 client", "error", err)
		os.Exit(1)
	}

	awsConf, err := awscfg.LoadDefaultConfig(ctx)
	if err != nil {
		slog.Error("email_inbound: load AWS config", "error", err)
		os.Exit(1)
	}
	sqsClient := sqssdk.NewFromConfig(awsConf)
	publisher = sqspkg.NewPublisher(sqsClient)
}

func handler(ctx context.Context, s3Event awsevents.S3Event) error {
	for _, record := range s3Event.Records {
		// S3 keys are URL-encoded.
		key, err := url.QueryUnescape(record.S3.Object.Key)
		if err != nil {
			key = record.S3.Object.Key
		}
		if err := processS3Object(ctx, key); err != nil {
			slog.Error("email_inbound: process S3 object", "key", key, "error", err)
			return err // return error so Lambda retries
		}
	}
	return nil
}

func processS3Object(ctx context.Context, s3Key string) error {
	rawData, err := emailsS3.Download(ctx, s3Key)
	if err != nil {
		return fmt.Errorf("download from S3 %s: %w", s3Key, err)
	}

	parsed, err := mimepkg.Parse(bytes.NewReader(rawData))
	if err != nil {
		slog.Warn("email_inbound: MIME parse error, creating minimal record", "s3_key", s3Key, "error", err)
		parsed = &mimepkg.ParsedEmail{}
	}

	// Extract recipients from To + CC headers.
	var recipients []mimepkg.EmailAddress
	recipients = append(recipients, parsed.To...)
	recipients = append(recipients, parsed.CC...)

	if len(recipients) == 0 {
		slog.Warn("email_inbound: no recipients found in headers, skipping", "s3_key", s3Key)
		return nil
	}

	for _, rcpt := range recipients {
		if err := processForRecipient(ctx, s3Key, rawData, parsed, rcpt); err != nil {
			slog.Error("email_inbound: process for recipient",
				"recipient", rcpt.Email, "s3_key", s3Key, "error", err)
		}
	}
	return nil
}

func processForRecipient(
	ctx context.Context,
	rawS3Key string,
	rawData []byte,
	parsed *mimepkg.ParsedEmail,
	recipient mimepkg.EmailAddress,
) error {
	inbox, err := emailSt.GetInboxByAddress(ctx, recipient.Email)
	if err != nil {
		if dbpkg.IsNotFound(err) {
			slog.Debug("email_inbound: no inbox for recipient, skipping", "recipient", recipient.Email)
			return nil
		}
		return fmt.Errorf("lookup inbox for %s: %w", recipient.Email, err)
	}

	msgID := uuid.New()
	now := time.Now().UTC()

	podSegment := "no-pod"
	if inbox.PodID != nil {
		podSegment = inbox.PodID.String()
	}
	prefix := fmt.Sprintf("%s/%s/%s", inbox.OrgID, podSegment, inbox.ID)

	// Upload text body.
	var textKey string
	if len(parsed.BodyText) > 0 {
		textKey = fmt.Sprintf("%s/text/%s.txt", prefix, msgID)
		if err := emailsS3.Upload(ctx, textKey, parsed.BodyText, "text/plain; charset=utf-8"); err != nil {
			slog.Warn("email_inbound: upload text body", "error", err)
			textKey = ""
		}
	}

	// Upload HTML body.
	var htmlKey string
	if len(parsed.BodyHTML) > 0 {
		htmlKey = fmt.Sprintf("%s/html/%s.html", prefix, msgID)
		if err := emailsS3.Upload(ctx, htmlKey, parsed.BodyHTML, "text/html; charset=utf-8"); err != nil {
			slog.Warn("email_inbound: upload html body", "error", err)
			htmlKey = ""
		}
	}

	snippet := buildSnippet(parsed.BodyText, 200)

	// Upload attachments before the transaction.
	type attUpload struct {
		model *models.Attachment
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
		if err := emailsS3.Upload(ctx, attKey, part.Data, part.ContentType); err != nil {
			slog.Warn("email_inbound: upload attachment", "filename", filename, "error", err)
			continue
		}
		attUploads = append(attUploads, attUpload{model: &models.Attachment{
			ID:          uuid.New(),
			OrgID:       inbox.OrgID,
			MessageID:   &msgID,
			Filename:    filename,
			ContentType: part.ContentType,
			SizeBytes:   int64(len(part.Data)),
			S3Key:       attKey,
			ContentID:   part.ContentID,
			Inline:      part.IsInline,
			CreatedAt:   now,
		}})
	}

	var message *models.Message
	var isNewThread bool

	err = dbpkg.WithOrgTx(ctx, pool, inbox.OrgID, func(tx pgx.Tx) error {
		// Thread deduplication via In-Reply-To / References.
		var thread *models.Thread
		lookupIDs := make([]string, 0, 1+len(parsed.References))
		if parsed.InReplyTo != "" {
			lookupIDs = append(lookupIDs, parsed.InReplyTo)
		}
		lookupIDs = append(lookupIDs, parsed.References...)

		if len(lookupIDs) > 0 {
			found, findErr := emailSt.FindThreadByMessageIDs(ctx, tx, inbox.OrgID, inbox.ID, lookupIDs)
			if findErr != nil && !dbpkg.IsNotFound(findErr) {
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
			if err := emailSt.CreateThread(ctx, tx, thread); err != nil {
				return fmt.Errorf("create thread: %w", err)
			}
		}

		hdrs := parsed.Headers
		if hdrs == nil {
			hdrs = make(map[string][]string)
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
			RawS3Key:   rawS3Key,
			SizeBytes:      int64(len(rawData)),
			Headers:        hdrs,
			HasAttachments: len(attUploads) > 0,
			ReceivedAt:     &now,
			CreatedAt:      now,
			UpdatedAt:      now,
		}

		if err := emailSt.CreateMessage(ctx, tx, message); err != nil {
			return fmt.Errorf("create message: %w", err)
		}

		for _, a := range attUploads {
			if err := emailSt.CreateAttachment(ctx, tx, a.model); err != nil {
				slog.Warn("email_inbound: insert attachment record", "filename", a.model.Filename, "error", err)
			}
		}

		if err := emailSt.IncrThreadMessageCount(ctx, tx, thread.ID, now, snippet); err != nil {
			return err
		}

		if !isNewThread {
			newParts := buildParticipants(parsed)
			if err := emailSt.MergeThreadParticipants(ctx, tx, thread.ID, newParts); err != nil {
				slog.Warn("email_inbound: merge thread participants", "thread_id", thread.ID, "error", err)
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("db transaction: %w", err)
	}

	// Publish events.
	if pubErr := publisher.PublishEvent(ctx, &domainevents.MessageReceivedEvent{
		BaseEvent: domainevents.NewBase(domainevents.EventMessageReceived, inbox.OrgID),
		Data: domainevents.MessageReceivedData{
			MessageID: message.ID.String(),
			InboxID:   inbox.ID,
			ThreadID:  message.ThreadID,
			From:      parsed.From.Email,
			Subject:   parsed.Subject,
			RawS3Key:  rawS3Key,
		},
	}); pubErr != nil {
		slog.Error("email_inbound: publish MessageReceived", "message_id", msgID, "error", pubErr)
	}

	if isNewThread {
		if pubErr := publisher.PublishEvent(ctx, &domainevents.ThreadCreatedEvent{
			BaseEvent: domainevents.NewBase(domainevents.EventThreadCreated, inbox.OrgID),
			Data: domainevents.ThreadCreatedData{
				ThreadID:  message.ThreadID,
				InboxID:   inbox.ID,
				Subject:   parsed.Subject,
				MessageID: message.ID.String(),
			},
		}); pubErr != nil {
			slog.Error("email_inbound: publish ThreadCreated", "thread_id", message.ThreadID, "error", pubErr)
		}
	}

	return nil
}

// buildSnippet trims and truncates body text for use as a preview.
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

func main() {
	lambda.Start(handler)
}
