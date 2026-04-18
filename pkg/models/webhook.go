/*
 * Copyright (c) 2026 nyklabs.com. All rights reserved.
 *
 * Licensed under the nGX Commercial Source License v1.0.
 * See LICENSE file in the project root for full license information.
 */

package models

import (
	"time"

	"github.com/google/uuid"
)

type Webhook struct {
	ID            uuid.UUID  `json:"id"              db:"id"`
	OrgID         uuid.UUID  `json:"org_id"          db:"org_id"`
	URL           string     `json:"url"             db:"url"`
	Secret        string     `json:"-"               db:"secret"`
	Events        []string   `json:"events"          db:"events"`
	PodID         *uuid.UUID `json:"pod_id"          db:"pod_id"`
	InboxID       *uuid.UUID `json:"inbox_id"        db:"inbox_id"`
	IsActive      bool       `json:"is_active"       db:"is_active"`
	FailureCount  int        `json:"failure_count"   db:"failure_count"`
	LastSuccessAt *time.Time `json:"last_success_at" db:"last_success_at"`
	LastFailureAt *time.Time `json:"last_failure_at" db:"last_failure_at"`
	CreatedAt     time.Time  `json:"created_at"      db:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"      db:"updated_at"`

	// Caller-supplied auth header injected on every outbound delivery call.
	// AuthHeaderName is the header name (e.g. "Authorization"). Never nil when
	// AuthHeaderValueEnc is set.
	AuthHeaderName     *string `json:"-" db:"auth_header_name"`
	// AuthHeaderValueEnc is the AES-256-GCM encrypted header value stored in the DB.
	AuthHeaderValueEnc []byte  `json:"-" db:"auth_header_value_enc"`
	// AuthHeaderValue is the decrypted value populated in memory after decryption.
	// It is never persisted and never returned in API responses.
	AuthHeaderValue string `json:"-" db:"-"`
}

type WebhookDeliveryStatus string

const (
	DeliveryStatusPending  WebhookDeliveryStatus = "pending"
	DeliveryStatusSuccess  WebhookDeliveryStatus = "success"
	DeliveryStatusFailed   WebhookDeliveryStatus = "failed"
	DeliveryStatusRetrying WebhookDeliveryStatus = "retrying"
)

type WebhookDelivery struct {
	ID             uuid.UUID             `json:"id"               db:"id"`
	WebhookID      uuid.UUID             `json:"webhook_id"       db:"webhook_id"`
	EventID        string                `json:"event_id"         db:"event_id"`
	EventType      string                `json:"event_type"       db:"event_type"`
	Payload        map[string]any        `json:"payload"          db:"payload"`
	Status         WebhookDeliveryStatus `json:"status"           db:"status"`
	AttemptCount   int                   `json:"attempt_count"    db:"attempt_count"`
	NextAttemptAt  *time.Time            `json:"next_attempt_at"  db:"next_attempt_at"`
	LastAttemptAt  *time.Time            `json:"last_attempt_at"  db:"last_attempt_at"`
	ResponseStatus *int                  `json:"response_status"  db:"response_status"`
	ResponseBody   string                `json:"response_body"    db:"response_body"`
	ErrorMessage   string                `json:"error_message"    db:"error_message"`
	CreatedAt      time.Time             `json:"created_at"       db:"created_at"`
	UpdatedAt      time.Time             `json:"updated_at"       db:"updated_at"`
}
