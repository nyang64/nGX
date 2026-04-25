/*
 * Copyright (c) 2026 nyklabs.com. All rights reserved.
 *
 * Licensed under the nGX Commercial Source License v1.0.
 * See LICENSE file in the project root for full license information.
 */

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	awscfg "github.com/aws/aws-sdk-go-v2/config"
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

var (
	pool      *pgxpool.Pool
	emailSt   *emailstore.EmailStore
	emailsS3  *s3pkg.Client
	publisher *sqspkg.Publisher
)

func init() {
	// Skip DB/AWS init when DATABASE_URL is not set (e.g. unit test environments).
	if os.Getenv("DATABASE_URL") == "" {
		return
	}
	pool = shared.InitDB()
	emailSt = emailstore.NewEmailStore(pool)

	ctx := context.Background()

	var err error
	emailsS3, err = s3pkg.NewFromAWS(ctx, os.Getenv("S3_BUCKET_EMAILS"))
	if err != nil {
		slog.Warn("ses_events: init emails S3 client (body download disabled)", "error", err)
		emailsS3 = nil
	}

	awsConf, err := awscfg.LoadDefaultConfig(ctx)
	if err != nil {
		slog.Error("ses_events: load AWS config", "error", err)
		os.Exit(1)
	}
	sqsClient := sqssdk.NewFromConfig(awsConf)
	publisher = sqspkg.NewPublisher(sqsClient)
}

// ── EventBridge envelope types ────────────────────────────────────────────────

// sesMailHeader is a single header from the SES notification.
type sesMailHeader struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// sesMail contains the original email metadata in the SES notification.
type sesMail struct {
	MessageID string          `json:"messageId"`
	Headers   []sesMailHeader `json:"headers"`
}

// ebBounce carries bounce classification details.
type ebBounce struct {
	BounceType    string `json:"bounceType"`
	BounceSubType string `json:"bounceSubType"`
}

// ebClick carries link click details from SES engagement tracking.
type ebClick struct {
	IPAddress string `json:"ipAddress"`
	Link      string `json:"link"`
	UserAgent string `json:"userAgent"`
}

// ebOpen carries open details from SES engagement tracking.
type ebOpen struct {
	IPAddress string `json:"ipAddress"`
	UserAgent string `json:"userAgent"`
}

// ebSESEvent is the EventBridge envelope for native SES events.
// SES publishes to the default event bus with source "aws.ses".
// Official detail-type values: https://docs.aws.amazon.com/ses/latest/dg/monitoring-eventbridge.html
type ebSESEvent struct {
	DetailType string   `json:"detail-type"`
	Detail     ebDetail `json:"detail"`
}

type ebDetail struct {
	Mail   sesMail  `json:"mail"`
	Bounce ebBounce `json:"bounce"`
	Click  ebClick  `json:"click"`
	Open   ebOpen   `json:"open"`
}

// ── Handler ───────────────────────────────────────────────────────────────────

func handler(ctx context.Context, sqsEvent events.SQSEvent) (events.SQSEventResponse, error) {
	var failures []events.SQSBatchItemFailure
	for _, record := range sqsEvent.Records {
		if err := processRecord(ctx, record); err != nil {
			slog.Error("ses_events: process record failed", "id", record.MessageId, "error", err)
			failures = append(failures, events.SQSBatchItemFailure{ItemIdentifier: record.MessageId})
		}
	}
	return events.SQSEventResponse{BatchItemFailures: failures}, nil
}

