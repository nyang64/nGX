package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

const keyPrefix = "am_live_"

// GenerateAPIKey creates a new API key.
// Returns (plaintext, hash, displayPrefix, error).
// The plaintext is shown once; only the hash should be stored.
func GenerateAPIKey() (plaintext, hash, displayPrefix string, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return "", "", "", fmt.Errorf("generate random bytes: %w", err)
	}
	plaintext = keyPrefix + base64.RawURLEncoding.EncodeToString(b)
	h := sha256.Sum256([]byte(plaintext))
	hash = hex.EncodeToString(h[:])
	// displayPrefix: first 16 chars (covers "am_live_" + 8 chars of the random segment)
	if len(plaintext) >= 16 {
		displayPrefix = plaintext[:16]
	} else {
		displayPrefix = plaintext
	}
	return plaintext, hash, displayPrefix, nil
}

// HashAPIKey returns the SHA-256 hex hash of a plaintext key.
func HashAPIKey(plaintext string) string {
	h := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(h[:])
}

// VerifyAPIKey reports whether plaintext matches the stored hash.
func VerifyAPIKey(plaintext, hash string) bool {
	return HashAPIKey(plaintext) == hash
}
