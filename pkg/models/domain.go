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

// DomainConfig represents a custom domain registered by an enterprise org.
// SES manages DKIM signing; this record tracks verification state and links
// the domain to the org so inbound emails can be routed correctly.
type DomainConfig struct {
	ID           uuid.UUID  `json:"id"            db:"id"`
	OrgID        uuid.UUID  `json:"org_id"        db:"org_id"`
	PodID        *uuid.UUID `json:"pod_id"        db:"pod_id"`
	Domain       string     `json:"domain"        db:"domain"`
	Status       string     `json:"status"        db:"status"` // pending | verifying | active | failed
	DKIMSelector string     `json:"dkim_selector" db:"dkim_selector"`
	VerifiedAt   *time.Time `json:"verified_at"   db:"verified_at"`
	CreatedAt    time.Time  `json:"created_at"    db:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"    db:"updated_at"`
}

// DNSRecord is a DNS record the customer must add to their domain registrar.
type DNSRecord struct {
	Type    string `json:"type"`    // TXT | MX | CNAME
	Name    string `json:"name"`    // hostname
	Value   string `json:"value"`   // record value
	Purpose string `json:"purpose"` // human description
}
