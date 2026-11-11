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
	"github.com/berbagibitesjogja/core-system/internal/database"
	"github.com/berbagibitesjogja/core-system/internal/middleware"
	"github.com/berbagibitesjogja/core-system/internal/response"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func getTestPool(t *testing.T) *pgxpool.Pool {
	cfg := config.LoadConfig()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	pool, err := database.NewPool(ctx, &cfg.DBConfig)
	if err != nil {
		t.Skipf("skipping test: database not reachable: %v", err)
	}
	return pool
}

func setAuthContext(r *http.Request, userID uuid.UUID, role string) *http.Request {
	claims := &auth.Claims{
		UserID:  userID,
		Role:    role,
		Email:   "tester@berbagibitesjogja.com",
		IsStaff: true,
	}
	ctx := middleware.WithClaims(r.Context(), claims)
	return r.WithContext(ctx)
}

func TestInvitationRoleGuardrails(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	cfg := config.LoadConfig()
	h := NewInvitationHandler(pool, &cfg, nil)

	superID := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	coreID := uuid.MustParse("00000000-0000-0000-0000-000000000003")
	staffID := uuid.MustParse("00000000-0000-0000-0000-000000000004")

	tests := []struct {
		name           string
		callerID       uuid.UUID
		callerRole     string
		targetRole     string
		targetEmail    string
		expectedStatus int
	}{
		{
			name:           "super can invite core",
			callerID:       superID,
			callerRole:     "super",
			targetRole:     "core",
			targetEmail:    "super.invites.core@example.com",
			expectedStatus: http.StatusCreated,
		},
		{
			name:           "super can invite staff",
			callerID:       superID,
			callerRole:     "super",
			targetRole:     "staff",
			targetEmail:    "super.invites.staff@example.com",
			expectedStatus: http.StatusCreated,
		},
		{
			name:           "super can invite super",
			callerID:       superID,
			callerRole:     "super",
			targetRole:     "super",
			targetEmail:    "super.invites.super@example.com",
			expectedStatus: http.StatusCreated,
		},
		{
			name:           "core can invite staff",
			callerID:       coreID,
			callerRole:     "core",
			targetRole:     "staff",
			targetEmail:    "core.invites.staff@example.com",
			expectedStatus: http.StatusCreated,
		},
		{
			name:           "core CANNOT invite core (403)",
			callerID:       coreID,
			callerRole:     "core",
			targetRole:     "core",
			targetEmail:    "core.invites.core@example.com",
			expectedStatus: http.StatusForbidden,
		},
		{
			name:           "core CANNOT invite super (403)",
			callerID:       coreID,
			callerRole:     "core",
			targetRole:     "super",
			targetEmail:    "core.invites.super@example.com",
			expectedStatus: http.StatusForbidden,
		},
		{
			name:           "staff CANNOT invite anyone (403)",
			callerID:       staffID,
			callerRole:     "staff",
			targetRole:     "staff",
			targetEmail:    "staff.invites.staff@example.com",
			expectedStatus: http.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := CreateInvitationRequest{
				Email:      tt.targetEmail,
				Role:       tt.targetRole,
				DivisionID: 2,
				Name:       "Test User",
			}
			bodyBytes, _ := json.Marshal(payload)

			req := httptest.NewRequest(http.MethodPost, "/api/v1/invitations", bytes.NewReader(bodyBytes))
			req = setAuthContext(req, tt.callerID, tt.callerRole)
			rec := httptest.NewRecorder()

			h.CreateInvitationHandler(rec, req)

			if rec.Code != tt.expectedStatus {
				t.Fatalf("expected status %d, got %d. Body: %s", tt.expectedStatus, rec.Code, rec.Body.String())
			}

			if tt.expectedStatus == http.StatusCreated {
				_, _ = pool.Exec(context.Background(), "DELETE FROM user_invitations WHERE email = $1", tt.targetEmail)
			}
		})
	}
}

