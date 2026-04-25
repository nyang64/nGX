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

// Attachment represents a file attached to a message or draft.
type Attachment struct {
	ID        uuid.UUID  `json:"id"                   db:"id"`
	OrgID     uuid.UUID  `json:"org_id"               db:"org_id"`
	MessageID *uuid.UUID `json:"message_id,omitempty" db:"message_id"`
	DraftID   *uuid.UUID `json:"draft_id,omitempty"   db:"draft_id"`

	Filename    string `json:"filename"     db:"filename"`
	ContentType string `json:"content_type" db:"content_type"`
	// ContentID holds the Content-ID header value used for inline attachments.
	ContentID string `json:"content_id,omitempty" db:"content_id"`
	Inline    bool   `json:"inline"               db:"inline"`
	SizeBytes int64  `json:"size_bytes"           db:"size_bytes"`

	// S3Key is the object key in S3 where the attachment data is stored.
	S3Key string `json:"s3_key" db:"s3_key"`

	CreatedAt time.Time `json:"created_at" db:"created_at"`
}
