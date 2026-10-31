package middleware

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/berbagibitesjogja/core-system/internal/auth"
	"github.com/berbagibitesjogja/core-system/internal/response"
)

type authContextKey struct{}

func WithClaims(ctx context.Context, claims *auth.Claims) context.Context {
	return context.WithValue(ctx, authContextKey{}, claims)
}

func Authenticate(jwtSecret string) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := r.Header.Get("Authorization")
			if header == "" {
				response.SetError(r.Context(), errors.New("missing authorization header"))
				response.Unauthorized(w, r, "Missing auth header")
				return
			}

			parts := strings.SplitN(header, " ", 2)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
				response.SetError(r.Context(), errors.New("invalid authorization header format"))
				response.Unauthorized(w, r, "Invalid auth header")
				return
			}

			claims, err := auth.ParseToken(parts[1], jwtSecret)
			if err != nil {
				response.SetError(r.Context(), err)
				response.Unauthorized(w, r, "Invalid token: "+err.Error())
				return
			}

			ctx := WithClaims(r.Context(), claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func GetClaims(ctx context.Context) (*auth.Claims, bool) {
	if ctx == nil {
		return nil, false
	}
	claims, ok := ctx.Value(authContextKey{}).(*auth.Claims)
	return claims, ok
}
