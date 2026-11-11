package middleware_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/berbagibitesjogja/core-system/internal/auth"
	"github.com/berbagibitesjogja/core-system/internal/middleware"
)

func TestRequireStaff(t *testing.T) {
	jwtSecret := "super-secure-jwt-secret-key-32-bytes!!"
	userID := uuid.New()

	staffToken, err := auth.SignAccessToken(userID, "staff@bbj.com", true, "staff", 1, false, jwtSecret, 15*time.Minute)
	if err != nil {
		t.Fatalf("failed to sign staff token: %v", err)
	}

	heroToken, err := auth.SignAccessToken(userID, "hero@ugm.ac.id", false, "", 0, true, jwtSecret, 15*time.Minute)
	if err != nil {
		t.Fatalf("failed to sign hero token: %v", err)
	}

	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("staff-access-granted"))
	})

	stack := middleware.Authenticate(jwtSecret)(middleware.RequireStaff(nextHandler))

	t.Run("staff succeeds", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Authorization", "Bearer "+staffToken)
		rec := httptest.NewRecorder()

		stack.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rec.Code)
		}
	})

	t.Run("hero blocked with 403 Forbidden", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Authorization", "Bearer "+heroToken)
		rec := httptest.NewRecorder()

		stack.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 Forbidden, got %d", rec.Code)
		}
	})

	t.Run("unauthenticated blocked with 401 Unauthorized", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()

		stack.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 Unauthorized, got %d", rec.Code)
		}
	})

	t.Run("invalid or expired token returns sanitized error message", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Authorization", "Bearer invalid-or-malformed-token")
		rec := httptest.NewRecorder()

		stack.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 Unauthorized, got %d", rec.Code)
		}

		var resp map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to parse JSON response: %v", err)
		}
		if msg, ok := resp["message"].(string); !ok || msg != "Invalid or expired authentication token" {
			t.Fatalf("expected sanitized message 'Invalid or expired authentication token', got %q", msg)
		}
	})
}

func TestRequireHero(t *testing.T) {
	jwtSecret := "super-secure-jwt-secret-key-32-bytes!!"
	userID := uuid.New()

	staffToken, _ := auth.SignAccessToken(userID, "staff@bbj.com", true, "staff", 1, false, jwtSecret, 15*time.Minute)
	heroToken, _ := auth.SignAccessToken(userID, "hero@ugm.ac.id", false, "", 0, true, jwtSecret, 15*time.Minute)

	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("hero-access-granted"))
	})

	stack := middleware.Authenticate(jwtSecret)(middleware.RequireHero(nextHandler))

	t.Run("hero succeeds", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Authorization", "Bearer "+heroToken)
		rec := httptest.NewRecorder()

		stack.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rec.Code)
		}
	})

	t.Run("staff blocked with 403 Forbidden", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Authorization", "Bearer "+staffToken)
		rec := httptest.NewRecorder()

		stack.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 Forbidden, got %d", rec.Code)
		}
	})
}
