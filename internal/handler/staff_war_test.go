package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/berbagibitesjogja/core-system/internal/auth"
	"github.com/berbagibitesjogja/core-system/internal/config"
	"github.com/berbagibitesjogja/core-system/internal/middleware"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

var testStaffWarPhoneSeq atomic.Int64

func init() {
	testStaffWarPhoneSeq.Store(time.Now().UnixNano() % 100000000)
}

func createTestStaffForWar(t *testing.T, pool *pgxpool.Pool, email, name, role string, divisionID int32) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	var userID uuid.UUID
	phone := fmt.Sprintf("6286%08d", testStaffWarPhoneSeq.Add(1)%100000000)
	err := pool.QueryRow(ctx, `
		INSERT INTO users (email, name, phone_number, is_active, email_verified_at)
		VALUES ($1, $2, $3, true, NOW())
		RETURNING id
	`, email, name, phone).Scan(&userID)
	if err != nil {
		t.Fatalf("failed to insert test staff user: %v", err)
	}

	_, err = pool.Exec(ctx, `
		INSERT INTO staff (user_id, division_id, role)
		VALUES ($1, $2, $3)
	`, userID, divisionID, role)
	if err != nil {
		t.Fatalf("failed to insert test staff record: %v", err)
	}

	return userID
}