func processRecord(ctx context.Context, record events.SQSMessage) error {
	// SES → EventBridge → SQS: the SQS body is the EventBridge event JSON.
	var evt ebSESEvent
	if err := json.Unmarshal([]byte(record.Body), &evt); err != nil {
		slog.Warn("ses_events: unmarshal EventBridge event, skipping", "error", err)
		return nil
	}

	// Extract RFC 5322 Message-ID from the mail headers.
	var rfc5322MsgID string
	for _, h := range evt.Detail.Mail.Headers {
		if h.Name == "Message-ID" || h.Name == "Message-Id" {
			rfc5322MsgID = h.Value
			// Strip angle brackets.
			if len(rfc5322MsgID) > 2 && rfc5322MsgID[0] == '<' {
				rfc5322MsgID = rfc5322MsgID[1 : len(rfc5322MsgID)-1]
			}
			break
		}
	}

	if rfc5322MsgID == "" {
		slog.Debug("ses_events: no Message-ID header, skipping", "ses_msg_id", evt.Detail.Mail.MessageID)
		return nil
	}

	switch evt.DetailType {
	case "Email Bounced":
		if pool == nil {
			return fmt.Errorf("ses_events: database pool not initialised")
		}
		var msgIDStr, orgIDStr string
		err := pool.QueryRow(ctx,
			`UPDATE messages SET status = $1, updated_at = NOW()
			 WHERE message_id_header = $2
			 RETURNING id::text, org_id::text`,
			string(models.MessageStatusBounced), rfc5322MsgID,
		).Scan(&msgIDStr, &orgIDStr)
		if err != nil {
			if err == pgx.ErrNoRows {
				slog.Debug("ses_events: no message found for bounce", "message_id", rfc5322MsgID)
				return nil
			}
			return err
		}
		slog.Info("ses_events: marked message bounced", "message_id", rfc5322MsgID)
		bounceCode := fmt.Sprintf("%s/%s", evt.Detail.Bounce.BounceType, evt.Detail.Bounce.BounceSubType)
		publishMsgBounced(ctx, msgIDStr, orgIDStr, bounceCode, "SES bounce notification")

	case "Email Complaint Received", "Email Rejected", "Email Rendering Failed":
		if pool == nil {
			return fmt.Errorf("ses_events: database pool not initialised")
		}
		var msgIDStr, orgIDStr string
		err := pool.QueryRow(ctx,
			`UPDATE messages SET status = $1, updated_at = NOW()
			 WHERE message_id_header = $2
			 RETURNING id::text, org_id::text`,
			string(models.MessageStatusFailed), rfc5322MsgID,
		).Scan(&msgIDStr, &orgIDStr)
		if err != nil {
			if err == pgx.ErrNoRows {
				slog.Debug("ses_events: no message found for failure", "type", evt.DetailType, "message_id", rfc5322MsgID)
				return nil
			}
			return err
		}
		slog.Info("ses_events: marked message failed", "type", evt.DetailType, "message_id", rfc5322MsgID)
		publishMsgBounced(ctx, msgIDStr, orgIDStr, "500", evt.DetailType)

	case "Email Delivered":
		if pool == nil {
			return fmt.Errorf("ses_events: database pool not initialised")
		}
		// Always update sent_at to the SES delivery confirmation time.
		// Also advance status to 'sent' if still in 'sending', but leave it
		// unchanged if already at a terminal status.
		var msgIDStr, orgIDStr string
		err := pool.QueryRow(ctx,
			`UPDATE messages
			 SET sent_at    = NOW(),
			     updated_at = NOW(),
			     status     = CASE WHEN status = 'sending' THEN 'sent' ELSE status END
			 WHERE message_id_header = $1
			 RETURNING id::text, org_id::text`,
			rfc5322MsgID,
		).Scan(&msgIDStr, &orgIDStr)
		if err != nil {
			if err == pgx.ErrNoRows {
				slog.Debug("ses_events: no message found for delivery", "message_id", rfc5322MsgID)
				return nil
			}
			return err
		}
		slog.Info("ses_events: confirmed delivery", "message_id", rfc5322MsgID)
		publishMsgSent(ctx, msgIDStr, orgIDStr)

	case "Email Delivery Delayed":
		// Transient delay — SES will eventually send Email Delivered or Email Bounced.
		// No status change; log for observability.
		slog.Warn("ses_events: delivery delayed", "message_id", rfc5322MsgID)

	case "Email Opened":
		publishMsgEngagement(ctx, rfc5322MsgID, "opened", "", evt.Detail.Open.IPAddress, evt.Detail.Open.UserAgent)

	case "Email Clicked":
		publishMsgEngagement(ctx, rfc5322MsgID, "clicked", evt.Detail.Click.Link, evt.Detail.Click.IPAddress, evt.Detail.Click.UserAgent)

	default:
		slog.Debug("ses_events: unhandled detail-type, skipping", "detail-type", evt.DetailType)
	}
	return nil
}

// ── Event helpers ─────────────────────────────────────────────────────────────

// loadMsg fetches a message and its attachments inside an org-scoped transaction,
// together with the body text/HTML from S3 when available.
// Returns nil if msgIDStr or orgIDStr cannot be parsed as UUIDs.
func loadMsg(ctx context.Context, msgIDStr, orgIDStr string) (*models.Message, string, string, []models.Attachment) {
	if publisher == nil {
		return nil, "", "", nil
	}
	msgID, err := uuid.Parse(msgIDStr)
	if err != nil {
		slog.Warn("ses_events: invalid message UUID in RETURNING", "id", msgIDStr)
		return nil, "", "", nil
	}
	orgID, err := uuid.Parse(orgIDStr)
	if err != nil {
		slog.Warn("ses_events: invalid org UUID in RETURNING", "id", orgIDStr)
		return nil, "", "", nil
	}

	var msg *models.Message
	var atts []*models.Attachment
	_ = dbpkg.WithOrgTx(ctx, pool, orgID, func(tx pgx.Tx) error {
		msg, err = emailSt.GetMessageByID(ctx, tx, orgID, msgID)
		if err != nil {
			return err
		}
		atts, _ = emailSt.GetAttachmentsByMessageID(ctx, orgID, msgID)
		return nil
	})
	if msg == nil {
		return nil, "", "", nil
	}

	bodyText, bodyHTML := fetchBodies(ctx, msg)
	return msg, bodyText, bodyHTML, derefAtts(atts)
}

