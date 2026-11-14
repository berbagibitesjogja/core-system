package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/berbagibitesjogja/core-system/internal/auth"
	"github.com/berbagibitesjogja/core-system/internal/config"
	"github.com/berbagibitesjogja/core-system/internal/middleware"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

var testConcurrencyPhoneSeq atomic.Int64

func init() {
	testConcurrencyPhoneSeq.Store(time.Now().UnixNano() % 100000000)
}

func createTestUserForConcurrency(t *testing.T, pool *pgxpool.Pool, emailPrefix, name string) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	var userID uuid.UUID
	seq := testConcurrencyPhoneSeq.Add(1)
	phone := fmt.Sprintf("6284%08d", seq%100000000)
	email := fmt.Sprintf("%s_%d@concurrency.test", emailPrefix, seq)
	err := pool.QueryRow(ctx, `
		INSERT INTO users (email, name, phone_number, is_active, email_verified_at)
		VALUES ($1, $2, $3, true, NOW())
		RETURNING id
	`, email, name, phone).Scan(&userID)
	if err != nil {
		t.Fatalf("failed to insert concurrency test user: %v", err)
	}
	return userID
}

func createTestHeroForConcurrency(t *testing.T, pool *pgxpool.Pool, emailPrefix, name string) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	userID := createTestUserForConcurrency(t, pool, emailPrefix, name)
	_, err := pool.Exec(ctx, `
		INSERT INTO heroes (user_id, remind_me_at)
		VALUES ($1, NOW() + INTERVAL '12 hours')
	`, userID)
	if err != nil {
		t.Fatalf("failed to insert hero record: %v", err)
	}
	return userID
}

func createTestStaffForConcurrency(t *testing.T, pool *pgxpool.Pool, emailPrefix, name, role string, divisionID int32) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	userID := createTestUserForConcurrency(t, pool, emailPrefix, name)
	_, err := pool.Exec(ctx, `
		INSERT INTO staff (user_id, division_id, role)
		VALUES ($1, $2, $3)
	`, userID, divisionID, role)
	if err != nil {
		t.Fatalf("failed to insert staff record: %v", err)
	}
	return userID
}

