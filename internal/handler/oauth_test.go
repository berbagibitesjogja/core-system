package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/berbagibitesjogja/core-system/internal/auth"
	"github.com/berbagibitesjogja/core-system/internal/config"
	"github.com/berbagibitesjogja/core-system/internal/dbgen"
	"github.com/berbagibitesjogja/core-system/internal/response"
	"github.com/google/uuid"
	"golang.org/x/oauth2"
)

type mockGoogleService struct {
	authCodeURLFunc   func(state string, opts ...oauth2.AuthCodeOption) string
	exchangeFunc      func(ctx context.Context, code string, opts ...oauth2.AuthCodeOption) (*oauth2.Token, error)
	fetchUserInfoFunc func(ctx context.Context, token *oauth2.Token) (*auth.GoogleUserInfo, error)
}

func (m *mockGoogleService) AuthCodeURL(state string, opts ...oauth2.AuthCodeOption) string {
	if m.authCodeURLFunc != nil {
		return m.authCodeURLFunc(state, opts...)
	}
	return "https://accounts.google.com/o/oauth2/auth?state=" + state
}

func (m *mockGoogleService) Exchange(ctx context.Context, code string, opts ...oauth2.AuthCodeOption) (*oauth2.Token, error) {
	if m.exchangeFunc != nil {
		return m.exchangeFunc(ctx, code, opts...)
	}
	return &oauth2.Token{AccessToken: "mock-access-token"}, nil
}

func (m *mockGoogleService) FetchUserInfo(ctx context.Context, token *oauth2.Token) (*auth.GoogleUserInfo, error) {
	if m.fetchUserInfoFunc != nil {
		return m.fetchUserInfoFunc(ctx, token)
	}
	return &auth.GoogleUserInfo{
		Sub:           "mock-sub-12345",
		Email:         "mock.user@gmail.com",
		EmailVerified: true,
		Name:          "Mock User",
		Picture:       "https://mock.com/pic.jpg",
	}, nil
}

func setupOAuthHandler(t *testing.T) (*OAuthHandler, *mockGoogleService, func()) {
	pool := getTestPool(t)
	cfg := config.LoadConfig()
	cfg.OAuth.GoogleClientID = "test-client-id"
	cfg.OAuth.GoogleClientSecret = "test-client-secret"
	cfg.OAuth.GoogleRedirectURL = "http://localhost:8080/api/v1/auth/google/callback"

	queries := dbgen.New(pool)
	h := NewOAuthHandler(pool, &cfg, queries)

	mockSvc := &mockGoogleService{}
	h.SetGoogleService(mockSvc)

	cleanup := func() {
		pool.Close()
	}

	return h, mockSvc, cleanup
}

func TestGoogleLoginHandler(t *testing.T) {
	h, _, cleanup := setupOAuthHandler(t)
	defer cleanup()

	t.Run("redirects to google consent and sets state cookie", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/google/login", nil)
		rec := httptest.NewRecorder()

		h.GoogleLoginHandler(rec, req)

		if rec.Code != http.StatusTemporaryRedirect {
			t.Fatalf("expected 307 Temporary Redirect, got %d", rec.Code)
		}

		cookies := rec.Result().Cookies()
		var foundCookie bool
		for _, c := range cookies {
			if c.Name == auth.OAuthStateCookieName && c.Value != "" && c.HttpOnly {
				foundCookie = true
				break
			}
		}
		if !foundCookie {
			t.Fatal("expected oauth_state cookie to be set with HttpOnly")
		}

		loc := rec.Header().Get("Location")
		if loc == "" || !strings.Contains(loc, "https://accounts.google.com") {
			t.Fatalf("unexpected redirect location: %s", loc)
		}
	})

	t.Run("preserves invitation token inside state payload", func(t *testing.T) {
		invitationToken := "invite-secret-token-abc-123"
		req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/google/login?invitation_token="+invitationToken, nil)
		rec := httptest.NewRecorder()

		h.GoogleLoginHandler(rec, req)

		var cookieValue string
		for _, c := range rec.Result().Cookies() {
			if c.Name == auth.OAuthStateCookieName {
				cookieValue = c.Value
				break
			}
		}

		loc := rec.Header().Get("Location")
		stateIdx := strings.Index(loc, "state=")
		stateParam := loc[stateIdx+6:]

		extractedToken, err := auth.VerifyOAuthState(h.config.AppConfig.JWTSecret, stateParam, cookieValue)
		if err != nil {
			t.Fatalf("failed to verify oauth state: %v", err)
		}

		if extractedToken != invitationToken {
			t.Fatalf("expected extracted token %q, got %q", invitationToken, extractedToken)
		}
	})
}

