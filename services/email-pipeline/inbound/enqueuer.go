/*
 * Copyright (c) 2026 nyklabs.com. All rights reserved.
 *
 * Licensed under the nGX Commercial Source License v1.0.
 * See LICENSE file in the project root for full license information.
 */

package inbound

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"time"

	"github.com/emersion/go-smtp"
	"github.com/google/uuid"

	"agentmail/pkg/events"
	"agentmail/pkg/s3"
	"agentmail/services/email-pipeline/emailauth"
)

// RawEmailJob is the payload published to email.inbound.raw.
type RawEmailJob struct {
	JobID      string    `json:"job_id"`
	S3Key      string    `json:"s3_key"`
	From       string    `json:"from"`
	To         []string  `json:"to"`
	SizeBytes  int64     `json:"size_bytes"`
	ReceivedAt time.Time `json:"received_at"`
	RemoteIP   string    `json:"remote_ip"`
	HELO       string    `json:"helo"`
	SPFResult  string    `json:"spf_result"` // emailauth.SPFResult
}

// Enqueuer is the lightweight component called by the SMTP server.
// It runs SPF, stores the raw RFC 5322 message to S3, and enqueues a job on
// email.inbound.raw so the SMTP session can send 250 OK immediately.
type Enqueuer struct {
	s3Client *s3.Client
	producer events.OutboundPublisher
}

// NewEnqueuer creates an Enqueuer wired to S3 and the inbound producer.
func NewEnqueuer(s3Client *s3.Client, producer events.OutboundPublisher) *Enqueuer {
	return &Enqueuer{s3Client: s3Client, producer: producer}
}

// Enqueue checks SPF, stores raw bytes to S3, and publishes a RawEmailJob.
func (e *Enqueuer) Enqueue(ctx context.Context, remoteIP net.IP, helo, from string, to []string, r io.Reader) error {
	// --- SPF check (fast: DNS-only, no parsing) ---
	spfResult := emailauth.CheckSPF(remoteIP, helo, from)
	slog.Info("SPF check", "from", from, "remote_ip", remoteIP, "result", spfResult)

	if spfResult == emailauth.SPFFail {
		// Hard SPF fail: return an SMTP permanent error so the sending MTA
		// does not retry. This is the standard rejection behaviour.
		return &smtp.SMTPError{
			Code:         550,
			EnhancedCode: smtp.EnhancedCode{5, 7, 23},
			Message:      "SPF check failed: " + from + " is not authorised to send from " + remoteIP.String(),
		}
	}

	rawData, err := io.ReadAll(r)
	if err != nil {
		return fmt.Errorf("read smtp data: %w", err)
	}

	jobID := uuid.New().String()
	s3Key := fmt.Sprintf("inbound/raw/%s/%s.eml", time.Now().UTC().Format("2006/01/02"), jobID)

	if err := e.s3Client.Upload(ctx, s3Key, rawData, "message/rfc822"); err != nil {
		return fmt.Errorf("store raw email to S3: %w", err)
	}

	ipStr := ""
	if remoteIP != nil {
		ipStr = remoteIP.String()
	}

	job := RawEmailJob{
		JobID:      jobID,
		S3Key:      s3Key,
		From:       from,
		To:         to,
		SizeBytes:  int64(len(rawData)),
		ReceivedAt: time.Now().UTC(),
		RemoteIP:   ipStr,
		HELO:       helo,
		SPFResult:  string(spfResult),
	}
	payload, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("marshal inbound job: %w", err)
	}

	if err := e.producer.Publish(ctx, jobID, payload); err != nil {
		slog.Error("failed to enqueue inbound job — email stored in S3 but not queued",
			"job_id", jobID,
			"s3_key", s3Key,
			"error", err,
		)
		return fmt.Errorf("enqueue inbound job: %w", err)
	}

	slog.Info("inbound email enqueued",
		"job_id", jobID,
		"from", from,
		"to", to,
		"size_bytes", job.SizeBytes,
		"spf", spfResult,
	)
	return nil
}
