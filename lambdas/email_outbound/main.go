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
	"encoding/base64"
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

// attData holds a downloaded attachment ready to embed in MIME.
type attData struct {
	filename    string
	contentType string
	data        []byte
}

var (
	pool          *pgxpool.Pool
	emailSt       *emailstore.EmailStore
	emailsS3      *s3pkg.Client
	attachmentsS3 *s3pkg.Client
	sesClient     *sesv2sdk.Client
	publisher     *sqspkg.Publisher
)

func init() {
	ctx := context.Background()
	pool = shared.InitDB()
	emailSt = emailstore.NewEmailStore(pool)

	var err error
	emailsS3, err = s3pkg.NewFromAWS(ctx, os.Getenv("S3_BUCKET_EMAILS"))
	if err != nil {
		slog.Error("email_outbound: init emails S3 client", "error", err)
		os.Exit(1)
	}
	attachmentsS3, err = s3pkg.NewFromAWS(ctx, os.Getenv("S3_BUCKET_ATTACHMENTS"))
	if err != nil {
		slog.Error("email_outbound: init attachments S3 client", "error", err)
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

	// Load attachments from S3.
	// modelAtts holds attachment metadata for the event payload.
	var modelAtts []*models.Attachment
	var attachments []attData
	if msg.HasAttachments {
		atts, err := emailSt.GetAttachmentsByMessageID(ctx, orgID, messageID)
		if err != nil {
			slog.Warn("email_outbound: load attachments", "message_id", messageID, "error", err)
		} else {
			modelAtts = atts
		}
		for _, att := range atts {
			data, err := attachmentsS3.Download(ctx, att.S3Key)
			if err != nil {
				slog.Warn("email_outbound: download attachment", "s3_key", att.S3Key, "error", err)
				continue
			}
			attachments = append(attachments, attData{
				filename:    att.Filename,
				contentType: att.ContentType,
				data:        data,
			})
		}
	}

	// Collect recipients.
	toAddrs := emailAddrsToStrings(msg.To)
	ccAddrs := emailAddrsToStrings(msg.Cc)
	bccAddrs := emailAddrsToStrings(msg.Bcc)

	// Build RFC 5322 MIME and send via SES.
	rawMIME := buildMIME(msg, toAddrs, ccAddrs, bodyText, bodyHTML, attachments)

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
			failedMsg := *msg
			failedMsg.Status = models.MessageStatusFailed
			failedMsg.UpdatedAt = now
			_ = publisher.PublishEvent(ctx, &domainevents.MessageBouncedEvent{
				BaseEvent: domainevents.NewBase(domainevents.EventMessageBounced, orgID),
				Data: domainevents.MessageBouncedData{
					MessagePayload: domainevents.MessagePayloadFromModel(
						&failedMsg, bodyText, bodyHTML,
						domainevents.BuildPreview(bodyText, 200),
						derefAtts(modelAtts),
					),
					BounceCode:   "500",
					BounceReason: sendErr.Error(),
				},
			})
			return sendErr
		}

		if err := emailSt.UpdateMessageSentAt(ctx, tx, messageID, now); err != nil {
			return err
		}
		sentMsg := *msg
		sentMsg.Status = models.MessageStatusSent
		sentMsg.SentAt = &now
		sentMsg.UpdatedAt = now
		_ = publisher.PublishEvent(ctx, &domainevents.MessageSentEvent{
			BaseEvent: domainevents.NewBase(domainevents.EventMessageSent, orgID),
			Data: domainevents.MessageSentData{
				MessagePayload: domainevents.MessagePayloadFromModel(
					&sentMsg, bodyText, bodyHTML,
					domainevents.BuildPreview(bodyText, 200),
					derefAtts(modelAtts),
				),
			},
		})
		return nil
	})
}

