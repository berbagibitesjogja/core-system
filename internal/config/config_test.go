package config

import (
	"os"
	"testing"
)

func TestLoadConfig_OAuth(t *testing.T) {
	t.Run("defaults when oauth env vars not set", func(t *testing.T) {
		_ = os.Unsetenv("GOOGLE_CLIENT_ID")
		_ = os.Unsetenv("GOOGLE_CLIENT_SECRET")
		_ = os.Unsetenv("GOOGLE_REDIRECT_URL")

		cfg := LoadConfig()

		if cfg.OAuth.GoogleClientID != "" {
			t.Errorf("expected empty GoogleClientID, got %s", cfg.OAuth.GoogleClientID)
		}
		if cfg.OAuth.GoogleClientSecret != "" {
			t.Errorf("expected empty GoogleClientSecret, got %s", cfg.OAuth.GoogleClientSecret)
		}
		if cfg.OAuth.GoogleRedirectURL != "http://localhost:8080/api/v1/auth/google/callback" {
			t.Errorf("expected default redirect url, got %s", cfg.OAuth.GoogleRedirectURL)
		}
		if cfg.OAuth.IsGoogleConfigured() {
			t.Error("expected IsGoogleConfigured to be false when credentials are empty")
		}
	})

	t.Run("loads custom oauth env vars when set", func(t *testing.T) {
		t.Setenv("GOOGLE_CLIENT_ID", "test-client-id-123")
		t.Setenv("GOOGLE_CLIENT_SECRET", "test-client-secret-456")
		t.Setenv("GOOGLE_REDIRECT_URL", "https://app.bbj.org/api/v1/auth/google/callback")

		cfg := LoadConfig()

		if cfg.OAuth.GoogleClientID != "test-client-id-123" {
			t.Errorf("expected test-client-id-123, got %s", cfg.OAuth.GoogleClientID)
		}
		if cfg.OAuth.GoogleClientSecret != "test-client-secret-456" {
			t.Errorf("expected test-client-secret-456, got %s", cfg.OAuth.GoogleClientSecret)
		}
		if cfg.OAuth.GoogleRedirectURL != "https://app.bbj.org/api/v1/auth/google/callback" {
			t.Errorf("expected custom redirect url, got %s", cfg.OAuth.GoogleRedirectURL)
		}
		if !cfg.OAuth.IsGoogleConfigured() {
			t.Error("expected IsGoogleConfigured to be true when both client_id and secret are set")
		}
	})
}