func TestGoogleCallbackSecurity(t *testing.T) {
	h, _, cleanup := setupOAuthHandler(t)
	defer cleanup()

	t.Run("rejects request missing state or code with 400 Bad Request", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/google/callback", nil)
		req.Header.Set("Accept", "application/json")
		rec := httptest.NewRecorder()

		h.GoogleCallbackHandler(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d", rec.Code)
		}
	})

	t.Run("rejects request with missing or mismatched state cookie", func(t *testing.T) {
		state, _, _ := auth.GenerateOAuthState(h.config.AppConfig.JWTSecret, "")
		req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/google/callback?code=mock-code&state="+state, nil)
		req.Header.Set("Accept", "application/json")
		req.AddCookie(&http.Cookie{
			Name:  auth.OAuthStateCookieName,
			Value: "wrong-nonce",
		})
		rec := httptest.NewRecorder()

		h.GoogleCallbackHandler(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request for mismatched cookie nonce, got %d", rec.Code)
		}
	})

	t.Run("rejects unverified Google email address", func(t *testing.T) {
		state, nonce, _ := auth.GenerateOAuthState(h.config.AppConfig.JWTSecret, "")

		h.SetGoogleService(&mockGoogleService{
			fetchUserInfoFunc: func(ctx context.Context, token *oauth2.Token) (*auth.GoogleUserInfo, error) {
				return &auth.GoogleUserInfo{
					Sub:           "unverified-sub",
					Email:         "unverified@gmail.com",
					EmailVerified: false,
					Name:          "Unverified User",
				}, nil
			},
		})

		req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/google/callback?code=mock-code&state="+state, nil)
		req.Header.Set("Accept", "application/json")
		req.AddCookie(&http.Cookie{
			Name:  auth.OAuthStateCookieName,
			Value: nonce,
		})
		rec := httptest.NewRecorder()

		h.GoogleCallbackHandler(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request for unverified Google email, got %d. Body: %s", rec.Code, rec.Body.String())
		}
		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.Code != "EMAIL_NOT_VERIFIED" {
			t.Fatalf("expected EMAIL_NOT_VERIFIED, got %s", resp.Code)
		}
	})
}

