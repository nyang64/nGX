package redis

import "fmt"

// RateLimitKey returns the Redis key for org+endpoint rate limiting.
func RateLimitKey(orgID, endpoint string) string { return fmt.Sprintf("rl:%s:%s", orgID, endpoint) }

// WebSocketChannel returns the pub/sub channel name for streaming events to an org.
func WebSocketChannel(orgID string) string { return fmt.Sprintf("ws:events:%s", orgID) }

// InboxCacheKey returns the cache key for an inbox record.
func InboxCacheKey(inboxID string) string { return fmt.Sprintf("inbox:%s", inboxID) }

// SessionKey returns the key for an authenticated session by key ID.
func SessionKey(keyID string) string { return fmt.Sprintf("session:%s", keyID) }

// APIKeyHashKey returns the key for a cached API key lookup by hash.
func APIKeyHashKey(hash string) string { return fmt.Sprintf("apikey:%s", hash) }
