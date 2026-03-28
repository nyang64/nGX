package middleware

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"agentmail/pkg/auth"
	redispkg "agentmail/pkg/redis"

	"github.com/redis/go-redis/v9"
)

// RateLimit returns middleware that applies a sliding-window counter rate limit
// per org, keyed by path. limit is the maximum number of requests per window.
// On Redis errors the middleware fails open (allows the request through).
func RateLimit(client *redis.Client, limit int, window time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims := auth.ClaimsFromCtx(r.Context())
			if claims == nil {
				next.ServeHTTP(w, r)
				return
			}

			key := redispkg.RateLimitKey(claims.OrgID.String(), r.URL.Path)
			ctx := r.Context()

			pipe := client.Pipeline()
			incr := pipe.Incr(ctx, key)
			pipe.Expire(ctx, key, window)
			if _, err := pipe.Exec(ctx); err != nil {
				next.ServeHTTP(w, r) // fail open
				return
			}

			count := incr.Val()
			remaining := limit - int(count)
			if remaining < 0 {
				remaining = 0
			}

			w.Header().Set("X-RateLimit-Limit", strconv.Itoa(limit))
			w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(remaining))

			if int(count) > limit {
				w.Header().Set("Retry-After", fmt.Sprintf("%d", int(window.Seconds())))
				writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