// buildMIME assembles a RFC 5322 message.
// With attachments: multipart/mixed wrapping the body + each attachment.
// Without attachments: multipart/alternative for text+html, text/plain otherwise.
func buildMIME(msg *models.Message, toAddrs, ccAddrs []string, textBody, htmlBody string, attachments []attData) []byte {
	var buf bytes.Buffer

	fromAddr := msg.From.Email
	if msg.From.Name != "" {
		a := mail.Address{Name: msg.From.Name, Address: msg.From.Email}
		fromAddr = a.String()
	}

	buf.WriteString("MIME-Version: 1.0\r\n")
	if msg.MessageID != "" {
		buf.WriteString(fmt.Sprintf("Message-ID: <%s>\r\n", msg.MessageID))
	}
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

	if len(attachments) > 0 {
		// Wrap everything in multipart/mixed.
		outerBoundary := uuid.New().String()
		buf.WriteString(fmt.Sprintf("Content-Type: multipart/mixed; boundary=%q\r\n\r\n", outerBoundary))

		// First part: the message body.
		buf.WriteString(fmt.Sprintf("--%s\r\n", outerBoundary))
		writeBodyPart(&buf, textBody, htmlBody)

		// Subsequent parts: attachments.
		for _, att := range attachments {
			buf.WriteString(fmt.Sprintf("\r\n--%s\r\n", outerBoundary))
			ct := att.contentType
			if ct == "" {
				ct = "application/octet-stream"
			}
			buf.WriteString(fmt.Sprintf("Content-Type: %s\r\n", ct))
			buf.WriteString(fmt.Sprintf("Content-Disposition: attachment; filename=%q\r\n", att.filename))
			buf.WriteString("Content-Transfer-Encoding: base64\r\n\r\n")
			encoded := base64.StdEncoding.EncodeToString(att.data)
			// Wrap base64 at 76 chars per RFC 2045.
			for i := 0; i < len(encoded); i += 76 {
				end := i + 76
				if end > len(encoded) {
					end = len(encoded)
				}
				buf.WriteString(encoded[i:end])
				buf.WriteString("\r\n")
			}
		}
		buf.WriteString(fmt.Sprintf("--%s--\r\n", outerBoundary))
	} else {
		writeBodyPart(&buf, textBody, htmlBody)
	}

	return buf.Bytes()
}

// writeBodyPart writes the text/HTML body section directly to buf.
// Uses multipart/alternative when both parts are present.
func writeBodyPart(buf *bytes.Buffer, textBody, htmlBody string) {
	if textBody != "" && htmlBody != "" {
		boundary := uuid.New().String()
		buf.WriteString(fmt.Sprintf("Content-Type: multipart/alternative; boundary=%q\r\n\r\n", boundary))

		buf.WriteString(fmt.Sprintf("--%s\r\n", boundary))
		buf.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
		buf.WriteString("Content-Transfer-Encoding: quoted-printable\r\n\r\n")
		buf.WriteString(textBody)
		buf.WriteString("\r\n")

		buf.WriteString(fmt.Sprintf("--%s\r\n", boundary))
		buf.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
		buf.WriteString("Content-Transfer-Encoding: quoted-printable\r\n\r\n")
		buf.WriteString(htmlBody)
		buf.WriteString("\r\n")

		buf.WriteString(fmt.Sprintf("--%s--\r\n", boundary))
	} else if htmlBody != "" {
		buf.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
		buf.WriteString("Content-Transfer-Encoding: quoted-printable\r\n\r\n")
		buf.WriteString(htmlBody)
		buf.WriteString("\r\n")
	} else {
		buf.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
		buf.WriteString("Content-Transfer-Encoding: quoted-printable\r\n\r\n")
		buf.WriteString(textBody)
		buf.WriteString("\r\n")
	}
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

// derefAtts converts a pointer slice to a value slice for event payload building.
func derefAtts(atts []*models.Attachment) []models.Attachment {
	out := make([]models.Attachment, 0, len(atts))
	for _, a := range atts {
		if a != nil {
			out = append(out, *a)
		}
	}
	return out
}
