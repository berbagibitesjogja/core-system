package auth

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestSignAndParseAccessToken(t *testing.T) {
	secret := "test-secret-key-that-is-at-least-32-bytes-long"
	userID := uuid.New()
	email := "test@example.com"
	role := "admin"
	divisionID := int32(1)

	t.Run("valid staff token round trip", func(t *testing.T) {
		tokenStr, err := SignAccessToken(userID, email, true, role, divisionID, false, secret, 15*time.Minute)
		if err != nil {
			t.Fatalf("unexpected error signing token: %v", err)
		}

		claims, err := ParseToken(tokenStr, secret)
		if err != nil {
			t.Fatalf("unexpected error parsing token: %v", err)
		}

		if claims.UserID != userID {
			t.Fatalf("expected UserID %s, got %s", userID, claims.UserID)
		}
		if claims.Email != email {
			t.Fatalf("expected Email %q, got %q", email, claims.Email)
		}
		if !claims.IsStaff {
			t.Fatalf("expected IsStaff to be true")
		}
		if claims.IsHero {
			t.Fatalf("expected IsHero to be false")
		}
		if claims.Role != role {
			t.Fatalf("expected Role %q, got %q", role, claims.Role)
		}
		if claims.DivisionID != divisionID {
			t.Fatalf("expected DivisionID %d, got %d", divisionID, claims.DivisionID)
		}
		if claims.Subject != userID.String() {
			t.Fatalf("expected Subject %q, got %q", userID.String(), claims.Subject)
		}
	})

	t.Run("valid hero token round trip", func(t *testing.T) {
		tokenStr, err := SignAccessToken(userID, "hero@ugm.ac.id", false, "", 0, true, secret, 15*time.Minute)
		if err != nil {
			t.Fatalf("unexpected error signing token: %v", err)
		}

		claims, err := ParseToken(tokenStr, secret)
		if err != nil {
			t.Fatalf("unexpected error parsing token: %v", err)
		}

		if claims.IsStaff {
			t.Fatalf("expected IsStaff to be false")
		}
		if !claims.IsHero {
			t.Fatalf("expected IsHero to be true")
		}
		if claims.Role != "" {
			t.Fatalf("expected empty Role, got %q", claims.Role)
		}
		if claims.DivisionID != 0 {
			t.Fatalf("expected DivisionID 0, got %d", claims.DivisionID)
		}
	})

	t.Run("fails with incorrect secret", func(t *testing.T) {
		tokenStr, err := SignAccessToken(userID, email, true, role, divisionID, false, secret, 15*time.Minute)
		if err != nil {
			t.Fatalf("unexpected error signing token: %v", err)
		}

		_, err = ParseToken(tokenStr, "wrong-secret-key")
		if err == nil {
			t.Fatal("expected error parsing token with wrong secret, got nil")
		}
	})

	t.Run("expired token fails", func(t *testing.T) {
		tokenStr, err := SignAccessToken(userID, email, true, role, divisionID, false, secret, -1*time.Minute)
		if err != nil {
			t.Fatalf("unexpected error signing token: %v", err)
		}

		_, err = ParseToken(tokenStr, secret)
		if err == nil {
			t.Fatal("expected error parsing expired token, got nil")
		}
	})
}

func TestGenerateSecureToken(t *testing.T) {
	t.Run("generates 64-char hex string and matching sha256 hash", func(t *testing.T) {
		raw, hash, err := GenerateSecureToken()
		if err != nil {
			t.Fatalf("unexpected error generating secure token: %v", err)
		}

		if len(raw) != 64 {
			t.Fatalf("expected raw token length 64 (32 bytes hex), got %d (%q)", len(raw), raw)
		}

		if len(hash) != 64 {
			t.Fatalf("expected sha256 hash length 64, got %d (%q)", len(hash), hash)
		}

		computedHash := HashToken(raw)
		if computedHash != hash {
			t.Fatalf("computed hash %q does not match returned hash %q", computedHash, hash)
		}
	})

	t.Run("uniqueness across iterations", func(t *testing.T) {
		seen := make(map[string]struct{})
		for range 1000 {
			raw, _, err := GenerateSecureToken()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if _, exists := seen[raw]; exists {
				t.Fatalf("duplicate secure token generated: %q", raw)
			}
			seen[raw] = struct{}{}
		}
	})
}

func TestGenerateVerificationCode(t *testing.T) {
	for range 100 {
		code, err := GenerateVerificationCode()
		if err != nil {
			t.Fatalf("unexpected error generating verification code: %v", err)
		}
		if len(code) != 6 {
			t.Fatalf("expected 6-digit code, got %q (len %d)", code, len(code))
		}
		for _, ch := range code {
			if ch < '0' || ch > '9' {
				t.Fatalf("expected only digits in code, got %q", code)
			}
		}
	}
}

func TestHashTokenDeterministic(t *testing.T) {
	raw := "abc123fixedtoken"
	h1 := HashToken(raw)
	h2 := HashToken(raw)

	if h1 != h2 {
		t.Fatalf("expected HashToken to be deterministic: %q != %q", h1, h2)
	}

	if strings.Contains(h1, " ") {
		t.Fatalf("hash contains unexpected spaces: %q", h1)
	}
}
