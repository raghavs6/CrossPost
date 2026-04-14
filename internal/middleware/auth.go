package middleware

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

// contextKey is an unexported type used as the key for context values set by
// this package.  Using a dedicated type (instead of a plain string) prevents
// accidental key collisions with other packages that might also store values
// in the request context.
type contextKey string

const userIDKey contextKey = "user_id"

// jwtClaims mirrors the payload produced by handler.issueJWT.
// Both packages define this struct independently so neither depends on the
// other — keeping the dependency graph clean.
type jwtClaims struct {
	UserID uint `json:"user_id"`
	jwt.RegisteredClaims
}

// RequireAuth returns a middleware that validates the JWT in every request's
// Authorization header.
//
//   - If the header is missing or malformed → 401
//   - If the token is expired or has a bad signature → 401
//   - If the token is valid → the user ID is stored in the request context
//     and the next handler in the chain is called
//
// Usage in main.go:
//
//	r.Group(func(r chi.Router) {
//	    r.Use(middleware.RequireAuth(cfg.JWTSecret))
//	    r.Get("/api/posts", postsHandler.List)
//	})
func RequireAuth(jwtSecret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := r.Header.Get("Authorization")
			if header == "" {
				http.Error(w, "missing authorization header", http.StatusUnauthorized)
				return
			}

			// The Authorization header must be: "Bearer <token>"
			// strings.TrimPrefix is a no-op if the prefix isn't present,
			// so we check whether the string changed to detect a bad format.
			if !strings.HasPrefix(header, "Bearer ") {
				http.Error(w, "authorization header must start with 'Bearer '", http.StatusUnauthorized)
				return
			}
			tokenStr := strings.TrimPrefix(header, "Bearer ")

			var claims jwtClaims
			token, err := jwt.ParseWithClaims(tokenStr, &claims, func(t *jwt.Token) (interface{}, error) {
				// Guard against algorithm substitution attacks: reject any token
				// that was signed with a different algorithm than we expect.
				if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
					return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
				}
				return []byte(jwtSecret), nil
			})

			if err != nil || !token.Valid {
				http.Error(w, "invalid or expired token", http.StatusUnauthorized)
				return
			}

			// Attach the user ID to the request context so downstream handlers
			// can identify the caller without an extra database lookup.
			ctx := context.WithValue(r.Context(), userIDKey, claims.UserID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// UserIDFromContext retrieves the user ID stored by RequireAuth.
// Returns 0 if the context has no user ID (this should not happen inside a
// route group protected by RequireAuth).
func UserIDFromContext(ctx context.Context) uint {
	id, _ := ctx.Value(userIDKey).(uint)
	return id
}
