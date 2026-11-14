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

var testAttendancePhoneSeq atomic.Int64

func init() {
	testAttendancePhoneSeq.Store(time.Now().UnixNano() % 100000000)
}

func createTestUserForAttendance(t *testing.T, pool *pgxpool.Pool, email, name string) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	var userID uuid.UUID
	phone := fmt.Sprintf("6285%08d", testAttendancePhoneSeq.Add(1)%100000000)
	err := pool.QueryRow(ctx, `
		INSERT INTO users (email, name, phone_number, is_active, email_verified_at)
		VALUES ($1, $2, $3, true, NOW())
		RETURNING id
	`, email, name, phone).Scan(&userID)
	if err != nil {
		t.Fatalf("failed to insert test user: %v", err)
	}
	return userID
}

func createTestStaffForAttendance(t *testing.T, pool *pgxpool.Pool, email, name, role string, divisionID int32) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	userID := createTestUserForAttendance(t, pool, email, name)
	_, err := pool.Exec(ctx, `
		INSERT INTO staff (user_id, division_id, role)
		VALUES ($1, $2, $3)
	`, userID, divisionID, role)
	if err != nil {
		t.Fatalf("failed to insert test staff record: %v", err)
	}
	return userID
}

func TestAttendanceTrackingAndBans(t *testing.T) {
	pool := getTestPool(t)

	cfg := config.LoadConfig()
	cfg.War.HeroBanEventCount = 2
	cfg.War.StaffBanEventCount = 1
	h := NewAttendanceHandler(pool, &cfg)
	ctx := context.Background()

	// Cleanup test artifacts upfront and on finish
	cleanup := func() {
		for _, stmt := range []string{
			"DELETE FROM user_bans",
			"DELETE FROM staff_attendances",
			"DELETE FROM hero_attendances",
			"DELETE FROM war_slots",
			"DELETE FROM donation_target WHERE donation_id IN (SELECT id FROM donations WHERE address LIKE 'Jl. Malioboro%' OR address LIKE 'Jl. Kaliurang%')",
			"DELETE FROM donations WHERE address LIKE 'Jl. Malioboro%' OR address LIKE 'Jl. Kaliurang%'",
			"DELETE FROM hero_categories WHERE hero_id IN (SELECT id FROM users WHERE email LIKE '%@concurrency.test' OR email LIKE '%@hero.war.test' OR email LIKE '%@attendance.test' OR email LIKE '%@staff.war.test')",
			"DELETE FROM heroes WHERE user_id IN (SELECT id FROM users WHERE email LIKE '%@concurrency.test' OR email LIKE '%@hero.war.test' OR email LIKE '%@attendance.test' OR email LIKE '%@staff.war.test')",
			"DELETE FROM staff WHERE user_id IN (SELECT id FROM users WHERE email LIKE '%@concurrency.test' OR email LIKE '%@hero.war.test' OR email LIKE '%@attendance.test' OR email LIKE '%@staff.war.test')",
			"DELETE FROM users WHERE email LIKE '%@concurrency.test' OR email LIKE '%@hero.war.test' OR email LIKE '%@attendance.test' OR email LIKE '%@staff.war.test'",
		} {
			if _, err := pool.Exec(ctx, stmt); err != nil {
				fmt.Printf("[attendance_cleanup_err] %s -> %v\n", stmt, err)
			}
		}
	}
	cleanup()
	defer func() {
		cleanup()
		pool.Close()
	}()

	createDonationFixture := func(status string, eventAt time.Time) uuid.UUID {
		var sponsorID uuid.UUID
		_ = pool.QueryRow(ctx, `
			INSERT INTO sponsors (name, email, variant)
			VALUES ('Attendance Sponsor ' || gen_random_uuid(), 'attendancesponsor@example.com', 'individual')
			RETURNING id
		`).Scan(&sponsorID)

		var donationID uuid.UUID
		err := pool.QueryRow(ctx, `
			INSERT INTO donations (
				sponsor_id, event_at, quota, hero_quota, remain,
				address, status, hide_from_public, hide_sponsor
			) VALUES (
				$1, $2, 20, 10, 10,
				'Jl. Kaliurang Attendance Test', $3, FALSE, FALSE
			) RETURNING id
		`, sponsorID, eventAt, status).Scan(&donationID)
		if err != nil {
			t.Fatalf("failed to insert donation fixture: %v", err)
		}
		return donationID
	}

	t.Run("1. List Hero Attendances and unmarked filter", func(t *testing.T) {
		// Create past donation (event_at in past) to test unmarked
		pastDonationID := createDonationFixture("completed", time.Now().Add(-2*time.Hour))
		heroID := createTestUserForAttendance(t, pool, "hero_att1@attendance.test", "Hero Att 1")
		_, _ = pool.Exec(ctx, "INSERT INTO heroes (user_id) VALUES ($1)", heroID)

		// Create registered attendance
		_, err := pool.Exec(ctx, `
			INSERT INTO hero_attendances (donation_id, hero_id, status)
			VALUES ($1, $2, 'registered')
		`, pastDonationID, heroID)
		if err != nil {
			t.Fatalf("failed to insert hero attendance: %v", err)
		}

		opStaffID := createTestStaffForAttendance(t, pool, "op_att1@attendance.test", "Op Att 1", "staff", 3)
		claims := &auth.Claims{UserID: opStaffID, Email: "op_att1@attendance.test", IsStaff: true, DivisionID: 3, Role: "staff"}

		// List all attendances
		reqAll := httptest.NewRequest(http.MethodGet, "/api/v1/donations/"+pastDonationID.String()+"/attendances", nil)
		reqAll.SetPathValue("id", pastDonationID.String())
		reqAll = reqAll.WithContext(middleware.WithClaims(reqAll.Context(), claims))
		recAll := httptest.NewRecorder()
		h.ListHeroAttendancesHandler(recAll, reqAll)

		if recAll.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", recAll.Code, recAll.Body.String())
		}

		// List with status=unmarked filter (event_at < NOW() and status='registered')
		reqUnmarked := httptest.NewRequest(http.MethodGet, "/api/v1/donations/"+pastDonationID.String()+"/attendances?status=unmarked", nil)
		reqUnmarked.SetPathValue("id", pastDonationID.String())
		reqUnmarked = reqUnmarked.WithContext(middleware.WithClaims(reqUnmarked.Context(), claims))
		recUnmarked := httptest.NewRecorder()
		h.ListHeroAttendancesHandler(recUnmarked, reqUnmarked)

		if recUnmarked.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on unmarked filter, got %d: %s", recUnmarked.Code, recUnmarked.Body.String())
		}
		var unmarkedResp map[string]any
		_ = json.Unmarshal(recUnmarked.Body.Bytes(), &unmarkedResp)
		data := unmarkedResp["data"].([]any)
		if len(data) != 1 {
			t.Errorf("expected 1 unmarked attendance, got %d", len(data))
		}

		// Past CANCELLED donation with registered attendance must be EXCLUDED from unmarked
		cancelledDonationID := createDonationFixture("cancelled", time.Now().Add(-2*time.Hour))
		_, err = pool.Exec(ctx, `
			INSERT INTO hero_attendances (donation_id, hero_id, status)
			VALUES ($1, $2, 'registered')
		`, cancelledDonationID, heroID)
		if err != nil {
			t.Fatalf("failed to insert registered attendance on cancelled donation: %v", err)
		}

		reqCancelledUnmarked := httptest.NewRequest(http.MethodGet, "/api/v1/donations/"+cancelledDonationID.String()+"/attendances?status=unmarked", nil)
		reqCancelledUnmarked.SetPathValue("id", cancelledDonationID.String())
		reqCancelledUnmarked = reqCancelledUnmarked.WithContext(middleware.WithClaims(reqCancelledUnmarked.Context(), claims))
		recCancelledUnmarked := httptest.NewRecorder()
		h.ListHeroAttendancesHandler(recCancelledUnmarked, reqCancelledUnmarked)

		if recCancelledUnmarked.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", recCancelledUnmarked.Code, recCancelledUnmarked.Body.String())
		}
		var cancelledResp map[string]any
		_ = json.Unmarshal(recCancelledUnmarked.Body.Bytes(), &cancelledResp)
		cancelledData := cancelledResp["data"].([]any)
		if len(cancelledData) != 0 {
			t.Errorf("expected 0 unmarked attendances on cancelled donation, got %d", len(cancelledData))
		}
	})

	t.Run("2. Mark Hero Attendance: absent creates ban, attended lifts, upsert collision safety, pardon preserved", func(t *testing.T) {
		donationID := createDonationFixture("open", time.Now().Add(-30*time.Minute))
		heroID := createTestUserForAttendance(t, pool, "hero_ban@attendance.test", "Hero Ban")
		_, _ = pool.Exec(ctx, "INSERT INTO heroes (user_id) VALUES ($1)", heroID)

		var attID uuid.UUID
		err := pool.QueryRow(ctx, `
			INSERT INTO hero_attendances (donation_id, hero_id, status)
			VALUES ($1, $2, 'registered')
			RETURNING id
		`, donationID, heroID).Scan(&attID)
		if err != nil {
			t.Fatalf("failed to insert hero attendance: %v", err)
		}

		opStaffID := createTestStaffForAttendance(t, pool, "op_coord@attendance.test", "Op Coord", "staff", 3)
		claimsOp := &auth.Claims{UserID: opStaffID, Email: "op_coord@attendance.test", IsStaff: true, DivisionID: 3, Role: "staff"}

		// A. Mark absent -> Ban created in user_bans
		absentBody := MarkAttendanceRequest{Status: "absent", Notes: strPtr("No show on pickup")}
		absentBytes, _ := json.Marshal(absentBody)
		reqAbsent := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/donations/%s/attendances/%s", donationID.String(), attID.String()), bytes.NewReader(absentBytes))
		reqAbsent.SetPathValue("id", donationID.String())
		reqAbsent.SetPathValue("attendance_id", attID.String())
		reqAbsent = reqAbsent.WithContext(middleware.WithClaims(reqAbsent.Context(), claimsOp))
		recAbsent := httptest.NewRecorder()
		h.MarkHeroAttendanceHandler(recAbsent, reqAbsent)

		if recAbsent.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on mark absent, got %d: %s", recAbsent.Code, recAbsent.Body.String())
		}

		var banCount int
		_ = pool.QueryRow(ctx, "SELECT COUNT(*) FROM user_bans WHERE source_hero_attendance_id = $1 AND lifted_at IS NULL", attID).Scan(&banCount)
		if banCount != 1 {
			t.Fatalf("expected 1 active ban created on absent, got %d", banCount)
		}

		// B. Correct to attended -> Ban is auto-lifted!
		attendedBody := MarkAttendanceRequest{Status: "attended", Notes: strPtr("Corrected, was present")}
		attendedBytes, _ := json.Marshal(attendedBody)
		reqAttended := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/donations/%s/attendances/%s", donationID.String(), attID.String()), bytes.NewReader(attendedBytes))
		reqAttended.SetPathValue("id", donationID.String())
		reqAttended.SetPathValue("attendance_id", attID.String())
		reqAttended = reqAttended.WithContext(middleware.WithClaims(reqAttended.Context(), claimsOp))
		recAttended := httptest.NewRecorder()
		h.MarkHeroAttendanceHandler(recAttended, reqAttended)

		if recAttended.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on correct to attended, got %d: %s", recAttended.Code, recAttended.Body.String())
		}

		var activeBans int
		_ = pool.QueryRow(ctx, "SELECT COUNT(*) FROM user_bans WHERE source_hero_attendance_id = $1 AND lifted_at IS NULL", attID).Scan(&activeBans)
		if activeBans != 0 {
			t.Errorf("expected 0 active bans after auto-lift, got %d", activeBans)
		}

		// C. Flip back to absent -> Re-activates ban via UPSERT without collision!
		recAbsentAgain := httptest.NewRecorder()
		reqAbsentAgain := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/donations/%s/attendances/%s", donationID.String(), attID.String()), bytes.NewReader(absentBytes))
		reqAbsentAgain.SetPathValue("id", donationID.String())
		reqAbsentAgain.SetPathValue("attendance_id", attID.String())
		reqAbsentAgain = reqAbsentAgain.WithContext(middleware.WithClaims(reqAbsentAgain.Context(), claimsOp))
		h.MarkHeroAttendanceHandler(recAbsentAgain, reqAbsentAgain)

		if recAbsentAgain.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on re-absent upsert, got %d: %s", recAbsentAgain.Code, recAbsentAgain.Body.String())
		}

		_ = pool.QueryRow(ctx, "SELECT COUNT(*) FROM user_bans WHERE source_hero_attendance_id = $1 AND lifted_at IS NULL", attID).Scan(&activeBans)
		if activeBans != 1 {
			t.Errorf("expected 1 active ban after re-activation, got %d", activeBans)
		}

		// D. Manual coordinator pardon is preserved across flips:
		var banID int64
		_ = pool.QueryRow(ctx, "SELECT id FROM user_bans WHERE source_hero_attendance_id = $1", attID).Scan(&banID)

		// Coordinator manually pardons ban (lifted_by set to opStaffID)
		coreStaffID := createTestStaffForAttendance(t, pool, "core_coord@attendance.test", "Core Coord", "core", 1)
		claimsCore := &auth.Claims{UserID: coreStaffID, Email: "core_coord@attendance.test", IsStaff: true, Role: "core", DivisionID: 1}

		liftReq := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/bans/%d/lift", banID), bytes.NewReader([]byte(`{"note": "Special pardon by coordinator"}`)))
		liftReq.SetPathValue("id", fmt.Sprintf("%d", banID))
		liftReq = liftReq.WithContext(middleware.WithClaims(liftReq.Context(), claimsCore))
		recLift := httptest.NewRecorder()
		h.LiftBanHandler(recLift, liftReq)

		if recLift.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on manual lift, got %d: %s", recLift.Code, recLift.Body.String())
		}

		// Verify lifted_by IS NOT NULL
		var liftedBy *uuid.UUID
		_ = pool.QueryRow(ctx, "SELECT lifted_by FROM user_bans WHERE id = $1", banID).Scan(&liftedBy)
		if liftedBy == nil || *liftedBy != coreStaffID {
			t.Errorf("expected lifted_by=%v, got %v", coreStaffID, liftedBy)
		}

		// Now flip back to absent again: because lifted_by IS NOT NULL, the ban stays lifted!
		recAbsentPardon := httptest.NewRecorder()
		reqAbsentPardon := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/donations/%s/attendances/%s", donationID.String(), attID.String()), bytes.NewReader(absentBytes))
		reqAbsentPardon.SetPathValue("id", donationID.String())
		reqAbsentPardon.SetPathValue("attendance_id", attID.String())
		reqAbsentPardon = reqAbsentPardon.WithContext(middleware.WithClaims(reqAbsentPardon.Context(), claimsOp))
		h.MarkHeroAttendanceHandler(recAbsentPardon, reqAbsentPardon)

		if recAbsentPardon.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on mark absent when manual pardon exists, got %d: %s", recAbsentPardon.Code, recAbsentPardon.Body.String())
		}

		var banActiveAfterPardon int
		_ = pool.QueryRow(ctx, "SELECT COUNT(*) FROM user_bans WHERE source_hero_attendance_id = $1 AND lifted_at IS NULL", attID).Scan(&banActiveAfterPardon)
		if banActiveAfterPardon != 0 {
			t.Errorf("expected pardon to be preserved (active=0), got %d", banActiveAfterPardon)
		}
	})

	t.Run("3. Staff Attendance Marking and Core/Super permission guard", func(t *testing.T) {
		donationID := createDonationFixture("open", time.Now().Add(-30*time.Minute))
		var slotID int64
		_ = pool.QueryRow(ctx, `
			INSERT INTO war_slots (donation_id, division_id, quota, remain)
			VALUES ($1, 2, 3, 3)
			RETURNING id
		`, donationID).Scan(&slotID)

		shiftStaffID := createTestStaffForAttendance(t, pool, "shift_worker@attendance.test", "Shift Worker", "staff", 2)
		var staffAttID int64
		_ = pool.QueryRow(ctx, `
			INSERT INTO staff_attendances (war_slot_id, staff_id, status)
			VALUES ($1, $2, 'registered')
			RETURNING id
		`, slotID, shiftStaffID).Scan(&staffAttID)

		// Regular staff (role='staff') attempts marking staff attendance -> 403 Forbidden (requires core or super)
		regStaff := createTestStaffForAttendance(t, pool, "reg_staff@attendance.test", "Reg Staff", "staff", 1)
		claimsReg := &auth.Claims{UserID: regStaff, Email: "reg_staff@attendance.test", IsStaff: true, Role: "staff", DivisionID: 1}

		bodyAbsent := MarkAttendanceRequest{Status: "absent", Notes: strPtr("Staff missed shift")}
		bodyBytes, _ := json.Marshal(bodyAbsent)

		reqReg := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/donations/%s/war-slots/%d/attendances/%d", donationID.String(), slotID, staffAttID), bytes.NewReader(bodyBytes))
		reqReg.SetPathValue("id", donationID.String())
		reqReg.SetPathValue("slot_id", fmt.Sprintf("%d", slotID))
		reqReg.SetPathValue("staff_attendance_id", fmt.Sprintf("%d", staffAttID))
		reqReg = reqReg.WithContext(middleware.WithClaims(reqReg.Context(), claimsReg))
		recReg := httptest.NewRecorder()
		middleware.RequireCoreStaff(http.HandlerFunc(h.MarkStaffAttendanceHandler)).ServeHTTP(recReg, reqReg)

		if recReg.Code != http.StatusForbidden {
			t.Fatalf("expected 403 Forbidden for non-core staff, got %d", recReg.Code)
		}

		// Core staff marks staff attendance as absent -> 200 OK and ban created in user_bans
		coreStaff := createTestStaffForAttendance(t, pool, "core_marker@attendance.test", "Core Marker", "core", 1)
		claimsCore := &auth.Claims{UserID: coreStaff, Email: "core_marker@attendance.test", IsStaff: true, Role: "core", DivisionID: 1}

		reqCore := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/donations/%s/war-slots/%d/attendances/%d", donationID.String(), slotID, staffAttID), bytes.NewReader(bodyBytes))
		reqCore.SetPathValue("id", donationID.String())
		reqCore.SetPathValue("slot_id", fmt.Sprintf("%d", slotID))
		reqCore.SetPathValue("staff_attendance_id", fmt.Sprintf("%d", staffAttID))
		reqCore = reqCore.WithContext(middleware.WithClaims(reqCore.Context(), claimsCore))
		recCore := httptest.NewRecorder()
		middleware.RequireCoreStaff(http.HandlerFunc(h.MarkStaffAttendanceHandler)).ServeHTTP(recCore, reqCore)

		if recCore.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on mark staff absent, got %d: %s", recCore.Code, recCore.Body.String())
		}

		var staffBanCount int
		_ = pool.QueryRow(ctx, "SELECT COUNT(*) FROM user_bans WHERE source_staff_attendance_id = $1 AND lifted_at IS NULL", staffAttID).Scan(&staffBanCount)
		if staffBanCount != 1 {
			t.Errorf("expected 1 active staff ban, got %d", staffBanCount)
		}

		// Core staff corrects to attended -> auto-lifted
		bodyAttended := MarkAttendanceRequest{Status: "attended"}
		bodyAttendedBytes, _ := json.Marshal(bodyAttended)
		reqCorrect := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/donations/%s/war-slots/%d/attendances/%d", donationID.String(), slotID, staffAttID), bytes.NewReader(bodyAttendedBytes))
		reqCorrect.SetPathValue("id", donationID.String())
		reqCorrect.SetPathValue("slot_id", fmt.Sprintf("%d", slotID))
		reqCorrect.SetPathValue("staff_attendance_id", fmt.Sprintf("%d", staffAttID))
		reqCorrect = reqCorrect.WithContext(middleware.WithClaims(reqCorrect.Context(), claimsCore))
		recCorrect := httptest.NewRecorder()
		h.MarkStaffAttendanceHandler(recCorrect, reqCorrect)

		if recCorrect.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on staff attendance correction, got %d: %s", recCorrect.Code, recCorrect.Body.String())
		}

		_ = pool.QueryRow(ctx, "SELECT COUNT(*) FROM user_bans WHERE source_staff_attendance_id = $1 AND lifted_at IS NULL", staffAttID).Scan(&staffBanCount)
		if staffBanCount != 0 {
			t.Errorf("expected 0 active staff bans after auto-lift, got %d", staffBanCount)
		}
	})

	t.Run("4. List User Bans and Core Lift Ban endpoint", func(t *testing.T) {
		targetUserID := createTestUserForAttendance(t, pool, "banned_user@attendance.test", "Banned User")
		donationID := createDonationFixture("open", time.Now().Add(-30*time.Minute))

		// Insert manual ban
		var banID int64
		err := pool.QueryRow(ctx, `
			INSERT INTO user_bans (user_id, scope, donation_id, donation_count, reason, note)
			VALUES ($1, 'all', $2, 2, 'manual', 'manual discipline test')
			RETURNING id
		`, targetUserID, donationID).Scan(&banID)
		if err != nil {
			t.Fatalf("failed to insert manual ban: %v", err)
		}

		coreStaff := createTestStaffForAttendance(t, pool, "core_lifter@attendance.test", "Core Lifter", "super", 1)
		claims := &auth.Claims{UserID: coreStaff, Email: "core_lifter@attendance.test", IsStaff: true, Role: "super", DivisionID: 1}

		// List user bans
		reqList := httptest.NewRequest(http.MethodGet, "/api/v1/users/"+targetUserID.String()+"/bans", nil)
		reqList.SetPathValue("id", targetUserID.String())
		reqList = reqList.WithContext(middleware.WithClaims(reqList.Context(), claims))
		recList := httptest.NewRecorder()
		h.ListUserBansHandler(recList, reqList)

		if recList.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on list bans, got %d: %s", recList.Code, recList.Body.String())
		}

		var listResp map[string]any
		_ = json.Unmarshal(recList.Body.Bytes(), &listResp)
		bans := listResp["data"].([]any)
		if len(bans) != 1 {
			t.Fatalf("expected 1 ban in response, got %d", len(bans))
		}
		listMeta, ok := listResp["meta"].(map[string]any)
		if !ok {
			t.Fatalf("expected meta object in ListUserBansHandler response, got %T", listResp["meta"])
		}
		if listMeta["page"] != float64(1) || listMeta["limit"] != float64(20) || listMeta["total"] != float64(1) || listMeta["total_pages"] != float64(1) {
			t.Errorf("unexpected meta in ListUserBansHandler: %v", listMeta)
		}
		banItem := bans[0].(map[string]any)
		for _, nullableKey := range []string{"source_hero_attendance_id", "source_staff_attendance_id", "created_by", "lifted_at", "lifted_by"} {
			val, exists := banItem[nullableKey]
			if !exists {
				t.Errorf("expected key %q to be explicitly emitted in ListUserBansHandler", nullableKey)
			} else if val != nil {
				t.Errorf("expected key %q to be null before lift, got %v", nullableKey, val)
			}
		}

		// Lift ban
		reqLift := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/bans/%d/lift", banID), bytes.NewReader([]byte(`{"note": "Early pardon granted"}`)))
		reqLift.SetPathValue("id", fmt.Sprintf("%d", banID))
		reqLift = reqLift.WithContext(middleware.WithClaims(reqLift.Context(), claims))
		recLift := httptest.NewRecorder()
		h.LiftBanHandler(recLift, reqLift)

		if recLift.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on lift ban, got %d: %s", recLift.Code, recLift.Body.String())
		}

		// Verify lifted ban response emits explicit nulls for nil pointers
		var liftResp map[string]any
		if err := json.Unmarshal(recLift.Body.Bytes(), &liftResp); err != nil {
			t.Fatalf("failed to unmarshal lift response: %v", err)
		}
		liftData := liftResp["data"].(map[string]any)
		for _, nullableKey := range []string{"source_hero_attendance_id", "source_staff_attendance_id", "created_by"} {
			val, exists := liftData[nullableKey]
			if !exists {
				t.Errorf("expected key %q to be explicitly emitted in LiftBanHandler response", nullableKey)
			} else if val != nil {
				t.Errorf("expected key %q to be null, got %v", nullableKey, val)
			}
		}
		if liftData["lifted_at"] == nil || liftData["lifted_by"] == nil {
			t.Errorf("expected lifted_at and lifted_by to be populated after lift")
		}

		var liftedAt *time.Time
		var liftedBy *uuid.UUID
		_ = pool.QueryRow(ctx, "SELECT lifted_at, lifted_by FROM user_bans WHERE id = $1", banID).Scan(&liftedAt, &liftedBy)
		if liftedAt == nil || liftedBy == nil || *liftedBy != coreStaff {
			t.Errorf("expected ban lifted with lifted_by=%v, got %v", coreStaff, liftedBy)
		}
	})

	t.Run("5. Pre-event marking time window guards", func(t *testing.T) {
		futureDonationID := createDonationFixture("open", time.Now().Add(2*time.Hour))
		futureHeroID := createTestUserForAttendance(t, pool, "future_hero@attendance.test", "Future Hero")
		_, _ = pool.Exec(ctx, "INSERT INTO heroes (user_id) VALUES ($1)", futureHeroID)

		var futureAttID uuid.UUID
		err := pool.QueryRow(ctx, `
			INSERT INTO hero_attendances (donation_id, hero_id, status)
			VALUES ($1, $2, 'registered')
			RETURNING id
		`, futureDonationID, futureHeroID).Scan(&futureAttID)
		if err != nil {
			t.Fatalf("failed to insert hero attendance: %v", err)
		}

		opStaffID := createTestStaffForAttendance(t, pool, "op_window@attendance.test", "Op Window", "staff", 3)
		claimsOp := &auth.Claims{UserID: opStaffID, Email: "op_window@attendance.test", IsStaff: true, DivisionID: 3, Role: "staff"}

		// At T-2h, marking absent rejected
		bodyAbsent, _ := json.Marshal(MarkAttendanceRequest{Status: "absent"})
		reqAbsent := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/donations/%s/attendances/%s", futureDonationID.String(), futureAttID.String()), bytes.NewReader(bodyAbsent))
		reqAbsent.SetPathValue("id", futureDonationID.String())
		reqAbsent.SetPathValue("attendance_id", futureAttID.String())
		reqAbsent = reqAbsent.WithContext(middleware.WithClaims(reqAbsent.Context(), claimsOp))
		recAbsent := httptest.NewRecorder()
		h.MarkHeroAttendanceHandler(recAbsent, reqAbsent)

		if recAbsent.Code != http.StatusBadRequest {
			t.Errorf("expected 400 Bad Request marking absent at T-2h, got %d", recAbsent.Code)
		}

		// At T-2h, marking attended rejected
		bodyAttended, _ := json.Marshal(MarkAttendanceRequest{Status: "attended"})
		reqAttended := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/donations/%s/attendances/%s", futureDonationID.String(), futureAttID.String()), bytes.NewReader(bodyAttended))
		reqAttended.SetPathValue("id", futureDonationID.String())
		reqAttended.SetPathValue("attendance_id", futureAttID.String())
		reqAttended = reqAttended.WithContext(middleware.WithClaims(reqAttended.Context(), claimsOp))
		recAttended := httptest.NewRecorder()
		h.MarkHeroAttendanceHandler(recAttended, reqAttended)

		if recAttended.Code != http.StatusBadRequest {
			t.Errorf("expected 400 Bad Request marking attended at T-2h, got %d", recAttended.Code)
		}

		// Update event_at to T+30m (within 1h checkin window)
		_, _ = pool.Exec(ctx, "UPDATE donations SET event_at = NOW() + INTERVAL '30 minutes' WHERE id = $1", futureDonationID)

		// At T-30m, marking absent is STILL rejected (only allowed >= event_at)
		recAbsent30m := httptest.NewRecorder()
		reqAbsent30m := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/donations/%s/attendances/%s", futureDonationID.String(), futureAttID.String()), bytes.NewReader(bodyAbsent))
		reqAbsent30m.SetPathValue("id", futureDonationID.String())
		reqAbsent30m.SetPathValue("attendance_id", futureAttID.String())
		reqAbsent30m = reqAbsent30m.WithContext(middleware.WithClaims(reqAbsent30m.Context(), claimsOp))
		h.MarkHeroAttendanceHandler(recAbsent30m, reqAbsent30m)

		if recAbsent30m.Code != http.StatusBadRequest {
			t.Errorf("expected 400 Bad Request marking absent at T-30m, got %d", recAbsent30m.Code)
		}

		// At T-30m, marking attended SUCCEEDS (within 1h checkin window)
		recAttended30m := httptest.NewRecorder()
		reqAttended30m := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/donations/%s/attendances/%s", futureDonationID.String(), futureAttID.String()), bytes.NewReader(bodyAttended))
		reqAttended30m.SetPathValue("id", futureDonationID.String())
		reqAttended30m.SetPathValue("attendance_id", futureAttID.String())
		reqAttended30m = reqAttended30m.WithContext(middleware.WithClaims(reqAttended30m.Context(), claimsOp))
		h.MarkHeroAttendanceHandler(recAttended30m, reqAttended30m)

		if recAttended30m.Code != http.StatusOK {
			t.Errorf("expected 200 OK marking attended at T-30m, got %d: %s", recAttended30m.Code, recAttended30m.Body.String())
		}
	})

	t.Run("6. Attendance and slot cross-ownership tampering check", func(t *testing.T) {
		donationA := createDonationFixture("open", time.Now().Add(-1*time.Hour))
		donationB := createDonationFixture("open", time.Now().Add(-1*time.Hour))

		heroUser := createTestUserForAttendance(t, pool, "tamper_hero@attendance.test", "Tamper Hero")
		_, _ = pool.Exec(ctx, "INSERT INTO heroes (user_id) VALUES ($1)", heroUser)

		var attA uuid.UUID
		_ = pool.QueryRow(ctx, "INSERT INTO hero_attendances (donation_id, hero_id, status) VALUES ($1, $2, 'registered') RETURNING id", donationA, heroUser).Scan(&attA)

		opStaffID := createTestStaffForAttendance(t, pool, "tamper_op@attendance.test", "Tamper Op", "staff", 3)
		claimsOp := &auth.Claims{UserID: opStaffID, Email: "tamper_op@attendance.test", IsStaff: true, DivisionID: 3, Role: "staff"}

		body, _ := json.Marshal(MarkAttendanceRequest{Status: "attended"})

		// Calling attendance A with donation B -> 404 NOT FOUND
		reqTamperHero := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/donations/%s/attendances/%s", donationB.String(), attA.String()), bytes.NewReader(body))
		reqTamperHero.SetPathValue("id", donationB.String())
		reqTamperHero.SetPathValue("attendance_id", attA.String())
		reqTamperHero = reqTamperHero.WithContext(middleware.WithClaims(reqTamperHero.Context(), claimsOp))
		recTamperHero := httptest.NewRecorder()
		h.MarkHeroAttendanceHandler(recTamperHero, reqTamperHero)

		if recTamperHero.Code != http.StatusNotFound {
			t.Errorf("expected 404 Not Found on cross-donation hero attendance tamper, got %d", recTamperHero.Code)
		}

		// Staff cross-slot tampering
		var slotA, slotB int64
		_ = pool.QueryRow(ctx, "INSERT INTO war_slots (donation_id, division_id, quota, remain) VALUES ($1, 2, 2, 2) RETURNING id", donationA).Scan(&slotA)
		_ = pool.QueryRow(ctx, "INSERT INTO war_slots (donation_id, division_id, quota, remain) VALUES ($1, 2, 2, 2) RETURNING id", donationB).Scan(&slotB)

		staffUser := createTestStaffForAttendance(t, pool, "tamper_staff@attendance.test", "Tamper Staff", "staff", 2)
		var staffAttA int64
		_ = pool.QueryRow(ctx, "INSERT INTO staff_attendances (war_slot_id, staff_id, status) VALUES ($1, $2, 'registered') RETURNING id", slotA, staffUser).Scan(&staffAttA)

		coreStaff := createTestStaffForAttendance(t, pool, "core_tamper@attendance.test", "Core Tamper", "core", 1)
		claimsCore := &auth.Claims{UserID: coreStaff, Email: "core_tamper@attendance.test", IsStaff: true, Role: "core", DivisionID: 1}

		// Slot B does not belong to Donation A -> 404 NOT FOUND
		reqTamperSlot := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/donations/%s/war-slots/%d/attendances/%d", donationA.String(), slotB, staffAttA), bytes.NewReader(body))
		reqTamperSlot.SetPathValue("id", donationA.String())
		reqTamperSlot.SetPathValue("slot_id", fmt.Sprintf("%d", slotB))
		reqTamperSlot.SetPathValue("staff_attendance_id", fmt.Sprintf("%d", staffAttA))
		reqTamperSlot = reqTamperSlot.WithContext(middleware.WithClaims(reqTamperSlot.Context(), claimsCore))
		recTamperSlot := httptest.NewRecorder()
		h.MarkStaffAttendanceHandler(recTamperSlot, reqTamperSlot)

		if recTamperSlot.Code != http.StatusNotFound {
			t.Errorf("expected 404 Not Found on cross-donation slot tamper, got %d", recTamperSlot.Code)
		}

		// Staff Attendance A does not belong to Slot B on Donation B -> 404 NOT FOUND
		reqTamperAtt := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/donations/%s/war-slots/%d/attendances/%d", donationB.String(), slotB, staffAttA), bytes.NewReader(body))
		reqTamperAtt.SetPathValue("id", donationB.String())
		reqTamperAtt.SetPathValue("slot_id", fmt.Sprintf("%d", slotB))
		reqTamperAtt.SetPathValue("staff_attendance_id", fmt.Sprintf("%d", staffAttA))
		reqTamperAtt = reqTamperAtt.WithContext(middleware.WithClaims(reqTamperAtt.Context(), claimsCore))
		recTamperAtt := httptest.NewRecorder()
		h.MarkStaffAttendanceHandler(recTamperAtt, reqTamperAtt)

		if recTamperAtt.Code != http.StatusNotFound {
			t.Errorf("expected 404 Not Found on cross-slot staff attendance tamper, got %d", recTamperAtt.Code)
		}
	})

	t.Run("7. Manual Ban Administration Endpoint (POST /api/v1/users/:id/bans)", func(t *testing.T) {
		coreStaff := createTestStaffForAttendance(t, pool, "core_manual_admin@attendance.test", "Core Manual Admin", "core", 1)
		claimsCore := &auth.Claims{UserID: coreStaff, Email: "core_manual_admin@attendance.test", IsStaff: true, Role: "core", DivisionID: 1}

		regStaff := createTestStaffForAttendance(t, pool, "reg_manual_admin@attendance.test", "Reg Manual Admin", "staff", 1)
		claimsReg := &auth.Claims{UserID: regStaff, Email: "reg_manual_admin@attendance.test", IsStaff: true, Role: "staff", DivisionID: 1}

		heroUser := createTestUserForAttendance(t, pool, "manual_hero_target@attendance.test", "Manual Hero Target")
		_, _ = pool.Exec(ctx, "INSERT INTO heroes (user_id) VALUES ($1)", heroUser)

		// A. Permission Guard: non-core staff rejected with 403 Forbidden
		bodyValid, _ := json.Marshal(CreateManualBanRequest{Scope: "hero_war", DonationCount: 2, Note: "Test manual ban"})
		reqReg := httptest.NewRequest(http.MethodPost, "/api/v1/users/"+heroUser.String()+"/bans", bytes.NewReader(bodyValid))
		reqReg.SetPathValue("id", heroUser.String())
		reqReg = reqReg.WithContext(middleware.WithClaims(reqReg.Context(), claimsReg))
		recReg := httptest.NewRecorder()
		middleware.RequireCoreStaff(http.HandlerFunc(h.CreateManualBanHandler)).ServeHTTP(recReg, reqReg)

		if recReg.Code != http.StatusForbidden {
			t.Errorf("expected 403 Forbidden for non-core staff, got %d", recReg.Code)
		}

		// B. Self-Ban Guard: staff cannot ban themselves
		reqSelf := httptest.NewRequest(http.MethodPost, "/api/v1/users/"+coreStaff.String()+"/bans", bytes.NewReader(bodyValid))
		reqSelf.SetPathValue("id", coreStaff.String())
		reqSelf = reqSelf.WithContext(middleware.WithClaims(reqSelf.Context(), claimsCore))
		recSelf := httptest.NewRecorder()
		h.CreateManualBanHandler(recSelf, reqSelf)

		if recSelf.Code != http.StatusBadRequest {
			t.Errorf("expected 400 Bad Request on self-ban, got %d", recSelf.Code)
		}

		// C. Range & Validation Guards
		// donation_count = 0
		bodyZeroCount, _ := json.Marshal(CreateManualBanRequest{Scope: "hero_war", DonationCount: 0, Note: "Zero count"})
		reqZero := httptest.NewRequest(http.MethodPost, "/api/v1/users/"+heroUser.String()+"/bans", bytes.NewReader(bodyZeroCount))
		reqZero.SetPathValue("id", heroUser.String())
		reqZero = reqZero.WithContext(middleware.WithClaims(reqZero.Context(), claimsCore))
		recZero := httptest.NewRecorder()
		h.CreateManualBanHandler(recZero, reqZero)
		if recZero.Code != http.StatusBadRequest {
			t.Errorf("expected 400 Bad Request on donation_count=0, got %d", recZero.Code)
		}

		// donation_count = 11
		bodyMaxCount, _ := json.Marshal(CreateManualBanRequest{Scope: "hero_war", DonationCount: 11, Note: "Above max"})
		reqMax := httptest.NewRequest(http.MethodPost, "/api/v1/users/"+heroUser.String()+"/bans", bytes.NewReader(bodyMaxCount))
		reqMax.SetPathValue("id", heroUser.String())
		reqMax = reqMax.WithContext(middleware.WithClaims(reqMax.Context(), claimsCore))
		recMax := httptest.NewRecorder()
		h.CreateManualBanHandler(recMax, reqMax)
		if recMax.Code != http.StatusBadRequest {
			t.Errorf("expected 400 Bad Request on donation_count=11, got %d", recMax.Code)
		}

		// empty note
		bodyEmptyNote, _ := json.Marshal(CreateManualBanRequest{Scope: "hero_war", DonationCount: 2, Note: "   "})
		reqEmptyNote := httptest.NewRequest(http.MethodPost, "/api/v1/users/"+heroUser.String()+"/bans", bytes.NewReader(bodyEmptyNote))
		reqEmptyNote.SetPathValue("id", heroUser.String())
		reqEmptyNote = reqEmptyNote.WithContext(middleware.WithClaims(reqEmptyNote.Context(), claimsCore))
		recEmptyNote := httptest.NewRecorder()
		h.CreateManualBanHandler(recEmptyNote, reqEmptyNote)
		if recEmptyNote.Code != http.StatusBadRequest {
			t.Errorf("expected 400 Bad Request on empty note, got %d", recEmptyNote.Code)
		}

		// D. Profile matching: target has hero profile only
		bodyStaffScope, _ := json.Marshal(CreateManualBanRequest{Scope: "staff_war", DonationCount: 2, Note: "Wrong scope"})
		reqStaffScope := httptest.NewRequest(http.MethodPost, "/api/v1/users/"+heroUser.String()+"/bans", bytes.NewReader(bodyStaffScope))
		reqStaffScope.SetPathValue("id", heroUser.String())
		reqStaffScope = reqStaffScope.WithContext(middleware.WithClaims(reqStaffScope.Context(), claimsCore))
		recStaffScope := httptest.NewRecorder()
		h.CreateManualBanHandler(recStaffScope, reqStaffScope)
		if recStaffScope.Code != http.StatusBadRequest {
			t.Errorf("expected 400 Bad Request for staff_war on hero-only user, got %d", recStaffScope.Code)
		}

		bodyAllScope, _ := json.Marshal(CreateManualBanRequest{Scope: "all", DonationCount: 2, Note: "All scope on hero only"})
		reqAllScope := httptest.NewRequest(http.MethodPost, "/api/v1/users/"+heroUser.String()+"/bans", bytes.NewReader(bodyAllScope))
		reqAllScope.SetPathValue("id", heroUser.String())
		reqAllScope = reqAllScope.WithContext(middleware.WithClaims(reqAllScope.Context(), claimsCore))
		recAllScope := httptest.NewRecorder()
		h.CreateManualBanHandler(recAllScope, reqAllScope)
		if recAllScope.Code != http.StatusBadRequest {
			t.Errorf("expected 400 Bad Request for scope=all on single-profile user, got %d", recAllScope.Code)
		}

		// E. Successful creation
		reqSuccess := httptest.NewRequest(http.MethodPost, "/api/v1/users/"+heroUser.String()+"/bans", bytes.NewReader(bodyValid))
		reqSuccess.SetPathValue("id", heroUser.String())
		reqSuccess = reqSuccess.WithContext(middleware.WithClaims(reqSuccess.Context(), claimsCore))
		recSuccess := httptest.NewRecorder()
		h.CreateManualBanHandler(recSuccess, reqSuccess)

		if recSuccess.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created on manual ban, got %d: %s", recSuccess.Code, recSuccess.Body.String())
		}

		// Verify manual ban DTO emits explicit nulls on unset nullable fields
		var manualResp map[string]any
		if err := json.Unmarshal(recSuccess.Body.Bytes(), &manualResp); err != nil {
			t.Fatalf("failed to unmarshal manual ban response: %v", err)
		}
		manualData := manualResp["data"].(map[string]any)
		for _, nullableKey := range []string{"donation_id", "source_hero_attendance_id", "source_staff_attendance_id", "lifted_at", "lifted_by"} {
			val, exists := manualData[nullableKey]
			if !exists {
				t.Errorf("expected key %q to be explicitly emitted in CreateManualBanHandler response", nullableKey)
			} else if val != nil {
				t.Errorf("expected key %q to be null, got %v", nullableKey, val)
			}
		}
		if manualData["created_by"] == nil {
			t.Errorf("expected created_by to be populated on manual ban")
		}

		// Verify created_by and note in DB
		var banID int64
		var banCreatedBy uuid.UUID
		var banNote, banReason string
		err := pool.QueryRow(ctx, "SELECT id, created_by, note, reason FROM user_bans WHERE user_id = $1 AND lifted_at IS NULL", heroUser).Scan(&banID, &banCreatedBy, &banNote, &banReason)
		if err != nil {
			t.Fatalf("failed to query created manual ban: %v", err)
		}
		if banCreatedBy != coreStaff || banReason != "manual" || banNote != "Test manual ban" {
			t.Errorf("unexpected ban data: created_by=%v, reason=%s, note=%s", banCreatedBy, banReason, banNote)
		}

		// F. Duplicate active manual ban rejected with 409 Conflict
		recDup := httptest.NewRecorder()
		reqDup := httptest.NewRequest(http.MethodPost, "/api/v1/users/"+heroUser.String()+"/bans", bytes.NewReader(bodyValid))
		reqDup.SetPathValue("id", heroUser.String())
		reqDup = reqDup.WithContext(middleware.WithClaims(reqDup.Context(), claimsCore))
		h.CreateManualBanHandler(recDup, reqDup)

		if recDup.Code != http.StatusConflict {
			t.Errorf("expected 409 Conflict on duplicate active manual ban, got %d: %s", recDup.Code, recDup.Body.String())
		}

		// G. Verify manual ban blocks hero claim
		hHero := NewHeroWarHandler(pool, &cfg)
		claimsHero := &auth.Claims{UserID: heroUser, Email: "manual_hero_target@attendance.test", IsHero: true}
		openDonationID := createDonationFixture("open", time.Now().Add(24*time.Hour))
		reqClaim := httptest.NewRequest(http.MethodPost, "/api/v1/donations/"+openDonationID.String()+"/claim", nil)
		reqClaim.SetPathValue("id", openDonationID.String())
		reqClaim = reqClaim.WithContext(middleware.WithClaims(reqClaim.Context(), claimsHero))
		recClaim := httptest.NewRecorder()
		hHero.ClaimDonationHandler(recClaim, reqClaim)

		if recClaim.Code != http.StatusForbidden {
			t.Errorf("expected 403 Forbidden while manual ban is active, got %d: %s", recClaim.Code, recClaim.Body.String())
		}
	})

	t.Run("8. Ban Flip-Flop Sequence & Baseline Reset Verification", func(t *testing.T) {
		donationA := createDonationFixture("open", time.Now().Add(-2*time.Hour))
		heroID := createTestUserForAttendance(t, pool, "flipflop_hero@attendance.test", "FlipFlop Hero")
		_, _ = pool.Exec(ctx, "INSERT INTO heroes (user_id) VALUES ($1)", heroID)
		heroClaims := &auth.Claims{UserID: heroID, Email: "flipflop_hero@attendance.test", IsHero: true}

		opStaffID := createTestStaffForAttendance(t, pool, "flipflop_op@attendance.test", "FlipFlop Op", "staff", 3)
		opClaims := &auth.Claims{UserID: opStaffID, Email: "flipflop_op@attendance.test", IsStaff: true, DivisionID: 3, Role: "staff"}

		var attID uuid.UUID
		err := pool.QueryRow(ctx, "INSERT INTO hero_attendances (donation_id, hero_id, status) VALUES ($1, $2, 'registered') RETURNING id", donationA, heroID).Scan(&attID)
		if err != nil {
			t.Fatalf("failed to insert attendance: %v", err)
		}

		// Step 1: Coordinator marks hero 'absent' on Event A -> ban created
		bodyAbsent, _ := json.Marshal(MarkAttendanceRequest{Status: "absent"})
		reqAbsent1 := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/donations/%s/attendances/%s", donationA.String(), attID.String()), bytes.NewReader(bodyAbsent))
		reqAbsent1.SetPathValue("id", donationA.String())
		reqAbsent1.SetPathValue("attendance_id", attID.String())
		reqAbsent1 = reqAbsent1.WithContext(middleware.WithClaims(reqAbsent1.Context(), opClaims))
		recAbsent1 := httptest.NewRecorder()
		h.MarkHeroAttendanceHandler(recAbsent1, reqAbsent1)
		if recAbsent1.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on mark absent, got %d", recAbsent1.Code)
		}

		// Step 2: Correct to 'attended' -> ban is auto-lifted
		bodyAttended, _ := json.Marshal(MarkAttendanceRequest{Status: "attended"})
		reqAttended := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/donations/%s/attendances/%s", donationA.String(), attID.String()), bytes.NewReader(bodyAttended))
		reqAttended.SetPathValue("id", donationA.String())
		reqAttended.SetPathValue("attendance_id", attID.String())
		reqAttended = reqAttended.WithContext(middleware.WithClaims(reqAttended.Context(), opClaims))
		recAttended := httptest.NewRecorder()
		h.MarkHeroAttendanceHandler(recAttended, reqAttended)
		if recAttended.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on mark attended, got %d", recAttended.Code)
		}

		// Step 3: Two eligible events B and C complete while ban was lifted!
		time.Sleep(10 * time.Millisecond)
		donB := createDonationFixture("completed", time.Now().Add(-1*time.Hour))
		donC := createDonationFixture("completed", time.Now().Add(-30*time.Minute))
		defer func() {
			_, _ = pool.Exec(ctx, "DELETE FROM donations WHERE id IN ($1, $2)", donB, donC)
		}()

		// Step 4: Coordinator flips Event A back to 'absent' -> ban re-activates!
		time.Sleep(10 * time.Millisecond)
		reAbsentTime := time.Now()
		reqAbsent2 := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/donations/%s/attendances/%s", donationA.String(), attID.String()), bytes.NewReader(bodyAbsent))
		reqAbsent2.SetPathValue("id", donationA.String())
		reqAbsent2.SetPathValue("attendance_id", attID.String())
		reqAbsent2 = reqAbsent2.WithContext(middleware.WithClaims(reqAbsent2.Context(), opClaims))
		recAbsent2 := httptest.NewRecorder()
		h.MarkHeroAttendanceHandler(recAbsent2, reqAbsent2)
		if recAbsent2.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on re-absent mark, got %d", recAbsent2.Code)
		}

		// Verify created_at in user_bans was reset to >= reAbsentTime!
		var banCreatedAt time.Time
		_ = pool.QueryRow(ctx, "SELECT created_at FROM user_bans WHERE source_hero_attendance_id = $1", attID).Scan(&banCreatedAt)
		if banCreatedAt.Before(reAbsentTime.Add(-100 * time.Millisecond)) {
			t.Errorf("expected ban created_at to be reset to re-activation time (%v), got %v", reAbsentTime, banCreatedAt)
		}

		// Crucial assertion: Attempting to claim open Event D MUST BE BLOCKED (403 Forbidden)!
		// Events B and C that completed while the ban was lifted do NOT count as served!
		hHero := NewHeroWarHandler(pool, &cfg)
		openDonD := createDonationFixture("open", time.Now().Add(24*time.Hour))
		reqClaimD := httptest.NewRequest(http.MethodPost, "/api/v1/donations/"+openDonD.String()+"/claim", nil)
		reqClaimD.SetPathValue("id", openDonD.String())
		reqClaimD = reqClaimD.WithContext(middleware.WithClaims(reqClaimD.Context(), heroClaims))
		recClaimD := httptest.NewRecorder()
		hHero.ClaimDonationHandler(recClaimD, reqClaimD)

		if recClaimD.Code != http.StatusForbidden {
			t.Fatalf("expected 403 Forbidden because re-activated ban restarted baseline, got %d: %s", recClaimD.Code, recClaimD.Body.String())
		}
	})

	t.Run("9. Pardon Preserved Across Status Flip-Flops (absent -> pardon -> attended -> absent)", func(t *testing.T) {
		donation := createDonationFixture("open", time.Now().Add(-2*time.Hour))
		heroID := createTestUserForAttendance(t, pool, "pardon_flip_hero@attendance.test", "Pardon Flip Hero")
		_, _ = pool.Exec(ctx, "INSERT INTO heroes (user_id) VALUES ($1)", heroID)

		opStaffID := createTestStaffForAttendance(t, pool, "pardon_flip_op@attendance.test", "Pardon Flip Op", "staff", 3)
		opClaims := &auth.Claims{UserID: opStaffID, Email: "pardon_flip_op@attendance.test", IsStaff: true, DivisionID: 3, Role: "staff"}

		coreStaffID := createTestStaffForAttendance(t, pool, "pardon_flip_core@attendance.test", "Pardon Flip Core", "core", 1)
		coreClaims := &auth.Claims{UserID: coreStaffID, Email: "pardon_flip_core@attendance.test", IsStaff: true, DivisionID: 1, Role: "core"}

		var attID uuid.UUID
		err := pool.QueryRow(ctx, "INSERT INTO hero_attendances (donation_id, hero_id, status) VALUES ($1, $2, 'registered') RETURNING id", donation, heroID).Scan(&attID)
		if err != nil {
			t.Fatalf("failed to insert attendance: %v", err)
		}

		// 1. Mark absent -> ban created
		bodyAbsent, _ := json.Marshal(MarkAttendanceRequest{Status: "absent"})
		req1 := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/donations/%s/attendances/%s", donation.String(), attID.String()), bytes.NewReader(bodyAbsent))
		req1.SetPathValue("id", donation.String())
		req1.SetPathValue("attendance_id", attID.String())
		req1 = req1.WithContext(middleware.WithClaims(req1.Context(), opClaims))
		rec1 := httptest.NewRecorder()
		h.MarkHeroAttendanceHandler(rec1, req1)
		if rec1.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rec1.Code)
		}

		// Get ban ID
		var banID int64
		_ = pool.QueryRow(ctx, "SELECT id FROM user_bans WHERE source_hero_attendance_id = $1", attID).Scan(&banID)

		// 2. Coordinator manually pardons ban
		bodyLift, _ := json.Marshal(LiftBanRequest{Note: strPtr("Coordinator pardon")})
		reqLift := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/bans/%d/lift", banID), bytes.NewReader(bodyLift))
		reqLift.SetPathValue("id", fmt.Sprintf("%d", banID))
		reqLift = reqLift.WithContext(middleware.WithClaims(reqLift.Context(), coreClaims))
		recLift := httptest.NewRecorder()
		h.LiftBanHandler(recLift, reqLift)
		if recLift.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on lift, got %d", recLift.Code)
		}

		// 3. Flip to attended
		bodyAttended, _ := json.Marshal(MarkAttendanceRequest{Status: "attended"})
		req2 := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/donations/%s/attendances/%s", donation.String(), attID.String()), bytes.NewReader(bodyAttended))
		req2.SetPathValue("id", donation.String())
		req2.SetPathValue("attendance_id", attID.String())
		req2 = req2.WithContext(middleware.WithClaims(req2.Context(), opClaims))
		rec2 := httptest.NewRecorder()
		h.MarkHeroAttendanceHandler(rec2, req2)
		if rec2.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rec2.Code)
		}

		// 4. Flip back to absent
		req3 := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/donations/%s/attendances/%s", donation.String(), attID.String()), bytes.NewReader(bodyAbsent))
		req3.SetPathValue("id", donation.String())
		req3.SetPathValue("attendance_id", attID.String())
		req3 = req3.WithContext(middleware.WithClaims(req3.Context(), opClaims))
		rec3 := httptest.NewRecorder()
		h.MarkHeroAttendanceHandler(rec3, req3)
		if rec3.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rec3.Code)
		}

		// Assert: ban MUST retain lifted_by = coreStaffID (pardon survived!)
		var banLiftedBy uuid.UUID
		_ = pool.QueryRow(ctx, "SELECT lifted_by FROM user_bans WHERE id = $1", banID).Scan(&banLiftedBy)
		if banLiftedBy != coreStaffID {
			t.Errorf("expected pardon lifted_by=%v to survive flip-flop, got %v", coreStaffID, banLiftedBy)
		}
	})

	t.Run("10. Non-default Config Override (HeroBanEventCount=3, StaffBanEventCount=3)", func(t *testing.T) {
		customCfg := cfg
		customCfg.War.HeroBanEventCount = 3
		customCfg.War.StaffBanEventCount = 3
		hCustom := NewAttendanceHandler(pool, &customCfg)

		donation := createDonationFixture("open", time.Now().Add(-2*time.Hour))
		heroID := createTestUserForAttendance(t, pool, "cfg_hero@attendance.test", "Cfg Hero")
		_, _ = pool.Exec(ctx, "INSERT INTO heroes (user_id) VALUES ($1)", heroID)

		opStaffID := createTestStaffForAttendance(t, pool, "cfg_op@attendance.test", "Cfg Op", "staff", 3)
		opClaims := &auth.Claims{UserID: opStaffID, Email: "cfg_op@attendance.test", IsStaff: true, DivisionID: 3, Role: "staff"}

		var attID uuid.UUID
		err := pool.QueryRow(ctx, "INSERT INTO hero_attendances (donation_id, hero_id, status) VALUES ($1, $2, 'registered') RETURNING id", donation, heroID).Scan(&attID)
		if err != nil {
			t.Fatalf("failed to insert attendance: %v", err)
		}

		bodyAbsent, _ := json.Marshal(MarkAttendanceRequest{Status: "absent"})
		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/donations/%s/attendances/%s", donation.String(), attID.String()), bytes.NewReader(bodyAbsent))
		req.SetPathValue("id", donation.String())
		req.SetPathValue("attendance_id", attID.String())
		req = req.WithContext(middleware.WithClaims(req.Context(), opClaims))
		rec := httptest.NewRecorder()
		hCustom.MarkHeroAttendanceHandler(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rec.Code)
		}

		var banCount int32
		_ = pool.QueryRow(ctx, "SELECT donation_count FROM user_bans WHERE source_hero_attendance_id = $1", attID).Scan(&banCount)
		if banCount != 3 {
			t.Errorf("expected donation_count=3 from custom config override, got %d", banCount)
		}
	})

	t.Run("11. Extended Manual Ban Validations", func(t *testing.T) {
		coreStaffID := createTestStaffForAttendance(t, pool, "val_core@attendance.test", "Val Core", "core", 1)
		coreClaims := &auth.Claims{UserID: coreStaffID, Email: "val_core@attendance.test", IsStaff: true, DivisionID: 1, Role: "core"}

		heroOnlyID := createTestUserForAttendance(t, pool, "val_heroonly@attendance.test", "Val Hero Only")
		_, _ = pool.Exec(ctx, "INSERT INTO heroes (user_id) VALUES ($1)", heroOnlyID)

		// 11A. Self-ban rejected with 400
		bodySelf, _ := json.Marshal(CreateManualBanRequest{Scope: "hero_war", DonationCount: 2, Note: "Self ban attempt"})
		reqSelf := httptest.NewRequest(http.MethodPost, "/api/v1/users/"+coreStaffID.String()+"/bans", bytes.NewReader(bodySelf))
		reqSelf.SetPathValue("id", coreStaffID.String())
		reqSelf = reqSelf.WithContext(middleware.WithClaims(reqSelf.Context(), coreClaims))
		recSelf := httptest.NewRecorder()
		h.CreateManualBanHandler(recSelf, reqSelf)
		if recSelf.Code != http.StatusBadRequest {
			t.Errorf("expected 400 Bad Request on self-ban, got %d: %s", recSelf.Code, recSelf.Body.String())
		}

		// 11B. Scope all with only hero profile rejected with 400
		bodyAll, _ := json.Marshal(CreateManualBanRequest{Scope: "all", DonationCount: 2, Note: "Scope all test"})
		reqAll := httptest.NewRequest(http.MethodPost, "/api/v1/users/"+heroOnlyID.String()+"/bans", bytes.NewReader(bodyAll))
		reqAll.SetPathValue("id", heroOnlyID.String())
		reqAll = reqAll.WithContext(middleware.WithClaims(reqAll.Context(), coreClaims))
		recAll := httptest.NewRecorder()
		h.CreateManualBanHandler(recAll, reqAll)
		if recAll.Code != http.StatusBadRequest {
			t.Errorf("expected 400 Bad Request on scope=all without staff profile, got %d: %s", recAll.Code, recAll.Body.String())
		}

		// 11C. DonationCount 0 or 11 rejected with 400
		body0, _ := json.Marshal(CreateManualBanRequest{Scope: "hero_war", DonationCount: 0, Note: "Count 0 test"})
		req0 := httptest.NewRequest(http.MethodPost, "/api/v1/users/"+heroOnlyID.String()+"/bans", bytes.NewReader(body0))
		req0.SetPathValue("id", heroOnlyID.String())
		req0 = req0.WithContext(middleware.WithClaims(req0.Context(), coreClaims))
		rec0 := httptest.NewRecorder()
		h.CreateManualBanHandler(rec0, req0)
		if rec0.Code != http.StatusBadRequest {
			t.Errorf("expected 400 Bad Request on donation_count=0, got %d", rec0.Code)
		}

		// 11D. Empty note rejected with 400
		bodyEmptyNote, _ := json.Marshal(CreateManualBanRequest{Scope: "hero_war", DonationCount: 2, Note: "   "})
		reqEmptyNote := httptest.NewRequest(http.MethodPost, "/api/v1/users/"+heroOnlyID.String()+"/bans", bytes.NewReader(bodyEmptyNote))
		reqEmptyNote.SetPathValue("id", heroOnlyID.String())
		reqEmptyNote = reqEmptyNote.WithContext(middleware.WithClaims(reqEmptyNote.Context(), coreClaims))
		recEmptyNote := httptest.NewRecorder()
		h.CreateManualBanHandler(recEmptyNote, reqEmptyNote)
		if recEmptyNote.Code != http.StatusBadRequest {
			t.Errorf("expected 400 Bad Request on empty note, got %d", recEmptyNote.Code)
		}
	})

	t.Run("12. Auto-lift on Donation Cancellation Rigorous DB Assertion", func(t *testing.T) {
		don := createDonationFixture("open", time.Now().Add(2*time.Hour))
		hero1 := createTestUserForAttendance(t, pool, "autolift_hero1@attendance.test", "Autolift Hero 1")
		hero2 := createTestUserForAttendance(t, pool, "autolift_hero2@attendance.test", "Autolift Hero 2")
		hero3 := createTestUserForAttendance(t, pool, "autolift_hero3@attendance.test", "Autolift Hero 3")
		_, _ = pool.Exec(ctx, "INSERT INTO heroes (user_id) VALUES ($1), ($2), ($3)", hero1, hero2, hero3)

		coreStaff := createTestStaffForAttendance(t, pool, "autolift_core@attendance.test", "Autolift Core", "core", 1)
		opStaff := createTestStaffForAttendance(t, pool, "autolift_op@attendance.test", "Autolift Op", "staff", 3)
		opClaims := &auth.Claims{UserID: opStaff, Email: "autolift_op@attendance.test", IsStaff: true, DivisionID: 3, Role: "staff"}

		// Ban 1: System ban created by late cancellation
		var attID uuid.UUID
		_ = pool.QueryRow(ctx, "INSERT INTO hero_attendances (donation_id, hero_id, status) VALUES ($1, $2, 'registered') RETURNING id", don, hero1).Scan(&attID)
		var ban1ID int64
		_ = pool.QueryRow(ctx, `
			INSERT INTO user_bans (user_id, scope, donation_id, donation_count, reason, source_hero_attendance_id)
			VALUES ($1, 'hero_war', $2, 2, 'late_cancellation', $3)
			RETURNING id
		`, hero1, don, attID).Scan(&ban1ID)

		// Ban 2: Manual ban on same donation
		var ban2ID int64
		_ = pool.QueryRow(ctx, `
			INSERT INTO user_bans (user_id, scope, donation_id, donation_count, reason, created_by, note)
			VALUES ($1, 'hero_war', $2, 2, 'manual', $3, 'manual note')
			RETURNING id
		`, hero2, don, coreStaff).Scan(&ban2ID)

		// Ban 3: Manually pardoned system ban on same donation
		var att3ID uuid.UUID
		_ = pool.QueryRow(ctx, "INSERT INTO hero_attendances (donation_id, hero_id, status) VALUES ($1, $2, 'registered') RETURNING id", don, hero3).Scan(&att3ID)
		var ban3ID int64
		_ = pool.QueryRow(ctx, `
			INSERT INTO user_bans (user_id, scope, donation_id, donation_count, reason, source_hero_attendance_id, lifted_at, lifted_by, note)
			VALUES ($1, 'hero_war', $2, 2, 'late_cancellation', $3, NOW(), $4, 'coordinator pardon')
			RETURNING id
		`, hero3, don, att3ID, coreStaff).Scan(&ban3ID)

		// Staff cancels the donation via API
		donationHandler := NewDonationHandler(pool, &cfg)
		reqCancel := httptest.NewRequest(http.MethodPost, "/api/v1/donations/"+don.String()+"/cancel", nil)
		reqCancel.SetPathValue("id", don.String())
		reqCancel = reqCancel.WithContext(middleware.WithClaims(reqCancel.Context(), opClaims))
		recCancel := httptest.NewRecorder()
		donationHandler.CancelDonationHandler(recCancel, reqCancel)
		if recCancel.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on cancel donation, got %d: %s", recCancel.Code, recCancel.Body.String())
		}

		// Assert Ban 1: Automatically lifted with note
		var ban1LiftedAt *time.Time
		var ban1Note string
		_ = pool.QueryRow(ctx, "SELECT lifted_at, note FROM user_bans WHERE id = $1", ban1ID).Scan(&ban1LiftedAt, &ban1Note)
		if ban1LiftedAt == nil || ban1Note != "auto-lifted: donation cancelled by staff" {
			t.Errorf("expected ban1 to be auto-lifted, got lifted_at=%v, note=%s", ban1LiftedAt, ban1Note)
		}

		// Assert Ban 2: Manual ban is UNTOUCHED
		var ban2LiftedAt *time.Time
		_ = pool.QueryRow(ctx, "SELECT lifted_at FROM user_bans WHERE id = $1", ban2ID).Scan(&ban2LiftedAt)
		if ban2LiftedAt != nil {
			t.Errorf("expected manual ban2 to remain active (lifted_at=nil), got %v", ban2LiftedAt)
		}

		// Assert Ban 3: Manually pardoned ban retains its original coordinator pardon
		var ban3LiftedBy uuid.UUID
		var ban3Note string
		_ = pool.QueryRow(ctx, "SELECT lifted_by, note FROM user_bans WHERE id = $1", ban3ID).Scan(&ban3LiftedBy, &ban3Note)
		if ban3LiftedBy != coreStaff || ban3Note != "coordinator pardon" {
			t.Errorf("expected ban3 to preserve coordinator pardon, got lifted_by=%v, note=%s", ban3LiftedBy, ban3Note)
		}
	})

	t.Run("13. Invalid Status Filter Validation (400 VAL_001)", func(t *testing.T) {
		donID := createDonationFixture("open", time.Now().Add(2*time.Hour))
		var slotID int64
		err := pool.QueryRow(ctx, `
			INSERT INTO war_slots (donation_id, division_id, quota, remain)
			VALUES ($1, 1, 2, 2)
			RETURNING id
		`, donID).Scan(&slotID)
		if err != nil {
			t.Fatalf("failed to insert war slot: %v", err)
		}

		staffID := createTestStaffForAttendance(t, pool, "status_filter_staff@attendance.test", "Filter Staff", "staff", 3)
		claims := &auth.Claims{UserID: staffID, Email: "status_filter_staff@attendance.test", IsStaff: true, DivisionID: 3, Role: "staff"}

		// Invalid ?status=bogus on ListHeroAttendancesHandler -> 400 VAL_001
		reqHero := httptest.NewRequest(http.MethodGet, "/api/v1/donations/"+donID.String()+"/attendances?status=bogus", nil)
		reqHero.SetPathValue("id", donID.String())
		reqHero = reqHero.WithContext(middleware.WithClaims(reqHero.Context(), claims))
		recHero := httptest.NewRecorder()
		h.ListHeroAttendancesHandler(recHero, reqHero)

		if recHero.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request on invalid hero status filter, got %d: %s", recHero.Code, recHero.Body.String())
		}
		var heroErrResp map[string]any
		_ = json.Unmarshal(recHero.Body.Bytes(), &heroErrResp)
		if heroErrResp["code"] != "VAL_001" {
			t.Errorf("expected VAL_001 code for invalid hero status filter, got %v", heroErrResp["code"])
		}

		// Invalid ?status=bogus on ListStaffAttendancesHandler -> 400 VAL_001
		reqStaff := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/donations/%s/war-slots/%d/attendances?status=bogus", donID.String(), slotID), nil)
		reqStaff.SetPathValue("id", donID.String())
		reqStaff.SetPathValue("slot_id", fmt.Sprintf("%d", slotID))
		reqStaff = reqStaff.WithContext(middleware.WithClaims(reqStaff.Context(), claims))
		recStaff := httptest.NewRecorder()
		h.ListStaffAttendancesHandler(recStaff, reqStaff)

		if recStaff.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request on invalid staff status filter, got %d: %s", recStaff.Code, recStaff.Body.String())
		}
		var staffErrResp map[string]any
		_ = json.Unmarshal(recStaff.Body.Bytes(), &staffErrResp)
		if staffErrResp["code"] != "VAL_001" {
			t.Errorf("expected VAL_001 code for invalid staff status filter, got %v", staffErrResp["code"])
		}
	})

	t.Run("14. ListUserBansHandler Filtering, Pagination & 404 Guard", func(t *testing.T) {
		coreStaff := createTestStaffForAttendance(t, pool, "ban_filter_core@attendance.test", "Ban Filter Core", "core", 1)
		claims := &auth.Claims{UserID: coreStaff, Email: "ban_filter_core@attendance.test", IsStaff: true, DivisionID: 1, Role: "core"}

		// A. Unknown user ID -> 404 NOT_FOUND
		missingUserID := uuid.New()
		req404 := httptest.NewRequest(http.MethodGet, "/api/v1/users/"+missingUserID.String()+"/bans", nil)
		req404.SetPathValue("id", missingUserID.String())
		req404 = req404.WithContext(middleware.WithClaims(req404.Context(), claims))
		rec404 := httptest.NewRecorder()
		h.ListUserBansHandler(rec404, req404)
		if rec404.Code != http.StatusNotFound {
			t.Fatalf("expected 404 Not Found for nonexistent user, got %d: %s", rec404.Code, rec404.Body.String())
		}

		// B. Seed user with 1 active hero_war ban and 1 lifted staff_war ban
		targetUser := createTestUserForAttendance(t, pool, "ban_filter_target@attendance.test", "Ban Filter Target")
		donID := createDonationFixture("open", time.Now().Add(-1*time.Hour))
		_, err := pool.Exec(ctx, `
			INSERT INTO user_bans (user_id, scope, donation_id, donation_count, reason, note)
			VALUES ($1, 'hero_war', $2, 2, 'manual', 'active hero ban')
		`, targetUser, donID)
		if err != nil {
			t.Fatalf("failed to insert active hero ban: %v", err)
		}
		_, err = pool.Exec(ctx, `
			INSERT INTO user_bans (user_id, scope, donation_id, donation_count, reason, lifted_at, lifted_by, note)
			VALUES ($1, 'staff_war', $2, 1, 'manual', NOW(), $3, 'lifted staff ban')
		`, targetUser, donID, coreStaff)
		if err != nil {
			t.Fatalf("failed to insert lifted staff ban: %v", err)
		}

		// C. Invalid ?scope=bogus -> 400 VAL_001
		reqBadScope := httptest.NewRequest(http.MethodGet, "/api/v1/users/"+targetUser.String()+"/bans?scope=bogus", nil)
		reqBadScope.SetPathValue("id", targetUser.String())
		reqBadScope = reqBadScope.WithContext(middleware.WithClaims(reqBadScope.Context(), claims))
		recBadScope := httptest.NewRecorder()
		h.ListUserBansHandler(recBadScope, reqBadScope)
		if recBadScope.Code != http.StatusBadRequest {
			t.Errorf("expected 400 Bad Request for invalid scope filter, got %d", recBadScope.Code)
		}

		// D. Invalid ?active_only=maybe -> 400 VAL_001
		reqBadActive := httptest.NewRequest(http.MethodGet, "/api/v1/users/"+targetUser.String()+"/bans?active_only=maybe", nil)
		reqBadActive.SetPathValue("id", targetUser.String())
		reqBadActive = reqBadActive.WithContext(middleware.WithClaims(reqBadActive.Context(), claims))
		recBadActive := httptest.NewRecorder()
		h.ListUserBansHandler(recBadActive, reqBadActive)
		if recBadActive.Code != http.StatusBadRequest {
			t.Errorf("expected 400 Bad Request for invalid active_only filter, got %d", recBadActive.Code)
		}

		// E. ?scope=staff_war returns only staff_war ban (1 item) with meta
		reqScope := httptest.NewRequest(http.MethodGet, "/api/v1/users/"+targetUser.String()+"/bans?scope=staff_war", nil)
		reqScope.SetPathValue("id", targetUser.String())
		reqScope = reqScope.WithContext(middleware.WithClaims(reqScope.Context(), claims))
		recScope := httptest.NewRecorder()
		h.ListUserBansHandler(recScope, reqScope)
		if recScope.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on scope=staff_war, got %d", recScope.Code)
		}
		var scopeResp map[string]any
		_ = json.Unmarshal(recScope.Body.Bytes(), &scopeResp)
		scopeData := scopeResp["data"].([]any)
		if len(scopeData) != 1 || scopeData[0].(map[string]any)["scope"] != "staff_war" {
			t.Errorf("expected 1 staff_war ban, got %v", scopeData)
		}
		scopeMeta := scopeResp["meta"].(map[string]any)
		if scopeMeta["page"] != float64(1) || scopeMeta["limit"] != float64(20) || scopeMeta["total"] != float64(1) || scopeMeta["total_pages"] != float64(1) {
			t.Errorf("unexpected meta on scope=staff_war: %v", scopeMeta)
		}

		// F. ?active_only=true returns only active hero_war ban (1 item) with meta
		reqActive := httptest.NewRequest(http.MethodGet, "/api/v1/users/"+targetUser.String()+"/bans?active_only=true", nil)
		reqActive.SetPathValue("id", targetUser.String())
		reqActive = reqActive.WithContext(middleware.WithClaims(reqActive.Context(), claims))
		recActive := httptest.NewRecorder()
		h.ListUserBansHandler(recActive, reqActive)
		if recActive.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on active_only=true, got %d", recActive.Code)
		}
		var activeResp map[string]any
		_ = json.Unmarshal(recActive.Body.Bytes(), &activeResp)
		activeData := activeResp["data"].([]any)
		if len(activeData) != 1 || activeData[0].(map[string]any)["scope"] != "hero_war" {
			t.Errorf("expected 1 active hero_war ban, got %v", activeData)
		}
		activeMeta := activeResp["meta"].(map[string]any)
		if activeMeta["total"] != float64(1) || activeMeta["total_pages"] != float64(1) {
			t.Errorf("unexpected meta on active_only=true: %v", activeMeta)
		}

		// G. ?active_only=false returns both bans (2 items) with meta
		reqAll := httptest.NewRequest(http.MethodGet, "/api/v1/users/"+targetUser.String()+"/bans?active_only=false", nil)
		reqAll.SetPathValue("id", targetUser.String())
		reqAll = reqAll.WithContext(middleware.WithClaims(reqAll.Context(), claims))
		recAll := httptest.NewRecorder()
		h.ListUserBansHandler(recAll, reqAll)
		if recAll.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on active_only=false, got %d", recAll.Code)
		}
		var allResp map[string]any
		_ = json.Unmarshal(recAll.Body.Bytes(), &allResp)
		allData := allResp["data"].([]any)
		if len(allData) != 2 {
			t.Errorf("expected 2 bans when active_only=false, got %d", len(allData))
		}
		allMeta := allResp["meta"].(map[string]any)
		if allMeta["total"] != float64(2) || allMeta["total_pages"] != float64(1) {
			t.Errorf("unexpected meta on active_only=false: %v", allMeta)
		}

		// H. Pagination ?page=1&limit=1 and ?page=2&limit=1 (ordered by created_at DESC, id DESC)
		reqP1 := httptest.NewRequest(http.MethodGet, "/api/v1/users/"+targetUser.String()+"/bans?page=1&limit=1", nil)
		reqP1.SetPathValue("id", targetUser.String())
		reqP1 = reqP1.WithContext(middleware.WithClaims(reqP1.Context(), claims))
		recP1 := httptest.NewRecorder()
		h.ListUserBansHandler(recP1, reqP1)
		if recP1.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on page=1&limit=1, got %d", recP1.Code)
		}
		var p1Resp map[string]any
		_ = json.Unmarshal(recP1.Body.Bytes(), &p1Resp)
		p1Data := p1Resp["data"].([]any)
		p1Meta := p1Resp["meta"].(map[string]any)
		if len(p1Data) != 1 || p1Data[0].(map[string]any)["scope"] != "staff_war" {
			t.Errorf("expected newest ban (staff_war) on page 1, got %v", p1Data)
		}
		if p1Meta["page"] != float64(1) || p1Meta["limit"] != float64(1) || p1Meta["total"] != float64(2) || p1Meta["total_pages"] != float64(2) {
			t.Errorf("unexpected meta on page=1&limit=1: %v", p1Meta)
		}

		reqP2 := httptest.NewRequest(http.MethodGet, "/api/v1/users/"+targetUser.String()+"/bans?page=2&limit=1", nil)
		reqP2.SetPathValue("id", targetUser.String())
		reqP2 = reqP2.WithContext(middleware.WithClaims(reqP2.Context(), claims))
		recP2 := httptest.NewRecorder()
		h.ListUserBansHandler(recP2, reqP2)
		if recP2.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on page=2&limit=1, got %d", recP2.Code)
		}
		var p2Resp map[string]any
		_ = json.Unmarshal(recP2.Body.Bytes(), &p2Resp)
		p2Data := p2Resp["data"].([]any)
		p2Meta := p2Resp["meta"].(map[string]any)
		if len(p2Data) != 1 || p2Data[0].(map[string]any)["scope"] != "hero_war" {
			t.Errorf("expected older ban (hero_war) on page 2, got %v", p2Data)
		}
		if p2Meta["page"] != float64(2) || p2Meta["limit"] != float64(1) || p2Meta["total"] != float64(2) || p2Meta["total_pages"] != float64(2) {
			t.Errorf("unexpected meta on page=2&limit=1: %v", p2Meta)
		}

		// I. Empty result (?scope=all) returns [] and total=0, total_pages=0
		reqEmpty := httptest.NewRequest(http.MethodGet, "/api/v1/users/"+targetUser.String()+"/bans?scope=all", nil)
		reqEmpty.SetPathValue("id", targetUser.String())
		reqEmpty = reqEmpty.WithContext(middleware.WithClaims(reqEmpty.Context(), claims))
		recEmpty := httptest.NewRecorder()
		h.ListUserBansHandler(recEmpty, reqEmpty)
		if recEmpty.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on empty scope=all, got %d", recEmpty.Code)
		}
		var emptyResp map[string]any
		_ = json.Unmarshal(recEmpty.Body.Bytes(), &emptyResp)
		emptyData, ok := emptyResp["data"].([]any)
		if !ok || len(emptyData) != 0 {
			t.Errorf("expected [] on scope=all, got %T %v", emptyResp["data"], emptyResp["data"])
		}
		emptyMeta := emptyResp["meta"].(map[string]any)
		if emptyMeta["total"] != float64(0) || emptyMeta["total_pages"] != float64(0) {
			t.Errorf("unexpected meta on empty result: %v", emptyMeta)
		}
	})

	t.Run("15. LiftBanHandler Hardening: io.EOF, Malformed JSON & 409 BAN_ALREADY_LIFTED", func(t *testing.T) {
		coreStaff := createTestStaffForAttendance(t, pool, "lift_hard_core@attendance.test", "Lift Hard Core", "core", 1)
		secondCore := createTestStaffForAttendance(t, pool, "lift_hard_core2@attendance.test", "Lift Hard Core 2", "core", 1)
		claims1 := &auth.Claims{UserID: coreStaff, Email: "lift_hard_core@attendance.test", IsStaff: true, DivisionID: 1, Role: "core"}
		claims2 := &auth.Claims{UserID: secondCore, Email: "lift_hard_core2@attendance.test", IsStaff: true, DivisionID: 1, Role: "core"}

		targetUser := createTestUserForAttendance(t, pool, "lift_hard_target@attendance.test", "Lift Hard Target")
		donID := createDonationFixture("open", time.Now().Add(-1*time.Hour))
		var banID int64
		err := pool.QueryRow(ctx, `
			INSERT INTO user_bans (user_id, scope, donation_id, donation_count, reason, note)
			VALUES ($1, 'hero_war', $2, 2, 'manual', 'original ban note')
			RETURNING id
		`, targetUser, donID).Scan(&banID)
		if err != nil {
			t.Fatalf("failed to insert ban: %v", err)
		}

		// A. Malformed JSON rejected with 400 VAL_001
		reqMalformed := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/bans/%d/lift", banID), bytes.NewReader([]byte(`{"note":`)))
		reqMalformed.SetPathValue("id", fmt.Sprintf("%d", banID))
		reqMalformed = reqMalformed.WithContext(middleware.WithClaims(reqMalformed.Context(), claims1))
		recMalformed := httptest.NewRecorder()
		h.LiftBanHandler(recMalformed, reqMalformed)
		if recMalformed.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request on malformed JSON, got %d: %s", recMalformed.Code, recMalformed.Body.String())
		}
		var malformedResp map[string]any
		_ = json.Unmarshal(recMalformed.Body.Bytes(), &malformedResp)
		if malformedResp["code"] != "VAL_001" {
			t.Errorf("expected VAL_001 on malformed JSON, got %v", malformedResp["code"])
		}

		// B. Non-existent ban ID returns 404 NOT_FOUND
		req404 := httptest.NewRequest(http.MethodPost, "/api/v1/bans/99999999/lift", nil)
		req404.SetPathValue("id", "99999999")
		req404 = req404.WithContext(middleware.WithClaims(req404.Context(), claims1))
		rec404 := httptest.NewRecorder()
		h.LiftBanHandler(rec404, req404)
		if rec404.Code != http.StatusNotFound {
			t.Errorf("expected 404 Not Found for missing ban ID, got %d", rec404.Code)
		}

		// C. Empty body (io.EOF) succeeds with default note
		reqEmpty := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/bans/%d/lift", banID), nil)
		reqEmpty.SetPathValue("id", fmt.Sprintf("%d", banID))
		reqEmpty = reqEmpty.WithContext(middleware.WithClaims(reqEmpty.Context(), claims1))
		recEmpty := httptest.NewRecorder()
		h.LiftBanHandler(recEmpty, reqEmpty)
		if recEmpty.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on empty-body lift, got %d: %s", recEmpty.Code, recEmpty.Body.String())
		}

		var origLiftedAt time.Time
		var origLiftedBy uuid.UUID
		var origNote string
		if err := pool.QueryRow(ctx, "SELECT lifted_at, lifted_by, note FROM user_bans WHERE id = $1", banID).Scan(&origLiftedAt, &origLiftedBy, &origNote); err != nil {
			t.Fatalf("failed to query lifted ban: %v", err)
		}
		if origLiftedBy != coreStaff || origNote != "manual pardon by coordinator" {
			t.Fatalf("unexpected lift audit fields: lifted_by=%v, note=%q", origLiftedBy, origNote)
		}

		// D. Second lift attempt returns 409 Conflict (BAN_ALREADY_LIFTED) & preserves original audit fields
		reqReLift := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/bans/%d/lift", banID), bytes.NewReader([]byte(`{"note": "Attempt overwrite"}`)))
		reqReLift.SetPathValue("id", fmt.Sprintf("%d", banID))
		reqReLift = reqReLift.WithContext(middleware.WithClaims(reqReLift.Context(), claims2))
		recReLift := httptest.NewRecorder()
		h.LiftBanHandler(recReLift, reqReLift)
		if recReLift.Code != http.StatusConflict {
			t.Fatalf("expected 409 Conflict on double lift, got %d: %s", recReLift.Code, recReLift.Body.String())
		}
		var conflictResp map[string]any
		_ = json.Unmarshal(recReLift.Body.Bytes(), &conflictResp)
		if conflictResp["code"] != "BAN_ALREADY_LIFTED" {
			t.Errorf("expected code BAN_ALREADY_LIFTED, got %v", conflictResp["code"])
		}

		var afterLiftedAt time.Time
		var afterLiftedBy uuid.UUID
		var afterNote string
		if err := pool.QueryRow(ctx, "SELECT lifted_at, lifted_by, note FROM user_bans WHERE id = $1", banID).Scan(&afterLiftedAt, &afterLiftedBy, &afterNote); err != nil {
			t.Fatalf("failed to re-query lifted ban: %v", err)
		}
		if !afterLiftedAt.Equal(origLiftedAt) || afterLiftedBy != origLiftedBy || afterNote != origNote {
			t.Errorf("double lift mutated original audit trail: got (%v, %v, %q), want (%v, %v, %q)", afterLiftedAt, afterLiftedBy, afterNote, origLiftedAt, origLiftedBy, origNote)
		}
	})

	t.Run("16. Custom HeroAttendanceWindowBefore & StaffAttendanceWindowBefore config overrides", func(t *testing.T) {
		customCfg := cfg
		customCfg.War.HeroAttendanceWindowBefore = 30 * time.Minute
		customCfg.War.StaffAttendanceWindowBefore = 2 * time.Hour
		hCustom := NewAttendanceHandler(pool, &customCfg)

		bodyAttended, _ := json.Marshal(MarkAttendanceRequest{Status: "attended"})
		opStaffID := createTestStaffForAttendance(t, pool, "win_op@attendance.test", "Win Op", "staff", 3)
		claimsOp := &auth.Claims{UserID: opStaffID, Email: "win_op@attendance.test", IsStaff: true, DivisionID: 3, Role: "staff"}
		coreStaffID := createTestStaffForAttendance(t, pool, "win_core@attendance.test", "Win Core", "core", 1)
		claimsCore := &auth.Claims{UserID: coreStaffID, Email: "win_core@attendance.test", IsStaff: true, DivisionID: 1, Role: "core"}

		// Part A (Hero): HeroAttendanceWindowBefore = 30m
		heroID := createTestUserForAttendance(t, pool, "win_hero@attendance.test", "Win Hero")
		_, _ = pool.Exec(ctx, "INSERT INTO heroes (user_id) VALUES ($1)", heroID)

		// At T-45m (inside default 1h, outside custom 30m) -> 400 ATTENDANCE_WINDOW_NOT_OPEN with "30 minutes prior"
		donHero45 := createDonationFixture("open", time.Now().Add(45*time.Minute))
		var attHero45 uuid.UUID
		_ = pool.QueryRow(ctx, "INSERT INTO hero_attendances (donation_id, hero_id, status) VALUES ($1, $2, 'registered') RETURNING id", donHero45, heroID).Scan(&attHero45)

		reqHero45 := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/donations/%s/attendances/%s", donHero45.String(), attHero45.String()), bytes.NewReader(bodyAttended))
		reqHero45.SetPathValue("id", donHero45.String())
		reqHero45.SetPathValue("attendance_id", attHero45.String())
		reqHero45 = reqHero45.WithContext(middleware.WithClaims(reqHero45.Context(), claimsOp))
		recHero45 := httptest.NewRecorder()
		hCustom.MarkHeroAttendanceHandler(recHero45, reqHero45)
		if recHero45.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request at T-45m with 30m hero window, got %d: %s", recHero45.Code, recHero45.Body.String())
		}
		if !strings.Contains(recHero45.Body.String(), "ATTENDANCE_WINDOW_NOT_OPEN") || !strings.Contains(recHero45.Body.String(), "30 minutes prior to event time") {
			t.Errorf("expected dynamic 30 minutes ATTENDANCE_WINDOW_NOT_OPEN message, got %s", recHero45.Body.String())
		}

		// At T-15m (inside custom 30m) -> 200 OK
		_, _ = pool.Exec(ctx, "UPDATE donations SET event_at = NOW() + INTERVAL '15 minutes' WHERE id = $1", donHero45)
		reqHero15 := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/donations/%s/attendances/%s", donHero45.String(), attHero45.String()), bytes.NewReader(bodyAttended))
		reqHero15.SetPathValue("id", donHero45.String())
		reqHero15.SetPathValue("attendance_id", attHero45.String())
		reqHero15 = reqHero15.WithContext(middleware.WithClaims(reqHero15.Context(), claimsOp))
		recHero15 := httptest.NewRecorder()
		hCustom.MarkHeroAttendanceHandler(recHero15, reqHero15)
		if recHero15.Code != http.StatusOK {
			t.Errorf("expected 200 OK at T-15m with 30m hero window, got %d: %s", recHero15.Code, recHero15.Body.String())
		}

		// Part B (Staff): StaffAttendanceWindowBefore = 2h
		shiftStaff := createTestStaffForAttendance(t, pool, "win_shift@attendance.test", "Win Shift", "staff", 2)

		// At T-3h (outside custom 2h) -> 400 ATTENDANCE_WINDOW_NOT_OPEN with "2 hours prior"
		donStaff := createDonationFixture("open", time.Now().Add(3*time.Hour))
		var slotID, staffAttID int64
		_ = pool.QueryRow(ctx, "INSERT INTO war_slots (donation_id, division_id, quota, remain) VALUES ($1, 2, 3, 2) RETURNING id", donStaff).Scan(&slotID)
		_ = pool.QueryRow(ctx, "INSERT INTO staff_attendances (war_slot_id, staff_id, status) VALUES ($1, $2, 'registered') RETURNING id", slotID, shiftStaff).Scan(&staffAttID)

		reqStaff3h := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/donations/%s/war-slots/%d/attendances/%d", donStaff.String(), slotID, staffAttID), bytes.NewReader(bodyAttended))
		reqStaff3h.SetPathValue("id", donStaff.String())
		reqStaff3h.SetPathValue("slot_id", fmt.Sprintf("%d", slotID))
		reqStaff3h.SetPathValue("staff_attendance_id", fmt.Sprintf("%d", staffAttID))
		reqStaff3h = reqStaff3h.WithContext(middleware.WithClaims(reqStaff3h.Context(), claimsCore))
		recStaff3h := httptest.NewRecorder()
		hCustom.MarkStaffAttendanceHandler(recStaff3h, reqStaff3h)
		if recStaff3h.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request at T-3h with 2h staff window, got %d: %s", recStaff3h.Code, recStaff3h.Body.String())
		}
		if !strings.Contains(recStaff3h.Body.String(), "ATTENDANCE_WINDOW_NOT_OPEN") || !strings.Contains(recStaff3h.Body.String(), "2 hours prior to event time") {
			t.Errorf("expected dynamic 2 hours ATTENDANCE_WINDOW_NOT_OPEN message, got %s", recStaff3h.Body.String())
		}

		// At T-90m (outside default 1h, inside custom 2h) -> 200 OK
		_, _ = pool.Exec(ctx, "UPDATE donations SET event_at = NOW() + INTERVAL '90 minutes' WHERE id = $1", donStaff)
		reqStaff90m := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/donations/%s/war-slots/%d/attendances/%d", donStaff.String(), slotID, staffAttID), bytes.NewReader(bodyAttended))
		reqStaff90m.SetPathValue("id", donStaff.String())
		reqStaff90m.SetPathValue("slot_id", fmt.Sprintf("%d", slotID))
		reqStaff90m.SetPathValue("staff_attendance_id", fmt.Sprintf("%d", staffAttID))
		reqStaff90m = reqStaff90m.WithContext(middleware.WithClaims(reqStaff90m.Context(), claimsCore))
		recStaff90m := httptest.NewRecorder()
		hCustom.MarkStaffAttendanceHandler(recStaff90m, reqStaff90m)
		if recStaff90m.Code != http.StatusOK {
			t.Errorf("expected 200 OK at T-90m with 2h staff window, got %d: %s", recStaff90m.Code, recStaff90m.Body.String())
		}
	})

	t.Run("17. ListStaffAttendancesHandler returns 404 when parent donation is soft-deleted", func(t *testing.T) {
		donID := createDonationFixture("draft", time.Now().Add(24*time.Hour))
		var slotID int64
		if err := pool.QueryRow(ctx, "INSERT INTO war_slots (donation_id, division_id, quota, remain) VALUES ($1, 1, 2, 2) RETURNING id", donID).Scan(&slotID); err != nil {
			t.Fatalf("failed to insert war slot: %v", err)
		}

		coreStaffID := createTestStaffForAttendance(t, pool, "deleted_don_core@attendance.test", "Deleted Don Core", "core", 1)
		claimsCore := &auth.Claims{UserID: coreStaffID, Email: "deleted_don_core@attendance.test", IsStaff: true, DivisionID: 1, Role: "core"}

		if _, err := pool.Exec(ctx, "UPDATE donations SET deleted_at = NOW() WHERE id = $1", donID); err != nil {
			t.Fatalf("failed to soft-delete donation: %v", err)
		}

		req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/donations/%s/war-slots/%d/attendances", donID.String(), slotID), nil)
		req.SetPathValue("id", donID.String())
		req.SetPathValue("slot_id", fmt.Sprintf("%d", slotID))
		req = req.WithContext(middleware.WithClaims(req.Context(), claimsCore))
		rec := httptest.NewRecorder()
		h.ListStaffAttendancesHandler(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 Not Found for soft-deleted donation's slot attendances, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("18. Soft-deleted staff target is rejected with 400 VAL_002 on CreateManualBanHandler", func(t *testing.T) {
		coreStaffID := createTestStaffForAttendance(t, pool, "bug02_core@attendance.test", "Bug02 Core", "core", 1)
		claimsCore := &auth.Claims{UserID: coreStaffID, Email: "bug02_core@attendance.test", IsStaff: true, DivisionID: 1, Role: "core"}

		targetID := createTestStaffForAttendance(t, pool, "bug02_target@attendance.test", "Bug02 Target", "staff", 1)
		// Also give target a hero profile to verify scope='all' rejects once staff is soft-deleted
		if _, err := pool.Exec(ctx, "INSERT INTO heroes (user_id) VALUES ($1)", targetID); err != nil {
			t.Fatalf("failed to insert hero profile: %v", err)
		}
		if _, err := pool.Exec(ctx, "UPDATE staff SET deleted_at = NOW() WHERE user_id = $1", targetID); err != nil {
			t.Fatalf("failed to soft-delete staff profile: %v", err)
		}

		for _, scope := range []string{"staff_war", "all"} {
			body, _ := json.Marshal(CreateManualBanRequest{Scope: scope, DonationCount: 2, Note: "Soft-deleted staff ban guard"})
			req := httptest.NewRequest(http.MethodPost, "/api/v1/users/"+targetID.String()+"/bans", bytes.NewReader(body))
			req.SetPathValue("id", targetID.String())
			req = req.WithContext(middleware.WithClaims(req.Context(), claimsCore))
			rec := httptest.NewRecorder()

			h.CreateManualBanHandler(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("scope=%s: expected 400 Bad Request for soft-deleted staff target, got %d: %s", scope, rec.Code, rec.Body.String())
			}
			var errResp map[string]any
			_ = json.Unmarshal(rec.Body.Bytes(), &errResp)
			if errResp["code"] != "VAL_002" {
				t.Errorf("scope=%s: expected VAL_002 error code, got %v", scope, errResp["code"])
			}
		}
	})
}

func strPtr(s string) *string {
	return &s
}
