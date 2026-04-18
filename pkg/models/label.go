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

type Label struct {
	ID          uuid.UUID `json:"id"          db:"id"`
	OrgID       uuid.UUID `json:"org_id"      db:"org_id"`
	Name        string    `json:"name"        db:"name"`
	Color       string    `json:"color"       db:"color"`
	Description string    `json:"description" db:"description"`
	CreatedAt   time.Time `json:"created_at"  db:"created_at"`
}

type ThreadLabel struct {
	ThreadID  uuid.UUID `json:"thread_id"  db:"thread_id"`
	LabelID   uuid.UUID `json:"label_id"   db:"label_id"`
	AppliedAt time.Time `json:"applied_at" db:"applied_at"`
}
