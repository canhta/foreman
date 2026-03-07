package dashboard

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
)

// tokenSalt is a constant salt prepended before hashing auth tokens.
// Using HMAC-SHA256 with this key prevents rainbow table attacks against
// the stored token hashes.
const tokenSalt = "foreman-auth-token-v1"

// AuthValidator is a subset of db.Database for token validation.
type AuthValidator interface {
	ValidateAuthToken(ctx context.Context, tokenHash string) (bool, error)
}

func hashToken(token string) string {
	mac := hmac.New(sha256.New, []byte(tokenSalt))
	mac.Write([]byte(token))
	return hex.EncodeToString(mac.Sum(nil))
}

func extractBearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		return ""
	}
	return strings.TrimPrefix(auth, "Bearer ")
}

func authMiddleware(db AuthValidator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := extractBearerToken(r)
			if token == "" {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			hash := hashToken(token)
			valid, err := db.ValidateAuthToken(r.Context(), hash)
			if err != nil || !valid {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
