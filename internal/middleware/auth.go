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
				response.Unauthorized(w, r, "Invalid or expired authentication token")
				return
			}

			ctx := WithClaims(r.Context(), claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func OptionalAuthenticate(jwtSecret string) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := r.Header.Get("Authorization")
			if header == "" {
				next.ServeHTTP(w, r)
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
				response.Unauthorized(w, r, "Invalid or expired authentication token")
				return
			}

			ctx := WithClaims(r.Context(), claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func RequireStaff(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := GetClaims(r.Context())
		if !ok || claims == nil {
			response.SetError(r.Context(), errors.New("authentication required"))
			response.Unauthorized(w, r, "Authentication required")
			return
		}

		if !claims.IsStaff {
			response.SetError(r.Context(), errors.New("insufficient permissions: staff access required"))
			response.Forbidden(w, r, "Staff access required")
			return
		}

		next.ServeHTTP(w, r)
	})
}

func RequireOperationalStaff(divisionOperational int32) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := GetClaims(r.Context())
			if !ok || claims == nil {
				response.SetError(r.Context(), errors.New("authentication required"))
				response.Unauthorized(w, r, "Authentication required")
				return
			}

			if !claims.IsStaff {
				response.SetError(r.Context(), errors.New("insufficient permissions: staff access required"))
				response.Forbidden(w, r, "Staff access required")
				return
			}

			if claims.Role != "super" && claims.DivisionID != divisionOperational {
				response.SetError(r.Context(), errors.New("insufficient permissions: operational division or super admin access required"))
				response.Forbidden(w, r, "Operational division or super admin access required")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func RequireCoreStaff(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := GetClaims(r.Context())
		if !ok || claims == nil {
			response.SetError(r.Context(), errors.New("authentication required"))
			response.Unauthorized(w, r, "Authentication required")
			return
		}

		if !claims.IsStaff {
			response.SetError(r.Context(), errors.New("insufficient permissions: staff access required"))
			response.Forbidden(w, r, "Staff access required")
			return
		}

		if claims.Role != "super" && claims.Role != "core" {
			response.SetError(r.Context(), errors.New("insufficient permissions: core or super admin access required"))
			response.Forbidden(w, r, "Core or super admin access required")
			return
		}

		next.ServeHTTP(w, r)
	})
}

func RequireHero(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := GetClaims(r.Context())
		if !ok || claims == nil {
			response.SetError(r.Context(), errors.New("authentication required"))
			response.Unauthorized(w, r, "Authentication required")
			return
		}

		if !claims.IsHero {
			response.SetError(r.Context(), errors.New("insufficient permissions: hero access required"))
			response.Forbidden(w, r, "Hero access required")
			return
		}

		next.ServeHTTP(w, r)
	})
}

func GetClaims(ctx context.Context) (*auth.Claims, bool) {
	if ctx == nil {
		return nil, false
	}
	claims, ok := ctx.Value(authContextKey{}).(*auth.Claims)
	return claims, ok
}
