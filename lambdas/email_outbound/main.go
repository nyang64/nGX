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
	"encoding/json"
	"fmt"
	"log/slog"
	"net/mail"
	"os"
	"strings"
	"time"

	awsevents "github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	awscfg "github.com/aws/aws-sdk-go-v2/config"
	sesv2sdk "github.com/aws/aws-sdk-go-v2/service/sesv2"
	sesv2types "github.com/aws/aws-sdk-go-v2/service/sesv2/types"
	sqssdk "github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"agentmail/lambdas/shared"
	dbpkg "agentmail/pkg/db"
	domainevents "agentmail/pkg/events"
	"agentmail/pkg/models"
	s3pkg "agentmail/pkg/s3"
	sqspkg "agentmail/pkg/sqs"
	emailstore "agentmail/services/email-pipeline/store"
)

// outboundJob matches the JSON published to the email_outbound SQS queue.
type outboundJob struct {
	MessageID string `json:"message_id"`
	OrgID     string `json:"org_id"`
	InboxID   string `json:"inbox_id"`
	BodyText  string `json:"body_text"`
	BodyHTML  string `json:"body_html"`
}

var (
	pool      *pgxpool.Pool
	emailSt   *emailstore.EmailStore
	emailsS3  *s3pkg.Client
	sesClient *sesv2sdk.Client
	publisher *sqspkg.Publisher
)

func init() {
	ctx := context.Background()
	pool = shared.InitDB()
	emailSt = emailstore.NewEmailStore(pool)

	var err error
	emailsS3, err = s3pkg.NewFromAWS(ctx, os.Getenv("S3_BUCKET_EMAILS"))
	if err != nil {
		slog.Error("email_outbound: init S3 client", "error", err)
		os.Exit(1)
	}

	awsConf, err := awscfg.LoadDefaultConfig(ctx)
	if err != nil {
		slog.Error("email_outbound: load AWS config", "error", err)
		os.Exit(1)
	}
	sesClient = sesv2sdk.NewFromConfig(awsConf)
	sqsClient := sqssdk.NewFromConfig(awsConf)
	publisher = sqspkg.NewPublisher(sqsClient)
}

func handler(ctx context.Context, sqsEvent awsevents.SQSEvent) (awsevents.SQSEventResponse, error) {
	var failures []awsevents.SQSBatchItemFailure
	for _, record := range sqsEvent.Records {
		if err := processRecord(ctx, record); err != nil {
			slog.Error("email_outbound: process failed", "id", record.MessageId, "error", err)
			failures = append(failures, awsevents.SQSBatchItemFailure{ItemIdentifier: record.MessageId})
		}
	}
	return awsevents.SQSEventResponse{BatchItemFailures: failures}, nil
}

func processRecord(ctx context.Context, record awsevents.SQSMessage) error {
	var job outboundJob
	if err := json.Unmarshal([]byte(record.Body), &job); err != nil {
		slog.Warn("email_outbound: unmarshal job, skipping", "error", err)
		return nil
	}

	messageID, err := uuid.Parse(job.MessageID)
	if err != nil {
		slog.Warn("email_outbound: invalid message_id, skipping", "raw", job.MessageID)
		return nil
	}
	orgID, err := uuid.Parse(job.OrgID)
	if err != nil {
		slog.Warn("email_outbound: invalid org_id, skipping", "raw", job.OrgID)
		return nil
	}

	var msg *models.Message
	err = dbpkg.WithOrgTx(ctx, pool, orgID, func(tx pgx.Tx) error {
		var loadErr error
		msg, loadErr = emailSt.GetMessageByID(ctx, tx, orgID, messageID)
		if loadErr != nil {
			return fmt.Errorf("load message: %w", loadErr)
		}
		return emailSt.UpdateMessageStatus(ctx, tx, messageID, models.MessageStatusSending)
	})
	if err != nil {
		return err
	}

	// Load body from S3 or fall back to inline values from the job.
	bodyText := job.BodyText
	bodyHTML := job.BodyHTML
	if msg.TextS3Key != "" {
		if data, err := emailsS3.Download(ctx, msg.TextS3Key); err == nil {
			bodyText = string(data)
		}
	}
	if msg.HtmlS3Key != "" {
		if data, err := emailsS3.Download(ctx, msg.HtmlS3Key); err == nil {
			bodyHTML = string(data)
		}
	}

	// Collect recipients.
	toAddrs := emailAddrsToStrings(msg.To)
	ccAddrs := emailAddrsToStrings(msg.Cc)
	bccAddrs := emailAddrsToStrings(msg.Bcc)

	// Build RFC 5322 MIME and send via SES.
	rawMIME := buildMIME(msg, toAddrs, ccAddrs, bodyText, bodyHTML)

	sesInput := &sesv2sdk.SendEmailInput{
		Content: &sesv2types.EmailContent{
			Raw: &sesv2types.RawMessage{Data: rawMIME},
		},
	}
	if cs := os.Getenv("SES_CONFIGURATION_SET"); cs != "" {
		sesInput.ConfigurationSetName = aws.String(cs)
	}
	// BCC recipients must be passed via Destinations (not in raw MIME headers).
	if len(bccAddrs) > 0 || len(toAddrs) > 0 || len(ccAddrs) > 0 {
		all := append(append(toAddrs, ccAddrs...), bccAddrs...)
		sesInput.Destination = &sesv2types.Destination{
			ToAddresses:  all, // SES delivers to all; headers in MIME control display.
		}
	}

	now := time.Now().UTC()
	var sendErr error
	_, sendErr = sesClient.SendEmail(ctx, sesInput)

	return dbpkg.WithOrgTx(ctx, pool, orgID, func(tx pgx.Tx) error {
		if sendErr != nil {
			slog.Error("email_outbound: SES send failed", "message_id", messageID, "error", sendErr)
			if err := emailSt.UpdateMessageStatus(ctx, tx, messageID, models.MessageStatusFailed); err != nil {
				return err
			}
			_ = publisher.PublishEvent(ctx, &domainevents.MessageBouncedEvent{
				BaseEvent: domainevents.NewBase(domainevents.EventMessageBounced, orgID),
				Data: domainevents.MessageBouncedData{
					MessageID:    messageID.String(),
					InboxID:      msg.InboxID,
					ThreadID:     msg.ThreadID,
					BounceCode:   "500",
					BounceReason: sendErr.Error(),
				},
			})
			return sendErr
		}

		if err := emailSt.UpdateMessageSentAt(ctx, tx, messageID, now); err != nil {
			return err
		}
		_ = publisher.PublishEvent(ctx, &domainevents.MessageSentEvent{
			BaseEvent: domainevents.NewBase(domainevents.EventMessageSent, orgID),
			Data: domainevents.MessageSentData{
				MessageID: messageID.String(),
				InboxID:   msg.InboxID,
				ThreadID:  msg.ThreadID,
				Subject:   msg.Subject,
				To:        toAddrs,
			},
		})
		return nil
	})
}

