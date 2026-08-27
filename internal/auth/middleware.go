package auth

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"
)

const bearerPrefix = "Bearer "

// RequireToken wraps next, rejecting any request whose Authorization header
// doesn't carry exactly token. Comparison uses subtle.ConstantTimeCompare
// rather than ==, since a length/byte-at-a-time short-circuit on == would
// let a network attacker recover the token by timing many guesses.
func RequireToken(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, ok := bearerToken(r)
		if !ok || subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
			writeUnauthorized(w)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func bearerToken(r *http.Request) (string, bool) {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, bearerPrefix) {
		return "", false
	}
	return strings.TrimPrefix(h, bearerPrefix), true
}

func writeUnauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
}
