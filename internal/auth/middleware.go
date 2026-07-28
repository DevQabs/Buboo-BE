package auth

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
)

type contextKey string

const (
	ctxUserID   contextKey = "user_id"
	ctxCoupleID contextKey = "couple_id"
	ctxRole     contextKey = "role"
)

// RequireAuth is a chi middleware that verifies Bearer JWT and injects claims into context.
func RequireAuth(secret []byte) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := r.Header.Get("Authorization")
			if !strings.HasPrefix(header, "Bearer ") {
				log.Printf("auth: missing bearer path=%s ua=%q header=%q", r.URL.Path, r.UserAgent(), header)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
				return
			}
			tokenStr := strings.TrimPrefix(header, "Bearer ")
			claims, err := VerifyToken(secret, tokenStr)
			if err != nil {
				tail := tokenStr
				if len(tail) > 16 {
					tail = tail[len(tail)-16:]
				}
				log.Printf("auth: invalid token path=%s ua=%q len=%d tail=%q err=%v", r.URL.Path, r.UserAgent(), len(tokenStr), tail, err)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				json.NewEncoder(w).Encode(map[string]string{"error": "invalid token"})
				return
			}
			ctx := context.WithValue(r.Context(), ctxUserID, claims.UserID)
			ctx = context.WithValue(ctx, ctxCoupleID, claims.CoupleID)
			ctx = context.WithValue(ctx, ctxRole, claims.Role)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func UserIDFromCtx(ctx context.Context) string {
	v, _ := ctx.Value(ctxUserID).(string)
	return v
}

func CoupleIDFromCtx(ctx context.Context) string {
	v, _ := ctx.Value(ctxCoupleID).(string)
	return v
}

func RoleFromCtx(ctx context.Context) string {
	v, _ := ctx.Value(ctxRole).(string)
	return v
}