func TestCreateInvitationValidationAndConstraints(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	cfg := config.LoadConfig()
	h := NewInvitationHandler(pool, &cfg, nil)
	superID := uuid.MustParse("00000000-0000-0000-0000-000000000001")

	t.Run("duplicate active invitation rejected with 400 VAL_003", func(t *testing.T) {
		targetEmail := "dup.invitation@example.com"
		defer func() {
			_, _ = pool.Exec(context.Background(), "DELETE FROM user_invitations WHERE email = $1", targetEmail)
		}()

		payload := CreateInvitationRequest{
			Email:      targetEmail,
			Role:       "staff",
			DivisionID: 2,
		}
		bodyBytes, _ := json.Marshal(payload)

		req1 := httptest.NewRequest(http.MethodPost, "/api/v1/invitations", bytes.NewReader(bodyBytes))
		req1 = setAuthContext(req1, superID, "super")
		rec1 := httptest.NewRecorder()
		h.CreateInvitationHandler(rec1, req1)

		if rec1.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created for first invitation, got %d", rec1.Code)
		}

		req2 := httptest.NewRequest(http.MethodPost, "/api/v1/invitations", bytes.NewReader(bodyBytes))
		req2 = setAuthContext(req2, superID, "super")
		rec2 := httptest.NewRecorder()
		h.CreateInvitationHandler(rec2, req2)

		if rec2.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request on second invitation, got %d", rec2.Code)
		}

		var resp response.Response
		_ = json.Unmarshal(rec2.Body.Bytes(), &resp)
		if resp.Code != "VAL_003" {
			t.Fatalf("expected code VAL_003, got %s", resp.Code)
		}
	})

	t.Run("inviting existing registered user email rejected with 400 VAL_002", func(t *testing.T) {
		existingEmail := "it@berbagibitesjogja.com"

		payload := CreateInvitationRequest{
			Email:      existingEmail,
			Role:       "staff",
			DivisionID: 2,
		}
		bodyBytes, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/invitations", bytes.NewReader(bodyBytes))
		req = setAuthContext(req, superID, "super")
		rec := httptest.NewRecorder()
		h.CreateInvitationHandler(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d", rec.Code)
		}

		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.Code != "VAL_002" {
			t.Fatalf("expected code VAL_002, got %s", resp.Code)
		}
	})

	t.Run("inviting existing hero succeeds with 201 Created", func(t *testing.T) {
		heroEmail := "hero.for.invite@gmail.com"
		var heroUserID uuid.UUID
		err := pool.QueryRow(context.Background(), `
			INSERT INTO users (email, name, is_active, email_verified_at)
			VALUES ($1, 'Hero To Invite', true, NOW())
			RETURNING id
		`, heroEmail).Scan(&heroUserID)
		if err != nil {
			t.Fatalf("failed to create hero user: %v", err)
		}
		_, err = pool.Exec(context.Background(), `
			INSERT INTO heroes (user_id) VALUES ($1)
		`, heroUserID)
		if err != nil {
			t.Fatalf("failed to create hero record: %v", err)
		}
		defer func() {
			_, _ = pool.Exec(context.Background(), "DELETE FROM user_invitations WHERE email = $1", heroEmail)
			_, _ = pool.Exec(context.Background(), "DELETE FROM heroes WHERE user_id = $1", heroUserID)
			_, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", heroUserID)
		}()

		payload := CreateInvitationRequest{
			Email:      heroEmail,
			Role:       "staff",
			DivisionID: 2,
		}
		bodyBytes, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/invitations", bytes.NewReader(bodyBytes))
		req = setAuthContext(req, superID, "super")
		rec := httptest.NewRecorder()
		h.CreateInvitationHandler(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created for inviting existing hero, got %d. Body: %s", rec.Code, rec.Body.String())
		}
	})


	t.Run("invalid email format returns 400 VAL_001", func(t *testing.T) {
		payload := CreateInvitationRequest{
			Email:      "invalid-email",
			Role:       "staff",
			DivisionID: 2,
		}
		bodyBytes, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/invitations", bytes.NewReader(bodyBytes))
		req = setAuthContext(req, superID, "super")
		rec := httptest.NewRecorder()
		h.CreateInvitationHandler(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d", rec.Code)
		}
	})

	t.Run("missing division_id returns 400 VAL_001", func(t *testing.T) {
		payload := CreateInvitationRequest{
			Email:      "valid@example.com",
			Role:       "staff",
			DivisionID: 0,
		}
		bodyBytes, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/invitations", bytes.NewReader(bodyBytes))
		req = setAuthContext(req, superID, "super")
		rec := httptest.NewRecorder()
		h.CreateInvitationHandler(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d", rec.Code)
		}
	})

	t.Run("phone number exceeding 20 chars returns 400 Bad Request", func(t *testing.T) {
		payload := CreateInvitationRequest{
			Email:       "valid.phone.too.long@example.com",
			Role:        "staff",
			DivisionID:  2,
			PhoneNumber: "+62 812-3456-7890-123456",
		}
		bodyBytes, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/invitations", bytes.NewReader(bodyBytes))
		req = setAuthContext(req, superID, "super")
		rec := httptest.NewRecorder()
		h.CreateInvitationHandler(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request for long phone, got %d", rec.Code)
		}
	})

	t.Run("name exceeding 255 chars returns 400 Bad Request", func(t *testing.T) {
		payload := CreateInvitationRequest{
			Email:      "valid.name.too.long@example.com",
			Role:       "staff",
			DivisionID: 2,
			Name:       strings.Repeat("A", 256),
		}
		bodyBytes, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/invitations", bytes.NewReader(bodyBytes))
		req = setAuthContext(req, superID, "super")
		rec := httptest.NewRecorder()
		h.CreateInvitationHandler(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request for long name, got %d", rec.Code)
		}
	})
}

