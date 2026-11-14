package config

import (
	"os"
	"testing"
	"time"
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

func TestLoadConfig_War(t *testing.T) {
	warEnvKeys := []string{
		"WAR_LATE_CANCELLATION_WINDOW",
		"WAR_HERO_BAN_EVENT_COUNT",
		"WAR_STAFF_BAN_EVENT_COUNT",
		"WAR_HERO_CANCEL_GRACE_PERIOD",
		"WAR_STAFF_CANCEL_GRACE_PERIOD",
		"WAR_HERO_ATTENDANCE_WINDOW_BEFORE",
		"WAR_STAFF_ATTENDANCE_WINDOW_BEFORE",
		"WAR_STAFF_SELF_CHECKIN_BEFORE",
		"WAR_STAFF_SELF_CHECKIN_AFTER",
	}

	t.Run("defaults when war env vars not set", func(t *testing.T) {
		for _, k := range warEnvKeys {
			t.Setenv(k, "")
			_ = os.Unsetenv(k)
		}

		cfg := LoadConfig()

		if cfg.War.LateCancellationWindow != 12*time.Hour {
			t.Errorf("expected LateCancellationWindow=12h, got %s", cfg.War.LateCancellationWindow)
		}
		if cfg.War.HeroBanEventCount != 2 {
			t.Errorf("expected HeroBanEventCount=2, got %d", cfg.War.HeroBanEventCount)
		}
		if cfg.War.StaffBanEventCount != 1 {
			t.Errorf("expected StaffBanEventCount=1, got %d", cfg.War.StaffBanEventCount)
		}
		if cfg.War.HeroCancelGracePeriod != 15*time.Minute {
			t.Errorf("expected HeroCancelGracePeriod=15m, got %s", cfg.War.HeroCancelGracePeriod)
		}
		if cfg.War.StaffCancelGracePeriod != 15*time.Minute {
			t.Errorf("expected StaffCancelGracePeriod=15m, got %s", cfg.War.StaffCancelGracePeriod)
		}
		if cfg.War.HeroAttendanceWindowBefore != 1*time.Hour {
			t.Errorf("expected HeroAttendanceWindowBefore=1h, got %s", cfg.War.HeroAttendanceWindowBefore)
		}
		if cfg.War.StaffAttendanceWindowBefore != 1*time.Hour {
			t.Errorf("expected StaffAttendanceWindowBefore=1h, got %s", cfg.War.StaffAttendanceWindowBefore)
		}
		if cfg.War.StaffSelfCheckInBefore != 1*time.Hour {
			t.Errorf("expected StaffSelfCheckInBefore=1h, got %s", cfg.War.StaffSelfCheckInBefore)
		}
		if cfg.War.StaffSelfCheckInAfter != 4*time.Hour {
			t.Errorf("expected StaffSelfCheckInAfter=4h, got %s", cfg.War.StaffSelfCheckInAfter)
		}
	})

	t.Run("loads custom separated hero and staff war env vars", func(t *testing.T) {
		t.Setenv("WAR_LATE_CANCELLATION_WINDOW", "6h")
		t.Setenv("WAR_HERO_BAN_EVENT_COUNT", "3")
		t.Setenv("WAR_STAFF_BAN_EVENT_COUNT", "2")
		t.Setenv("WAR_HERO_CANCEL_GRACE_PERIOD", "10m")
		t.Setenv("WAR_STAFF_CANCEL_GRACE_PERIOD", "30m")
		t.Setenv("WAR_HERO_ATTENDANCE_WINDOW_BEFORE", "45m")
		t.Setenv("WAR_STAFF_ATTENDANCE_WINDOW_BEFORE", "2h")
		t.Setenv("WAR_STAFF_SELF_CHECKIN_BEFORE", "48h")
		t.Setenv("WAR_STAFF_SELF_CHECKIN_AFTER", "6h")

		cfg := LoadConfig()

		if cfg.War.LateCancellationWindow != 6*time.Hour {
			t.Errorf("expected LateCancellationWindow=6h, got %s", cfg.War.LateCancellationWindow)
		}
		if cfg.War.HeroBanEventCount != 3 {
			t.Errorf("expected HeroBanEventCount=3, got %d", cfg.War.HeroBanEventCount)
		}
		if cfg.War.StaffBanEventCount != 2 {
			t.Errorf("expected StaffBanEventCount=2, got %d", cfg.War.StaffBanEventCount)
		}
		if cfg.War.HeroCancelGracePeriod != 10*time.Minute {
			t.Errorf("expected HeroCancelGracePeriod=10m, got %s", cfg.War.HeroCancelGracePeriod)
		}
		if cfg.War.StaffCancelGracePeriod != 30*time.Minute {
			t.Errorf("expected StaffCancelGracePeriod=30m, got %s", cfg.War.StaffCancelGracePeriod)
		}
		if cfg.War.HeroAttendanceWindowBefore != 45*time.Minute {
			t.Errorf("expected HeroAttendanceWindowBefore=45m, got %s", cfg.War.HeroAttendanceWindowBefore)
		}
		if cfg.War.StaffAttendanceWindowBefore != 2*time.Hour {
			t.Errorf("expected StaffAttendanceWindowBefore=2h, got %s", cfg.War.StaffAttendanceWindowBefore)
		}
		if cfg.War.StaffSelfCheckInBefore != 48*time.Hour {
			t.Errorf("expected StaffSelfCheckInBefore=48h, got %s", cfg.War.StaffSelfCheckInBefore)
		}
		if cfg.War.StaffSelfCheckInAfter != 6*time.Hour {
			t.Errorf("expected StaffSelfCheckInAfter=6h, got %s", cfg.War.StaffSelfCheckInAfter)
		}
	})
}