func TestGoogleCallbackFlows(t *testing.T) {
	h, _, cleanup := setupOAuthHandler(t)
	defer cleanup()

	superID := uuid.MustParse("00000000-0000-0000-0000-000000000001")

	t.Run("Flow 1: Existing linked OAuth account logs in successfully", func(t *testing.T) {
		testEmail := "linked.test@example.com"
		testSub := "google-sub-linked-12345"

		// Pre-create user and staff and oauth record
		var userID uuid.UUID
		err := h.pool.QueryRow(context.Background(), `
			INSERT INTO users (email, name, phone_number, is_active, email_verified_at)
			VALUES ($1, 'Delivery User', '628999999999', true, NOW())
			RETURNING id
		`, testEmail).Scan(&userID)
		if err != nil {
			t.Fatalf("failed to insert test user: %v", err)
		}
		defer func() {
			_, _ = h.pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", userID)
		}()

		_, err = h.pool.Exec(context.Background(), `
			INSERT INTO staff (user_id, division_id, role)
			VALUES ($1, 2, 'staff')
		`, userID)
		if err != nil {
			t.Fatalf("failed to insert staff: %v", err)
		}

		_, err = h.pool.Exec(context.Background(), `
			INSERT INTO oauth_accounts (user_id, provider, provider_user_id, email)
			VALUES ($1, 'google', $2, $3)
		`, userID, testSub, testEmail)
		if err != nil {
			t.Fatalf("failed to insert oauth account: %v", err)
		}

		state, nonce, _ := auth.GenerateOAuthState(h.config.AppConfig.JWTSecret, "")
		h.SetGoogleService(&mockGoogleService{
			fetchUserInfoFunc: func(ctx context.Context, token *oauth2.Token) (*auth.GoogleUserInfo, error) {
				return &auth.GoogleUserInfo{
					Sub:           testSub,
					Email:         testEmail,
					EmailVerified: true,
					Name:          "Linked User Google",
				}, nil
			},
		})

		req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/google/callback?code=mock-code&state="+state, nil)
		req.Header.Set("Accept", "application/json")
		req.AddCookie(&http.Cookie{
			Name:  auth.OAuthStateCookieName,
			Value: nonce,
		})
		rec := httptest.NewRecorder()

		h.GoogleCallbackHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		var resp response.Response
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		dataBytes, _ := json.Marshal(resp.Data)
		var tokenResp AuthTokenResponse
		_ = json.Unmarshal(dataBytes, &tokenResp)

		if tokenResp.AccessToken == "" || tokenResp.RefreshToken == "" {
			t.Fatal("expected access and refresh tokens")
		}
		if tokenResp.User.Email != testEmail {
			t.Fatalf("expected email %s, got %s", testEmail, tokenResp.User.Email)
		}
		if !tokenResp.User.IsStaff || tokenResp.User.IsHero {
			t.Fatalf("expected is_staff=true and is_hero=false, got: %+v", tokenResp.User)
		}
		if tokenResp.User.Role != "staff" || tokenResp.User.DivisionID != 2 {
			t.Fatalf("expected role='staff' and division_id=2, got: %+v", tokenResp.User)
		}
	})

	t.Run("Flow 2: Auto-links existing user by email and clears password if unverified", func(t *testing.T) {
		testEmail := "autolink.test@example.com"
		testSub := "google-sub-autolink-998877"

		hashedPass, _ := auth.HashPassword("victimPassword123")
		var userID uuid.UUID
		err := h.pool.QueryRow(context.Background(), `
			INSERT INTO users (email, name, password_hash, phone_number, is_active, email_verified_at)
			VALUES ($1, 'Pre-existing Email User', $2, '628222222222', true, NULL)
			RETURNING id
		`, testEmail, hashedPass).Scan(&userID)
		if err != nil {
			t.Fatalf("failed to insert test user: %v", err)
		}
		defer func() {
			_, _ = h.pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", userID)
		}()

		_, err = h.pool.Exec(context.Background(), `
			INSERT INTO staff (user_id, division_id, role)
			VALUES ($1, 2, 'core')
		`, userID)
		if err != nil {
			t.Fatalf("failed to insert staff: %v", err)
		}

		_, err = h.pool.Exec(context.Background(), `
			INSERT INTO refresh_tokens (user_id, token_hash, expires_at)
			VALUES ($1, 'pre-existing-attacker-token-hash', NOW() + INTERVAL '7 days')
		`, userID)
		if err != nil {
			t.Fatalf("failed to insert attacker refresh token: %v", err)
		}

		state, nonce, _ := auth.GenerateOAuthState(h.config.AppConfig.JWTSecret, "")
		h.SetGoogleService(&mockGoogleService{
			fetchUserInfoFunc: func(ctx context.Context, token *oauth2.Token) (*auth.GoogleUserInfo, error) {
				return &auth.GoogleUserInfo{
					Sub:           testSub,
					Email:         testEmail,
					EmailVerified: true,
					Name:          "Auto Link User",
				}, nil
			},
		})

		req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/google/callback?code=mock-code&state="+state, nil)
		req.Header.Set("Accept", "application/json")
		req.AddCookie(&http.Cookie{
			Name:  auth.OAuthStateCookieName,
			Value: nonce,
		})
		rec := httptest.NewRecorder()

		h.GoogleCallbackHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		// Verify oauth_accounts has been linked
		var linkedUserID uuid.UUID
		err = h.pool.QueryRow(context.Background(), `
			SELECT user_id FROM oauth_accounts WHERE provider = 'google' AND provider_user_id = $1
		`, testSub).Scan(&linkedUserID)
		if err != nil || linkedUserID != userID {
			t.Fatalf("expected oauth_accounts to link to user %s, got err: %v, linked: %s", userID, err, linkedUserID)
		}

		// Verify hardening rule (§4.3.2): password_hash must be cleared to NULL and email_verified_at set
		var passHash *string
		var emailVerifiedAt *time.Time
		err = h.pool.QueryRow(context.Background(), `
			SELECT password_hash, email_verified_at FROM users WHERE id = $1
		`, userID).Scan(&passHash, &emailVerifiedAt)
		if err != nil {
			t.Fatalf("failed to query user verification state: %v", err)
		}
		if passHash != nil {
			t.Fatalf("expected password_hash to be cleared to NULL for previously unverified account, got %v", *passHash)
		}
		if emailVerifiedAt == nil {
			t.Fatal("expected email_verified_at to be marked as verified")
		}

		// Verify pre-existing refresh token was revoked (H2)
		var attackerTokenRevokedAt *time.Time
		err = h.pool.QueryRow(context.Background(), `
			SELECT revoked_at FROM refresh_tokens WHERE user_id = $1 AND token_hash = 'pre-existing-attacker-token-hash'
		`, userID).Scan(&attackerTokenRevokedAt)
		if err != nil || attackerTokenRevokedAt == nil {
			t.Fatalf("expected pre-existing refresh token to be revoked, got err: %v, revoked_at: %v", err, attackerTokenRevokedAt)
		}
	})

	t.Run("Flow 3: Onboards invited user without password honoring inviter authority", func(t *testing.T) {
		invitedEmail := "invited.oauth@example.com"
		rawToken, tokenHash, _ := auth.GenerateSecureToken()

		_, err := h.pool.Exec(context.Background(), `
			INSERT INTO user_invitations (invited_by, email, role, division_id, name, phone_number, token_hash, expires_at)
			VALUES ($1, $2, 'core', 2, 'Inviter Given Name', '+62 812-3456-7890', $3, NOW() + INTERVAL '2 hours')
		`, superID, invitedEmail, tokenHash)
		if err != nil {
			t.Fatalf("failed to insert test invitation: %v", err)
		}
		defer func() {
			_, _ = h.pool.Exec(context.Background(), "DELETE FROM user_invitations WHERE email = $1", invitedEmail)
			_, _ = h.pool.Exec(context.Background(), "DELETE FROM users WHERE email = $1", invitedEmail)
		}()

		// Initiate OAuth callback carrying invitation token
		state, nonce, _ := auth.GenerateOAuthState(h.config.AppConfig.JWTSecret, rawToken)
		h.SetGoogleService(&mockGoogleService{
			fetchUserInfoFunc: func(ctx context.Context, token *oauth2.Token) (*auth.GoogleUserInfo, error) {
				return &auth.GoogleUserInfo{
					Sub:           "google-sub-invited-onboard-555",
					Email:         invitedEmail,
					EmailVerified: true,
					Name:          "Google Display Name",
				}, nil
			},
		})

		req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/google/callback?code=mock-code&state="+state, nil)
		req.Header.Set("Accept", "application/json")
		req.AddCookie(&http.Cookie{
			Name:  auth.OAuthStateCookieName,
			Value: nonce,
		})
		rec := httptest.NewRecorder()

		h.GoogleCallbackHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		dataBytes, _ := json.Marshal(resp.Data)
		var tokenResp AuthTokenResponse
		_ = json.Unmarshal(dataBytes, &tokenResp)

		// Inviter authority: name must be 'Inviter Given Name' (not 'Google Display Name')
		if tokenResp.User.Name != "Inviter Given Name" {
			t.Fatalf("expected inviter name 'Inviter Given Name', got '%s'", tokenResp.User.Name)
		}
		// Phone must be normalized '6281234567890'
		if tokenResp.User.PhoneNumber != "6281234567890" {
			t.Fatalf("expected normalized phone '6281234567890', got '%s'", tokenResp.User.PhoneNumber)
		}
		if !tokenResp.User.IsStaff || tokenResp.User.IsHero {
			t.Fatalf("expected is_staff=true, is_hero=false, got: %+v", tokenResp.User)
		}
		if tokenResp.User.Role != "core" || tokenResp.User.DivisionID != 2 {
			t.Fatalf("expected role='core' and division_id=2, got: %+v", tokenResp.User)
		}

		// Verify invitation accepted_at is marked
		var acceptedAt *time.Time
		_ = h.pool.QueryRow(context.Background(), "SELECT accepted_at FROM user_invitations WHERE email = $1", invitedEmail).Scan(&acceptedAt)
		if acceptedAt == nil {
			t.Fatal("expected invitation accepted_at to be marked")
		}

		// Verify password_hash is null
		var passwordHash *string
		_ = h.pool.QueryRow(context.Background(), "SELECT password_hash FROM users WHERE email = $1", invitedEmail).Scan(&passwordHash)
		if passwordHash != nil {
			t.Fatalf("expected password_hash to be NULL for OAuth registration, got %v", *passwordHash)
		}
	})

	t.Run("Flow 3b: Invitation token provided with mismatched Google email returns 403 Forbidden AUTH_007", func(t *testing.T) {
		invitedEmail := "staff.intended@example.com"
		rawToken, tokenHash, _ := auth.GenerateSecureToken()

		_, err := h.pool.Exec(context.Background(), `
			INSERT INTO user_invitations (invited_by, email, role, division_id, name, token_hash, expires_at)
			VALUES ($1, $2, 'staff', 2, 'Intended Staff', $3, NOW() + INTERVAL '2 hours')
		`, superID, invitedEmail, tokenHash)
		if err != nil {
			t.Fatalf("failed to insert test invitation: %v", err)
		}
		defer func() {
			_, _ = h.pool.Exec(context.Background(), "DELETE FROM user_invitations WHERE email = $1", invitedEmail)
		}()

		state, nonce, _ := auth.GenerateOAuthState(h.config.AppConfig.JWTSecret, rawToken)
		h.SetGoogleService(&mockGoogleService{
			fetchUserInfoFunc: func(ctx context.Context, token *oauth2.Token) (*auth.GoogleUserInfo, error) {
				return &auth.GoogleUserInfo{
					Sub:           "google-sub-attacker-diff-email",
					Email:         "attacker.different@gmail.com",
					EmailVerified: true,
					Name:          "Attacker Name",
				}, nil
			},
		})

		req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/google/callback?code=mock-code&state="+state, nil)
		req.Header.Set("Accept", "application/json")
		req.AddCookie(&http.Cookie{
			Name:  auth.OAuthStateCookieName,
			Value: nonce,
		})
		rec := httptest.NewRecorder()

		h.GoogleCallbackHandler(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 Forbidden for mismatched email, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.Code != "INVITATION_EMAIL_MISMATCH" {
			t.Fatalf("expected code AUTH_007, got %s", resp.Code)
		}
	})

	t.Run("Flow 3c: Invalid or expired invitation token returns 400 Bad Request AUTH_005", func(t *testing.T) {
		state, nonce, _ := auth.GenerateOAuthState(h.config.AppConfig.JWTSecret, "non-existent-or-expired-token")
		h.SetGoogleService(&mockGoogleService{
			fetchUserInfoFunc: func(ctx context.Context, token *oauth2.Token) (*auth.GoogleUserInfo, error) {
				return &auth.GoogleUserInfo{
					Sub:           "google-sub-someone",
					Email:         "someone@gmail.com",
					EmailVerified: true,
					Name:          "Someone",
				}, nil
			},
		})

		req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/google/callback?code=mock-code&state="+state, nil)
		req.Header.Set("Accept", "application/json")
		req.AddCookie(&http.Cookie{
			Name:  auth.OAuthStateCookieName,
			Value: nonce,
		})
		rec := httptest.NewRecorder()

		h.GoogleCallbackHandler(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request for invalid invitation token, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.Code != "INVITATION_INVALID" {
			t.Fatalf("expected code AUTH_005, got %s", resp.Code)
		}
	})

	t.Run("Flow 4: Stranger without invitation registers as Hero with auto-enrollment", func(t *testing.T) {
		strangerEmail := "stranger.student@mail.ugm.ac.id"
		testSub := "google-sub-stranger-hero-9999"
		defer func() {
			_, _ = h.pool.Exec(context.Background(), "DELETE FROM users WHERE email = $1", strangerEmail)
		}()

		state, nonce, _ := auth.GenerateOAuthState(h.config.AppConfig.JWTSecret, "")

		h.SetGoogleService(&mockGoogleService{
			fetchUserInfoFunc: func(ctx context.Context, token *oauth2.Token) (*auth.GoogleUserInfo, error) {
				return &auth.GoogleUserInfo{
					Sub:           testSub,
					Email:         strangerEmail,
					EmailVerified: true,
					Name:          "Random UGM Stranger",
				}, nil
			},
		})

		req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/google/callback?code=mock-code&state="+state, nil)
		req.Header.Set("Accept", "application/json")
		req.AddCookie(&http.Cookie{
			Name:  auth.OAuthStateCookieName,
			Value: nonce,
		})
		rec := httptest.NewRecorder()

		h.GoogleCallbackHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK for stranger hero registration, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		dataBytes, _ := json.Marshal(resp.Data)
		var tokenResp AuthTokenResponse
		_ = json.Unmarshal(dataBytes, &tokenResp)

		if tokenResp.User.IsStaff || !tokenResp.User.IsHero {
			t.Fatalf("expected stranger to be registered as Hero (is_staff: false, is_hero: true), got: %+v", tokenResp.User)
		}
		if tokenResp.User.Role != "" || tokenResp.User.DivisionID != 0 {
			t.Fatalf("expected empty role and division_id=0, got: role=%q, division_id=%d", tokenResp.User.Role, tokenResp.User.DivisionID)
		}

		// Verify database state: users, heroes, oauth_accounts, hero_categories
		var heroUserID uuid.UUID
		err := h.pool.QueryRow(context.Background(), `
			SELECT h.user_id
			FROM heroes h
			JOIN users u ON u.id = h.user_id
			WHERE u.email = $1
		`, strangerEmail).Scan(&heroUserID)
		if err != nil {
			t.Fatalf("expected hero record in DB: %v", err)
		}

		var linkedSub string
		err = h.pool.QueryRow(context.Background(), `
			SELECT provider_user_id FROM oauth_accounts WHERE user_id = $1 AND provider = 'google'
		`, heroUserID).Scan(&linkedSub)
		if err != nil || linkedSub != testSub {
			t.Fatalf("expected oauth_accounts linked with sub %s, got err: %v, sub: %s", testSub, err, linkedSub)
		}

		// Verify auto-enrollment into UGM category
		var enrolledCat string
		err = h.pool.QueryRow(context.Background(), `
			SELECT bc.name
			FROM hero_categories hc
			JOIN beneficiary_categories bc ON bc.id = hc.beneficiary_category_id
			WHERE hc.hero_id = $1
		`, heroUserID).Scan(&enrolledCat)
		if err != nil {
			t.Fatalf("expected auto-enrollment into university category: %v", err)
		}
		if enrolledCat != "Universitas Gadjah Mada" {
			t.Fatalf("expected 'Universitas Gadjah Mada', got %q", enrolledCat)
		}
	})

	t.Run("Rejects deactivated account with 401 Unauthorized", func(t *testing.T) {
		deactivatedEmail := "deactivated.oauth@example.com"
		testSub := "google-sub-deactivated-000"

		var userID uuid.UUID
		err := h.pool.QueryRow(context.Background(), `
			INSERT INTO users (email, name, phone_number, is_active)
			VALUES ($1, 'Deactivated User', '628333333333', false)
			RETURNING id
		`, deactivatedEmail).Scan(&userID)
		if err != nil {
			t.Fatalf("failed to insert test user: %v", err)
		}
		defer func() {
			_, _ = h.pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", userID)
		}()

		_, err = h.pool.Exec(context.Background(), `
			INSERT INTO staff (user_id, division_id, role)
			VALUES ($1, 2, 'staff')
		`, userID)
		if err != nil {
			t.Fatalf("failed to insert staff: %v", err)
		}

		_, err = h.pool.Exec(context.Background(), `
			INSERT INTO oauth_accounts (user_id, provider, provider_user_id, email)
			VALUES ($1, 'google', $2, $3)
		`, userID, testSub, deactivatedEmail)
		if err != nil {
			t.Fatalf("failed to insert oauth account: %v", err)
		}

		state, nonce, _ := auth.GenerateOAuthState(h.config.AppConfig.JWTSecret, "")
		h.SetGoogleService(&mockGoogleService{
			fetchUserInfoFunc: func(ctx context.Context, token *oauth2.Token) (*auth.GoogleUserInfo, error) {
				return &auth.GoogleUserInfo{
					Sub:           testSub,
					Email:         deactivatedEmail,
					EmailVerified: true,
					Name:          "Deactivated User",
				}, nil
			},
		})

		req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/google/callback?code=mock-code&state="+state, nil)
		req.Header.Set("Accept", "application/json")
		req.AddCookie(&http.Cookie{
			Name:  auth.OAuthStateCookieName,
			Value: nonce,
		})
		rec := httptest.NewRecorder()

		h.GoogleCallbackHandler(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 Unauthorized, got %d. Body: %s", rec.Code, rec.Body.String())
		}
	})
}

func TestGoogleDeliveryModes(t *testing.T) {
	h, _, cleanup := setupOAuthHandler(t)
	defer cleanup()

	testEmail := "delivery.mode@example.com"
	var userID uuid.UUID
	err := h.pool.QueryRow(context.Background(), `
		INSERT INTO users (email, name, phone_number, is_active)
		VALUES ($1, 'Delivery User', '628999999999', true)
		RETURNING id
	`, testEmail).Scan(&userID)
	if err != nil {
		t.Fatalf("failed to insert test user: %v", err)
	}
	defer func() {
		_, _ = h.pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", userID)
	}()

	_, err = h.pool.Exec(context.Background(), `
		INSERT INTO staff (user_id, division_id, role)
		VALUES ($1, 2, 'staff')
	`, userID)
	if err != nil {
		t.Fatalf("failed to insert staff: %v", err)
	}

	testSub := "google-sub-delivery-123"
	_, _ = h.pool.Exec(context.Background(), `
		INSERT INTO oauth_accounts (user_id, provider, provider_user_id, email)
		VALUES ($1, 'google', $2, $3)
	`, userID, testSub, testEmail)

	h.SetGoogleService(&mockGoogleService{
		fetchUserInfoFunc: func(ctx context.Context, token *oauth2.Token) (*auth.GoogleUserInfo, error) {
			return &auth.GoogleUserInfo{
				Sub:           testSub,
				Email:         testEmail,
				EmailVerified: true,
				Name:          "Delivery User",
			}, nil
		},
	})

	t.Run("outputs JSON directly when FRONTEND_URL points to backend (backend-only mode)", func(t *testing.T) {
		h.config.AppConfig.FrontendURL = "http://localhost:8080"
		state, nonce, _ := auth.GenerateOAuthState(h.config.AppConfig.JWTSecret, "")

		req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/google/callback?code=mock-code&state="+state, nil)
		// Notice: Browser request without Accept: application/json
		req.Header.Set("Accept", "text/html,application/xhtml+xml")
		req.AddCookie(&http.Cookie{
			Name:  auth.OAuthStateCookieName,
			Value: nonce,
		})
		rec := httptest.NewRecorder()

		h.GoogleCallbackHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK directly, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		var resp response.Response
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("expected valid JSON response, got error: %v", err)
		}
		if resp.Status != "success" {
			t.Fatalf("expected success status, got %s", resp.Status)
		}
	})

	t.Run("redirects to frontend SPA when FRONTEND_URL points to separate frontend (:5173)", func(t *testing.T) {
		h.config.AppConfig.FrontendURL = "http://localhost:5173"
		state, nonce, _ := auth.GenerateOAuthState(h.config.AppConfig.JWTSecret, "")

		req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/google/callback?code=mock-code&state="+state, nil)
		req.Header.Set("Accept", "text/html,application/xhtml+xml")
		req.AddCookie(&http.Cookie{
			Name:  auth.OAuthStateCookieName,
			Value: nonce,
		})
		rec := httptest.NewRecorder()

		h.GoogleCallbackHandler(rec, req)

		if rec.Code != http.StatusTemporaryRedirect {
			t.Fatalf("expected 307 Temporary Redirect to frontend, got %d", rec.Code)
		}

		loc := rec.Header().Get("Location")
		if !strings.HasPrefix(loc, "http://localhost:5173/auth/callback?access_token=") {
			t.Fatalf("expected redirect to http://localhost:5173/auth/callback, got: %s", loc)
		}
	})
}