func TestStaffWarEngine(t *testing.T) {
	pool := getTestPool(t)

	cfg := config.LoadConfig()
	cfg.War.LateCancellationWindow = 12 * time.Hour
	cfg.War.StaffBanEventCount = 1
	h := NewStaffWarHandler(pool, &cfg)
	ctx := context.Background()

	// Cleanup test artifacts upfront and on finish
	cleanup := func() {
		_, _ = pool.Exec(ctx, "DELETE FROM user_bans")
		_, _ = pool.Exec(ctx, "DELETE FROM staff_attendances")
		_, _ = pool.Exec(ctx, "DELETE FROM hero_attendances")
		_, _ = pool.Exec(ctx, "DELETE FROM war_slots")
		_, _ = pool.Exec(ctx, "DELETE FROM donation_target WHERE donation_id IN (SELECT id FROM donations WHERE address LIKE 'Jl. Malioboro%' OR address LIKE 'Jl. Kaliurang%')")
		_, _ = pool.Exec(ctx, "DELETE FROM donations WHERE address LIKE 'Jl. Malioboro%' OR address LIKE 'Jl. Kaliurang%'")
		_, _ = pool.Exec(ctx, "DELETE FROM hero_categories WHERE hero_id IN (SELECT id FROM users WHERE email LIKE '%@concurrency.test' OR email LIKE '%@hero.war.test' OR email LIKE '%@attendance.test' OR email LIKE '%@staff.war.test')")
		_, _ = pool.Exec(ctx, "DELETE FROM heroes WHERE user_id IN (SELECT id FROM users WHERE email LIKE '%@concurrency.test' OR email LIKE '%@hero.war.test' OR email LIKE '%@attendance.test' OR email LIKE '%@staff.war.test')")
		_, _ = pool.Exec(ctx, "DELETE FROM staff WHERE user_id IN (SELECT id FROM users WHERE email LIKE '%@concurrency.test' OR email LIKE '%@hero.war.test' OR email LIKE '%@attendance.test' OR email LIKE '%@staff.war.test')")
		_, _ = pool.Exec(ctx, "DELETE FROM users WHERE email LIKE '%@concurrency.test' OR email LIKE '%@hero.war.test' OR email LIKE '%@attendance.test' OR email LIKE '%@staff.war.test'")
	}
	cleanup()
	defer func() {
		cleanup()
		pool.Close()
	}()

	// Helper to create a donation specifically for staff war tests
	createStaffDonation := func(status string, eventAt time.Time) uuid.UUID {
		var sponsorID uuid.UUID
		_ = pool.QueryRow(ctx, `
			INSERT INTO sponsors (name, email, variant)
			VALUES ('Staff Sponsor ' || gen_random_uuid(), 'staffsponsor@example.com', 'individual')
			RETURNING id
		`).Scan(&sponsorID)

		var donationID uuid.UUID
		err := pool.QueryRow(ctx, `
			INSERT INTO donations (
				sponsor_id, event_at, quota, hero_quota, remain,
				address, status, hide_from_public, hide_sponsor
			) VALUES (
				$1, $2, 20, 10, 10,
				'Jl. Malioboro Staff War', $3, FALSE, FALSE
			) RETURNING id
		`, sponsorID, eventAt, status).Scan(&donationID)
		if err != nil {
			t.Fatalf("failed to insert donation: %v", err)
		}
		return donationID
	}

	t.Run("1. Operational Staff configures war slots with quota safety", func(t *testing.T) {
		donationID := createStaffDonation("open", time.Now().Add(24*time.Hour))
		opStaffID := createTestStaffForWar(t, pool, "op1@staff.war.test", "Op Staff One", "staff", 3)

		bodyData := []ConfigureWarSlotItem{
			{DivisionID: 1, Quota: 3},
			{DivisionID: 2, Quota: 5},
		}
		bodyBytes, _ := json.Marshal(bodyData)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/donations/"+donationID.String()+"/war-slots", bytes.NewReader(bodyBytes))
		req.SetPathValue("id", donationID.String())
		claims := &auth.Claims{UserID: opStaffID, Email: "op1@staff.war.test", IsStaff: true, DivisionID: 3, Role: "staff"}
		req = req.WithContext(middleware.WithClaims(req.Context(), claims))
		rec := httptest.NewRecorder()

		h.ConfigureWarSlotsHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
		}

		// Verify ConfiguredWarSlotResponse uses snake_case and excludes deleted_at / PascalCase fields
		var configResp map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &configResp); err != nil {
			t.Fatalf("failed to unmarshal configure response: %v", err)
		}
		configSlots, ok := configResp["data"].([]any)
		if !ok || len(configSlots) != 2 {
			t.Fatalf("expected 2 configured slots in data, got %T (%v)", configResp["data"], configResp["data"])
		}
		slot0 := configSlots[0].(map[string]any)
		for _, requiredKey := range []string{"id", "donation_id", "division_id", "division_name", "quota", "remain", "active_claims", "created_at", "updated_at"} {
			if _, exists := slot0[requiredKey]; !exists {
				t.Errorf("expected snake_case key %q in ConfiguredWarSlotResponse", requiredKey)
			}
		}
		for _, forbiddenKey := range []string{"ID", "DivisionName", "deleted_at", "DeletedAt"} {
			if _, exists := slot0[forbiddenKey]; exists {
				t.Errorf("unexpected raw/internal key %q in ConfiguredWarSlotResponse", forbiddenKey)
			}
		}

		// Verify slots in database
		var count int
		_ = pool.QueryRow(ctx, "SELECT COUNT(*) FROM war_slots WHERE donation_id = $1 AND deleted_at IS NULL", donationID).Scan(&count)
		if count != 2 {
			t.Errorf("expected 2 war slots, got %d", count)
		}

		// List war slots to check division name and remain
		reqList := httptest.NewRequest(http.MethodGet, "/api/v1/donations/"+donationID.String()+"/war-slots", nil)
		reqList.SetPathValue("id", donationID.String())
		reqList = reqList.WithContext(middleware.WithClaims(reqList.Context(), claims))
		recList := httptest.NewRecorder()
		h.ListWarSlotsHandler(recList, reqList)

		if recList.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on list, got %d: %s", recList.Code, recList.Body.String())
		}

		var listResp map[string]any
		_ = json.Unmarshal(recList.Body.Bytes(), &listResp)
		slots := listResp["data"].([]any)
		if len(slots) != 2 {
			t.Fatalf("expected 2 slots in response, got %d", len(slots))
		}
	})

	t.Run("2. Staff claims a shift slot with division guard and duplicate check", func(t *testing.T) {
		donationID := createStaffDonation("open", time.Now().Add(24*time.Hour))
		staffDiv1 := createTestStaffForWar(t, pool, "div1_staff@staff.war.test", "Div1 Staff", "staff", 1)
		staffDiv2 := createTestStaffForWar(t, pool, "div2_staff@staff.war.test", "Div2 Staff", "staff", 2)

		// Create a war slot for Division 1 (quota=1, remain=1)
		var slotID int64
		err := pool.QueryRow(ctx, `
			INSERT INTO war_slots (donation_id, division_id, quota, remain)
			VALUES ($1, 1, 1, 1)
			RETURNING id
		`, donationID).Scan(&slotID)
		if err != nil {
			t.Fatalf("failed to insert war slot: %v", err)
		}

		// Division 2 staff tries to claim Division 1 slot -> 403 Forbidden
		claimsDiv2 := &auth.Claims{UserID: staffDiv2, Email: "div2_staff@staff.war.test", IsStaff: true, DivisionID: 2}
		reqDiv2 := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/donations/%s/war-slots/%d/claim", donationID.String(), slotID), nil)
		reqDiv2.SetPathValue("id", donationID.String())
		reqDiv2.SetPathValue("slot_id", fmt.Sprintf("%d", slotID))
		reqDiv2 = reqDiv2.WithContext(middleware.WithClaims(reqDiv2.Context(), claimsDiv2))
		recDiv2 := httptest.NewRecorder()
		h.ClaimStaffSlotHandler(recDiv2, reqDiv2)

		if recDiv2.Code != http.StatusForbidden {
			t.Fatalf("expected 403 Forbidden for division mismatch, got %d: %s", recDiv2.Code, recDiv2.Body.String())
		}

		// Division 1 staff claims successfully -> 201 Created
		claimsDiv1 := &auth.Claims{UserID: staffDiv1, Email: "div1_staff@staff.war.test", IsStaff: true, DivisionID: 1}
		reqDiv1 := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/donations/%s/war-slots/%d/claim", donationID.String(), slotID), nil)
		reqDiv1.SetPathValue("id", donationID.String())
		reqDiv1.SetPathValue("slot_id", fmt.Sprintf("%d", slotID))
		reqDiv1 = reqDiv1.WithContext(middleware.WithClaims(reqDiv1.Context(), claimsDiv1))
		recDiv1 := httptest.NewRecorder()
		h.ClaimStaffSlotHandler(recDiv1, reqDiv1)

		if recDiv1.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created, got %d: %s", recDiv1.Code, recDiv1.Body.String())
		}

		// Verify StaffAttendanceResponse emits explicit null (not "" or omitted) on nullable fields
		var claimResp map[string]any
		if err := json.Unmarshal(recDiv1.Body.Bytes(), &claimResp); err != nil {
			t.Fatalf("failed to unmarshal staff claim response: %v", err)
		}
		claimData := claimResp["data"].(map[string]any)
		for _, nullableKey := range []string{"checkin_source", "cancelled_at", "attended_at", "marked_by", "notes"} {
			val, exists := claimData[nullableKey]
			if !exists {
				t.Errorf("expected key %q to be explicitly present in StaffAttendanceResponse", nullableKey)
			} else if val != nil {
				t.Errorf("expected %q to be null on fresh claim, got %#v", nullableKey, val)
			}
		}

		// Verify slot remain decremented to 0
		var remain int32
		_ = pool.QueryRow(ctx, "SELECT remain FROM war_slots WHERE id = $1", slotID).Scan(&remain)
		if remain != 0 {
			t.Errorf("expected slot remain=0, got %d", remain)
		}

		// Duplicate claim by same staff -> 409 Conflict (ALREADY_CLAIMED)
		reqDup := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/donations/%s/war-slots/%d/claim", donationID.String(), slotID), nil)
		reqDup.SetPathValue("id", donationID.String())
		reqDup.SetPathValue("slot_id", fmt.Sprintf("%d", slotID))
		reqDup = reqDup.WithContext(middleware.WithClaims(reqDup.Context(), claimsDiv1))
		recDup := httptest.NewRecorder()
		h.ClaimStaffSlotHandler(recDup, reqDup)

		if recDup.Code != http.StatusConflict {
			t.Fatalf("expected 409 Conflict on duplicate claim, got %d: %s", recDup.Code, recDup.Body.String())
		}

		// Another Division 1 staff member tries when remain=0 -> 409 Conflict (WAR_001)
		staffDiv1Second := createTestStaffForWar(t, pool, "div1_staff2@staff.war.test", "Div1 Staff 2", "staff", 1)
		claimsDiv1Second := &auth.Claims{UserID: staffDiv1Second, Email: "div1_staff2@staff.war.test", IsStaff: true, DivisionID: 1}
		reqFull := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/donations/%s/war-slots/%d/claim", donationID.String(), slotID), nil)
		reqFull.SetPathValue("id", donationID.String())
		reqFull.SetPathValue("slot_id", fmt.Sprintf("%d", slotID))
		reqFull = reqFull.WithContext(middleware.WithClaims(reqFull.Context(), claimsDiv1Second))
		recFull := httptest.NewRecorder()
		h.ClaimStaffSlotHandler(recFull, reqFull)

		if recFull.Code != http.StatusConflict {
			t.Fatalf("expected 409 Conflict on full slot, got %d: %s", recFull.Code, recFull.Body.String())
		}
		var fullResp map[string]any
		_ = json.Unmarshal(recFull.Body.Bytes(), &fullResp)
		if fullResp["code"] != "WAR_001" {
			t.Errorf("expected code WAR_001, got %v", fullResp["code"])
		}

		// Quota reduction below active claims (quota=0 or less than 1 active claim) -> 409 Conflict (WAR_003)
		opStaff := createTestStaffForWar(t, pool, "op_quota@staff.war.test", "Op Staff", "staff", 3)
		claimsOp := &auth.Claims{UserID: opStaff, Email: "op_quota@staff.war.test", IsStaff: true, DivisionID: 3}
		badQuotaData := []ConfigureWarSlotItem{{DivisionID: 1, Quota: 0}}
		badBytes, _ := json.Marshal(badQuotaData)
		reqBadQuota := httptest.NewRequest(http.MethodPost, "/api/v1/donations/"+donationID.String()+"/war-slots", bytes.NewReader(badBytes))
		reqBadQuota.SetPathValue("id", donationID.String())
		reqBadQuota = reqBadQuota.WithContext(middleware.WithClaims(reqBadQuota.Context(), claimsOp))
		recBadQuota := httptest.NewRecorder()
		h.ConfigureWarSlotsHandler(recBadQuota, reqBadQuota)
		if recBadQuota.Code != http.StatusBadRequest { // quota <= 0 rejected upfront
			t.Errorf("expected 400 for quota 0, got %d", recBadQuota.Code)
		}
	})

	t.Run("3. Staff cancellation with grace period and late penalty burn-off", func(t *testing.T) {
		// Event in 6 hours (inside 12h cancellation window)
		donationID := createStaffDonation("open", time.Now().Add(6*time.Hour))
		staffID := createTestStaffForWar(t, pool, "cancel_staff@staff.war.test", "Cancel Staff", "staff", 1)
		claims := &auth.Claims{UserID: staffID, Email: "cancel_staff@staff.war.test", IsStaff: true, DivisionID: 1}

		var slotID int64
		_ = pool.QueryRow(ctx, `
			INSERT INTO war_slots (donation_id, division_id, quota, remain)
			VALUES ($1, 1, 2, 2)
			RETURNING id
		`, donationID).Scan(&slotID)

		// Claim shift
		reqClaim := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/donations/%s/war-slots/%d/claim", donationID.String(), slotID), nil)
		reqClaim.SetPathValue("id", donationID.String())
		reqClaim.SetPathValue("slot_id", fmt.Sprintf("%d", slotID))
		reqClaim = reqClaim.WithContext(middleware.WithClaims(reqClaim.Context(), claims))
		recClaim := httptest.NewRecorder()
		h.ClaimStaffSlotHandler(recClaim, reqClaim)
		if recClaim.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created on claim, got %d", recClaim.Code)
		}

		// Immediate cancellation within 15m grace period -> no penalty!
		reqCancelGrace := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/donations/%s/war-slots/%d/cancel", donationID.String(), slotID), nil)
		reqCancelGrace.SetPathValue("id", donationID.String())
		reqCancelGrace.SetPathValue("slot_id", fmt.Sprintf("%d", slotID))
		reqCancelGrace = reqCancelGrace.WithContext(middleware.WithClaims(reqCancelGrace.Context(), claims))
		recCancelGrace := httptest.NewRecorder()
		h.CancelStaffSlotHandler(recCancelGrace, reqCancelGrace)

		if recCancelGrace.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on cancel, got %d: %s", recCancelGrace.Code, recCancelGrace.Body.String())
		}
		var graceResp map[string]any
		_ = json.Unmarshal(recCancelGrace.Body.Bytes(), &graceResp)
		if graceResp["data"].(map[string]any)["cancelled_late"] != false {
			t.Errorf("expected cancelled_late=false within grace period")
		}

		// Re-claim shift
		recClaim2 := httptest.NewRecorder()
		h.ClaimStaffSlotHandler(recClaim2, reqClaim)
		if recClaim2.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created on re-claim, got %d", recClaim2.Code)
		}

		// Backdate attendance to 30 minutes ago (outside grace period)
		_, err := pool.Exec(ctx, "UPDATE staff_attendances SET created_at = NOW() - INTERVAL '30 minutes' WHERE war_slot_id = $1 AND staff_id = $2", slotID, staffID)
		if err != nil {
			t.Fatalf("failed to backdate attendance: %v", err)
		}

		// Cancel shift now -> Late cancellation triggers penalty!
		recCancelLate := httptest.NewRecorder()
		h.CancelStaffSlotHandler(recCancelLate, reqCancelGrace)

		if recCancelLate.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on late cancel, got %d: %s", recCancelLate.Code, recCancelLate.Body.String())
		}
		var lateResp map[string]any
		_ = json.Unmarshal(recCancelLate.Body.Bytes(), &lateResp)
		if lateResp["data"].(map[string]any)["cancelled_late"] != true {
			t.Errorf("expected cancelled_late=true for late cancel")
		}

		// Verify user_bans record created with scope 'staff_war'
		var banCount int
		_ = pool.QueryRow(ctx, "SELECT COUNT(*) FROM user_bans WHERE user_id = $1 AND scope = 'staff_war' AND lifted_at IS NULL", staffID).Scan(&banCount)
		if banCount != 1 {
			t.Fatalf("expected 1 active staff ban, got %d", banCount)
		}

		// Attempt to claim next event -> 403 Forbidden
		nextDonationID := createStaffDonation("open", time.Now().Add(48*time.Hour))
		var nextSlotID int64
		_ = pool.QueryRow(ctx, `
			INSERT INTO war_slots (donation_id, division_id, quota, remain)
			VALUES ($1, 1, 2, 2)
			RETURNING id
		`, nextDonationID).Scan(&nextSlotID)

		reqNext := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/donations/%s/war-slots/%d/claim", nextDonationID.String(), nextSlotID), nil)
		reqNext.SetPathValue("id", nextDonationID.String())
		reqNext.SetPathValue("slot_id", fmt.Sprintf("%d", nextSlotID))
		reqNext = reqNext.WithContext(middleware.WithClaims(reqNext.Context(), claims))
		recNext := httptest.NewRecorder()
		h.ClaimStaffSlotHandler(recNext, reqNext)

		if recNext.Code != http.StatusForbidden {
			t.Fatalf("expected 403 Forbidden while banned, got %d: %s", recNext.Code, recNext.Body.String())
		}
		if !strings.Contains(recNext.Body.String(), "WAR_002") {
			t.Fatalf("expected WAR_002 error code while banned, got %s", recNext.Body.String())
		}

		// Create 1 completed event with active slot for Division 1 after the late cancelled event
		compDonationID := createStaffDonation("completed", time.Now().Add(12*time.Hour))
		_, _ = pool.Exec(ctx, "INSERT INTO war_slots (donation_id, division_id, quota, remain) VALUES ($1, 1, 2, 2)", compDonationID)

		// Now ban is dynamically served! Staff can claim the next donation shift
		recPostBan := httptest.NewRecorder()
		h.ClaimStaffSlotHandler(recPostBan, reqNext)

		if recPostBan.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created after staff ban served, got %d: %s", recPostBan.Code, recPostBan.Body.String())
		}
	})

	t.Run("4. Staff Self Check-in with guards", func(t *testing.T) {
		// Event starts in 30 minutes (within event_at - 1h window)
		donationID := createStaffDonation("open", time.Now().Add(30*time.Minute))
		staffID := createTestStaffForWar(t, pool, "checkin_staff@staff.war.test", "CheckIn Staff", "staff", 1)
		claims := &auth.Claims{UserID: staffID, Email: "checkin_staff@staff.war.test", IsStaff: true, DivisionID: 1}

		var slotID int64
		_ = pool.QueryRow(ctx, `
			INSERT INTO war_slots (donation_id, division_id, quota, remain)
			VALUES ($1, 1, 2, 2)
			RETURNING id
		`, donationID).Scan(&slotID)

		// Claim shift
		reqClaim := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/donations/%s/war-slots/%d/claim", donationID.String(), slotID), nil)
		reqClaim.SetPathValue("id", donationID.String())
		reqClaim.SetPathValue("slot_id", fmt.Sprintf("%d", slotID))
		reqClaim = reqClaim.WithContext(middleware.WithClaims(reqClaim.Context(), claims))
		recClaim := httptest.NewRecorder()
		h.ClaimStaffSlotHandler(recClaim, reqClaim)
		if recClaim.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created on claim, got %d", recClaim.Code)
		}

		// Execute self check-in
		reqCheckIn := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/donations/%s/war-slots/%d/check-in", donationID.String(), slotID), nil)
		reqCheckIn.SetPathValue("id", donationID.String())
		reqCheckIn.SetPathValue("slot_id", fmt.Sprintf("%d", slotID))
		reqCheckIn = reqCheckIn.WithContext(middleware.WithClaims(reqCheckIn.Context(), claims))
		recCheckIn := httptest.NewRecorder()
		h.StaffSelfCheckInHandler(recCheckIn, reqCheckIn)

		if recCheckIn.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on self check-in, got %d: %s", recCheckIn.Code, recCheckIn.Body.String())
		}

		// Verify attendance status is attended and checkin_source is 'self'
		var status, checkinSource string
		var attendedAt *time.Time
		_ = pool.QueryRow(ctx, "SELECT status, checkin_source, attended_at FROM staff_attendances WHERE war_slot_id = $1 AND staff_id = $2", slotID, staffID).Scan(&status, &checkinSource, &attendedAt)
		if status != "attended" || checkinSource != "self" || attendedAt == nil {
			t.Errorf("expected attended with self source, got status=%s, source=%s, attended_at=%v", status, checkinSource, attendedAt)
		}

		// Second check-in is idempotent -> 200 OK
		recCheckInDup := httptest.NewRecorder()
		h.StaffSelfCheckInHandler(recCheckInDup, reqCheckIn)
		if recCheckInDup.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on already checked-in, got %d", recCheckInDup.Code)
		}

		// Self check-in rejected if marked 'absent' by coordinator
		staffAbsent := createTestStaffForWar(t, pool, "absent_staff@staff.war.test", "Absent Staff", "staff", 1)
		claimsAbsent := &auth.Claims{UserID: staffAbsent, Email: "absent_staff@staff.war.test", IsStaff: true, DivisionID: 1}
		// Insert attendance directly as 'absent'
		_, _ = pool.Exec(ctx, "INSERT INTO staff_attendances (war_slot_id, staff_id, status) VALUES ($1, $2, 'absent')", slotID, staffAbsent)

		reqCheckInAbsent := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/donations/%s/war-slots/%d/check-in", donationID.String(), slotID), nil)
		reqCheckInAbsent.SetPathValue("id", donationID.String())
		reqCheckInAbsent.SetPathValue("slot_id", fmt.Sprintf("%d", slotID))
		reqCheckInAbsent = reqCheckInAbsent.WithContext(middleware.WithClaims(reqCheckInAbsent.Context(), claimsAbsent))
		recCheckInAbsent := httptest.NewRecorder()
		h.StaffSelfCheckInHandler(recCheckInAbsent, reqCheckInAbsent)

		if recCheckInAbsent.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request when marked absent, got %d: %s", recCheckInAbsent.Code, recCheckInAbsent.Body.String())
		}
	})

	t.Run("5. Staff cancel shift on cancelled donation skips late penalty", func(t *testing.T) {
		donationID := createStaffDonation("open", time.Now().Add(2*time.Hour))
		var slotID int64
		_ = pool.QueryRow(ctx, `
			INSERT INTO war_slots (donation_id, division_id, quota, remain)
			VALUES ($1, 1, 2, 2)
			RETURNING id
		`, donationID).Scan(&slotID)

		staffID := createTestStaffForWar(t, pool, "cancel_shift_staff@staff.war.test", "Cancel Shift Staff", "staff", 1)
		claims := &auth.Claims{UserID: staffID, Email: "cancel_shift_staff@staff.war.test", IsStaff: true, DivisionID: 1, Role: "staff"}

		// Claim shift
		reqClaim := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/donations/%s/war-slots/%d/claim", donationID.String(), slotID), nil)
		reqClaim.SetPathValue("id", donationID.String())
		reqClaim.SetPathValue("slot_id", fmt.Sprintf("%d", slotID))
		reqClaim = reqClaim.WithContext(middleware.WithClaims(reqClaim.Context(), claims))
		recClaim := httptest.NewRecorder()
		h.ClaimStaffSlotHandler(recClaim, reqClaim)
		if recClaim.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created on claim, got %d", recClaim.Code)
		}

		// Backdate attendance to 30m ago (outside 15m grace)
		_, _ = pool.Exec(ctx, "UPDATE staff_attendances SET created_at = NOW() - INTERVAL '30 minutes' WHERE war_slot_id = $1 AND staff_id = $2", slotID, staffID)

		// Staff cancels donation
		_, _ = pool.Exec(ctx, "UPDATE donations SET status = 'cancelled' WHERE id = $1", donationID)

		// Staff cancels shift
		reqCancel := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/donations/%s/war-slots/%d/cancel", donationID.String(), slotID), nil)
		reqCancel.SetPathValue("id", donationID.String())
		reqCancel.SetPathValue("slot_id", fmt.Sprintf("%d", slotID))
		reqCancel = reqCancel.WithContext(middleware.WithClaims(reqCancel.Context(), claims))
		recCancel := httptest.NewRecorder()
		h.CancelStaffSlotHandler(recCancel, reqCancel)

		if recCancel.Code != http.StatusOK {
			t.Fatalf("expected 200 OK cancelling shift on cancelled donation, got %d: %s", recCancel.Code, recCancel.Body.String())
		}

		var resp map[string]any
		_ = json.Unmarshal(recCancel.Body.Bytes(), &resp)
		data := resp["data"].(map[string]any)
		if data["cancelled_late"] != false {
			t.Errorf("expected cancelled_late=false on cancelled donation, got %v", data["cancelled_late"])
		}

		var banCount int
		_ = pool.QueryRow(ctx, "SELECT COUNT(*) FROM user_bans WHERE user_id = $1 AND scope = 'staff_war' AND lifted_at IS NULL", staffID).Scan(&banCount)
		if banCount != 0 {
			t.Errorf("expected 0 user_bans created, got %d", banCount)
		}
	})

	t.Run("6. Staff anti-churn grace period exhaustion on repeated cancellation", func(t *testing.T) {
		donationID := createStaffDonation("open", time.Now().Add(2*time.Hour))
		var slotID int64
		_ = pool.QueryRow(ctx, `
			INSERT INTO war_slots (donation_id, division_id, quota, remain)
			VALUES ($1, 1, 2, 2)
			RETURNING id
		`, donationID).Scan(&slotID)

		staffID := createTestStaffForWar(t, pool, "churn_staff@staff.war.test", "Churn Staff", "staff", 1)
		claims := &auth.Claims{UserID: staffID, Email: "churn_staff@staff.war.test", IsStaff: true, DivisionID: 1, Role: "staff"}

		// First claim
		reqClaim1 := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/donations/%s/war-slots/%d/claim", donationID.String(), slotID), nil)
		reqClaim1.SetPathValue("id", donationID.String())
		reqClaim1.SetPathValue("slot_id", fmt.Sprintf("%d", slotID))
		reqClaim1 = reqClaim1.WithContext(middleware.WithClaims(reqClaim1.Context(), claims))
		recClaim1 := httptest.NewRecorder()
		h.ClaimStaffSlotHandler(recClaim1, reqClaim1)
		if recClaim1.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created on first claim, got %d", recClaim1.Code)
		}

		// First cancellation: within 15m grace period -> penalty waived (cancelled_late=false)
		reqCancel1 := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/donations/%s/war-slots/%d/cancel", donationID.String(), slotID), nil)
		reqCancel1.SetPathValue("id", donationID.String())
		reqCancel1.SetPathValue("slot_id", fmt.Sprintf("%d", slotID))
		reqCancel1 = reqCancel1.WithContext(middleware.WithClaims(reqCancel1.Context(), claims))
		recCancel1 := httptest.NewRecorder()
		h.CancelStaffSlotHandler(recCancel1, reqCancel1)
		if recCancel1.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on first cancel, got %d", recCancel1.Code)
		}
		var resp1 map[string]any
		_ = json.Unmarshal(recCancel1.Body.Bytes(), &resp1)
		data1 := resp1["data"].(map[string]any)
		if data1["cancelled_late"].(bool) != false {
			t.Fatalf("expected cancelled_late=false on first cancel within grace period")
		}

		// Re-claim shift
		reqClaim2 := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/donations/%s/war-slots/%d/claim", donationID.String(), slotID), nil)
		reqClaim2.SetPathValue("id", donationID.String())
		reqClaim2.SetPathValue("slot_id", fmt.Sprintf("%d", slotID))
		reqClaim2 = reqClaim2.WithContext(middleware.WithClaims(reqClaim2.Context(), claims))
		recClaim2 := httptest.NewRecorder()
		h.ClaimStaffSlotHandler(recClaim2, reqClaim2)
		if recClaim2.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created on re-claim, got %d", recClaim2.Code)
		}

		// Second cancellation: grace period is WAIVED -> cancelled_late=true, ban created!
		reqCancel2 := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/donations/%s/war-slots/%d/cancel", donationID.String(), slotID), nil)
		reqCancel2.SetPathValue("id", donationID.String())
		reqCancel2.SetPathValue("slot_id", fmt.Sprintf("%d", slotID))
		reqCancel2 = reqCancel2.WithContext(middleware.WithClaims(reqCancel2.Context(), claims))
		recCancel2 := httptest.NewRecorder()
		h.CancelStaffSlotHandler(recCancel2, reqCancel2)
		if recCancel2.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on second cancel, got %d", recCancel2.Code)
		}
		var resp2 map[string]any
		_ = json.Unmarshal(recCancel2.Body.Bytes(), &resp2)
		data2 := resp2["data"].(map[string]any)
		if data2["cancelled_late"].(bool) != true {
			t.Fatalf("expected cancelled_late=true on re-cancel due to anti-churn grace waiver")
		}

		var bans int
		_ = pool.QueryRow(ctx, "SELECT COUNT(*) FROM user_bans WHERE user_id = $1 AND donation_id = $2 AND lifted_at IS NULL", staffID, donationID).Scan(&bans)
		if bans != 1 {
			t.Errorf("expected 1 active ban created on second churn cancellation, got %d", bans)
		}
	})

	t.Run("7. Custom StaffCancelGracePeriod and StaffSelfCheckInBefore/After config overrides", func(t *testing.T) {
		customCfg := cfg
		customCfg.War.StaffCancelGracePeriod = 30 * time.Minute
		customCfg.War.StaffSelfCheckInBefore = 48 * time.Hour
		customCfg.War.StaffSelfCheckInAfter = 6 * time.Hour
		hCustom := NewStaffWarHandler(pool, &customCfg)

		// Part A: StaffCancelGracePeriod=30m waives penalty when cancelled 20m after claiming
		donGrace := createStaffDonation("open", time.Now().Add(6*time.Hour))
		var graceSlotID int64
		_ = pool.QueryRow(ctx, "INSERT INTO war_slots (donation_id, division_id, quota, remain) VALUES ($1, 1, 2, 2) RETURNING id", donGrace).Scan(&graceSlotID)
		staffGrace := createTestStaffForWar(t, pool, "grace_override@staff.war.test", "Grace Override", "staff", 1)
		claimsGrace := &auth.Claims{UserID: staffGrace, Email: "grace_override@staff.war.test", IsStaff: true, DivisionID: 1}

		reqClaim := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/donations/%s/war-slots/%d/claim", donGrace.String(), graceSlotID), nil)
		reqClaim.SetPathValue("id", donGrace.String())
		reqClaim.SetPathValue("slot_id", fmt.Sprintf("%d", graceSlotID))
		reqClaim = reqClaim.WithContext(middleware.WithClaims(reqClaim.Context(), claimsGrace))
		recClaim := httptest.NewRecorder()
		hCustom.ClaimStaffSlotHandler(recClaim, reqClaim)
		if recClaim.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created on claim, got %d", recClaim.Code)
		}

		_, err := pool.Exec(ctx, "UPDATE staff_attendances SET created_at = NOW() - INTERVAL '20 minutes' WHERE war_slot_id = $1 AND staff_id = $2", graceSlotID, staffGrace)
		if err != nil {
			t.Fatalf("failed to backdate staff attendance: %v", err)
		}

		reqCancel := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/donations/%s/war-slots/%d/cancel", donGrace.String(), graceSlotID), nil)
		reqCancel.SetPathValue("id", donGrace.String())
		reqCancel.SetPathValue("slot_id", fmt.Sprintf("%d", graceSlotID))
		reqCancel = reqCancel.WithContext(middleware.WithClaims(reqCancel.Context(), claimsGrace))
		recCancel := httptest.NewRecorder()
		hCustom.CancelStaffSlotHandler(recCancel, reqCancel)
		if recCancel.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on cancel, got %d: %s", recCancel.Code, recCancel.Body.String())
		}
		var cancelResp map[string]any
		_ = json.Unmarshal(recCancel.Body.Bytes(), &cancelResp)
		if cancelResp["data"].(map[string]any)["cancelled_late"] != false {
			t.Errorf("expected cancelled_late=false with 30m grace period override at 20m, got %v", cancelResp["data"])
		}

		// Part B1: StaffSelfCheckInBefore=48h allows check-in at T-24h (outside default 1h)
		donEarly := createStaffDonation("open", time.Now().Add(24*time.Hour))
		var slotEarly int64
		_ = pool.QueryRow(ctx, "INSERT INTO war_slots (donation_id, division_id, quota, remain) VALUES ($1, 1, 2, 1) RETURNING id", donEarly).Scan(&slotEarly)
		staffEarly := createTestStaffForWar(t, pool, "early_checkin@staff.war.test", "Early CheckIn", "staff", 1)
		claimsEarly := &auth.Claims{UserID: staffEarly, Email: "early_checkin@staff.war.test", IsStaff: true, DivisionID: 1}
		_, _ = pool.Exec(ctx, "INSERT INTO staff_attendances (war_slot_id, staff_id, status) VALUES ($1, $2, 'registered')", slotEarly, staffEarly)

		reqEarly := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/donations/%s/war-slots/%d/check-in", donEarly.String(), slotEarly), nil)
		reqEarly.SetPathValue("id", donEarly.String())
		reqEarly.SetPathValue("slot_id", fmt.Sprintf("%d", slotEarly))
		reqEarly = reqEarly.WithContext(middleware.WithClaims(reqEarly.Context(), claimsEarly))
		recEarly := httptest.NewRecorder()
		hCustom.StaffSelfCheckInHandler(recEarly, reqEarly)
		if recEarly.Code != http.StatusOK {
			t.Fatalf("expected 200 OK at T-24h with 48h self check-in override, got %d: %s", recEarly.Code, recEarly.Body.String())
		}

		// Part B2: StaffSelfCheckInAfter=6h allows check-in at T+5h (outside default 4h)
		donAfter := createStaffDonation("open", time.Now().Add(-5*time.Hour))
		var slotAfter int64
		_ = pool.QueryRow(ctx, "INSERT INTO war_slots (donation_id, division_id, quota, remain) VALUES ($1, 1, 2, 1) RETURNING id", donAfter).Scan(&slotAfter)
		_, _ = pool.Exec(ctx, "INSERT INTO staff_attendances (war_slot_id, staff_id, status) VALUES ($1, $2, 'registered')", slotAfter, staffEarly)

		reqAfter := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/donations/%s/war-slots/%d/check-in", donAfter.String(), slotAfter), nil)
		reqAfter.SetPathValue("id", donAfter.String())
		reqAfter.SetPathValue("slot_id", fmt.Sprintf("%d", slotAfter))
		reqAfter = reqAfter.WithContext(middleware.WithClaims(reqAfter.Context(), claimsEarly))
		recAfter := httptest.NewRecorder()
		hCustom.StaffSelfCheckInHandler(recAfter, reqAfter)
		if recAfter.Code != http.StatusOK {
			t.Fatalf("expected 200 OK at T+5h with 6h self check-in after override, got %d: %s", recAfter.Code, recAfter.Body.String())
		}

		// Part B3: Outside custom 48h..6h window (T-60h) rejects and formats configured bounds dynamically
		donTooEarly := createStaffDonation("open", time.Now().Add(60*time.Hour))
		var slotTooEarly int64
		_ = pool.QueryRow(ctx, "INSERT INTO war_slots (donation_id, division_id, quota, remain) VALUES ($1, 1, 2, 1) RETURNING id", donTooEarly).Scan(&slotTooEarly)
		_, _ = pool.Exec(ctx, "INSERT INTO staff_attendances (war_slot_id, staff_id, status) VALUES ($1, $2, 'registered')", slotTooEarly, staffEarly)

		reqTooEarly := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/donations/%s/war-slots/%d/check-in", donTooEarly.String(), slotTooEarly), nil)
		reqTooEarly.SetPathValue("id", donTooEarly.String())
		reqTooEarly.SetPathValue("slot_id", fmt.Sprintf("%d", slotTooEarly))
		reqTooEarly = reqTooEarly.WithContext(middleware.WithClaims(reqTooEarly.Context(), claimsEarly))
		recTooEarly := httptest.NewRecorder()
		hCustom.StaffSelfCheckInHandler(recTooEarly, reqTooEarly)
		if recTooEarly.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request at T-60h, got %d: %s", recTooEarly.Code, recTooEarly.Body.String())
		}
		if !strings.Contains(recTooEarly.Body.String(), "48 hours before and 6 hours after") {
			t.Errorf("expected dynamic window message in response, got %s", recTooEarly.Body.String())
		}
	})

	t.Run("8. UpdateWarSlotHandler single-slot quota updates and guards", func(t *testing.T) {
		donationID := createStaffDonation("open", time.Now().Add(24*time.Hour))
		otherDonationID := createStaffDonation("open", time.Now().Add(24*time.Hour))
		opStaffID := createTestStaffForWar(t, pool, "op_patch@staff.war.test", "Op Patch", "staff", 3)
		claimsOp := &auth.Claims{UserID: opStaffID, Email: "op_patch@staff.war.test", IsStaff: true, DivisionID: 3, Role: "staff"}

		// Slot with quota=3, remain=1 (2 active claims)
		var slotID int64
		if err := pool.QueryRow(ctx, `
			INSERT INTO war_slots (donation_id, division_id, quota, remain)
			VALUES ($1, 1, 3, 1)
			RETURNING id
		`, donationID).Scan(&slotID); err != nil {
			t.Fatalf("failed to insert war slot: %v", err)
		}
		staffA := createTestStaffForWar(t, pool, "patch_a@staff.war.test", "Patch A", "staff", 1)
		staffB := createTestStaffForWar(t, pool, "patch_b@staff.war.test", "Patch B", "staff", 1)
		_, _ = pool.Exec(ctx, "INSERT INTO staff_attendances (war_slot_id, staff_id, status) VALUES ($1, $2, 'registered'), ($1, $3, 'registered')", slotID, staffA, staffB)

		doPatch := func(donID uuid.UUID, sID string, payload any) *httptest.ResponseRecorder {
			body, _ := json.Marshal(payload)
			req := httptest.NewRequest(http.MethodPatch, fmt.Sprintf("/api/v1/donations/%s/war-slots/%s", donID.String(), sID), bytes.NewReader(body))
			req.SetPathValue("id", donID.String())
			req.SetPathValue("slot_id", sID)
			req = req.WithContext(middleware.WithClaims(req.Context(), claimsOp))
			rec := httptest.NewRecorder()
			h.UpdateWarSlotHandler(rec, req)
			return rec
		}

		// Happy path: increase quota 3 -> 5 => remain becomes 5 - 2 = 3
		recUp := doPatch(donationID, fmt.Sprintf("%d", slotID), UpdateWarSlotRequest{Quota: 5})
		if recUp.Code != http.StatusOK {
			t.Fatalf("expected 200 OK updating quota to 5, got %d: %s", recUp.Code, recUp.Body.String())
		}
		var upResp map[string]any
		_ = json.Unmarshal(recUp.Body.Bytes(), &upResp)
		upData := upResp["data"].(map[string]any)
		if upData["quota"].(float64) != 5 || upData["remain"].(float64) != 3 || upData["active_claims"].(float64) != 2 {
			t.Errorf("unexpected updated slot metrics: quota=%v remain=%v active_claims=%v", upData["quota"], upData["remain"], upData["active_claims"])
		}
		if upData["division_name"] == "" {
			t.Errorf("expected division_name in response, got empty")
		}

		// Happy path boundary: reduce quota to exact active_claims (2) => remain=0
		recExact := doPatch(donationID, fmt.Sprintf("%d", slotID), UpdateWarSlotRequest{Quota: 2})
		if recExact.Code != http.StatusOK {
			t.Fatalf("expected 200 OK reducing quota to exact active_claims, got %d: %s", recExact.Code, recExact.Body.String())
		}
		var exactResp map[string]any
		_ = json.Unmarshal(recExact.Body.Bytes(), &exactResp)
		exactData := exactResp["data"].(map[string]any)
		if exactData["quota"].(float64) != 2 || exactData["remain"].(float64) != 0 {
			t.Errorf("expected quota=2, remain=0, got quota=%v remain=%v", exactData["quota"], exactData["remain"])
		}

		// 409 WAR_003: reduce quota below active_claims (try quota=1 when active_claims=2)
		recConflict := doPatch(donationID, fmt.Sprintf("%d", slotID), UpdateWarSlotRequest{Quota: 1})
		if recConflict.Code != http.StatusConflict {
			t.Fatalf("expected 409 Conflict when quota < active_claims, got %d: %s", recConflict.Code, recConflict.Body.String())
		}
		if !strings.Contains(recConflict.Body.String(), "WAR_003") {
			t.Errorf("expected WAR_003 code, got %s", recConflict.Body.String())
		}

		// 400 VAL_001: quota <= 0
		for _, invalidQuota := range []int32{0, -3} {
			recZero := doPatch(donationID, fmt.Sprintf("%d", slotID), UpdateWarSlotRequest{Quota: invalidQuota})
			if recZero.Code != http.StatusBadRequest {
				t.Fatalf("expected 400 Bad Request for quota=%d, got %d: %s", invalidQuota, recZero.Code, recZero.Body.String())
			}
			if !strings.Contains(recZero.Body.String(), "VAL_001") {
				t.Errorf("expected VAL_001 for quota=%d, got %s", invalidQuota, recZero.Body.String())
			}
		}

		// 404 NotFound: cross-donation and non-existent slot ID
		recCross := doPatch(otherDonationID, fmt.Sprintf("%d", slotID), UpdateWarSlotRequest{Quota: 4})
		if recCross.Code != http.StatusNotFound {
			t.Fatalf("expected 404 Not Found on cross-donation PATCH, got %d: %s", recCross.Code, recCross.Body.String())
		}
		recMissing := doPatch(donationID, "999999999", UpdateWarSlotRequest{Quota: 4})
		if recMissing.Code != http.StatusNotFound {
			t.Fatalf("expected 404 Not Found on non-existent slot PATCH, got %d: %s", recMissing.Code, recMissing.Body.String())
		}

		// 400 VAL_002: closed, completed, cancelled donations
		for _, st := range []string{"closed", "completed", "cancelled"} {
			_, _ = pool.Exec(ctx, "UPDATE donations SET status = $2 WHERE id = $1", donationID, st)
			recStatus := doPatch(donationID, fmt.Sprintf("%d", slotID), UpdateWarSlotRequest{Quota: 4})
			if recStatus.Code != http.StatusBadRequest {
				t.Fatalf("expected 400 Bad Request for donation status %q, got %d: %s", st, recStatus.Code, recStatus.Body.String())
			}
		}
	})

	t.Run("9. DeleteWarSlotHandler soft deletion, guards, and POST re-creation", func(t *testing.T) {
		donationID := createStaffDonation("open", time.Now().Add(24*time.Hour))
		otherDonationID := createStaffDonation("open", time.Now().Add(24*time.Hour))
		opStaffID := createTestStaffForWar(t, pool, "op_delete@staff.war.test", "Op Delete", "staff", 3)
		claimsOp := &auth.Claims{UserID: opStaffID, Email: "op_delete@staff.war.test", IsStaff: true, DivisionID: 3, Role: "staff"}

		// Create Division 1 slot with 1 active claim and Division 2 slot with 1 cancelled historical claim
		var slotWithActive int64
		if err := pool.QueryRow(ctx, `
			INSERT INTO war_slots (donation_id, division_id, quota, remain)
			VALUES ($1, 1, 2, 1)
			RETURNING id
		`, donationID).Scan(&slotWithActive); err != nil {
			t.Fatalf("failed to insert slotWithActive: %v", err)
		}
		staffActive := createTestStaffForWar(t, pool, "del_active@staff.war.test", "Del Active", "staff", 1)
		_, _ = pool.Exec(ctx, "INSERT INTO staff_attendances (war_slot_id, staff_id, status) VALUES ($1, $2, 'registered')", slotWithActive, staffActive)

		var slotClean int64
		if err := pool.QueryRow(ctx, `
			INSERT INTO war_slots (donation_id, division_id, quota, remain)
			VALUES ($1, 2, 2, 2)
			RETURNING id
		`, donationID).Scan(&slotClean); err != nil {
			t.Fatalf("failed to insert slotClean: %v", err)
		}
		staffCancelled := createTestStaffForWar(t, pool, "del_cancelled@staff.war.test", "Del Cancelled", "staff", 2)
		_, _ = pool.Exec(ctx, "INSERT INTO staff_attendances (war_slot_id, staff_id, status, cancelled_at) VALUES ($1, $2, 'cancelled', NOW())", slotClean, staffCancelled)

		doDelete := func(donID uuid.UUID, sID int64) *httptest.ResponseRecorder {
			req := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/v1/donations/%s/war-slots/%d", donID.String(), sID), nil)
			req.SetPathValue("id", donID.String())
			req.SetPathValue("slot_id", fmt.Sprintf("%d", sID))
			req = req.WithContext(middleware.WithClaims(req.Context(), claimsOp))
			rec := httptest.NewRecorder()
			h.DeleteWarSlotHandler(rec, req)
			return rec
		}

		// 409 WAR_004: slot with active staff reservations cannot be deleted
		recActive := doDelete(donationID, slotWithActive)
		if recActive.Code != http.StatusConflict {
			t.Fatalf("expected 409 Conflict deleting slot with active claim, got %d: %s", recActive.Code, recActive.Body.String())
		}
		if !strings.Contains(recActive.Body.String(), "WAR_004") {
			t.Errorf("expected WAR_004 code, got %s", recActive.Body.String())
		}

		// 404 NotFound: cross-donation and non-existent slot
		recCross := doDelete(otherDonationID, slotClean)
		if recCross.Code != http.StatusNotFound {
			t.Fatalf("expected 404 Not Found on cross-donation DELETE, got %d: %s", recCross.Code, recCross.Body.String())
		}
		recMissing := doDelete(donationID, 999999999)
		if recMissing.Code != http.StatusNotFound {
			t.Fatalf("expected 404 Not Found on non-existent slot DELETE, got %d: %s", recMissing.Code, recMissing.Body.String())
		}

		// Happy path: slot with only cancelled historical staff_attendances succeeds (soft delete)
		recDel := doDelete(donationID, slotClean)
		if recDel.Code != http.StatusOK {
			t.Fatalf("expected 200 OK soft-deleting clean slot, got %d: %s", recDel.Code, recDel.Body.String())
		}
		var delResp map[string]any
		_ = json.Unmarshal(recDel.Body.Bytes(), &delResp)
		if delResp["data"].(map[string]any)["id"].(float64) != float64(slotClean) {
			t.Errorf("expected deleted slot id %d in response, got %v", slotClean, delResp["data"])
		}

		// Verify deleted_at is set and cancelled attendance row is preserved
		var isDeleted bool
		_ = pool.QueryRow(ctx, "SELECT deleted_at IS NOT NULL FROM war_slots WHERE id = $1", slotClean).Scan(&isDeleted)
		if !isDeleted {
			t.Errorf("expected war_slot %d to have deleted_at set", slotClean)
		}
		var histCount int
		_ = pool.QueryRow(ctx, "SELECT COUNT(*) FROM staff_attendances WHERE war_slot_id = $1", slotClean).Scan(&histCount)
		if histCount != 1 {
			t.Errorf("expected historical cancelled attendance row preserved, got count=%d", histCount)
		}

		// Re-deleting soft-deleted slot -> 404 NotFound
		recDelAgain := doDelete(donationID, slotClean)
		if recDelAgain.Code != http.StatusNotFound {
			t.Fatalf("expected 404 Not Found re-deleting soft-deleted slot, got %d: %s", recDelAgain.Code, recDelAgain.Body.String())
		}

		// Re-create slot for Division 2 via POST upsert -> succeeds with a new slot ID
		recreateBytes, _ := json.Marshal([]ConfigureWarSlotItem{{DivisionID: 2, Quota: 4}})
		reqPost := httptest.NewRequest(http.MethodPost, "/api/v1/donations/"+donationID.String()+"/war-slots", bytes.NewReader(recreateBytes))
		reqPost.SetPathValue("id", donationID.String())
		reqPost = reqPost.WithContext(middleware.WithClaims(reqPost.Context(), claimsOp))
		recPost := httptest.NewRecorder()
		h.ConfigureWarSlotsHandler(recPost, reqPost)
		if recPost.Code != http.StatusOK {
			t.Fatalf("expected 200 OK re-creating division 2 slot via POST, got %d: %s", recPost.Code, recPost.Body.String())
		}

		var newSlotID int64
		if err := pool.QueryRow(ctx, "SELECT id FROM war_slots WHERE donation_id = $1 AND division_id = 2 AND deleted_at IS NULL", donationID).Scan(&newSlotID); err != nil {
			t.Fatalf("failed to find newly re-created slot: %v", err)
		}
		if newSlotID == slotClean {
			t.Errorf("expected re-created slot to have a new ID distinct from soft-deleted ID %d", slotClean)
		}

		// 400 VAL_002: closed/completed/cancelled donation rejects DELETE
		_, _ = pool.Exec(ctx, "UPDATE donations SET status = 'closed' WHERE id = $1", donationID)
		recClosed := doDelete(donationID, newSlotID)
		if recClosed.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request deleting slot on closed donation, got %d: %s", recClosed.Code, recClosed.Body.String())
		}
	})

	t.Run("10. Reject CancelStaffSlot after self check-in attended (400 VAL_002)", func(t *testing.T) {
		donationID := createStaffDonation("open", time.Now().Add(30*time.Minute))
		staffID := createTestStaffForWar(t, pool, "cancel_attended@staff.war.test", "Cancel Attended Staff", "staff", 1)
		claims := &auth.Claims{UserID: staffID, Email: "cancel_attended@staff.war.test", IsStaff: true, DivisionID: 1}

		var slotID int64
		_ = pool.QueryRow(ctx, "INSERT INTO war_slots (donation_id, division_id, quota, remain) VALUES ($1, 1, 2, 2) RETURNING id", donationID).Scan(&slotID)

		// Claim
		reqClaim := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/donations/%s/war-slots/%d/claim", donationID.String(), slotID), nil)
		reqClaim.SetPathValue("id", donationID.String())
		reqClaim.SetPathValue("slot_id", fmt.Sprintf("%d", slotID))
		reqClaim = reqClaim.WithContext(middleware.WithClaims(reqClaim.Context(), claims))
		recClaim := httptest.NewRecorder()
		h.ClaimStaffSlotHandler(recClaim, reqClaim)
		if recClaim.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created, got %d: %s", recClaim.Code, recClaim.Body.String())
		}

		// Self check-in within pre-event window -> attended
		reqCheckIn := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/donations/%s/war-slots/%d/check-in", donationID.String(), slotID), nil)
		reqCheckIn.SetPathValue("id", donationID.String())
		reqCheckIn.SetPathValue("slot_id", fmt.Sprintf("%d", slotID))
		reqCheckIn = reqCheckIn.WithContext(middleware.WithClaims(reqCheckIn.Context(), claims))
		recCheckIn := httptest.NewRecorder()
		h.StaffSelfCheckInHandler(recCheckIn, reqCheckIn)
		if recCheckIn.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on self check-in, got %d: %s", recCheckIn.Code, recCheckIn.Body.String())
		}

		// Attempt cancel after check-in -> 400 VAL_002 (no 500 check-constraint violation)
		reqCancel := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/donations/%s/war-slots/%d/cancel", donationID.String(), slotID), nil)
		reqCancel.SetPathValue("id", donationID.String())
		reqCancel.SetPathValue("slot_id", fmt.Sprintf("%d", slotID))
		reqCancel = reqCancel.WithContext(middleware.WithClaims(reqCancel.Context(), claims))
		recCancel := httptest.NewRecorder()
		h.CancelStaffSlotHandler(recCancel, reqCancel)

		if recCancel.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request cancelling attended shift, got %d: %s", recCancel.Code, recCancel.Body.String())
		}
		var errResp map[string]any
		_ = json.Unmarshal(recCancel.Body.Bytes(), &errResp)
		if errResp["code"] != "VAL_002" {
			t.Errorf("expected VAL_002 error code, got %v", errResp["code"])
		}

		var remain int32
		_ = pool.QueryRow(ctx, "SELECT remain FROM war_slots WHERE id = $1", slotID).Scan(&remain)
		if remain != 1 {
			t.Errorf("expected slot remain to stay 1, got %d", remain)
		}
		var status string
		_ = pool.QueryRow(ctx, "SELECT status FROM staff_attendances WHERE war_slot_id = $1 AND staff_id = $2 AND cancelled_at IS NULL", slotID, staffID).Scan(&status)
		if status != "attended" {
			t.Errorf("expected staff attendance status to stay 'attended', got %q", status)
		}
	})

	t.Run("11. StaffSelfCheckIn succeeds after claim -> cancel -> re-claim", func(t *testing.T) {
		donationID := createStaffDonation("open", time.Now().Add(30*time.Minute))
		staffID := createTestStaffForWar(t, pool, "reclaim_checkin@staff.war.test", "Reclaim CheckIn Staff", "staff", 1)
		claims := &auth.Claims{UserID: staffID, Email: "reclaim_checkin@staff.war.test", IsStaff: true, DivisionID: 1}

		var slotID int64
		_ = pool.QueryRow(ctx, "INSERT INTO war_slots (donation_id, division_id, quota, remain) VALUES ($1, 1, 2, 2) RETURNING id", donationID).Scan(&slotID)

		doPost := func(action string) *httptest.ResponseRecorder {
			req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/donations/%s/war-slots/%d/%s", donationID.String(), slotID, action), nil)
			req.SetPathValue("id", donationID.String())
			req.SetPathValue("slot_id", fmt.Sprintf("%d", slotID))
			req = req.WithContext(middleware.WithClaims(req.Context(), claims))
			rec := httptest.NewRecorder()
			switch action {
			case "claim":
				h.ClaimStaffSlotHandler(rec, req)
			case "cancel":
				h.CancelStaffSlotHandler(rec, req)
			case "check-in":
				h.StaffSelfCheckInHandler(rec, req)
			}
			return rec
		}

		if rec := doPost("claim"); rec.Code != http.StatusCreated {
			t.Fatalf("first claim failed: %d %s", rec.Code, rec.Body.String())
		}
		if rec := doPost("cancel"); rec.Code != http.StatusOK {
			t.Fatalf("cancel failed: %d %s", rec.Code, rec.Body.String())
		}
		if rec := doPost("claim"); rec.Code != http.StatusCreated {
			t.Fatalf("re-claim failed: %d %s", rec.Code, rec.Body.String())
		}

		recCheckIn := doPost("check-in")
		if recCheckIn.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on self check-in after re-claim, got %d: %s", recCheckIn.Code, recCheckIn.Body.String())
		}
		var checkInResp map[string]any
		_ = json.Unmarshal(recCheckIn.Body.Bytes(), &checkInResp)
		if checkInResp["data"].(map[string]any)["status"] != "attended" {
			t.Errorf("expected status='attended', got %v", checkInResp["data"])
		}
	})

	t.Run("12. Soft-deleted staff user is rejected with 403 on ClaimStaffSlotHandler", func(t *testing.T) {
		donationID := createStaffDonation("open", time.Now().Add(24*time.Hour))
		staffID := createTestStaffForWar(t, pool, "softdeleted_staff@staff.war.test", "SoftDeleted Staff", "staff", 1)
		claims := &auth.Claims{UserID: staffID, Email: "softdeleted_staff@staff.war.test", IsStaff: true, DivisionID: 1}

		var slotID int64
		if err := pool.QueryRow(ctx, "INSERT INTO war_slots (donation_id, division_id, quota, remain) VALUES ($1, 1, 2, 2) RETURNING id", donationID).Scan(&slotID); err != nil {
			t.Fatalf("failed to insert war slot: %v", err)
		}

		// Soft-delete the staff record ([SQLC-BUG-01] regression guard)
		if _, err := pool.Exec(ctx, "UPDATE staff SET deleted_at = NOW() WHERE user_id = $1", staffID); err != nil {
			t.Fatalf("failed to soft-delete staff record: %v", err)
		}

		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/donations/%s/war-slots/%d/claim", donationID.String(), slotID), nil)
		req.SetPathValue("id", donationID.String())
		req.SetPathValue("slot_id", fmt.Sprintf("%d", slotID))
		req = req.WithContext(middleware.WithClaims(req.Context(), claims))
		rec := httptest.NewRecorder()

		h.ClaimStaffSlotHandler(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 Forbidden for soft-deleted staff, got %d: %s", rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "Staff profile not found") {
			t.Errorf("expected message containing 'Staff profile not found', got %s", rec.Body.String())
		}
	})
}
