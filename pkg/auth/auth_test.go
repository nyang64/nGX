/*
 * Copyright (c) 2026 nyklabs.com. All rights reserved.
 *
 * Licensed under the nGX Commercial Source License v1.0.
 * See LICENSE file in the project root for full license information.
 */

package auth

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// GenerateAPIKey
// ---------------------------------------------------------------------------

func TestGenerateAPIKey_Prefix(t *testing.T) {
	plaintext, _, _, err := GenerateAPIKey()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(plaintext, "am_live_") {
		t.Errorf("plaintext %q does not start with 'am_live_'", plaintext)
	}
}

func TestGenerateAPIKey_DisplayPrefix(t *testing.T) {
	plaintext, _, displayPrefix, err := GenerateAPIKey()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if displayPrefix != plaintext[:16] {
		t.Errorf("displayPrefix = %q, want first 16 chars %q", displayPrefix, plaintext[:16])
	}
}

func TestGenerateAPIKey_HashMatchesPlaintext(t *testing.T) {
	plaintext, hash, _, err := GenerateAPIKey()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if HashAPIKey(plaintext) != hash {
		t.Errorf("hash does not match HashAPIKey(plaintext)")
	}
}

func TestGenerateAPIKey_VerifyRoundTrip(t *testing.T) {
	plaintext, hash, _, err := GenerateAPIKey()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !VerifyAPIKey(plaintext, hash) {
		t.Errorf("VerifyAPIKey returned false for freshly generated key pair")
	}
}

func TestGenerateAPIKey_Uniqueness(t *testing.T) {
	pt1, _, _, err1 := GenerateAPIKey()
	pt2, _, _, err2 := GenerateAPIKey()
	if err1 != nil || err2 != nil {
		t.Fatalf("unexpected errors: %v, %v", err1, err2)
	}
	if pt1 == pt2 {
		t.Errorf("two GenerateAPIKey calls produced identical plaintext keys")
	}
}

// ---------------------------------------------------------------------------
// HashAPIKey
// ---------------------------------------------------------------------------

func TestHashAPIKey_Deterministic(t *testing.T) {
	h1 := HashAPIKey("am_live_test")
	h2 := HashAPIKey("am_live_test")
	if h1 != h2 {
		t.Errorf("HashAPIKey not deterministic: %q != %q", h1, h2)
	}
}

func TestHashAPIKey_DifferentInputs(t *testing.T) {
	h1 := HashAPIKey("am_live_aaa")
	h2 := HashAPIKey("am_live_bbb")
	if h1 == h2 {
		t.Errorf("HashAPIKey produced same hash for different inputs")
	}
}

func TestHashAPIKey_Length(t *testing.T) {
	h := HashAPIKey("am_live_test")
	if len(h) != 64 {
		t.Errorf("HashAPIKey returned %d-char string, want 64", len(h))
	}
}

// ---------------------------------------------------------------------------
// VerifyAPIKey
// ---------------------------------------------------------------------------

func TestVerifyAPIKey_Correct(t *testing.T) {
	plaintext := "am_live_correctkey"
	hash := HashAPIKey(plaintext)
	if !VerifyAPIKey(plaintext, hash) {
		t.Errorf("VerifyAPIKey returned false for correct pair")
	}
}

func TestVerifyAPIKey_WrongPlaintext(t *testing.T) {
	hash := HashAPIKey("am_live_correctkey")
	if VerifyAPIKey("am_live_wrongkey", hash) {
		t.Errorf("VerifyAPIKey returned true for wrong plaintext")
	}
}

func TestVerifyAPIKey_TamperedHash(t *testing.T) {
	plaintext := "am_live_correctkey"
	hash := HashAPIKey(plaintext)
	tampered := hash[:len(hash)-1] + "x"
	if VerifyAPIKey(plaintext, tampered) {
		t.Errorf("VerifyAPIKey returned true for tampered hash")
	}
}

// ---------------------------------------------------------------------------
// Claims.HasScope
// ---------------------------------------------------------------------------

func TestHasScope_Present(t *testing.T) {
	c := &Claims{Scopes: []Scope{ScopeInboxRead, ScopeInboxWrite}}
	if !c.HasScope(ScopeInboxRead) {
		t.Errorf("HasScope returned false for scope that is present")
	}
}

func TestHasScope_Absent(t *testing.T) {
	c := &Claims{Scopes: []Scope{ScopeInboxRead}}
	if c.HasScope(ScopeInboxWrite) {
		t.Errorf("HasScope returned true for scope that is absent")
	}
}

func TestHasScope_OrgAdminGrantsAll(t *testing.T) {
	c := &Claims{Scopes: []Scope{ScopeOrgAdmin}}
	unknownScope := Scope("completely:unknown")
	if !c.HasScope(unknownScope) {
		t.Errorf("ScopeOrgAdmin should grant any scope, but HasScope returned false for %q", unknownScope)
	}
	for _, s := range AllScopes {
		if !c.HasScope(s) {
			t.Errorf("ScopeOrgAdmin should grant %q, but HasScope returned false", s)
		}
	}
}

// ---------------------------------------------------------------------------
// Claims.CanAccessPod
// ---------------------------------------------------------------------------

func TestCanAccessPod_NilPodID(t *testing.T) {
	c := &Claims{PodID: nil}
	anyID := uuid.New()
	if !c.CanAccessPod(anyID) {
		t.Errorf("nil PodID should allow access to any pod")
	}
}

func TestCanAccessPod_MatchingPodID(t *testing.T) {
	id := uuid.New()
	c := &Claims{PodID: &id}
	if !c.CanAccessPod(id) {
		t.Errorf("CanAccessPod returned false for matching PodID")
	}
}

func TestCanAccessPod_NonMatchingPodID(t *testing.T) {
	id := uuid.New()
	other := uuid.New()
	c := &Claims{PodID: &id}
	if c.CanAccessPod(other) {
		t.Errorf("CanAccessPod returned true for non-matching PodID")
	}
}

// ---------------------------------------------------------------------------
// WithClaims / ClaimsFromCtx / OrgIDFromCtx
// ---------------------------------------------------------------------------

func TestClaimsRoundTrip(t *testing.T) {
	orgID := uuid.New()
	c := &Claims{OrgID: orgID, Scopes: []Scope{ScopeInboxRead}}
	ctx := WithClaims(context.Background(), c)
	got := ClaimsFromCtx(ctx)
	if got == nil {
		t.Fatal("ClaimsFromCtx returned nil after WithClaims")
	}
	if got.OrgID != orgID {
		t.Errorf("OrgID mismatch: got %v, want %v", got.OrgID, orgID)
	}
}

func TestClaimsFromCtx_Empty(t *testing.T) {
	got := ClaimsFromCtx(context.Background())
	if got != nil {
		t.Errorf("ClaimsFromCtx should return nil for empty context, got %+v", got)
	}
}

func TestOrgIDFromCtx_Empty(t *testing.T) {
	got := OrgIDFromCtx(context.Background())
	if got != uuid.Nil {
		t.Errorf("OrgIDFromCtx should return uuid.Nil for empty context, got %v", got)
	}
}

func TestOrgIDFromCtx_WithClaims(t *testing.T) {
	orgID := uuid.New()
	c := &Claims{OrgID: orgID}
	ctx := WithClaims(context.Background(), c)
	got := OrgIDFromCtx(ctx)
	if got != orgID {
		t.Errorf("OrgIDFromCtx = %v, want %v", got, orgID)
	}
}
