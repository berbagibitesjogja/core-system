package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/berbagibitesjogja/core-system/internal/auth"
	"github.com/berbagibitesjogja/core-system/internal/config"
	"github.com/berbagibitesjogja/core-system/internal/dbgen"
	"github.com/berbagibitesjogja/core-system/internal/response"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func TestHeroRegisterAndAutoEnroll(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	cfg := config.LoadConfig()
	h := NewHeroAuthHandler(pool, &cfg, nil)

	t.Run("successful hero registration with university email creates hero without auto-enrolling prior to verification", func(t *testing.T) {
		testEmail := "student.hero@mail.ugm.ac.id"
		defer func() {
			_, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE email = $1", testEmail)
		}()

		payload := HeroRegisterRequest{
			Email:       testEmail,
			Password:    "heroPassword123!",
			Name:        "UGM Student Hero",
			PhoneNumber: "+62 813-1122-3344",
		}
		bodyBytes, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/heroes/register", bytes.NewReader(bodyBytes))
		rec := httptest.NewRecorder()

		h.RegisterHandler(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		var resp response.Response
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		// Verify database state: users, heroes, hero_categories, verification_codes
		var userID uuid.UUID
		var isActive bool
		var emailVerifiedAt *time.Time
		err := pool.QueryRow(context.Background(), `
			SELECT id, is_active, email_verified_at FROM users WHERE email = $1
		`, testEmail).Scan(&userID, &isActive, &emailVerifiedAt)
		if err != nil {
			t.Fatalf("failed to find created user in DB: %v", err)
		}

		if !isActive || emailVerifiedAt != nil {
			t.Fatalf("expected is_active=true and email_verified_at=nil, got: active=%v, verified=%v", isActive, emailVerifiedAt)
		}

		// Verify 1:1 hero record exists
		var heroUserID uuid.UUID
		err = pool.QueryRow(context.Background(), `
			SELECT user_id FROM heroes WHERE user_id = $1
		`, userID).Scan(&heroUserID)
		if err != nil {
			t.Fatalf("hero record was not created: %v", err)
		}

		// Verify NO auto-enrollment prior to email verification (Topic 010 §4.5 & AR H1)
		var catCount int
		err = pool.QueryRow(context.Background(), `
			SELECT count(*) FROM hero_categories WHERE hero_id = $1
		`, userID).Scan(&catCount)
		if err != nil {
			t.Fatalf("failed to query hero_categories: %v", err)
		}
		if catCount != 0 {
			t.Fatalf("expected 0 categories before email verification, got %d", catCount)
		}

		// Verify verification code created
		var codeHash string
		var attempts int16
		var expiresAt time.Time
		err = pool.QueryRow(context.Background(), `
			SELECT code_hash, attempts, expires_at
			FROM verification_codes
			WHERE user_id = $1 AND purpose = 'email_verification'
		`, userID).Scan(&codeHash, &attempts, &expiresAt)
		if err != nil {
			t.Fatalf("expected verification code in DB: %v", err)
		}
		if attempts != 0 || expiresAt.Before(time.Now()) {
			t.Fatalf("unexpected verification code state: attempts=%d, expiresAt=%s", attempts, expiresAt)
		}
	})

	t.Run("successful hero registration with generic email does not auto-enroll into university", func(t *testing.T) {
		testEmail := "public.hero.regular@gmail.com"
		defer func() {
			_, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE email = $1", testEmail)
		}()

		payload := HeroRegisterRequest{
			Email:    testEmail,
			Password: "heroPassword123!",
			Name:     "Regular Public Hero",
		}
		bodyBytes, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/heroes/register", bytes.NewReader(bodyBytes))
		rec := httptest.NewRecorder()

		h.RegisterHandler(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		var userID uuid.UUID
		_ = pool.QueryRow(context.Background(), "SELECT id FROM users WHERE email = $1", testEmail).Scan(&userID)

		var catCount int
		err := pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM hero_categories WHERE hero_id = $1", userID).Scan(&catCount)
		if err != nil {
			t.Fatalf("failed to count categories: %v", err)
		}
		if catCount != 0 {
			t.Fatalf("expected 0 categories for generic email, got %d", catCount)
		}
	})

	t.Run("duplicate email returns 400 VAL_002", func(t *testing.T) {
		payload := HeroRegisterRequest{
			Email:    "hero.ugm@mail.ugm.ac.id", // Existing seeded hero
			Password: "heroPassword123!",
			Name:     "Duplicate Hero",
		}
		bodyBytes, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/heroes/register", bytes.NewReader(bodyBytes))
		rec := httptest.NewRecorder()

		h.RegisterHandler(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.Code != "VAL_002" {
			t.Fatalf("expected code VAL_002, got %q", resp.Code)
		}
	})

	t.Run("short password returns 400 VAL_001", func(t *testing.T) {
		payload := HeroRegisterRequest{
			Email:    "valid.email@example.com",
			Password: "short", // < 8 characters
			Name:     "Test Name",
		}
		bodyBytes, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/heroes/register", bytes.NewReader(bodyBytes))
		rec := httptest.NewRecorder()

		h.RegisterHandler(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.Code != "VAL_001" {
			t.Fatalf("expected code VAL_001, got %q", resp.Code)
		}
	})
	t.Run("malformed email @ returns 400 VAL_001", func(t *testing.T) {
		payload := HeroRegisterRequest{
			Email:    "@",
			Password: "password123!",
			Name:     "Test Name",
		}
		bodyBytes, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/heroes/register", bytes.NewReader(bodyBytes))
		rec := httptest.NewRecorder()

		h.RegisterHandler(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.Code != "VAL_001" {
			t.Fatalf("expected code VAL_001, got %q", resp.Code)
		}
	})
	t.Run("registration rejected with 400 VAL_003 when active staff invitation is pending", func(t *testing.T) {
		invitedEmail := "pending.hero.staff@example.com"
		_, tokenHash, _ := auth.GenerateSecureToken()
		superID := uuid.MustParse("00000000-0000-0000-0000-000000000001")
		_, err := pool.Exec(context.Background(), `
			INSERT INTO user_invitations (invited_by, email, role, division_id, token_hash, expires_at)
			VALUES ($1, $2, 'staff', 2, $3, NOW() + INTERVAL '2 hours')
		`, superID, invitedEmail, tokenHash)
		if err != nil {
			t.Fatalf("failed to insert test invitation: %v", err)
		}
		defer func() {
			_, _ = pool.Exec(context.Background(), "DELETE FROM user_invitations WHERE email = $1", invitedEmail)
			_, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE email = $1", invitedEmail)
		}()

		payload := HeroRegisterRequest{
			Email:    invitedEmail,
			Password: "heroPassword123!",
			Name:     "Pending Staff Candidate",
		}
		bodyBytes, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/heroes/register", bytes.NewReader(bodyBytes))
		rec := httptest.NewRecorder()

		h.RegisterHandler(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.Code != "VAL_003" {
			t.Fatalf("expected code VAL_003, got %q", resp.Code)
		}
	})
}

func TestHeroVerifyEmailAndResend(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	cfg := config.LoadConfig()
	h := NewHeroAuthHandler(pool, &cfg, nil)

	testEmail := "verify.hero@mail.ugm.ac.id"
	defer func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE email = $1", testEmail)
	}()

	// Register hero
	regPayload := HeroRegisterRequest{
		Email:    testEmail,
		Password: "heroPassword123!",
		Name:     "Verification Test Hero",
	}
	regBody, _ := json.Marshal(regPayload)
	regReq := httptest.NewRequest(http.MethodPost, "/api/v1/heroes/register", bytes.NewReader(regBody))
	regRec := httptest.NewRecorder()
	h.RegisterHandler(regRec, regReq)

	if regRec.Code != http.StatusCreated {
		t.Fatalf("failed to register test hero: %s", regRec.Body.String())
	}

	// Insert known verification code "123456" for deterministic test
	var heroUserID uuid.UUID
	_ = pool.QueryRow(context.Background(), "SELECT id FROM users WHERE email = $1", testEmail).Scan(&heroUserID)

	knownCode := "123456"
	knownCodeHash := auth.HashToken(knownCode)
	_, _ = pool.Exec(context.Background(), "DELETE FROM verification_codes WHERE user_id = $1", heroUserID)
	_, err := pool.Exec(context.Background(), `
		INSERT INTO verification_codes (user_id, purpose, code_hash, attempts, expires_at)
		VALUES ($1, 'email_verification', $2, 0, NOW() + INTERVAL '15 minutes')
	`, heroUserID, knownCodeHash)
	if err != nil {
		t.Fatalf("failed to seed known verification code: %v", err)
	}

	t.Run("verification with wrong code fails and increments attempts", func(t *testing.T) {
		payload := HeroVerifyEmailRequest{
			Email: testEmail,
			Code:  "999999", // wrong code
		}
		bodyBytes, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/heroes/verify-email", bytes.NewReader(bodyBytes))
		rec := httptest.NewRecorder()

		h.VerifyEmailHandler(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request on wrong code, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		var attempts int16
		_ = pool.QueryRow(context.Background(), "SELECT attempts FROM verification_codes WHERE user_id = $1", heroUserID).Scan(&attempts)
		if attempts != 1 {
			t.Fatalf("expected attempts to be 1, got %d", attempts)
		}
	})

	t.Run("verification exceeding max attempts fails with 429", func(t *testing.T) {
		// Set attempts to 5 in DB
		_, _ = pool.Exec(context.Background(), "UPDATE verification_codes SET attempts = 5 WHERE user_id = $1", heroUserID)

		// Insert a dummy pre-existing refresh token to assert revocation on verification (Topic 011 Phase 1)
		dummyTokenHash := auth.HashToken("dummy-pre-existing-refresh-token")
		_, _ = pool.Exec(context.Background(), "INSERT INTO refresh_tokens (user_id, token_hash, expires_at) VALUES ($1, $2, NOW() + INTERVAL '7 days')", heroUserID, dummyTokenHash)

		payload := HeroVerifyEmailRequest{
			Email: testEmail,
			Code:  knownCode,
		}
		bodyBytes, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/heroes/verify-email", bytes.NewReader(bodyBytes))
		rec := httptest.NewRecorder()

		h.VerifyEmailHandler(rec, req)

		if rec.Code != http.StatusTooManyRequests {
			t.Fatalf("expected 429 Too Many Requests when attempts >= 5, got %d. Body: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("resend verification code succeeds and generates new code", func(t *testing.T) {
		// Set created_at of current code to 2 minutes ago to bypass 60s cooldown
		_, _ = pool.Exec(context.Background(), "UPDATE verification_codes SET created_at = NOW() - INTERVAL '2 minutes' WHERE user_id = $1", heroUserID)

		payload := HeroResendVerificationRequest{Email: testEmail}
		bodyBytes, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/heroes/resend-verification", bytes.NewReader(bodyBytes))
		rec := httptest.NewRecorder()

		h.ResendVerificationHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on resend, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		// Calling resend again immediately should fail with 429 RESEND_TOO_FREQUENT
		rec2 := httptest.NewRecorder()
		req2 := httptest.NewRequest(http.MethodPost, "/api/v1/heroes/resend-verification", bytes.NewReader(bodyBytes))
		h.ResendVerificationHandler(rec2, req2)

		if rec2.Code != http.StatusTooManyRequests {
			t.Fatalf("expected 429 Too Many Requests on immediate second resend, got %d. Body: %s", rec2.Code, rec2.Body.String())
		}
	})

	t.Run("resend verification code does not enumerate non-existent or already-verified accounts", func(t *testing.T) {
		// Non-existent email
		payload1 := HeroResendVerificationRequest{Email: "nonexistent.account.xyz@mail.ugm.ac.id"}
		b1, _ := json.Marshal(payload1)
		req1 := httptest.NewRequest(http.MethodPost, "/api/v1/heroes/resend-verification", bytes.NewReader(b1))
		rec1 := httptest.NewRecorder()
		h.ResendVerificationHandler(rec1, req1)
		if rec1.Code != http.StatusOK {
			t.Fatalf("expected 200 OK for non-existent email resend, got %d. Body: %s", rec1.Code, rec1.Body.String())
		}

		// Already verified email (e.g. IT staff)
		payload2 := HeroResendVerificationRequest{Email: "it@berbagibitesjogja.com"}
		b2, _ := json.Marshal(payload2)
		req2 := httptest.NewRequest(http.MethodPost, "/api/v1/heroes/resend-verification", bytes.NewReader(b2))
		rec2 := httptest.NewRecorder()
		h.ResendVerificationHandler(rec2, req2)
		if rec2.Code != http.StatusOK {
			t.Fatalf("expected 200 OK for already-verified email resend, got %d. Body: %s", rec2.Code, rec2.Body.String())
		}

		// Deactivated email also returns 200 generic message without enumerating (Topic 011 Phase 1)
		deactEmail := "deactivated.hero.resend@example.com"
		var deactID uuid.UUID
		_ = pool.QueryRow(context.Background(), "INSERT INTO users (email, name, is_active) VALUES ($1, 'Deactivated', false) RETURNING id", deactEmail).Scan(&deactID)
		defer func() {
			_, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", deactID)
		}()
		payload3 := HeroResendVerificationRequest{Email: deactEmail}
		b3, _ := json.Marshal(payload3)
		req3 := httptest.NewRequest(http.MethodPost, "/api/v1/heroes/resend-verification", bytes.NewReader(b3))
		rec3 := httptest.NewRecorder()
		h.ResendVerificationHandler(rec3, req3)
		if rec3.Code != http.StatusOK {
			t.Fatalf("expected 200 OK for deactivated email resend, got %d. Body: %s", rec3.Code, rec3.Body.String())
		}
	})

	t.Run("successful verification with correct code marks email verified, returns tokens, and auto-enrolls", func(t *testing.T) {
		// Reset active code to known code
		_, _ = pool.Exec(context.Background(), "DELETE FROM verification_codes WHERE user_id = $1", heroUserID)
		_, _ = pool.Exec(context.Background(), `
			INSERT INTO verification_codes (user_id, purpose, code_hash, attempts, expires_at)
			VALUES ($1, 'email_verification', $2, 0, NOW() + INTERVAL '15 minutes')
		`, heroUserID, knownCodeHash)

		// Insert a dummy pre-existing refresh token to assert revocation on verification (Topic 011 Phase 1)
		dummyTokenHash := auth.HashToken("dummy-pre-existing-refresh-token")
		_, _ = pool.Exec(context.Background(), "INSERT INTO refresh_tokens (user_id, token_hash, expires_at) VALUES ($1, $2, NOW() + INTERVAL '7 days')", heroUserID, dummyTokenHash)

		payload := HeroVerifyEmailRequest{
			Email: testEmail,
			Code:  knownCode,
		}
		bodyBytes, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/heroes/verify-email", bytes.NewReader(bodyBytes))
		rec := httptest.NewRecorder()

		h.VerifyEmailHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on valid verification, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		dataBytes, _ := json.Marshal(resp.Data)
		var tokenResp AuthTokenResponse
		_ = json.Unmarshal(dataBytes, &tokenResp)

		if tokenResp.AccessToken == "" || tokenResp.RefreshToken == "" {
			t.Fatal("expected access and refresh tokens returned on successful verification")
		}
		if tokenResp.User.IsStaff || !tokenResp.User.IsHero {
			t.Fatalf("expected is_staff=false, is_hero=true, got: %+v", tokenResp.User)
		}
		if tokenResp.User.EmailVerifiedAt == nil {
			t.Fatal("expected email_verified_at to be populated")
		}

		// Verify database state: email_verified_at is set on users table
		var verifiedAt *time.Time
		err := pool.QueryRow(context.Background(), "SELECT email_verified_at FROM users WHERE id = $1", heroUserID).Scan(&verifiedAt)
		if err != nil || verifiedAt == nil {
			t.Fatalf("expected users.email_verified_at to be valid in DB, got: %v", verifiedAt)
		}

		// Verify auto-enrollment occurred upon email verification (Topic 010 §4.5 & AR H1)
		var catName string
		err = pool.QueryRow(context.Background(), `
			SELECT bc.name
			FROM hero_categories hc
			JOIN beneficiary_categories bc ON bc.id = hc.beneficiary_category_id
			WHERE hc.hero_id = $1
		`, heroUserID).Scan(&catName)
		if err != nil {
			t.Fatalf("expected auto-enrollment into university category after verification: %v", err)
		}
		if catName != "Universitas Gadjah Mada" {
			t.Fatalf("expected category 'Universitas Gadjah Mada', got %q", catName)
		}

		// Verify that pre-existing refresh tokens were revoked upon verification (Topic 011 Phase 1)
		var revokedAt *time.Time
		_ = pool.QueryRow(context.Background(), "SELECT revoked_at FROM refresh_tokens WHERE token_hash = $1", dummyTokenHash).Scan(&revokedAt)
		if revokedAt == nil {
			t.Fatal("expected pre-existing refresh token to be revoked upon email verification")
		}

		// Subsequent verification attempt on already verified email returns 400
		rec2 := httptest.NewRecorder()
		req2 := httptest.NewRequest(http.MethodPost, "/api/v1/heroes/verify-email", bytes.NewReader(bodyBytes))
		h.VerifyEmailHandler(rec2, req2)
		if rec2.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request when verifying already verified email, got %d", rec2.Code)
		}
	})

	t.Run("verification preserves staff role and division in token claims for dual-role user", func(t *testing.T) {
		dualEmail := "dual.role.staff.hero@berbagibitesjogja.com"
		defer func() {
			_, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE email = $1", dualEmail)
		}()

		// Create user with unverified email
		var dualUserID uuid.UUID
		err := pool.QueryRow(context.Background(), `
			INSERT INTO users (email, name, password_hash, is_active, email_verified_at)
			VALUES ($1, 'Dual Role Staff', 'hash', true, NULL)
			RETURNING id
		`, dualEmail).Scan(&dualUserID)
		if err != nil {
			t.Fatalf("failed to insert test dual user: %v", err)
		}

		// Add staff row
		_, err = pool.Exec(context.Background(), `
			INSERT INTO staff (user_id, division_id, role)
			VALUES ($1, 2, 'core')
		`, dualUserID)
		if err != nil {
			t.Fatalf("failed to insert staff record: %v", err)
		}

		// Add hero row
		_, err = pool.Exec(context.Background(), `
			INSERT INTO heroes (user_id) VALUES ($1)
		`, dualUserID)
		if err != nil {
			t.Fatalf("failed to insert hero record: %v", err)
		}

		// Add verification code
		code := "654321"
		_, err = pool.Exec(context.Background(), `
			INSERT INTO verification_codes (user_id, purpose, code_hash, attempts, expires_at)
			VALUES ($1, 'email_verification', $2, 0, NOW() + INTERVAL '15 minutes')
		`, dualUserID, auth.HashToken(code))
		if err != nil {
			t.Fatalf("failed to insert verification code: %v", err)
		}

		payload := HeroVerifyEmailRequest{
			Email: dualEmail,
			Code:  code,
		}
		bodyBytes, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/heroes/verify-email", bytes.NewReader(bodyBytes))
		rec := httptest.NewRecorder()
		h.VerifyEmailHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on dual role verification, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		dataBytes, _ := json.Marshal(resp.Data)
		var tokenResp AuthTokenResponse
		_ = json.Unmarshal(dataBytes, &tokenResp)

		if !tokenResp.User.IsStaff || !tokenResp.User.IsHero {
			t.Fatalf("expected is_staff=true and is_hero=true, got %+v", tokenResp.User)
		}
		if tokenResp.User.Role != "core" || tokenResp.User.DivisionID != 2 {
			t.Fatalf("expected role='core' and division_id=2, got role=%q div=%d", tokenResp.User.Role, tokenResp.User.DivisionID)
		}

		claims, err := auth.ParseToken(tokenResp.AccessToken, cfg.AppConfig.JWTSecret)
		if err != nil {
			t.Fatalf("failed to parse returned access token: %v", err)
		}
		if !claims.IsStaff || !claims.IsHero || claims.Role != "core" || claims.DivisionID != 2 {
			t.Fatalf("token claims corrupted: %+v", claims)
		}
	})
}

func TestAutoEnrollDomainMatchingLogic(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	queries := dbgen.New(pool)
	ctx := context.Background()

	t.Run("Exact domain match ugm.ac.id", func(t *testing.T) {
		cat, err := queries.MatchUniversityCategoryByEmailDomain(ctx, "ugm.ac.id")
		if err != nil {
			t.Fatalf("expected match for ugm.ac.id, got error: %v", err)
		}
		if cat.Name != "Universitas Gadjah Mada" {
			t.Fatalf("expected 'Universitas Gadjah Mada', got %q", cat.Name)
		}
	})

	t.Run("Subdomain match mail.ugm.ac.id", func(t *testing.T) {
		cat, err := queries.MatchUniversityCategoryByEmailDomain(ctx, "mail.ugm.ac.id")
		if err != nil {
			t.Fatalf("expected match for mail.ugm.ac.id, got error: %v", err)
		}
		if cat.Name != "Universitas Gadjah Mada" {
			t.Fatalf("expected 'Universitas Gadjah Mada', got %q", cat.Name)
		}
	})

	t.Run("Deep subdomain match sub.student.ugm.ac.id", func(t *testing.T) {
		cat, err := queries.MatchUniversityCategoryByEmailDomain(ctx, "sub.student.ugm.ac.id")
		if err != nil {
			t.Fatalf("expected match for sub.student.ugm.ac.id, got error: %v", err)
		}
		if cat.Name != "Universitas Gadjah Mada" {
			t.Fatalf("expected 'Universitas Gadjah Mada', got %q", cat.Name)
		}
	})

	t.Run("Partial label evilugm.ac.id must NOT match ugm.ac.id", func(t *testing.T) {
		_, err := queries.MatchUniversityCategoryByEmailDomain(ctx, "evilugm.ac.id")
		if !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("expected ErrNoRows for evilugm.ac.id, got %v", err)
		}
	})

	t.Run("Partial label notugm.ac.id must NOT match ugm.ac.id", func(t *testing.T) {
		_, err := queries.MatchUniversityCategoryByEmailDomain(ctx, "notugm.ac.id")
		if !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("expected ErrNoRows for notugm.ac.id, got %v", err)
		}
	})

	t.Run("Prefix without dot ugm.ac.id.attacker.com must NOT match ugm.ac.id", func(t *testing.T) {
		_, err := queries.MatchUniversityCategoryByEmailDomain(ctx, "ugm.ac.id.attacker.com")
		if !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("expected ErrNoRows for ugm.ac.id.attacker.com, got %v", err)
		}
	})

	t.Run("Longest domain wins when more specific category exists", func(t *testing.T) {
		var subCatID uuid.UUID
		err := pool.QueryRow(ctx, `
			INSERT INTO beneficiary_categories (name, variant, email_domain, is_priority, hidden)
			VALUES ('UGM Departemen Khusus', 'university', 'khusus.ugm.ac.id', false, false)
			RETURNING id
		`).Scan(&subCatID)
		if err != nil {
			t.Fatalf("failed to insert specific test category: %v", err)
		}
		defer func() {
			_, _ = pool.Exec(context.Background(), "DELETE FROM beneficiary_categories WHERE id = $1", subCatID)
		}()

		cat, err := queries.MatchUniversityCategoryByEmailDomain(ctx, "khusus.ugm.ac.id")
		if err != nil {
			t.Fatalf("expected match for khusus.ugm.ac.id: %v", err)
		}
		if cat.ID != subCatID {
			t.Fatalf("expected longest domain match category %s ('UGM Departemen Khusus'), got %s (%q)", subCatID, cat.ID, cat.Name)
		}
	})
}
