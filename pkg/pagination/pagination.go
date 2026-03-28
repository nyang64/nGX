package pagination

import (
	"encoding/base64"
	"fmt"
	"strings"
)

const (
	defaultLimit = 20
	maxLimit     = 100
)

// EncodeCursor base64-encodes a cursor string.
func EncodeCursor(parts ...string) string {
	raw := strings.Join(parts, "|")
	return base64.StdEncoding.EncodeToString([]byte(raw))
}

// DecodeCursor base64-decodes a cursor and splits on "|".
// Returns an empty slice if the cursor is empty or invalid.
func DecodeCursor(cursor string) ([]string, error) {
	if cursor == "" {
		return nil, nil
	}
	b, err := base64.StdEncoding.DecodeString(cursor)
	if err != nil {
		return nil, fmt.Errorf("invalid cursor: %w", err)
	}
	return strings.Split(string(b), "|"), nil
}

// ClampLimit returns limit clamped to [1, maxLimit], defaulting to defaultLimit when limit <= 0.
func ClampLimit(limit int) int {
	if limit <= 0 {
		return defaultLimit
	}
	if limit > maxLimit {
		return maxLimit
	}
	return limit
}