func TestValidateInvitationHandler(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	cfg := config.LoadConfig()
	h := NewInvitationHandler(pool, &cfg, nil)
	superID := uuid.MustParse("00000000-0000-0000-0000-000000000001")

	testEmail := "validate.me@example.com"
	rawToken, tokenHash, _ := auth.GenerateSecureToken()

	_, err := pool.Exec(context.Background(), `
		INSERT INTO user_invitations (invited_by, email, role, division_id, name, token_hash, expires_at)
		VALUES ($1, $2, 'core', 2, 'Validate User', $3, NOW() + INTERVAL '2 hours')
	`, superID, testEmail, tokenHash)
	if err != nil {
		t.Fatalf("failed to insert test invitation: %v", err)
	}
	defer func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM user_invitations WHERE email = $1", testEmail)
	}()

	t.Run("valid unexpired token returns 200 and details", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/invitations/validate?token="+rawToken, nil)
		rec := httptest.NewRecorder()

		h.ValidateInvitationHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		var resp response.Response
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}

		dataBytes, _ := json.Marshal(resp.Data)
		var valResp ValidateInvitationResponse
		_ = json.Unmarshal(dataBytes, &valResp)

		if !valResp.Valid || valResp.Email != testEmail || valResp.Role != "core" {
			t.Fatalf("unexpected validation response: %+v", valResp)
		}
	})

	t.Run("non-existent token returns 404 and sets error in context", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/invitations/validate?token=nonexistenttoken999", nil)
		ctx, _ := response.WithErrorContext(req.Context())
		req = req.WithContext(ctx)
		rec := httptest.NewRecorder()

		h.ValidateInvitationHandler(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 Not Found, got %d", rec.Code)
		}

		recordedErr := response.GetError(req.Context())
		if recordedErr == nil || !strings.Contains(recordedErr.Error(), "invitation token not found") {
			t.Fatalf("expected context error to mention token not found, got: %v", recordedErr)
		}
	})

	t.Run("empty token returns 400", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/invitations/validate", nil)
		rec := httptest.NewRecorder()

		h.ValidateInvitationHandler(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d", rec.Code)
		}
	})
}

