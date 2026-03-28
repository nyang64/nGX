package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	authpkg "agentmail/pkg/auth"
)

// ValidateKeyFunc is called with the raw Bearer token to produce Claims.
type ValidateKeyFunc func(ctx context.Context, key string) (*authpkg.Claims, error)

// Authenticator returns HTTP middleware that validates Bearer tokens using fn.
// On success the resolved Claims are stored in the request context.
func Authenticator(validateKey ValidateKeyFunc) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := extractBearerToken(r)
			if key == "" {
				writeError(w, http.StatusUnauthorized, "missing or invalid authorization header")
				return
			}
			claims, err := validateKey(r.Context(), key)
			if err != nil {
				writeError(w, http.StatusUnauthorized, "invalid or expired api key")
				return
			}
			ctx := authpkg.WithClaims(r.Context(), claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func extractBearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	return ""
}

func writeError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