func TestWarConcurrencySimulationAndEdgeCases(t *testing.T) {
	pool := getTestPool(t)

	cfg := config.LoadConfig()
	cfg.War.LateCancellationWindow = 12 * time.Hour
	cfg.War.HeroBanEventCount = 2
	cfg.War.StaffBanEventCount = 1

	heroWarHandler := NewHeroWarHandler(pool, &cfg)
	staffWarHandler := NewStaffWarHandler(pool, &cfg)
	attendanceHandler := NewAttendanceHandler(pool, &cfg)
	ctx := context.Background()

	// Cascade-safe cleanup function
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

	createDonation := func(address string, status string, quota, heroQuota, remain int, eventAt time.Time) uuid.UUID {
		var sponsorID uuid.UUID
		_ = pool.QueryRow(ctx, `
			INSERT INTO sponsors (name, email, variant)
			VALUES ('Sponsor ' || gen_random_uuid(), 'concurrency_sponsor@example.com', 'individual')
			RETURNING id
		`).Scan(&sponsorID)

		var donationID uuid.UUID
		err := pool.QueryRow(ctx, `
			INSERT INTO donations (
				sponsor_id, event_at, quota, hero_quota, remain,
				address, status, hide_from_public, hide_sponsor
			) VALUES (
				$1, $2, $3, $4, $5,
				$6, $7, FALSE, FALSE
			) RETURNING id
		`, sponsorID, eventAt, quota, heroQuota, remain, address, status).Scan(&donationID)
		if err != nil {
			t.Fatalf("failed to insert donation: %v", err)
		}
		return donationID
	}

	t.Run("1. High Concurrency: 50 Heroes rush for 10 slots (Zero Overselling Guarantee)", func(t *testing.T) {
		const totalCompetitors = 50
		const availableSlots = 10

		donationID := createDonation("Jl. Malioboro Concurrency Hero Rush", "open", 20, availableSlots, availableSlots, time.Now().Add(24*time.Hour))

		// Pre-create 50 distinct heroes
		heroIDs := make([]uuid.UUID, totalCompetitors)
		for i := range totalCompetitors {
			heroIDs[i] = createTestHeroForConcurrency(t, pool, fmt.Sprintf("rush_hero_%d", i), fmt.Sprintf("Rush Hero %d", i))
		}

		startBarrier := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(totalCompetitors)

		var successCount int64
		var conflictCount int64
		var otherCount int64

		for i := range totalCompetitors {
			go func(idx int) {
				defer wg.Done()
				<-startBarrier // synchronize all 50 goroutines to strike at the exact same instant

				hID := heroIDs[idx]
				req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/donations/%s/claim", donationID.String()), nil)
				req.SetPathValue("id", donationID.String())
				claims := &auth.Claims{UserID: hID, Email: fmt.Sprintf("rush_hero_%d@concurrency.test", idx), IsHero: true}
				req = req.WithContext(middleware.WithClaims(req.Context(), claims))
				rec := httptest.NewRecorder()

				heroWarHandler.ClaimDonationHandler(rec, req)

				switch rec.Code {
				case http.StatusCreated:
					atomic.AddInt64(&successCount, 1)
				case http.StatusConflict:
					var resp map[string]any
					_ = json.Unmarshal(rec.Body.Bytes(), &resp)
					if resp["code"] == "WAR_001" {
						atomic.AddInt64(&conflictCount, 1)
					} else {
						atomic.AddInt64(&otherCount, 1)
					}
				default:
					atomic.AddInt64(&otherCount, 1)
				}
			}(i)
		}

		close(startBarrier)
		wg.Wait()

		if successCount != int64(availableSlots) {
			t.Fatalf("concurrency failure: expected exactly %d successes, got %d (other=%d)", availableSlots, successCount, otherCount)
		}
		if conflictCount != int64(totalCompetitors-availableSlots) {
			t.Fatalf("concurrency failure: expected exactly %d conflicts, got %d", totalCompetitors-availableSlots, conflictCount)
		}

		// Verify database state: remain is exactly 0 and never below 0
		var dbRemain int32
		var dbStatus string
		err := pool.QueryRow(ctx, "SELECT remain, status FROM donations WHERE id = $1", donationID).Scan(&dbRemain, &dbStatus)
		if err != nil {
			t.Fatalf("failed to query donation: %v", err)
		}
		if dbRemain != 0 {
			t.Errorf("expected remain=0, got %d", dbRemain)
		}
		// Status must remain 'open' (NO status flapping!)
		if dbStatus != "open" {
			t.Errorf("expected status to remain 'open', got %s", dbStatus)
		}

		// Verify exact number of attendances in database
		var attCount int
		_ = pool.QueryRow(ctx, "SELECT COUNT(*) FROM hero_attendances WHERE donation_id = $1 AND cancelled_at IS NULL", donationID).Scan(&attCount)
		if attCount != availableSlots {
			t.Errorf("expected exactly %d attendance rows, got %d", availableSlots, attCount)
		}
	})

	t.Run("2. High Concurrency: 20 Staff rush for 5 shift slots", func(t *testing.T) {
		const totalStaff = 20
		const availableShiftSlots = 5

		donationID := createDonation("Jl. Malioboro Concurrency Staff Rush", "open", 20, 10, 10, time.Now().Add(24*time.Hour))

		var slotID int64
		err := pool.QueryRow(ctx, `
			INSERT INTO war_slots (donation_id, division_id, quota, remain)
			VALUES ($1, 1, $2, $2)
			RETURNING id
		`, donationID, availableShiftSlots).Scan(&slotID)
		if err != nil {
			t.Fatalf("failed to insert war slot: %v", err)
		}

		// Pre-create 20 staff members for Division 1
		staffIDs := make([]uuid.UUID, totalStaff)
		for i := range totalStaff {
			staffIDs[i] = createTestStaffForConcurrency(t, pool, fmt.Sprintf("rush_staff_%d", i), fmt.Sprintf("Rush Staff %d", i), "staff", 1)
		}

		startBarrier := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(totalStaff)

		var successCount int64
		var conflictCount int64
		var otherCount int64

		for i := range totalStaff {
			go func(idx int) {
				defer wg.Done()
				<-startBarrier

				sID := staffIDs[idx]
				req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/donations/%s/war-slots/%d/claim", donationID.String(), slotID), nil)
				req.SetPathValue("id", donationID.String())
				req.SetPathValue("slot_id", fmt.Sprintf("%d", slotID))
				claims := &auth.Claims{UserID: sID, Email: fmt.Sprintf("rush_staff_%d@concurrency.test", idx), IsStaff: true, DivisionID: 1}
				req = req.WithContext(middleware.WithClaims(req.Context(), claims))
				rec := httptest.NewRecorder()

				staffWarHandler.ClaimStaffSlotHandler(rec, req)

				switch rec.Code {
				case http.StatusCreated:
					atomic.AddInt64(&successCount, 1)
				case http.StatusConflict:
					var resp map[string]any
					_ = json.Unmarshal(rec.Body.Bytes(), &resp)
					if resp["code"] == "WAR_001" {
						atomic.AddInt64(&conflictCount, 1)
					} else {
						atomic.AddInt64(&otherCount, 1)
					}
				default:
					atomic.AddInt64(&otherCount, 1)
				}
			}(i)
		}

		close(startBarrier)
		wg.Wait()

		if successCount != int64(availableShiftSlots) {
			t.Fatalf("expected exactly %d staff claim successes, got %d (other=%d)", availableShiftSlots, successCount, otherCount)
		}
		if conflictCount != int64(totalStaff-availableShiftSlots) {
			t.Fatalf("expected exactly %d staff conflicts, got %d", totalStaff-availableShiftSlots, conflictCount)
		}

		var slotRemain int32
		_ = pool.QueryRow(ctx, "SELECT remain FROM war_slots WHERE id = $1", slotID).Scan(&slotRemain)
		if slotRemain != 0 {
			t.Errorf("expected slot remain=0, got %d", slotRemain)
		}
	})

	t.Run("3. Ban Burn-off Edge Cases: Late mark GREATEST baseline, ineligible category, and active claim loophole", func(t *testing.T) {
		// Edge Case A: Late mark absence uses GREATEST(event_at, created_at) baseline
		// Event A was 7 days ago. Event B completed 3 days ago.
		eventA := createDonation("Jl. Malioboro Concurrency Edge A", "completed", 20, 10, 10, time.Now().Add(-7*24*time.Hour))
		_ = createDonation("Jl. Malioboro Concurrency Edge B", "completed", 20, 10, 10, time.Now().Add(-3*24*time.Hour))

		heroID := createTestHeroForConcurrency(t, pool, "edge_hero", "Edge Hero")
		var attA uuid.UUID
		_ = pool.QueryRow(ctx, "INSERT INTO hero_attendances (donation_id, hero_id, status) VALUES ($1, $2, 'registered') RETURNING id", eventA, heroID).Scan(&attA)

		// Staff only marks absence NOW (7 days after Event A, and AFTER Event B already completed)
		opStaffID := createTestStaffForConcurrency(t, pool, "edge_op", "Edge Op", "staff", 3)
		claimsOp := &auth.Claims{UserID: opStaffID, Email: "edge_op@concurrency.test", IsStaff: true, DivisionID: 3}

		bodyAbsent := MarkAttendanceRequest{Status: "absent"}
		bBytes, _ := json.Marshal(bodyAbsent)
		reqAbsent := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/donations/%s/attendances/%s", eventA.String(), attA.String()), bytes.NewReader(bBytes))
		reqAbsent.SetPathValue("id", eventA.String())
		reqAbsent.SetPathValue("attendance_id", attA.String())
		reqAbsent = reqAbsent.WithContext(middleware.WithClaims(reqAbsent.Context(), claimsOp))
		recAbsent := httptest.NewRecorder()
		attendanceHandler.MarkHeroAttendanceHandler(recAbsent, reqAbsent)

		if recAbsent.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on mark absent, got %d: %s", recAbsent.Code, recAbsent.Body.String())
		}

		// Assert: Even though Event B completed in the past, it does NOT count because baseline = GREATEST(eventA, ban.created_at) = NOW()!
		// Hero attempts claiming open event C -> MUST BE 403 Forbidden!
		eventC := createDonation("Jl. Malioboro Concurrency Edge C", "open", 20, 10, 10, time.Now().Add(24*time.Hour))
		reqC := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/donations/%s/claim", eventC.String()), nil)
		reqC.SetPathValue("id", eventC.String())
		claimsHero := &auth.Claims{UserID: heroID, Email: "edge_hero@concurrency.test", IsHero: true}
		reqC = reqC.WithContext(middleware.WithClaims(reqC.Context(), claimsHero))
		recC := httptest.NewRecorder()
		heroWarHandler.ClaimDonationHandler(recC, reqC)

		if recC.Code != http.StatusForbidden {
			t.Fatalf("expected 403 Forbidden because Event B was before baseline, got %d: %s", recC.Code, recC.Body.String())
		}

		// Edge Case B: Events with hero_quota=0 or mismatched target categories do NOT burn off the ban
		// 1. Completed event with hero_quota = 0
		_ = createDonation("Jl. Malioboro Concurrency Edge ZeroQuota", "completed", 20, 0, 0, time.Now().Add(1*time.Hour))

		// 2. Completed event targeted to another category
		var catID uuid.UUID
		_ = pool.QueryRow(ctx, "INSERT INTO beneficiary_categories (name, variant) VALUES ('Exclusive Cat', 'university') RETURNING id").Scan(&catID)
		targetedDonation := createDonation("Jl. Malioboro Concurrency Edge Targeted", "completed", 20, 10, 10, time.Now().Add(2*time.Hour))
		_, _ = pool.Exec(ctx, "INSERT INTO donation_target (donation_id, beneficiary_category_id) VALUES ($1, $2)", targetedDonation, catID)

		// Hero attempts claiming event C again -> Still 403 Forbidden because neither event was eligible!
		recC2 := httptest.NewRecorder()
		heroWarHandler.ClaimDonationHandler(recC2, reqC)
		if recC2.Code != http.StatusForbidden {
			t.Fatalf("expected 403 Forbidden, ineligible events cannot burn off ban, got %d", recC2.Code)
		}

		// Edge Case C: Events where hero holds an active claim do NOT count as sitting out
		claimedDonation := createDonation("Jl. Malioboro Concurrency Edge Claimed", "completed", 20, 10, 10, time.Now().Add(3*time.Hour))
		_, _ = pool.Exec(ctx, "INSERT INTO hero_attendances (donation_id, hero_id, status) VALUES ($1, $2, 'registered')", claimedDonation, heroID)

		recC3 := httptest.NewRecorder()
		heroWarHandler.ClaimDonationHandler(recC3, reqC)
		if recC3.Code != http.StatusForbidden {
			t.Fatalf("expected 403 Forbidden, active claimed events cannot count as sitting out, got %d", recC3.Code)
		}

		// Now complete 2 genuinely eligible, sat-out donations
		_ = createDonation("Jl. Malioboro Concurrency Edge SatOut 1", "completed", 20, 10, 10, time.Now().Add(4*time.Hour))
		_ = createDonation("Jl. Malioboro Concurrency Edge SatOut 2", "completed", 20, 10, 10, time.Now().Add(5*time.Hour))

		// Now the 2 eligible sat-out events burn off the ban -> Hero claim on Event C succeeds!
		recCSuccess := httptest.NewRecorder()
		heroWarHandler.ClaimDonationHandler(recCSuccess, reqC)
		if recCSuccess.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created after ban served, got %d: %s", recCSuccess.Code, recCSuccess.Body.String())
		}
	})

	t.Run("4. Cross-Donation Slot Verification (URL Tampering Prevention)", func(t *testing.T) {
		donationA := createDonation("Jl. Malioboro Concurrency URL Don A", "open", 20, 10, 10, time.Now().Add(24*time.Hour))
		donationB := createDonation("Jl. Malioboro Concurrency URL Don B", "open", 20, 10, 10, time.Now().Add(24*time.Hour))

		var slotB int64
		_ = pool.QueryRow(ctx, "INSERT INTO war_slots (donation_id, division_id, quota, remain) VALUES ($1, 1, 2, 2) RETURNING id", donationB).Scan(&slotB)

		staff := createTestStaffForConcurrency(t, pool, "tamper_staff", "Tamper Staff", "staff", 1)
		claims := &auth.Claims{UserID: staff, Email: "tamper_staff@concurrency.test", IsStaff: true, DivisionID: 1}

		// Attacker passes donationA in URL, but slotB in path -> Must be rejected with 404 Not Found!
		reqTamper := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/donations/%s/war-slots/%d/claim", donationA.String(), slotB), nil)
		reqTamper.SetPathValue("id", donationA.String())
		reqTamper.SetPathValue("slot_id", fmt.Sprintf("%d", slotB))
		reqTamper = reqTamper.WithContext(middleware.WithClaims(reqTamper.Context(), claims))
		recTamper := httptest.NewRecorder()

		staffWarHandler.ClaimStaffSlotHandler(recTamper, reqTamper)

		if recTamper.Code != http.StatusNotFound {
			t.Fatalf("expected 404 Not Found for cross-donation slot claim attempt, got %d: %s", recTamper.Code, recTamper.Body.String())
		}
	})

	t.Run("Simulation 5: Concurrency Race - Cancel vs Mark on exact same row", func(t *testing.T) {
		donationID := createDonation("Jl. Malioboro Concurrency Race", "open", 10, 5, 5, time.Now().Add(-30*time.Minute))
		heroID := createTestHeroForConcurrency(t, pool, "race_hero", "Race Hero")
		heroClaims := &auth.Claims{UserID: heroID, Email: "race_hero@concurrency.test", IsHero: true}

		opStaffID := createTestStaffForConcurrency(t, pool, "race_op", "Race Op", "staff", 3)
		opClaims := &auth.Claims{UserID: opStaffID, Email: "race_op@concurrency.test", IsStaff: true, DivisionID: 3, Role: "staff"}

		var attID uuid.UUID
		err := pool.QueryRow(ctx, "INSERT INTO hero_attendances (donation_id, hero_id, status) VALUES ($1, $2, 'registered') RETURNING id", donationID, heroID).Scan(&attID)
		if err != nil {
			t.Fatalf("failed to insert hero attendance: %v", err)
		}

		const concurrentRoutings = 10
		var wg sync.WaitGroup
		startBarrier := make(chan struct{})

		for i := range concurrentRoutings {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				<-startBarrier

				if idx%2 == 0 {
					// Cancel attempt
					reqCancel := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/donations/%s/cancel-claim", donationID.String()), nil)
					reqCancel.SetPathValue("id", donationID.String())
					reqCancel = reqCancel.WithContext(middleware.WithClaims(reqCancel.Context(), heroClaims))
					recCancel := httptest.NewRecorder()
					heroWarHandler.CancelClaimHandler(recCancel, reqCancel)
				} else {
					// Mark attended attempt
					bodyMark, _ := json.Marshal(MarkAttendanceRequest{Status: "attended"})
					reqMark := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/donations/%s/attendances/%s", donationID.String(), attID.String()), bytes.NewReader(bodyMark))
					reqMark.SetPathValue("id", donationID.String())
					reqMark.SetPathValue("attendance_id", attID.String())
					reqMark = reqMark.WithContext(middleware.WithClaims(reqMark.Context(), opClaims))
					recMark := httptest.NewRecorder()
					attendanceHandler.MarkHeroAttendanceHandler(recMark, reqMark)
				}
			}(i)
		}

		close(startBarrier)
		wg.Wait()

		// Database row consistency verification:
		var finalStatus string
		var cancelledAt, attendedAt *time.Time
		err = pool.QueryRow(ctx, "SELECT status, cancelled_at, attended_at FROM hero_attendances WHERE id = $1", attID).Scan(&finalStatus, &cancelledAt, &attendedAt)
		if err != nil {
			t.Fatalf("failed to query final attendance state: %v", err)
		}

		if finalStatus == "cancelled" {
			if cancelledAt == nil || attendedAt != nil {
				t.Errorf("corrupted state: status is cancelled but cancelled_at=%v, attended_at=%v", cancelledAt, attendedAt)
			}
		} else if finalStatus == "attended" {
			if attendedAt == nil || cancelledAt != nil {
				t.Errorf("corrupted state: status is attended but attended_at=%v, cancelled_at=%v", attendedAt, cancelledAt)
			}
		} else {
			t.Errorf("unexpected terminal status: %s", finalStatus)
		}
	})

	t.Run("Simulation 6: Concurrency Race - Staff Cancel vs Staff Mark on exact same row", func(t *testing.T) {
		donationID := createDonation("Jl. Malioboro Staff Concurrency Race", "open", 10, 5, 5, time.Now().Add(-30*time.Minute))
		staffID := createTestStaffForConcurrency(t, pool, "race_staff_member", "Race Staff Member", "staff", 1)
		staffClaims := &auth.Claims{UserID: staffID, Email: "race_staff_member@concurrency.test", IsStaff: true, DivisionID: 1, Role: "staff"}

		coreStaffID := createTestStaffForConcurrency(t, pool, "race_core_staff", "Race Core Staff", "core", 1)
		coreClaims := &auth.Claims{UserID: coreStaffID, Email: "race_core_staff@concurrency.test", IsStaff: true, DivisionID: 1, Role: "core"}

		var slotID int64
		err := pool.QueryRow(ctx, "INSERT INTO war_slots (donation_id, division_id, quota, remain) VALUES ($1, 1, 5, 4) RETURNING id", donationID).Scan(&slotID)
		if err != nil {
			t.Fatalf("failed to insert war slot: %v", err)
		}

		var staffAttID int64
		err = pool.QueryRow(ctx, "INSERT INTO staff_attendances (war_slot_id, staff_id, status) VALUES ($1, $2, 'registered') RETURNING id", slotID, staffID).Scan(&staffAttID)
		if err != nil {
			t.Fatalf("failed to insert staff attendance: %v", err)
		}

		const concurrentRoutings = 10
		var wg sync.WaitGroup
		startBarrier := make(chan struct{})

		for i := range concurrentRoutings {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				<-startBarrier

				if idx%2 == 0 {
					reqCancel := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/donations/%s/war-slots/%d/cancel", donationID.String(), slotID), nil)
					reqCancel.SetPathValue("id", donationID.String())
					reqCancel.SetPathValue("slot_id", fmt.Sprintf("%d", slotID))
					reqCancel = reqCancel.WithContext(middleware.WithClaims(reqCancel.Context(), staffClaims))
					recCancel := httptest.NewRecorder()
					staffWarHandler.CancelStaffSlotHandler(recCancel, reqCancel)
				} else {
					bodyMark, _ := json.Marshal(MarkAttendanceRequest{Status: "attended"})
					reqMark := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/donations/%s/war-slots/%d/attendances/%d", donationID.String(), slotID, staffAttID), bytes.NewReader(bodyMark))
					reqMark.SetPathValue("id", donationID.String())
					reqMark.SetPathValue("slot_id", fmt.Sprintf("%d", slotID))
					reqMark.SetPathValue("staff_attendance_id", fmt.Sprintf("%d", staffAttID))
					reqMark = reqMark.WithContext(middleware.WithClaims(reqMark.Context(), coreClaims))
					recMark := httptest.NewRecorder()
					attendanceHandler.MarkStaffAttendanceHandler(recMark, reqMark)
				}
			}(i)
		}

		close(startBarrier)
		wg.Wait()

		var finalStatus string
		var cancelledAt, attendedAt *time.Time
		err = pool.QueryRow(ctx, "SELECT status, cancelled_at, attended_at FROM staff_attendances WHERE id = $1", staffAttID).Scan(&finalStatus, &cancelledAt, &attendedAt)
		if err != nil {
			t.Fatalf("failed to query final staff attendance state: %v", err)
		}

		if finalStatus == "cancelled" {
			if cancelledAt == nil || attendedAt != nil {
				t.Errorf("corrupted state: status is cancelled but cancelled_at=%v, attended_at=%v", cancelledAt, attendedAt)
			}
		} else if finalStatus == "attended" {
			if attendedAt == nil || cancelledAt != nil {
				t.Errorf("corrupted state: status is attended but attended_at=%v, cancelled_at=%v", attendedAt, cancelledAt)
			}
		} else {
			t.Errorf("unexpected terminal status: %s", finalStatus)
		}
	})
}