func TestAcceptInvitationHandler(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	cfg := config.LoadConfig()
	h := NewInvitationHandler(pool, &cfg, nil)
	superID := uuid.MustParse("00000000-0000-0000-0000-000000000001")

	testEmail := "accept.invite.hero@example.com"
	rawToken, tokenHash, _ := auth.GenerateSecureToken()

	// Pre-create existing user (hero)
	var heroUserID uuid.UUID
	err := pool.QueryRow(context.Background(), `
		INSERT INTO users (email, name, is_active, email_verified_at)
		VALUES ($1, 'Accepting Hero', true, NOW())
		RETURNING id
	`, testEmail).Scan(&heroUserID)
	if err != nil {
		t.Fatalf("failed to insert existing hero user: %v", err)
	}
	defer func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM user_invitations WHERE email = $1", testEmail)
		_, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", heroUserID)
	}()

	_, err = pool.Exec(context.Background(), `
		INSERT INTO heroes (user_id) VALUES ($1)
	`, heroUserID)
	if err != nil {
		t.Fatalf("failed to insert hero record: %v", err)
	}

	// Create staff invitation for this email
	_, err = pool.Exec(context.Background(), `
		INSERT INTO user_invitations (invited_by, email, role, division_id, name, token_hash, expires_at)
		VALUES ($1, $2, 'staff', 2, 'Staff Invite Name', $3, NOW() + INTERVAL '24 hours')
	`, superID, testEmail, tokenHash)
	if err != nil {
		t.Fatalf("failed to insert test invitation: %v", err)
	}

	t.Run("email mismatch returns 403 Forbidden", func(t *testing.T) {
		payload := AcceptInvitationRequest{Token: rawToken}
		bodyBytes, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/invitations/accept", bytes.NewReader(bodyBytes))
		// Authenticated as someone else
		claims := &auth.Claims{
			UserID:  uuid.New(),
			Email:   "different.email@example.com",
			IsStaff: false,
			IsHero:  true,
		}
		req = req.WithContext(middleware.WithClaims(req.Context(), claims))
		rec := httptest.NewRecorder()

		h.AcceptInvitationHandler(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 Forbidden for email mismatch, got %d. Body: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("successful invitation acceptance attaches staff role to hero", func(t *testing.T) {
		payload := AcceptInvitationRequest{Token: rawToken}
		bodyBytes, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/invitations/accept", bytes.NewReader(bodyBytes))
		claims := &auth.Claims{
			UserID:  heroUserID,
			Email:   testEmail,
			IsStaff: false,
			IsHero:  true,
		}
		req = req.WithContext(middleware.WithClaims(req.Context(), claims))
		rec := httptest.NewRecorder()

		h.AcceptInvitationHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on invitation accept, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		dataBytes, _ := json.Marshal(resp.Data)
		var tokenResp AuthTokenResponse
		_ = json.Unmarshal(dataBytes, &tokenResp)

		// User now has both is_staff=true and is_hero=true!
		if !tokenResp.User.IsStaff || !tokenResp.User.IsHero {
			t.Fatalf("expected both is_staff=true and is_hero=true, got: %+v", tokenResp.User)
		}
		if tokenResp.User.Role != "staff" || tokenResp.User.DivisionID != 2 || tokenResp.User.DivisionName != "Managerial" {
			t.Fatalf("expected role='staff', division_id=2, division_name='Managerial', got: %+v", tokenResp.User)
		}

		// Verify database state: staff row exists and invitation marked accepted
		var staffRole string
		err := pool.QueryRow(context.Background(), "SELECT role FROM staff WHERE user_id = $1", heroUserID).Scan(&staffRole)
		if err != nil || staffRole != "staff" {
			t.Fatalf("expected staff record in DB, got: %s (err: %v)", staffRole, err)
		}

		var acceptedAt *time.Time
		_ = pool.QueryRow(context.Background(), "SELECT accepted_at FROM user_invitations WHERE token_hash = $1", tokenHash).Scan(&acceptedAt)
		if acceptedAt == nil {
			t.Fatal("expected invitation to be marked as accepted")
		}
	})

	t.Run("re-accepting already used invitation fails with 410 Gone", func(t *testing.T) {
		payload := AcceptInvitationRequest{Token: rawToken}
		bodyBytes, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/invitations/accept", bytes.NewReader(bodyBytes))
		claims := &auth.Claims{
			UserID:  heroUserID,
			Email:   testEmail,
			IsStaff: true,
			IsHero:  true,
		}
		req = req.WithContext(middleware.WithClaims(req.Context(), claims))
		rec := httptest.NewRecorder()

		h.AcceptInvitationHandler(rec, req)

		if rec.Code != http.StatusGone {
			t.Fatalf("expected 410 Gone for already accepted invitation, got %d. Body: %s", rec.Code, rec.Body.String())
		}
	})
}
