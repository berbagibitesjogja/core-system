package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOAuthState_GenerateAndVerify(t *testing.T) {
	secret := "super-secure-jwt-secret-key-32b!"

	t.Run("successful roundtrip without invitation token", func(t *testing.T) {
		state, nonce, err := GenerateOAuthState(secret, "")
		if err != nil {
			t.Fatalf("unexpected error generating state: %v", err)
		}

		if state == "" || nonce == "" {
			t.Fatal("expected non-empty state and nonce")
		}

		invToken, err := VerifyOAuthState(secret, state, nonce)
		if err != nil {
			t.Fatalf("unexpected error verifying state: %v", err)
		}

		if invToken != "" {
			t.Errorf("expected empty invitation token, got %s", invToken)
		}
	})

	t.Run("successful roundtrip with invitation token", func(t *testing.T) {
		expectedInvToken := "my-raw-invitation-token-12345"
		state, nonce, err := GenerateOAuthState(secret, expectedInvToken)
		if err != nil {
			t.Fatalf("unexpected error generating state: %v", err)
		}

		invToken, err := VerifyOAuthState(secret, state, nonce)
		if err != nil {
			t.Fatalf("unexpected error verifying state: %v", err)
		}

		if invToken != expectedInvToken {
			t.Errorf("expected invitation token %s, got %s", expectedInvToken, invToken)
		}
	})

	t.Run("fails when nonce does not match cookie", func(t *testing.T) {
		state, _, err := GenerateOAuthState(secret, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		_, err = VerifyOAuthState(secret, state, "wrong-nonce")
		if err == nil || !strings.Contains(err.Error(), "nonce does not match") {
			t.Fatalf("expected nonce mismatch error, got: %v", err)
		}
	})

	t.Run("fails when signature is tampered", func(t *testing.T) {
		state, nonce, err := GenerateOAuthState(secret, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		tamperedState := state + "tampered"
		_, err = VerifyOAuthState(secret, tamperedState, nonce)
		if err == nil || !strings.Contains(err.Error(), "signature verification failed") {
			t.Fatalf("expected signature verification failure, got: %v", err)
		}
	})

	t.Run("fails when wrong secret is used", func(t *testing.T) {
		state, nonce, err := GenerateOAuthState(secret, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		_, err = VerifyOAuthState("different-secret-key", state, nonce)
		if err == nil || !strings.Contains(err.Error(), "signature verification failed") {
			t.Fatalf("expected signature verification failure, got: %v", err)
		}
	})

	t.Run("fails on empty state or empty cookie", func(t *testing.T) {
		_, err := VerifyOAuthState(secret, "", "some-nonce")
		if err == nil {
			t.Error("expected error on empty state")
		}

		_, err = VerifyOAuthState(secret, "some.state", "")
		if err == nil {
			t.Error("expected error on empty cookie nonce")
		}
	})
}

func TestOAuthCookies(t *testing.T) {
	t.Run("sets http-only cookie with proper attributes", func(t *testing.T) {
		rec := httptest.NewRecorder()
		SetOAuthStateCookie(rec, "test-nonce-123", true)

		res := rec.Result()
		cookies := res.Cookies()
		if len(cookies) == 0 {
			t.Fatal("expected cookie to be set")
		}

		c := cookies[0]
		if c.Name != OAuthStateCookieName {
			t.Errorf("expected cookie name %s, got %s", OAuthStateCookieName, c.Name)
		}
		if c.Value != "test-nonce-123" {
			t.Errorf("expected cookie value test-nonce-123, got %s", c.Value)
		}
		if !c.HttpOnly {
			t.Error("expected HttpOnly to be true")
		}
		if !c.Secure {
			t.Error("expected Secure to be true")
		}
		if c.SameSite != http.SameSiteLaxMode {
			t.Errorf("expected SameSite=Lax, got %v", c.SameSite)
		}
	})

	t.Run("clears cookie with negative max age", func(t *testing.T) {
		rec := httptest.NewRecorder()
		ClearOAuthStateCookie(rec, false)

		res := rec.Result()
		cookies := res.Cookies()
		if len(cookies) == 0 {
			t.Fatal("expected cookie to be cleared")
		}

		c := cookies[0]
		if c.MaxAge != -1 {
			t.Errorf("expected MaxAge -1, got %d", c.MaxAge)
		}
	})
}

func TestFetchGoogleUserInfo(t *testing.T) {
	t.Run("successfully parses valid userinfo response", func(t *testing.T) {
		expectedUser := GoogleUserInfo{
			Sub:           "google-sub-1029384756",
			Email:         "alex.dev@gmail.com",
			EmailVerified: true,
			Name:          "Alex Developer",
			Picture:       "https://googleusercontent.com/avatar.jpg",
		}

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(expectedUser)
		}))
		defer server.Close()

		ctx := context.Background()
		user, err := FetchGoogleUserInfo(ctx, http.DefaultClient, server.URL)
		if err != nil {
			t.Fatalf("unexpected error fetching user info: %v", err)
		}

		if user.Sub != expectedUser.Sub || user.Email != expectedUser.Email || user.Name != expectedUser.Name {
			t.Fatalf("userinfo mismatch, expected %+v, got %+v", expectedUser, *user)
		}
	})

	t.Run("fails when server returns 401 unauthorized", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "invalid token", http.StatusUnauthorized)
		}))
		defer server.Close()

		ctx := context.Background()
		_, err := FetchGoogleUserInfo(ctx, http.DefaultClient, server.URL)
		if err == nil || !strings.Contains(err.Error(), "status 401") {
			t.Fatalf("expected 401 status error, got: %v", err)
		}
	})

	t.Run("fails when response lacks sub or email", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"name": "No Sub User"})
		}))
		defer server.Close()

		ctx := context.Background()
		_, err := FetchGoogleUserInfo(ctx, http.DefaultClient, server.URL)
		if err == nil || !strings.Contains(err.Error(), "missing required sub or email") {
			t.Fatalf("expected missing sub/email error, got: %v", err)
		}
	})
}