// findMsgByHeader looks up a message by its RFC 5322 Message-ID header value
// using the raw (non-RLS) pool. Returns the DB id and org_id as strings.
func findMsgByHeader(ctx context.Context, rfc5322MsgID string) (msgIDStr, orgIDStr string, found bool) {
	err := pool.QueryRow(ctx,
		`SELECT id::text, org_id::text FROM messages WHERE message_id_header = $1 LIMIT 1`,
		rfc5322MsgID,
	).Scan(&msgIDStr, &orgIDStr)
	if err != nil {
		return "", "", false
	}
	return msgIDStr, orgIDStr, true
}

// fetchBodies downloads text and HTML bodies from S3 when keys are available.
func fetchBodies(ctx context.Context, msg *models.Message) (string, string) {
	if emailsS3 == nil {
		return "", ""
	}
	var bodyText, bodyHTML string
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
	return bodyText, bodyHTML
}

// publishMsgBounced loads the message and publishes a MessageBouncedEvent.
func publishMsgBounced(ctx context.Context, msgIDStr, orgIDStr, bounceCode, bounceReason string) {
	msg, bodyText, bodyHTML, atts := loadMsg(ctx, msgIDStr, orgIDStr)
	if msg == nil {
		return
	}
	orgID, _ := uuid.Parse(orgIDStr)
	_ = publisher.PublishEvent(ctx, &domainevents.MessageBouncedEvent{
		BaseEvent: domainevents.NewBase(domainevents.EventMessageBounced, orgID),
		Data: domainevents.MessageBouncedData{
			MessagePayload: domainevents.MessagePayloadFromModel(
				msg, bodyText, bodyHTML,
				domainevents.BuildPreview(bodyText, 200),
				atts,
			),
			BounceCode:   bounceCode,
			BounceReason: bounceReason,
		},
	})
}

// publishMsgSent loads the message and publishes a MessageSentEvent.
func publishMsgSent(ctx context.Context, msgIDStr, orgIDStr string) {
	msg, bodyText, bodyHTML, atts := loadMsg(ctx, msgIDStr, orgIDStr)
	if msg == nil {
		return
	}
	orgID, _ := uuid.Parse(orgIDStr)
	_ = publisher.PublishEvent(ctx, &domainevents.MessageSentEvent{
		BaseEvent: domainevents.NewBase(domainevents.EventMessageSent, orgID),
		Data: domainevents.MessageSentData{
			MessagePayload: domainevents.MessagePayloadFromModel(
				msg, bodyText, bodyHTML,
				domainevents.BuildPreview(bodyText, 200),
				atts,
			),
		},
	})
}

// publishMsgEngagement looks up the message by RFC 5322 Message-ID header and
// publishes a MessageEngagementEvent. No status change is made.
func publishMsgEngagement(ctx context.Context, rfc5322MsgID, engagementType, linkURL, ipAddress, userAgent string) {
	if publisher == nil || pool == nil {
		return
	}
	msgIDStr, orgIDStr, ok := findMsgByHeader(ctx, rfc5322MsgID)
	if !ok {
		slog.Debug("ses_events: no message found for engagement event",
			"type", engagementType, "message_id", rfc5322MsgID)
		return
	}
	msg, bodyText, bodyHTML, atts := loadMsg(ctx, msgIDStr, orgIDStr)
	if msg == nil {
		return
	}
	orgID, _ := uuid.Parse(orgIDStr)
	_ = publisher.PublishEvent(ctx, &domainevents.MessageEngagementEvent{
		BaseEvent: domainevents.NewBase(domainevents.EventMessageEngagement, orgID),
		Data: domainevents.MessageEngagementData{
			MessagePayload: domainevents.MessagePayloadFromModel(
				msg, bodyText, bodyHTML,
				domainevents.BuildPreview(bodyText, 200),
				atts,
			),
			EngagementType: engagementType,
			LinkURL:        linkURL,
			IPAddress:      ipAddress,
			UserAgent:      userAgent,
		},
	})
	slog.Info("ses_events: published engagement event",
		"type", engagementType, "message_id", rfc5322MsgID)
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

func main() {
	lambda.Start(handler)
}
