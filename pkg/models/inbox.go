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

// Inbox represents an email address managed by nGX.
type Inbox struct {
	ID          uuid.UUID      `json:"id"           db:"id"`
	OrgID       uuid.UUID      `json:"org_id"       db:"org_id"`
	PodID       *uuid.UUID     `json:"pod_id"       db:"pod_id"`
	Email       string         `json:"email"        db:"email"`
	DisplayName string         `json:"display_name" db:"display_name"`
	Status      InboxStatus    `json:"status"       db:"status"`
	Settings    map[string]any `json:"settings"     db:"settings"`
	CreatedAt   time.Time      `json:"created_at"   db:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"   db:"updated_at"`
}
