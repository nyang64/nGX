/*
 * Copyright (c) 2026 nyklabs.com. All rights reserved.
 *
 * Licensed under the nGX Commercial Source License v1.0.
 * See LICENSE file in the project root for full license information.
 */

package service

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// resolveInboxAddress — domain resolution for inbox provisioning
// ---------------------------------------------------------------------------

func TestResolveInboxAddress_FullAddress(t *testing.T) {
	// A full email address should pass through unchanged.
	got, err := resolveInboxAddress("agent@mail.acme.com", "mail.acme.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "agent@mail.acme.com" {
		t.Errorf("got %q, want %q", got, "agent@mail.acme.com")
	}
}

func TestResolveInboxAddress_UsernameOnly_AppendsDomain(t *testing.T) {
	// A username with no "@" should have the mail domain appended.
	got, err := resolveInboxAddress("my-agent", "mail.acme.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "my-agent@mail.acme.com" {
		t.Errorf("got %q, want %q", got, "my-agent@mail.acme.com")
	}
}

func TestResolveInboxAddress_UsernameOnly_NoDomain_ReturnsError(t *testing.T) {
	// Username with no "@" and no mailDomain configured should return an error.
	_, err := resolveInboxAddress("my-agent", "")
	if err == nil {
		t.Fatal("expected error when address has no domain and MAIL_DOMAIN is not set")
	}
	if !strings.Contains(err.Error(), "MAIL_DOMAIN is not configured") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestResolveInboxAddress_FullAddress_IgnoresMailDomain(t *testing.T) {
	// A full address with a custom domain should not be altered by mailDomain.
	got, err := resolveInboxAddress("support@custom.example.com", "mail.acme.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "support@custom.example.com" {
		t.Errorf("got %q, want %q", got, "support@custom.example.com")
	}
}

func TestResolveInboxAddress_UsernameWithHyphens(t *testing.T) {
	got, err := resolveInboxAddress("order-notifications-v2", "mail.enterprise.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "order-notifications-v2@mail.enterprise.com" {
		t.Errorf("got %q, want %q", got, "order-notifications-v2@mail.enterprise.com")
	}
}

func TestResolveInboxAddress_EmptyUsername_NoDomain_ReturnsError(t *testing.T) {
	// Edge case: completely empty address with no domain configured.
	_, err := resolveInboxAddress("", "")
	if err == nil {
		t.Fatal("expected error for empty address with no mail domain")
	}
}

func TestResolveInboxAddress_EmptyUsername_WithDomain(t *testing.T) {
	// Edge case: empty username appends domain — the service layer validates
	// that address is non-empty before calling this, but resolve itself should not panic.
	got, err := resolveInboxAddress("", "mail.acme.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "@mail.acme.com" {
		t.Errorf("got %q, want %q", got, "@mail.acme.com")
	}
}
