package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/berbagibitesjogja/core-system/internal/auth"
	"github.com/berbagibitesjogja/core-system/internal/config"
	"github.com/berbagibitesjogja/core-system/internal/middleware"
	"github.com/berbagibitesjogja/core-system/internal/response"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

var testStaffPhoneSeq atomic.Int64

func init() {
	testStaffPhoneSeq.Store(time.Now().UnixNano() % 100000000)
}

func createTestStaffUser(t *testing.T, pool *pgxpool.Pool, email, name, role string, divisionID int32) uuid.UUID {
	t.Helper()
	var userID uuid.UUID
	phone := fmt.Sprintf("6289%08d", testStaffPhoneSeq.Add(1)%100000000)
	err := pool.QueryRow(context.Background(), `
		INSERT INTO users (email, name, phone_number, is_active, email_verified_at)
		VALUES ($1, $2, $3, true, NOW())
		RETURNING id
	`, email, name, phone).Scan(&userID)
	if err != nil {
		t.Fatalf("failed to insert test user %s: %v", email, err)
	}

	_, err = pool.Exec(context.Background(), `
		INSERT INTO staff (user_id, division_id, role)
		VALUES ($1, $2, $3)
	`, userID, divisionID, role)
	if err != nil {
		t.Fatalf("failed to insert test staff %s: %v", email, err)
	}

	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", userID)
	})

	return userID
}

func setStaffClaims(r *http.Request, userID uuid.UUID, email, role string, divisionID int32) *http.Request {
	claims := &auth.Claims{
		UserID:     userID,
		Email:      email,
		Role:       role,
		DivisionID: divisionID,
		IsStaff:    true,
		IsHero:     false,
	}
	return r.WithContext(middleware.WithClaims(r.Context(), claims))
}

