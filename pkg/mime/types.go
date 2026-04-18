/*
 * Copyright (c) 2026 nyklabs.com. All rights reserved.
 *
 * Licensed under the nGX Commercial Source License v1.0.
 * See LICENSE file in the project root for full license information.
 */

package mime

import "time"

// ParsedEmail is the result of parsing a raw RFC 5322 email message.
type ParsedEmail struct {
	MessageID  string
	InReplyTo  string
	References []string
	From       EmailAddress
	To         []EmailAddress
	CC         []EmailAddress
	ReplyTo    string
	Subject    string
	Date       time.Time
	BodyText   []byte
	BodyHTML   []byte
	Headers    map[string][]string
	Parts      []Part
}

// EmailAddress represents a single address with an optional display name.
type EmailAddress struct {
	Email string
	Name  string
}

// Part represents a non-text MIME part (attachment or inline resource).
type Part struct {
	ContentType string
	Filename    string
	ContentID   string
	IsInline    bool
	Data        []byte
}
