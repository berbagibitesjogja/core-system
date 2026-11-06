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
	"github.com/berbagibitesjogja/core-system/internal/response"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/berbagibitesjogja/core-system/internal/middleware"
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
		UserID: userID,
		Role:   role,
		Email:  "tester@berbagibitesjogja.com",
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
			targetEmail:    "staff.invites.anyone@example.com",
			expectedStatus: http.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := CreateInvitationRequest{
				Email:      tt.targetEmail,
				Role:       tt.targetRole,
				DivisionID: 2,
				Name:       "Role Guardrail Test User",
			}
			bodyBytes, _ := json.Marshal(payload)

			req := httptest.NewRequest(http.MethodPost, "/api/v1/invitations", bytes.NewReader(bodyBytes))
			req = setAuthContext(req, tt.callerID, tt.callerRole)
			rec := httptest.NewRecorder()

			h.CreateInvitationHandler(rec, req)

			if rec.Code != tt.expectedStatus {
				t.Fatalf("expected status %d, got %d. Body: %s", tt.expectedStatus, rec.Code, rec.Body.String())
			}

			// Clean up created invitations
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
	cfg.AppConfig.FrontendURL = "https://custom.app.com"
	h := NewInvitationHandler(pool, &cfg, nil)

	superID := uuid.MustParse("00000000-0000-0000-0000-000000000001")

	t.Run("creates invitation with normalized phone and dynamic frontend URL", func(t *testing.T) {
		testEmail := "test.dynamic.invite@bbj.org"
		payload := CreateInvitationRequest{
			Email:       testEmail,
			Role:        "staff",
			DivisionID:  2,
			Name:        "Normalized Phone Test",
			PhoneNumber: "+62 812-9988-7766",
		}
		bodyBytes, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/invitations", bytes.NewReader(bodyBytes))
		req = setAuthContext(req, superID, "super")
		rec := httptest.NewRecorder()

		h.CreateInvitationHandler(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created, got %d. Body: %s", rec.Code, rec.Body.String())
		}
		defer func() {
			_, _ = pool.Exec(context.Background(), "DELETE FROM user_invitations WHERE email = $1", testEmail)
		}()

		var resp response.Response
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		dataBytes, _ := json.Marshal(resp.Data)
		var invResp InvitationResponse
		_ = json.Unmarshal(dataBytes, &invResp)

		if invResp.PhoneNumber == nil || *invResp.PhoneNumber != "6281299887766" {
			t.Fatalf("expected phone number to be normalized to '6281299887766', got %v", invResp.PhoneNumber)
		}

		if !strings.HasPrefix(invResp.InviteLink, "https://custom.app.com/register?token=") {
			t.Fatalf("expected invite_link to use FRONTEND_URL 'https://custom.app.com/register?token=', got %s", invResp.InviteLink)
		}

		if invResp.Token == "" {
			t.Fatal("expected raw token to be returned in invitation response")
		}
	})

	t.Run("inviting existing user sets error in context for logger", func(t *testing.T) {
		payload := CreateInvitationRequest{
			Email:      "it@berbagibitesjogja.com", // existing seed user
			Role:       "staff",
			DivisionID: 2,
		}
		bodyBytes, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/invitations", bytes.NewReader(bodyBytes))
		req = setAuthContext(req, superID, "super")
		ctx, _ := response.WithErrorContext(req.Context())
		req = req.WithContext(ctx)
		rec := httptest.NewRecorder()

		h.CreateInvitationHandler(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		recordedErr := response.GetError(req.Context())
		if recordedErr == nil || !strings.Contains(recordedErr.Error(), "active user already exists") {
			t.Fatalf("expected context error to mention active user already exists, got: %v", recordedErr)
		}
	})

	t.Run("non-existent division returns 400 Bad Request with field error", func(t *testing.T) {
		payload := CreateInvitationRequest{
			Email:      "invalid.div@example.com",
			Role:       "staff",
			DivisionID: 99999, // Non-existent foreign key
			Name:       "Div Test",
		}
		bodyBytes, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/invitations", bytes.NewReader(bodyBytes))
		req = setAuthContext(req, superID, "super")
		rec := httptest.NewRecorder()

		h.CreateInvitationHandler(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request for non-existent division, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		var resp response.Response
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if resp.Code != "VAL_002" {
			t.Fatalf("expected VAL_002, got %s", resp.Code)
		}

		errMap, ok := resp.Errors.(map[string]any)
		if !ok || errMap["division_id"] != "Selected division does not exist" {
			t.Fatalf("expected division_id error, got %+v", resp.Errors)
		}
	})

	t.Run("phone number exceeding 20 chars returns 400 Bad Request", func(t *testing.T) {
		payload := CreateInvitationRequest{
			Email:       "long.phone@example.com",
			Role:        "staff",
			DivisionID:  2,
			Name:        "Long Phone",
			PhoneNumber: "+62 812-3456-7890-123456",
		}
		bodyBytes, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/invitations", bytes.NewReader(bodyBytes))
		req = setAuthContext(req, superID, "super")
		rec := httptest.NewRecorder()

		h.CreateInvitationHandler(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request for phone > 20 chars, got %d", rec.Code)
		}
	})

	t.Run("name exceeding 255 chars returns 400 Bad Request", func(t *testing.T) {
		payload := CreateInvitationRequest{
			Email:      "long.name@example.com",
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
			t.Fatalf("expected 400 Bad Request for name > 255 chars, got %d", rec.Code)
		}
	})
}

func TestValidateInvitationHandler(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	cfg := config.LoadConfig()
	h := NewInvitationHandler(pool, &cfg, nil)

	superID := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	rawToken, tokenHash, _ := auth.GenerateSecureToken()

	// Seed an active invitation
	testEmail := "validate.test@example.com"
	_, err := pool.Exec(context.Background(), `
		INSERT INTO user_invitations (invited_by, email, role, division_id, name, token_hash, expires_at)
		VALUES ($1, $2, 'core', 2, 'Validation Target', $3, NOW() + INTERVAL '2 hours')
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
