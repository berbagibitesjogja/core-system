package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/berbagibitesjogja/core-system/internal/auth"
	"github.com/berbagibitesjogja/core-system/internal/config"
	"github.com/berbagibitesjogja/core-system/internal/response"
	"github.com/google/uuid"
)

func TestRegisterHandler(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	cfg := config.LoadConfig()
	h := NewAuthHandler(pool, &cfg)

	superID := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	rawToken, tokenHash, _ := auth.GenerateSecureToken()

	testEmail := "register.target@example.com"
	_, err := pool.Exec(context.Background(), `
		INSERT INTO user_invitations (invited_by, email, role, division_id, name, token_hash, expires_at)
		VALUES ($1, $2, 'core', 2, 'Registration Target', $3, NOW() + INTERVAL '24 hours')
	`, superID, testEmail, tokenHash)
	if err != nil {
		t.Fatalf("failed to insert test invitation: %v", err)
	}
	defer func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM user_invitations WHERE email = $1", testEmail)
		_, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE email = $1", testEmail)
	}()

	t.Run("successful registration with valid invitation token", func(t *testing.T) {
		payload := RegisterRequest{
			InvitationToken: rawToken,
			Password:        "securePassword123!",
			Name:            "Registration Target User",
			PhoneNumber:     "+62 812-9988-7766",
		}
		bodyBytes, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewReader(bodyBytes))
		rec := httptest.NewRecorder()

		h.RegisterHandler(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		var resp response.Response
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}

		dataBytes, _ := json.Marshal(resp.Data)
		var tokenResp AuthTokenResponse
		_ = json.Unmarshal(dataBytes, &tokenResp)

		if tokenResp.AccessToken == "" || tokenResp.RefreshToken == "" {
			t.Fatal("expected access and refresh tokens to be returned")
		}
		if tokenResp.User.Email != testEmail || tokenResp.User.Role != "core" {
			t.Fatalf("unexpected user response: %+v", tokenResp.User)
		}
		if tokenResp.User.PhoneNumber != "6281299887766" {
			t.Fatalf("expected normalized phone number '6281299887766', got %s", tokenResp.User.PhoneNumber)
		}
		if !tokenResp.User.IsActive || tokenResp.User.EmailVerifiedAt == nil {
			t.Fatalf("expected active and verified user, got: %+v", tokenResp.User)
		}

		// Verify invitation was marked as accepted
		var acceptedAt *time.Time
		err := pool.QueryRow(context.Background(), "SELECT accepted_at FROM user_invitations WHERE token_hash = $1", tokenHash).Scan(&acceptedAt)
		if err != nil || acceptedAt == nil {
			t.Fatalf("expected invitation to be marked as accepted, got: %v", acceptedAt)
		}
	})

	t.Run("attempting to re-use accepted invitation fails with 410 Gone", func(t *testing.T) {
		payload := RegisterRequest{
			InvitationToken: rawToken,
			Password:        "anotherPassword123!",
			Name:            "Second Attempt",
			PhoneNumber:     "0811223344",
		}
		bodyBytes, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewReader(bodyBytes))
		ctx, _ := response.WithErrorContext(req.Context())
		req = req.WithContext(ctx)
		rec := httptest.NewRecorder()

		h.RegisterHandler(rec, req)

		if rec.Code != http.StatusGone {
			t.Fatalf("expected 410 Gone for reused token, got %d. Body: %s", rec.Code, rec.Body.String())
		}
		recordedErr := response.GetError(req.Context())
		if recordedErr == nil || !strings.Contains(recordedErr.Error(), "already accepted") {
			t.Fatalf("expected context error to mention already accepted, got: %v", recordedErr)
		}
	})

	t.Run("invalid token fails with 403 Forbidden", func(t *testing.T) {
		payload := RegisterRequest{
			InvitationToken: "completely-bogus-token-12345",
			Password:        "anotherPassword123!",
			Name:            "Bogus Token Attempt",
			PhoneNumber:     "0811223344",
		}
		bodyBytes, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewReader(bodyBytes))
		ctx, _ := response.WithErrorContext(req.Context())
		req = req.WithContext(ctx)
		rec := httptest.NewRecorder()

		h.RegisterHandler(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 Forbidden for fake token, got %d", rec.Code)
		}
		recordedErr := response.GetError(req.Context())
		if recordedErr == nil || !strings.Contains(recordedErr.Error(), "invitation token not found") {
			t.Fatalf("expected context error to mention token not found, got: %v", recordedErr)
		}
	})

	t.Run("short password fails with 400 Bad Request", func(t *testing.T) {
		payload := RegisterRequest{
			InvitationToken: rawToken,
			Password:        "short",
			Name:            "Short Pass",
			PhoneNumber:     "0811223344",
		}
		bodyBytes, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewReader(bodyBytes))
		rec := httptest.NewRecorder()

		h.RegisterHandler(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request for short password, got %d", rec.Code)
		}
	})

	t.Run("duplicate phone number returns 400 with field error", func(t *testing.T) {
		dupRawToken, dupTokenHash, _ := auth.GenerateSecureToken()
		dupEmail := "dup.phone.candidate@example.com"

		_, err := pool.Exec(context.Background(), `
			INSERT INTO user_invitations (invited_by, email, role, division_id, name, token_hash, expires_at)
			VALUES ($1, $2, 'staff', 2, 'Dup Candidate', $3, NOW() + INTERVAL '24 hours')
		`, superID, dupEmail, dupTokenHash)
		if err != nil {
			t.Fatalf("failed to insert test invitation: %v", err)
		}
		defer func() {
			_, _ = pool.Exec(context.Background(), "DELETE FROM user_invitations WHERE email = $1", dupEmail)
			_, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE email = $1", dupEmail)
		}()

		// Use the phone number registered in the previous test case (+62 812-9988-7766)
		payload := RegisterRequest{
			InvitationToken: dupRawToken,
			Password:        "anotherPassword123!",
			Name:            "Dup Phone Candidate",
			PhoneNumber:     "+62 812-9988-7766",
		}
		bodyBytes, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewReader(bodyBytes))
		rec := httptest.NewRecorder()

		h.RegisterHandler(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request for duplicate phone, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		var resp response.Response
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if resp.Code != "VAL_002" {
			t.Fatalf("expected code VAL_002, got %s", resp.Code)
		}

		errMap, ok := resp.Errors.(map[string]any)
		if !ok || errMap["phone_number"] != "Phone number is already registered" {
			t.Fatalf("expected phone_number error, got %+v", resp.Errors)
		}
	})

	t.Run("duplicate email on registration returns 400 VAL_002 via atomic constraint trap", func(t *testing.T) {
		dupEmailRawToken, dupEmailTokenHash, _ := auth.GenerateSecureToken()
		existingEmail := "it@berbagibitesjogja.com"

		_, err := pool.Exec(context.Background(), `
			INSERT INTO user_invitations (invited_by, email, role, division_id, name, token_hash, expires_at)
			VALUES ($1, $2, 'staff', 2, 'Dup Email Target', $3, NOW() + INTERVAL '24 hours')
		`, superID, existingEmail, dupEmailTokenHash)
		if err != nil {
			t.Fatalf("failed to insert test invitation: %v", err)
		}
		defer func() {
			_, _ = pool.Exec(context.Background(), "DELETE FROM user_invitations WHERE email = $1", existingEmail)
		}()

		payload := RegisterRequest{
			InvitationToken: dupEmailRawToken,
			Password:        "anotherPassword123!",
			Name:            "Dup Email Candidate",
			PhoneNumber:     "+62 855-4433-2211",
		}
		bodyBytes, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewReader(bodyBytes))
		rec := httptest.NewRecorder()

		h.RegisterHandler(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		var resp response.Response
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if resp.Code != "VAL_002" {
			t.Fatalf("expected code VAL_002, got %s", resp.Code)
		}

		errMap, ok := resp.Errors.(map[string]any)
		if !ok || errMap["email"] != "Email address is already registered" {
			t.Fatalf("expected email error, got %+v", resp.Errors)
		}
	})

	t.Run("phone number exceeding 20 characters fails with 400 VAL_001", func(t *testing.T) {
		payload := RegisterRequest{
			InvitationToken: rawToken,
			Password:        "anotherPassword123!",
			Name:            "Long Phone",
			PhoneNumber:     "+62 812-3456-7890-123456",
		}
		bodyBytes, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewReader(bodyBytes))
		rec := httptest.NewRecorder()

		h.RegisterHandler(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d", rec.Code)
		}
	})

	t.Run("name exceeding 255 characters fails with 400 VAL_001", func(t *testing.T) {
		payload := RegisterRequest{
			InvitationToken: rawToken,
			Password:        "anotherPassword123!",
			Name:            strings.Repeat("A", 256),
			PhoneNumber:     "+62 812-3456-7890",
		}
		bodyBytes, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewReader(bodyBytes))
		rec := httptest.NewRecorder()

		h.RegisterHandler(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d", rec.Code)
		}
	})
}

