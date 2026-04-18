/*
 * Copyright (c) 2026 nyklabs.com. All rights reserved.
 *
 * Licensed under the nGX Commercial Source License v1.0.
 * See LICENSE file in the project root for full license information.
 */

package crypto

import (
	"bytes"
	"strings"
	"testing"
)

// testHexKey is a valid 64-char hex string encoding a 32-byte AES-256 key.
const testHexKey = "0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20"

// ---------------------------------------------------------------------------
// KeyFromHex
// ---------------------------------------------------------------------------

func TestKeyFromHex_EmptyReturnsError(t *testing.T) {
	_, err := KeyFromHex("")
	if err == nil {
		t.Error("expected error for empty string, got nil")
	}
}

func TestKeyFromHex_Valid(t *testing.T) {
	key, err := KeyFromHex(testHexKey)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(key) != 32 {
		t.Errorf("key length = %d, want 32", len(key))
	}
}

func TestKeyFromHex_InvalidHex(t *testing.T) {
	_, err := KeyFromHex("zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz")
	if err == nil {
		t.Error("expected error for invalid hex chars, got nil")
	}
}

func TestKeyFromHex_WrongLength(t *testing.T) {
	// 32 hex chars = 16 bytes — too short for AES-256
	shortHex := "0102030405060708090a0b0c0d0e0f10"
	_, err := KeyFromHex(shortHex)
	if err == nil {
		t.Error("expected error for 16-byte key, got nil")
	}
}

// ---------------------------------------------------------------------------
// Encrypt
// ---------------------------------------------------------------------------

func TestEncrypt_Success(t *testing.T) {
	key, _ := KeyFromHex(testHexKey)
	plaintext := []byte("hello, world")
	ct, err := Encrypt(key, plaintext)
	if err != nil {
		t.Fatalf("Encrypt error: %v", err)
	}
	if len(ct) == 0 {
		t.Error("ciphertext is empty")
	}
}

func TestEncrypt_RandomNonce(t *testing.T) {
	key, _ := KeyFromHex(testHexKey)
	plaintext := []byte("same plaintext every time")
	ct1, err1 := Encrypt(key, plaintext)
	ct2, err2 := Encrypt(key, plaintext)
	if err1 != nil || err2 != nil {
		t.Fatalf("Encrypt errors: %v, %v", err1, err2)
	}
	if bytes.Equal(ct1, ct2) {
		t.Error("two encryptions of the same plaintext produced identical ciphertext (nonce not random)")
	}
}

func TestEncrypt_LongerThanPlaintext(t *testing.T) {
	key, _ := KeyFromHex(testHexKey)
	plaintext := []byte("short")
	ct, err := Encrypt(key, plaintext)
	if err != nil {
		t.Fatalf("Encrypt error: %v", err)
	}
	// nonce (12 bytes) + plaintext + GCM tag (16 bytes) > len(plaintext)
	if len(ct) <= len(plaintext) {
		t.Errorf("ciphertext length %d should be > plaintext length %d", len(ct), len(plaintext))
	}
}

// ---------------------------------------------------------------------------
// Decrypt
// ---------------------------------------------------------------------------

func TestDecrypt_RoundTrip(t *testing.T) {
	key, _ := KeyFromHex(testHexKey)
	plaintext := []byte("round-trip test value")
	ct, err := Encrypt(key, plaintext)
	if err != nil {
		t.Fatalf("Encrypt error: %v", err)
	}
	got, err := Decrypt(key, ct)
	if err != nil {
		t.Fatalf("Decrypt error: %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Errorf("Decrypt = %q, want %q", got, plaintext)
	}
}

func TestDecrypt_WrongKey(t *testing.T) {
	key, _ := KeyFromHex(testHexKey)
	otherKeyHex := "a0a1a2a3a4a5a6a7a8a9aaabacadaeafb0b1b2b3b4b5b6b7b8b9babbbcbdbebf"
	otherKey, _ := KeyFromHex(otherKeyHex)

	ct, err := Encrypt(key, []byte("secret"))
	if err != nil {
		t.Fatalf("Encrypt error: %v", err)
	}
	_, err = Decrypt(otherKey, ct)
	if err == nil {
		t.Error("expected error when decrypting with wrong key, got nil")
	}
}

func TestDecrypt_TooShort(t *testing.T) {
	key, _ := KeyFromHex(testHexKey)
	// fewer bytes than the 12-byte nonce
	_, err := Decrypt(key, []byte("tooshort"))
	if err == nil {
		t.Fatal("expected error for truncated ciphertext, got nil")
	}
	if !strings.Contains(err.Error(), "ciphertext too short") {
		t.Errorf("error message %q does not contain 'ciphertext too short'", err.Error())
	}
}

func TestDecrypt_TamperedCiphertext(t *testing.T) {
	key, _ := KeyFromHex(testHexKey)
	ct, err := Encrypt(key, []byte("tamper me"))
	if err != nil {
		t.Fatalf("Encrypt error: %v", err)
	}
	// Flip the last byte to corrupt the GCM authentication tag.
	ct[len(ct)-1] ^= 0xff
	_, err = Decrypt(key, ct)
	if err == nil {
		t.Error("expected error for tampered ciphertext, got nil")
	}
}
