/*
 * Copyright (c) 2026 nyklabs.com. All rights reserved.
 *
 * Licensed under the nGX Commercial Source License v1.0.
 * See LICENSE file in the project root for full license information.
 */

package emailauth

import "testing"

func TestExtractFromDomain(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"alice@example.com", "example.com"},
		{"alice@Example.COM", "example.com"}, // lowercased
		{"noatsign", ""},
		{"", ""},
		{"trailing@", ""},        // nothing after @
		{"a@b@c.com", "c.com"},   // LastIndex picks the last @
		{"user@SUB.Domain.org", "sub.domain.org"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := ExtractFromDomain(tc.input)
			if got != tc.want {
				t.Errorf("ExtractFromDomain(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestNewDKIMSigner_EmptyKey(t *testing.T) {
	signer, err := NewDKIMSigner("", "selector", "example.com")
	if err != nil {
		t.Fatalf("expected no error for empty key, got %v", err)
	}
	if signer != nil {
		t.Error("expected nil signer for empty key")
	}
}

func TestNewDKIMSigner_InvalidPEM(t *testing.T) {
	_, err := NewDKIMSigner("not-a-pem-key", "selector", "example.com")
	if err == nil {
		t.Error("expected error for invalid PEM key")
	}
}