func TestStaffListAndGet(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	cfg := config.LoadConfig()
	h := NewStaffHandler(pool, &cfg)

	// Create distinct users for list and filter tests
	uniqueID := uuid.NewString()[:8]
	superEmail := fmt.Sprintf("super_%s@example.com", uniqueID)
	staffEmail := fmt.Sprintf("staff_%s@example.com", uniqueID)
	coreEmail := fmt.Sprintf("core_%s@example.com", uniqueID)

	superID := createTestStaffUser(t, pool, superEmail, "Super Tester "+uniqueID, "super", 1)
	staffID := createTestStaffUser(t, pool, staffEmail, "Staff Tester "+uniqueID, "staff", 2)
	coreID := createTestStaffUser(t, pool, coreEmail, "Core Tester "+uniqueID, "core", 2)

	t.Run("GET /api/v1/staff: list active staff with pagination", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/staff?page=1&limit=5", nil)
		req = setStaffClaims(req, superID, superEmail, "super", 1)
		rec := httptest.NewRecorder()

		h.ListStaffHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		var resp response.Response
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal response: %v", err)
		}

		if resp.Meta == nil {
			t.Fatalf("expected pagination metadata in response")
		}
		if resp.Meta.Limit != 5 {
			t.Errorf("expected limit 5, got %d", resp.Meta.Limit)
		}
	})

	t.Run("GET /api/v1/staff: filter by division_id", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/staff?division_id=2", nil)
		req = setStaffClaims(req, superID, superEmail, "super", 1)
		rec := httptest.NewRecorder()

		h.ListStaffHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		dataBytes, _ := json.Marshal(resp.Data)
		var list []StaffMemberResponse
		_ = json.Unmarshal(dataBytes, &list)

		for _, item := range list {
			if item.DivisionID != 2 {
				t.Errorf("expected division_id 2, got %d for user %s", item.DivisionID, item.Email)
			}
		}
	})

	t.Run("GET /api/v1/staff: filter by role", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/staff?role=core", nil)
		req = setStaffClaims(req, superID, superEmail, "super", 1)
		rec := httptest.NewRecorder()

		h.ListStaffHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		dataBytes, _ := json.Marshal(resp.Data)
		var list []StaffMemberResponse
		_ = json.Unmarshal(dataBytes, &list)

		for _, item := range list {
			if item.Role != "core" {
				t.Errorf("expected role core, got %s for user %s", item.Role, item.Email)
			}
		}
	})

	t.Run("GET /api/v1/staff: search by query string", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/staff?search=%s", uniqueID), nil)
		req = setStaffClaims(req, superID, superEmail, "super", 1)
		rec := httptest.NewRecorder()

		h.ListStaffHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		dataBytes, _ := json.Marshal(resp.Data)
		var list []StaffMemberResponse
		_ = json.Unmarshal(dataBytes, &list)

		if len(list) < 3 {
			t.Errorf("expected at least 3 matching results for search query %s, got %d", uniqueID, len(list))
		}
	})

	t.Run("GET /api/v1/staff/{id}: retrieve active staff member", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/staff/"+staffID.String(), nil)
		req.SetPathValue("id", staffID.String())
		req = setStaffClaims(req, superID, superEmail, "super", 1)
		rec := httptest.NewRecorder()

		h.GetStaffHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		dataBytes, _ := json.Marshal(resp.Data)
		var member StaffMemberResponse
		_ = json.Unmarshal(dataBytes, &member)

		if member.UserID != staffID.String() {
			t.Errorf("expected user_id %s, got %s", staffID.String(), member.UserID)
		}
		if member.Role != "staff" {
			t.Errorf("expected role staff, got %s", member.Role)
		}
		if member.DivisionID != 2 {
			t.Errorf("expected division_id 2, got %d", member.DivisionID)
		}
	})

	t.Run("GET /api/v1/staff/{id}: 404 for non-existent staff user", func(t *testing.T) {
		randomID := uuid.NewString()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/staff/"+randomID, nil)
		req.SetPathValue("id", randomID)
		req = setStaffClaims(req, superID, superEmail, "super", 1)
		rec := httptest.NewRecorder()

		h.GetStaffHandler(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 Not Found, got %d. Body: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("GET /api/v1/staff/{id}: 400 for invalid UUID parameter", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/staff/invalid-uuid", nil)
		req.SetPathValue("id", "invalid-uuid")
		req = setStaffClaims(req, superID, superEmail, "super", 1)
		rec := httptest.NewRecorder()

		h.GetStaffHandler(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d. Body: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("GET /api/v1/staff/{id}: 404 for soft-deleted staff member", func(t *testing.T) {
		// Soft delete staffID
		_, err := pool.Exec(context.Background(), "UPDATE staff SET deleted_at = NOW() WHERE user_id = $1", staffID)
		if err != nil {
			t.Fatalf("failed to soft delete: %v", err)
		}

		req := httptest.NewRequest(http.MethodGet, "/api/v1/staff/"+staffID.String(), nil)
		req.SetPathValue("id", staffID.String())
		req = setStaffClaims(req, superID, superEmail, "super", 1)
		rec := httptest.NewRecorder()

		h.GetStaffHandler(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 Not Found, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		// Verify soft-deleted staff also does not appear in ListStaff
		listReq := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/staff?search=%s", staffEmail), nil)
		listReq = setStaffClaims(listReq, superID, superEmail, "super", 1)
		listRec := httptest.NewRecorder()
		h.ListStaffHandler(listRec, listReq)

		var listResp response.Response
		_ = json.Unmarshal(listRec.Body.Bytes(), &listResp)
		dataBytes, _ := json.Marshal(listResp.Data)
		var list []StaffMemberResponse
		_ = json.Unmarshal(dataBytes, &list)

		for _, item := range list {
			if item.UserID == staffID.String() {
				t.Errorf("soft deleted staff member %s should not appear in list", staffID.String())
			}
		}

		// Restore for other subtests
		_, _ = pool.Exec(context.Background(), "UPDATE staff SET deleted_at = NULL WHERE user_id = $1", staffID)
		_ = coreID
	})
}

func TestSuperRoleTransitionsAndGuardrails(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	cfg := config.LoadConfig()
	h := NewStaffHandler(pool, &cfg)

	uid := uuid.NewString()[:8]
	superEmail := fmt.Sprintf("super_trans_%s@example.com", uid)
	superID := createTestStaffUser(t, pool, superEmail, "Super Trans "+uid, "super", 1)

	t.Run("Super promotes staff -> core (200 OK)", func(t *testing.T) {
		targetEmail := fmt.Sprintf("target1_%s@example.com", uid)
		targetID := createTestStaffUser(t, pool, targetEmail, "Target 1", "staff", 2)

		body, _ := json.Marshal(map[string]any{"role": "core"})
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/staff/"+targetID.String(), bytes.NewReader(body))
		req.SetPathValue("id", targetID.String())
		req = setStaffClaims(req, superID, superEmail, "super", 1)
		rec := httptest.NewRecorder()

		h.UpdateStaffHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		// Verify DB role
		var dbRole string
		_ = pool.QueryRow(context.Background(), "SELECT role FROM staff WHERE user_id = $1", targetID).Scan(&dbRole)
		if dbRole != "core" {
			t.Errorf("expected DB role core, got %s", dbRole)
		}
	})

	t.Run("Super promotes staff -> super (200 OK)", func(t *testing.T) {
		targetEmail := fmt.Sprintf("target2_%s@example.com", uid)
		targetID := createTestStaffUser(t, pool, targetEmail, "Target 2", "staff", 3)

		body, _ := json.Marshal(map[string]any{"role": "super"})
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/staff/"+targetID.String(), bytes.NewReader(body))
		req.SetPathValue("id", targetID.String())
		req = setStaffClaims(req, superID, superEmail, "super", 1)
		rec := httptest.NewRecorder()

		h.UpdateStaffHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		var dbRole string
		_ = pool.QueryRow(context.Background(), "SELECT role FROM staff WHERE user_id = $1", targetID).Scan(&dbRole)
		if dbRole != "super" {
			t.Errorf("expected DB role super, got %s", dbRole)
		}
	})

	t.Run("Super promotes core -> super (200 OK)", func(t *testing.T) {
		targetEmail := fmt.Sprintf("target3_%s@example.com", uid)
		targetID := createTestStaffUser(t, pool, targetEmail, "Target 3", "core", 4)

		body, _ := json.Marshal(map[string]any{"role": "super"})
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/staff/"+targetID.String(), bytes.NewReader(body))
		req.SetPathValue("id", targetID.String())
		req = setStaffClaims(req, superID, superEmail, "super", 1)
		rec := httptest.NewRecorder()

		h.UpdateStaffHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		var dbRole string
		_ = pool.QueryRow(context.Background(), "SELECT role FROM staff WHERE user_id = $1", targetID).Scan(&dbRole)
		if dbRole != "super" {
			t.Errorf("expected DB role super, got %s", dbRole)
		}
	})

	t.Run("Super demotes super -> core when > 1 super exists (200 OK)", func(t *testing.T) {
		targetEmail := fmt.Sprintf("target4_%s@example.com", uid)
		targetID := createTestStaffUser(t, pool, targetEmail, "Target 4", "super", 1)

		body, _ := json.Marshal(map[string]any{"role": "core"})
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/staff/"+targetID.String(), bytes.NewReader(body))
		req.SetPathValue("id", targetID.String())
		req = setStaffClaims(req, superID, superEmail, "super", 1)
		rec := httptest.NewRecorder()

		h.UpdateStaffHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		var dbRole string
		_ = pool.QueryRow(context.Background(), "SELECT role FROM staff WHERE user_id = $1", targetID).Scan(&dbRole)
		if dbRole != "core" {
			t.Errorf("expected DB role core, got %s", dbRole)
		}
	})

	t.Run("Super demotes core -> staff (200 OK)", func(t *testing.T) {
		targetEmail := fmt.Sprintf("target5_%s@example.com", uid)
		targetID := createTestStaffUser(t, pool, targetEmail, "Target 5", "core", 2)

		body, _ := json.Marshal(map[string]any{"role": "staff"})
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/staff/"+targetID.String(), bytes.NewReader(body))
		req.SetPathValue("id", targetID.String())
		req = setStaffClaims(req, superID, superEmail, "super", 1)
		rec := httptest.NewRecorder()

		h.UpdateStaffHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		var dbRole string
		_ = pool.QueryRow(context.Background(), "SELECT role FROM staff WHERE user_id = $1", targetID).Scan(&dbRole)
		if dbRole != "staff" {
			t.Errorf("expected DB role staff, got %s", dbRole)
		}
	})

	t.Run("Super reassigns division for a staff member (200 OK)", func(t *testing.T) {
		targetEmail := fmt.Sprintf("target6_%s@example.com", uid)
		targetID := createTestStaffUser(t, pool, targetEmail, "Target 6", "staff", 1)

		body, _ := json.Marshal(map[string]any{"division_id": 3})
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/staff/"+targetID.String(), bytes.NewReader(body))
		req.SetPathValue("id", targetID.String())
		req = setStaffClaims(req, superID, superEmail, "super", 1)
		rec := httptest.NewRecorder()

		h.UpdateStaffHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		var dbDiv int32
		_ = pool.QueryRow(context.Background(), "SELECT division_id FROM staff WHERE user_id = $1", targetID).Scan(&dbDiv)
		if dbDiv != 3 {
			t.Errorf("expected DB division_id 3, got %d", dbDiv)
		}
	})

	t.Run("Super reassigns division and promotes in single request (200 OK)", func(t *testing.T) {
		targetEmail := fmt.Sprintf("target7_%s@example.com", uid)
		targetID := createTestStaffUser(t, pool, targetEmail, "Target 7", "staff", 1)

		body, _ := json.Marshal(map[string]any{"division_id": 4, "role": "core"})
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/staff/"+targetID.String(), bytes.NewReader(body))
		req.SetPathValue("id", targetID.String())
		req = setStaffClaims(req, superID, superEmail, "super", 1)
		rec := httptest.NewRecorder()

		h.UpdateStaffHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		var dbRole string
		var dbDiv int32
		_ = pool.QueryRow(context.Background(), "SELECT role, division_id FROM staff WHERE user_id = $1", targetID).Scan(&dbRole, &dbDiv)
		if dbRole != "core" || dbDiv != 4 {
			t.Errorf("expected role core and division 4, got %s and %d", dbRole, dbDiv)
		}
	})

	t.Run("Last Super guardrails on demotion, kick, and resignation (400 ROLE_LAST_SUPER)", func(t *testing.T) {
		lastSuperEmail := fmt.Sprintf("only_super_%s@example.com", uid)
		lastSuperID := createTestStaffUser(t, pool, lastSuperEmail, "Only Super", "super", 1)

		// Soft-delete all other active supers temporarily
		_, err := pool.Exec(context.Background(), `
			UPDATE staff SET deleted_at = NOW()
			WHERE role = 'super' AND user_id != $1 AND deleted_at IS NULL
		`, lastSuperID)
		if err != nil {
			t.Fatalf("failed to isolate last super: %v", err)
		}

		defer func() {
			// Restore all soft-deleted supers
			_, _ = pool.Exec(context.Background(), `
				UPDATE staff SET deleted_at = NULL
				WHERE role = 'super' AND deleted_at IS NOT NULL
			`)
		}()

		// 1. Demoting last super is rejected with 400 ROLE_LAST_SUPER
		body, _ := json.Marshal(map[string]any{"role": "core"})
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/staff/"+lastSuperID.String(), bytes.NewReader(body))
		req.SetPathValue("id", lastSuperID.String())
		req = setStaffClaims(req, lastSuperID, lastSuperEmail, "super", 1)
		rec := httptest.NewRecorder()

		h.UpdateStaffHandler(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request on demoting last super, got %d. Body: %s", rec.Code, rec.Body.String())
		}
		var errResp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &errResp)
		if errResp.Code != "ROLE_LAST_SUPER" {
			t.Errorf("expected error code ROLE_LAST_SUPER, got %s", errResp.Code)
		}

		// 2. Kicking last super is rejected with 400 ROLE_LAST_SUPER (using a pseudo-super caller)
		kickReq := httptest.NewRequest(http.MethodDelete, "/api/v1/staff/"+lastSuperID.String(), nil)
		kickReq.SetPathValue("id", lastSuperID.String())
		kickReq = setStaffClaims(kickReq, superID, superEmail, "super", 1)
		kickRec := httptest.NewRecorder()

		h.KickStaffHandler(kickRec, kickReq)

		if kickRec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request on kicking last super, got %d. Body: %s", kickRec.Code, kickRec.Body.String())
		}
		var kickErrResp response.Response
		_ = json.Unmarshal(kickRec.Body.Bytes(), &kickErrResp)
		if kickErrResp.Code != "ROLE_LAST_SUPER" {
			t.Errorf("expected kick error code ROLE_LAST_SUPER, got %s", kickErrResp.Code)
		}

		// 3. Resigning as last super is rejected with 400 ROLE_LAST_SUPER
		resignReq := httptest.NewRequest(http.MethodDelete, "/api/v1/staff/me", nil)
		resignReq = setStaffClaims(resignReq, lastSuperID, lastSuperEmail, "super", 1)
		resignRec := httptest.NewRecorder()

		h.ResignStaffHandler(resignRec, resignReq)

		if resignRec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request on resigning last super, got %d. Body: %s", resignRec.Code, resignRec.Body.String())
		}
		var resignErrResp response.Response
		_ = json.Unmarshal(resignRec.Body.Bytes(), &resignErrResp)
		if resignErrResp.Code != "ROLE_LAST_SUPER" {
			t.Errorf("expected resign error code ROLE_LAST_SUPER, got %s", resignErrResp.Code)
		}
	})
}

func TestCoreRoleTransitionsAndGuardrails(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	cfg := config.LoadConfig()
	h := NewStaffHandler(pool, &cfg)

	uid := uuid.NewString()[:8]
	coreDiv2Email := fmt.Sprintf("core_div2_%s@example.com", uid)
	coreDiv2ID := createTestStaffUser(t, pool, coreDiv2Email, "Core Div 2", "core", 2)

	t.Run("Core promotes staff in same division -> core (200 OK)", func(t *testing.T) {
		staffDiv2Email := fmt.Sprintf("staff_div2_%s@example.com", uid)
		staffDiv2ID := createTestStaffUser(t, pool, staffDiv2Email, "Staff Div 2", "staff", 2)

		body, _ := json.Marshal(map[string]any{"role": "core"})
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/staff/"+staffDiv2ID.String(), bytes.NewReader(body))
		req.SetPathValue("id", staffDiv2ID.String())
		req = setStaffClaims(req, coreDiv2ID, coreDiv2Email, "core", 2)
		rec := httptest.NewRecorder()

		h.UpdateStaffHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		var dbRole string
		_ = pool.QueryRow(context.Background(), "SELECT role FROM staff WHERE user_id = $1", staffDiv2ID).Scan(&dbRole)
		if dbRole != "core" {
			t.Errorf("expected DB role core, got %s", dbRole)
		}
	})

	t.Run("Core attempts to promote staff in different division -> rejected (403 FORBIDDEN)", func(t *testing.T) {
		staffDiv3Email := fmt.Sprintf("staff_div3_%s@example.com", uid)
		staffDiv3ID := createTestStaffUser(t, pool, staffDiv3Email, "Staff Div 3", "staff", 3)

		body, _ := json.Marshal(map[string]any{"role": "core"})
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/staff/"+staffDiv3ID.String(), bytes.NewReader(body))
		req.SetPathValue("id", staffDiv3ID.String())
		req = setStaffClaims(req, coreDiv2ID, coreDiv2Email, "core", 2)
		rec := httptest.NewRecorder()

		h.UpdateStaffHandler(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 Forbidden, got %d. Body: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("Core attempts to promote staff -> super -> rejected (403 FORBIDDEN)", func(t *testing.T) {
		staffDiv2Email := fmt.Sprintf("staff_div2_super_%s@example.com", uid)
		staffDiv2ID := createTestStaffUser(t, pool, staffDiv2Email, "Staff Div 2", "staff", 2)

		body, _ := json.Marshal(map[string]any{"role": "super"})
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/staff/"+staffDiv2ID.String(), bytes.NewReader(body))
		req.SetPathValue("id", staffDiv2ID.String())
		req = setStaffClaims(req, coreDiv2ID, coreDiv2Email, "core", 2)
		rec := httptest.NewRecorder()

		h.UpdateStaffHandler(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 Forbidden, got %d. Body: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("Core attempts to demote another member -> rejected (403 FORBIDDEN)", func(t *testing.T) {
		peerCoreEmail := fmt.Sprintf("peer_core_%s@example.com", uid)
		peerCoreID := createTestStaffUser(t, pool, peerCoreEmail, "Peer Core", "core", 2)

		body, _ := json.Marshal(map[string]any{"role": "staff"})
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/staff/"+peerCoreID.String(), bytes.NewReader(body))
		req.SetPathValue("id", peerCoreID.String())
		req = setStaffClaims(req, coreDiv2ID, coreDiv2Email, "core", 2)
		rec := httptest.NewRecorder()

		h.UpdateStaffHandler(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 Forbidden, got %d. Body: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("Core attempts to reassign division -> rejected (403 FORBIDDEN)", func(t *testing.T) {
		staffDiv2Email := fmt.Sprintf("staff_reassign_%s@example.com", uid)
		staffDiv2ID := createTestStaffUser(t, pool, staffDiv2Email, "Staff Reassign", "staff", 2)

		body, _ := json.Marshal(map[string]any{"division_id": 1})
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/staff/"+staffDiv2ID.String(), bytes.NewReader(body))
		req.SetPathValue("id", staffDiv2ID.String())
		req = setStaffClaims(req, coreDiv2ID, coreDiv2Email, "core", 2)
		rec := httptest.NewRecorder()

		h.UpdateStaffHandler(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 Forbidden, got %d. Body: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("Core attempts to kick staff member -> rejected (403 FORBIDDEN)", func(t *testing.T) {
		staffDiv2Email := fmt.Sprintf("staff_kick_target_%s@example.com", uid)
		staffDiv2ID := createTestStaffUser(t, pool, staffDiv2Email, "Staff Kick Target", "staff", 2)

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/staff/"+staffDiv2ID.String(), nil)
		req.SetPathValue("id", staffDiv2ID.String())
		req = setStaffClaims(req, coreDiv2ID, coreDiv2Email, "core", 2)
		rec := httptest.NewRecorder()

		h.KickStaffHandler(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 Forbidden, got %d. Body: %s", rec.Code, rec.Body.String())
		}
	})
}

func TestStaffRoleGuardrails(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	cfg := config.LoadConfig()
	h := NewStaffHandler(pool, &cfg)

	uid := uuid.NewString()[:8]
	staffEmail := fmt.Sprintf("regular_staff_%s@example.com", uid)
	staffID := createTestStaffUser(t, pool, staffEmail, "Regular Staff", "staff", 2)

	targetEmail := fmt.Sprintf("target_staff_%s@example.com", uid)
	targetID := createTestStaffUser(t, pool, targetEmail, "Target Staff", "staff", 2)

	t.Run("Staff attempts to promote another member -> rejected (403 FORBIDDEN)", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"role": "core"})
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/staff/"+targetID.String(), bytes.NewReader(body))
		req.SetPathValue("id", targetID.String())
		req = setStaffClaims(req, staffID, staffEmail, "staff", 2)
		rec := httptest.NewRecorder()

		h.UpdateStaffHandler(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 Forbidden, got %d. Body: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("Staff attempts to reassign division -> rejected (403 FORBIDDEN)", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"division_id": 3})
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/staff/"+targetID.String(), bytes.NewReader(body))
		req.SetPathValue("id", targetID.String())
		req = setStaffClaims(req, staffID, staffEmail, "staff", 2)
		rec := httptest.NewRecorder()

		h.UpdateStaffHandler(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 Forbidden, got %d. Body: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("Staff attempts to kick another member -> rejected (403 FORBIDDEN)", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/staff/"+targetID.String(), nil)
		req.SetPathValue("id", targetID.String())
		req = setStaffClaims(req, staffID, staffEmail, "staff", 2)
		rec := httptest.NewRecorder()

		h.KickStaffHandler(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 Forbidden, got %d. Body: %s", rec.Code, rec.Body.String())
		}
	})
}

func TestSelfDemotionAndResignation(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	cfg := config.LoadConfig()
	h := NewStaffHandler(pool, &cfg)

	uid := uuid.NewString()[:8]

	t.Run("Core self-demotes core -> staff (returns fresh tokens)", func(t *testing.T) {
		coreEmail := fmt.Sprintf("core_selfdemote_%s@example.com", uid)
		coreID := createTestStaffUser(t, pool, coreEmail, "Core Demote Me", "core", 3)

		body, _ := json.Marshal(map[string]any{"role": "staff"})
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/staff/me", bytes.NewReader(body))
		req = setStaffClaims(req, coreID, coreEmail, "core", 3)
		rec := httptest.NewRecorder()

		h.SelfDemoteStaffHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		dataBytes, _ := json.Marshal(resp.Data)
		var tokenResp AuthTokenResponse
		_ = json.Unmarshal(dataBytes, &tokenResp)

		if tokenResp.AccessToken == "" || tokenResp.RefreshToken == "" {
			t.Errorf("expected access and refresh tokens in self-demotion response")
		}
		if tokenResp.User.Role != "staff" {
			t.Errorf("expected token user role to be staff, got %s", tokenResp.User.Role)
		}
		if !tokenResp.User.IsStaff {
			t.Errorf("expected is_staff=true")
		}

		// Verify database state
		var dbRole string
		_ = pool.QueryRow(context.Background(), "SELECT role FROM staff WHERE user_id = $1", coreID).Scan(&dbRole)
		if dbRole != "staff" {
			t.Errorf("expected DB role staff, got %s", dbRole)
		}
	})

	t.Run("Super self-demotes super -> core when multiple supers exist", func(t *testing.T) {
		superEmail := fmt.Sprintf("super_selfdemote_%s@example.com", uid)
		superID := createTestStaffUser(t, pool, superEmail, "Super Demote Me", "super", 1)

		body, _ := json.Marshal(map[string]any{"role": "core"})
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/staff/me", bytes.NewReader(body))
		req = setStaffClaims(req, superID, superEmail, "super", 1)
		rec := httptest.NewRecorder()

		h.SelfDemoteStaffHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		dataBytes, _ := json.Marshal(resp.Data)
		var tokenResp AuthTokenResponse
		_ = json.Unmarshal(dataBytes, &tokenResp)

		if tokenResp.User.Role != "core" {
			t.Errorf("expected token user role core, got %s", tokenResp.User.Role)
		}
	})

	t.Run("Staff self-demote attempts invalid transition -> rejected (400 VAL_001)", func(t *testing.T) {
		staffEmail := fmt.Sprintf("staff_bad_demote_%s@example.com", uid)
		staffID := createTestStaffUser(t, pool, staffEmail, "Staff Demote Fail", "staff", 2)

		body, _ := json.Marshal(map[string]any{"role": "staff"})
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/staff/me", bytes.NewReader(body))
		req = setStaffClaims(req, staffID, staffEmail, "staff", 2)
		rec := httptest.NewRecorder()

		h.SelfDemoteStaffHandler(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d. Body: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("Super kicks staff member to Hero (DELETE /api/v1/staff/{id})", func(t *testing.T) {
		superEmail := fmt.Sprintf("super_kicker_%s@example.com", uid)
		superID := createTestStaffUser(t, pool, superEmail, "Super Kicker", "super", 1)

		kickTargetEmail := fmt.Sprintf("kick_target_%s@example.com", uid)
		kickTargetID := createTestStaffUser(t, pool, kickTargetEmail, "Kick Target", "staff", 2)

		// Seed a refresh token for the kick target to verify revocation
		_, err := pool.Exec(context.Background(), `
			INSERT INTO refresh_tokens (user_id, token_hash, expires_at)
			VALUES ($1, 'test_hash_to_revoke', NOW() + INTERVAL '7 days')
		`, kickTargetID)
		if err != nil {
			t.Fatalf("failed to insert test refresh token: %v", err)
		}

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/staff/"+kickTargetID.String(), nil)
		req.SetPathValue("id", kickTargetID.String())
		req = setStaffClaims(req, superID, superEmail, "super", 1)
		rec := httptest.NewRecorder()

		h.KickStaffHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		// 1. Staff record has deleted_at IS NOT NULL
		var deletedAt *time.Time
		_ = pool.QueryRow(context.Background(), "SELECT deleted_at FROM staff WHERE user_id = $1", kickTargetID).Scan(&deletedAt)
		if deletedAt == nil {
			t.Fatalf("expected deleted_at to be set on kicked staff member")
		}

		// 2. Hero record exists
		var heroUserID uuid.UUID
		err = pool.QueryRow(context.Background(), "SELECT user_id FROM heroes WHERE user_id = $1", kickTargetID).Scan(&heroUserID)
		if err != nil || heroUserID != kickTargetID {
			t.Fatalf("expected heroes record to exist for kicked staff member: %v", err)
		}

		// 3. Refresh tokens are revoked
		var revokedAt *time.Time
		err = pool.QueryRow(context.Background(), "SELECT revoked_at FROM refresh_tokens WHERE user_id = $1", kickTargetID).Scan(&revokedAt)
		if err != nil || revokedAt == nil {
			t.Fatalf("expected refresh token to be revoked for kicked staff member: %v", err)
		}
	})

	t.Run("Super cannot kick themselves -> rejected (400 VAL_001)", func(t *testing.T) {
		superEmail := fmt.Sprintf("super_self_kick_%s@example.com", uid)
		superID := createTestStaffUser(t, pool, superEmail, "Super Self Kick", "super", 1)

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/staff/"+superID.String(), nil)
		req.SetPathValue("id", superID.String())
		req = setStaffClaims(req, superID, superEmail, "super", 1)
		rec := httptest.NewRecorder()

		h.KickStaffHandler(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d. Body: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("Staff member self-resigns via DELETE /api/v1/staff/me -> becomes pure Hero", func(t *testing.T) {
		resignEmail := fmt.Sprintf("staff_resigner_%s@example.com", uid)
		resignID := createTestStaffUser(t, pool, resignEmail, "Staff Resigner", "staff", 3)

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/staff/me", nil)
		req = setStaffClaims(req, resignID, resignEmail, "staff", 3)
		rec := httptest.NewRecorder()

		h.ResignStaffHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		dataBytes, _ := json.Marshal(resp.Data)
		var tokenResp AuthTokenResponse
		_ = json.Unmarshal(dataBytes, &tokenResp)

		if tokenResp.AccessToken == "" || tokenResp.RefreshToken == "" {
			t.Errorf("expected access and refresh tokens in resignation response")
		}
		if tokenResp.User.IsStaff {
			t.Errorf("expected is_staff=false in resigned response")
		}
		if !tokenResp.User.IsHero {
			t.Errorf("expected is_hero=true in resigned response")
		}
		if tokenResp.User.Role != "" {
			t.Errorf("expected role to be empty, got %s", tokenResp.User.Role)
		}

		// Verify database state: staff soft-deleted and heroes record present
		var deletedAt *time.Time
		_ = pool.QueryRow(context.Background(), "SELECT deleted_at FROM staff WHERE user_id = $1", resignID).Scan(&deletedAt)
		if deletedAt == nil {
			t.Fatalf("expected deleted_at to be set on resigned staff member")
		}

		var heroID uuid.UUID
		err := pool.QueryRow(context.Background(), "SELECT user_id FROM heroes WHERE user_id = $1", resignID).Scan(&heroID)
		if err != nil || heroID != resignID {
			t.Fatalf("expected heroes record to exist for resigned staff member: %v", err)
		}
	})
}