func TestLoginRefreshAndLogoutFlow(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	cfg := config.LoadConfig()
	h := NewAuthHandler(pool, &cfg)

	t.Run("successful login returns tokens and user profile", func(t *testing.T) {
		payload := LoginRequest{
			Email:    "it@berbagibitesjogja.com",
			Password: "password",
		}
		bodyBytes, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(bodyBytes))
		rec := httptest.NewRecorder()

		h.LoginHandler(rec, req)

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
		if tokenResp.User.Email != "it@berbagibitesjogja.com" {
			t.Fatalf("expected email 'it@berbagibitesjogja.com', got %q", tokenResp.User.Email)
		}
	})

	t.Run("login with wrong password returns 401 Unauthorized", func(t *testing.T) {
		payload := LoginRequest{
			Email:    "it@berbagibitesjogja.com",
			Password: "wrongpassword",
		}
		bodyBytes, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(bodyBytes))
		ctx, _ := response.WithErrorContext(req.Context())
		req = req.WithContext(ctx)
		rec := httptest.NewRecorder()

		h.LoginHandler(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 Unauthorized, got %d", rec.Code)
		}
		recordedErr := response.GetError(req.Context())
		if recordedErr == nil || !strings.Contains(recordedErr.Error(), "invalid password") {
			t.Fatalf("expected context error to mention invalid password, got: %v", recordedErr)
		}
	})

	t.Run("login with non-existent email returns 401 Unauthorized", func(t *testing.T) {
		payload := LoginRequest{
			Email:    "doesnotexist@example.com",
			Password: "password",
		}
		bodyBytes, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(bodyBytes))
		ctx, _ := response.WithErrorContext(req.Context())
		req = req.WithContext(ctx)
		rec := httptest.NewRecorder()

		h.LoginHandler(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 Unauthorized, got %d", rec.Code)
		}
		recordedErr := response.GetError(req.Context())
		if recordedErr == nil || !strings.Contains(recordedErr.Error(), "user not found") {
			t.Fatalf("expected context error to mention user not found, got: %v", recordedErr)
		}
	})
}
