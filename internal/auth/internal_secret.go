package auth

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
)

// RequireInternalSecret only lets through requests that carry the shared
// secret in X-Internal-Secret. Used on routes that trust a Google identity
// sent in the body, so only the Next.js server (which read it from its own
// session) can call them.
func RequireInternalSecret(secret []byte) func(http.Handler) http.Handler {
	return RequireHeaderSecret("X-Internal-Secret", secret)
}

// RequireHeaderSecret passes only requests whose header equals secret
// (constant-time). An empty secret rejects everything.
func RequireHeaderSecret(header string, secret []byte) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got := []byte(r.Header.Get(header))
			if len(secret) == 0 || subtle.ConstantTimeCompare(got, secret) != 1 {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
