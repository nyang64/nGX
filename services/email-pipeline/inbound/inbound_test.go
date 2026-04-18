/*
 * Copyright (c) 2026 nyklabs.com. All rights reserved.
 *
 * Licensed under the nGX Commercial Source License v1.0.
 * See LICENSE file in the project root for full license information.
 */

package inbound

import (
	"testing"

	"agentmail/pkg/mime"
	"agentmail/pkg/models"
)

// ---------------------------------------------------------------------------
// buildSnippet
// ---------------------------------------------------------------------------

func TestBuildSnippet_Short(t *testing.T) {
	got := buildSnippet([]byte("hello world"), 50)
	if got != "hello world" {
		t.Errorf("got %q, want %q", got, "hello world")
	}
}

func TestBuildSnippet_Truncates(t *testing.T) {
	got := buildSnippet([]byte("abcdefghij"), 5)
	if got != "abcde" {
		t.Errorf("got %q, want %q", got, "abcde")
	}
}

func TestBuildSnippet_StripsNewlines(t *testing.T) {
	got := buildSnippet([]byte("line1\nline2\r\nline3"), 100)
	if got != "line1 line2 line3" {
		t.Errorf("got %q, want %q", got, "line1 line2 line3")
	}
}

func TestBuildSnippet_TrimSpace(t *testing.T) {
	got := buildSnippet([]byte("  hello  "), 100)
	if got != "hello" {
		t.Errorf("got %q, want %q", got, "hello")
	}
}

func TestBuildSnippet_Empty(t *testing.T) {
	got := buildSnippet([]byte{}, 50)
	if got != "" {
		t.Errorf("got %q, want empty string", got)
	}
}

// ---------------------------------------------------------------------------
// convertAddresses
// ---------------------------------------------------------------------------

func TestConvertAddresses_Basic(t *testing.T) {
	input := []mime.EmailAddress{
		{Email: "a@example.com", Name: "Alice"},
		{Email: "b@example.com", Name: "Bob"},
	}
	got := convertAddresses(input)
	if len(got) != 2 {
		t.Fatalf("expected 2 addresses, got %d", len(got))
	}
	if got[0].Email != "a@example.com" || got[0].Name != "Alice" {
		t.Errorf("got[0] = %+v", got[0])
	}
	if got[1].Email != "b@example.com" || got[1].Name != "Bob" {
		t.Errorf("got[1] = %+v", got[1])
	}
}

func TestConvertAddresses_Empty(t *testing.T) {
	got := convertAddresses(nil)
	if len(got) != 0 {
		t.Errorf("expected empty slice, got %v", got)
	}
}

// ---------------------------------------------------------------------------
// buildParticipants
// ---------------------------------------------------------------------------

func TestBuildParticipants_Deduplicates(t *testing.T) {
	parsed := &mime.ParsedEmail{
		From: mime.EmailAddress{Email: "alice@example.com", Name: "Alice"},
		To: []mime.EmailAddress{
			{Email: "bob@example.com", Name: "Bob"},
			{Email: "alice@example.com", Name: "Alice"}, // duplicate of From
		},
		CC: []mime.EmailAddress{
			{Email: "carol@example.com", Name: "Carol"},
		},
	}
	got := buildParticipants(parsed)
	// alice appears in From and To, should only appear once
	if len(got) != 3 {
		t.Fatalf("expected 3 participants, got %d: %v", len(got), got)
	}
}

func TestBuildParticipants_SkipsEmptyEmail(t *testing.T) {
	parsed := &mime.ParsedEmail{
		From: mime.EmailAddress{Email: "", Name: "Unknown"},
		To:   []mime.EmailAddress{{Email: "b@example.com"}},
	}
	got := buildParticipants(parsed)
	if len(got) != 1 {
		t.Fatalf("expected 1 participant (empty From skipped), got %d: %v", len(got), got)
	}
}

func TestBuildParticipants_Order(t *testing.T) {
	parsed := &mime.ParsedEmail{
		From: mime.EmailAddress{Email: "from@example.com"},
		To:   []mime.EmailAddress{{Email: "to@example.com"}},
		CC:   []mime.EmailAddress{{Email: "cc@example.com"}},
	}
	got := buildParticipants(parsed)
	if len(got) != 3 {
		t.Fatalf("expected 3, got %d", len(got))
	}
	// From is always first
	if got[0].Email != "from@example.com" {
		t.Errorf("first participant should be From, got %q", got[0].Email)
	}
}

// verify the type mapping is correct
var _ models.EmailAddress = models.EmailAddress{}