// buildMIME assembles a minimal RFC 5322 multipart/alternative message.
func buildMIME(msg *models.Message, toAddrs, ccAddrs []string, textBody, htmlBody string) []byte {
	var buf bytes.Buffer
	boundary := uuid.New().String()

	fromAddr := msg.From.Email
	if msg.From.Name != "" {
		a := mail.Address{Name: msg.From.Name, Address: msg.From.Email}
		fromAddr = a.String()
	}

	buf.WriteString("MIME-Version: 1.0\r\n")
	buf.WriteString(fmt.Sprintf("From: %s\r\n", fromAddr))
	if len(toAddrs) > 0 {
		buf.WriteString(fmt.Sprintf("To: %s\r\n", strings.Join(toAddrs, ", ")))
	}
	if len(ccAddrs) > 0 {
		buf.WriteString(fmt.Sprintf("Cc: %s\r\n", strings.Join(ccAddrs, ", ")))
	}
	buf.WriteString(fmt.Sprintf("Subject: %s\r\n", msg.Subject))
	if msg.InReplyTo != "" {
		buf.WriteString(fmt.Sprintf("In-Reply-To: <%s>\r\n", msg.InReplyTo))
	}
	if len(msg.References) > 0 {
		refs := make([]string, len(msg.References))
		for i, r := range msg.References {
			refs[i] = "<" + r + ">"
		}
		buf.WriteString(fmt.Sprintf("References: %s\r\n", strings.Join(refs, " ")))
	}
	if msg.ReplyTo != "" {
		buf.WriteString(fmt.Sprintf("Reply-To: %s\r\n", msg.ReplyTo))
	}
	buf.WriteString(fmt.Sprintf("Content-Type: multipart/alternative; boundary=%q\r\n", boundary))
	buf.WriteString("\r\n")

	if textBody != "" {
		buf.WriteString(fmt.Sprintf("--%s\r\n", boundary))
		buf.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
		buf.WriteString("Content-Transfer-Encoding: quoted-printable\r\n\r\n")
		buf.WriteString(textBody)
		buf.WriteString("\r\n")
	}
	if htmlBody != "" {
		buf.WriteString(fmt.Sprintf("--%s\r\n", boundary))
		buf.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
		buf.WriteString("Content-Transfer-Encoding: quoted-printable\r\n\r\n")
		buf.WriteString(htmlBody)
		buf.WriteString("\r\n")
	}
	buf.WriteString(fmt.Sprintf("--%s--\r\n", boundary))
	return buf.Bytes()
}

func emailAddrsToStrings(addrs []models.EmailAddress) []string {
	result := make([]string, 0, len(addrs))
	for _, a := range addrs {
		if a.Email != "" {
			if a.Name != "" {
				result = append(result, (&mail.Address{Name: a.Name, Address: a.Email}).String())
			} else {
				result = append(result, a.Email)
			}
		}
	}
	return result
}

func main() {
	lambda.Start(handler)
}
