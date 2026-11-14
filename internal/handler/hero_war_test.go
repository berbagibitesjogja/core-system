package handler

import (
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

var testHeroPhoneSeq atomic.Int64

func init() {
	testHeroPhoneSeq.Store(time.Now().UnixNano() % 100000000)
}

func createTestHero(t *testing.T, pool *pgxpool.Pool, email, name string) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	var userID uuid.UUID
	phone := fmt.Sprintf("6287%08d", testHeroPhoneSeq.Add(1)%100000000)
	err := pool.QueryRow(ctx, `
		INSERT INTO users (email, name, phone_number, is_active, email_verified_at)
		VALUES ($1, $2, $3, true, NOW())
		RETURNING id
	`, email, name, phone).Scan(&userID)
	if err != nil {
		t.Fatalf("failed to insert test user: %v", err)
	}

	futureRemind := time.Now().Add(12 * time.Hour)
	_, err = pool.Exec(ctx, `
		INSERT INTO heroes (user_id, remind_me_at)
		VALUES ($1, $2)
	`, userID, futureRemind)
	if err != nil {
		t.Fatalf("failed to insert hero: %v", err)
	}

	return userID
}

func createTestDonation(t *testing.T, pool *pgxpool.Pool, quota, heroQuota, remain int, status string, eventAt time.Time) (uuid.UUID, uuid.UUID) {
	t.Helper()
	ctx := context.Background()

	var sponsorID uuid.UUID
	err := pool.QueryRow(ctx, `
		INSERT INTO sponsors (name, email, variant)
		VALUES ('Test Sponsor ' || gen_random_uuid(), 'sponsor@example.com', 'individual')
		RETURNING id
	`).Scan(&sponsorID)
	if err != nil {
		t.Fatalf("failed to insert sponsor: %v", err)
	}

	var donationID uuid.UUID
	err = pool.QueryRow(ctx, `
		INSERT INTO donations (
			sponsor_id, event_at, quota, hero_quota, remain,
			address, status, hide_from_public, hide_sponsor
		) VALUES (
			$1, $2, $3, $4, $5,
			'Jl. Malioboro No. 1', $6, FALSE, FALSE
		) RETURNING id
	`, sponsorID, eventAt, quota, heroQuota, remain, status).Scan(&donationID)
	if err != nil {
		t.Fatalf("failed to insert donation: %v", err)
	}

	return donationID, sponsorID
}

func TestHeroWarClaimAndCancel(t *testing.T) {
	pool := getTestPool(t)

	cfg := config.LoadConfig()
	cfg.War.LateCancellationWindow = 12 * time.Hour
	cfg.War.HeroBanEventCount = 2
	h := NewHeroWarHandler(pool, &cfg)
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

	t.Run("1. Hero successfully claims a slot (Positive Path)", func(t *testing.T) {
		heroID := createTestHero(t, pool, "hero1@hero.war.test", "Hero One")
		donationID, _ := createTestDonation(t, pool, 10, 5, 5, "open", time.Now().Add(24*time.Hour))

		req := httptest.NewRequest(http.MethodPost, "/api/v1/donations/"+donationID.String()+"/claim", nil)
		req.SetPathValue("id", donationID.String())
		claims := &auth.Claims{UserID: heroID, Email: "hero1@hero.war.test", IsHero: true}
		req = req.WithContext(middleware.WithClaims(req.Context(), claims))
		rec := httptest.NewRecorder()

		h.ClaimDonationHandler(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created, got %d: %s", rec.Code, rec.Body.String())
		}

		// Verify DTO snake_case keys and explicit null serialization on nullable fields
		var claimResp map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &claimResp); err != nil {
			t.Fatalf("failed to unmarshal claim response: %v", err)
		}
		claimData, ok := claimResp["data"].(map[string]any)
		if !ok {
			t.Fatalf("expected data object in claim response, got %T", claimResp["data"])
		}
		for _, key := range []string{"cancelled_at", "attended_at", "marked_by", "notes"} {
			val, exists := claimData[key]
			if !exists {
				t.Errorf("expected key %q to be explicitly present in HeroAttendanceResponse", key)
			} else if val != nil {
				t.Errorf("expected %q to be null, got %v", key, val)
			}
		}

		// Verify donations.remain decremented
		var remain int32
		err := pool.QueryRow(ctx, "SELECT remain FROM donations WHERE id = $1", donationID).Scan(&remain)
		if err != nil || remain != 4 {
			t.Errorf("expected remain=4, got %d (err: %v)", remain, err)
		}

		// Verify heroes.remind_me_at is cleared
		var remindMeAt *time.Time
		err = pool.QueryRow(ctx, "SELECT remind_me_at FROM heroes WHERE user_id = $1", heroID).Scan(&remindMeAt)
		if err != nil || remindMeAt != nil {
			t.Errorf("expected remind_me_at to be NULL, got %v", remindMeAt)
		}

		// Verify hero_attendances row created
		var attendanceStatus string
		err = pool.QueryRow(ctx, "SELECT status FROM hero_attendances WHERE donation_id = $1 AND hero_id = $2", donationID, heroID).Scan(&attendanceStatus)
		if err != nil || attendanceStatus != "registered" {
			t.Errorf("expected attendance status=registered, got %s (err: %v)", attendanceStatus, err)
		}
	})

	t.Run("2. Duplicate claim on same donation rejected with 409 Conflict", func(t *testing.T) {
		heroID := createTestHero(t, pool, "hero2@hero.war.test", "Hero Two")
		donationID, _ := createTestDonation(t, pool, 10, 5, 5, "open", time.Now().Add(24*time.Hour))

		claims := &auth.Claims{UserID: heroID, Email: "hero2@hero.war.test", IsHero: true}

		// First claim
		req1 := httptest.NewRequest(http.MethodPost, "/api/v1/donations/"+donationID.String()+"/claim", nil)
		req1.SetPathValue("id", donationID.String())
		req1 = req1.WithContext(middleware.WithClaims(req1.Context(), claims))
		rec1 := httptest.NewRecorder()
		h.ClaimDonationHandler(rec1, req1)
		if rec1.Code != http.StatusCreated {
			t.Fatalf("first claim expected 201, got %d", rec1.Code)
		}

		// Second claim
		req2 := httptest.NewRequest(http.MethodPost, "/api/v1/donations/"+donationID.String()+"/claim", nil)
		req2.SetPathValue("id", donationID.String())
		req2 = req2.WithContext(middleware.WithClaims(req2.Context(), claims))
		rec2 := httptest.NewRecorder()
		h.ClaimDonationHandler(rec2, req2)

		if rec2.Code != http.StatusConflict {
			t.Fatalf("expected 409 Conflict for duplicate claim, got %d: %s", rec2.Code, rec2.Body.String())
		}
		var resp map[string]any
		_ = json.Unmarshal(rec2.Body.Bytes(), &resp)
		if resp["code"] != "ALREADY_CLAIMED" {
			t.Errorf("expected code ALREADY_CLAIMED, got %v", resp["code"])
		}
	})

	t.Run("3. Claim rejected with 409 Conflict when slots are exhausted (remain=0)", func(t *testing.T) {
		heroID := createTestHero(t, pool, "hero3@hero.war.test", "Hero Three")
		donationID, _ := createTestDonation(t, pool, 10, 5, 0, "open", time.Now().Add(24*time.Hour))

		req := httptest.NewRequest(http.MethodPost, "/api/v1/donations/"+donationID.String()+"/claim", nil)
		req.SetPathValue("id", donationID.String())
		claims := &auth.Claims{UserID: heroID, Email: "hero3@hero.war.test", IsHero: true}
		req = req.WithContext(middleware.WithClaims(req.Context(), claims))
		rec := httptest.NewRecorder()

		h.ClaimDonationHandler(rec, req)

		if rec.Code != http.StatusConflict {
			t.Fatalf("expected 409 Conflict for remain=0, got %d: %s", rec.Code, rec.Body.String())
		}
		var resp map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp["code"] != "WAR_001" {
			t.Errorf("expected code WAR_001, got %v", resp["code"])
		}
	})

	t.Run("4. Target Category eligibility check", func(t *testing.T) {
		heroID := createTestHero(t, pool, "hero4@hero.war.test", "Hero Four")
		donationID, _ := createTestDonation(t, pool, 10, 5, 5, "open", time.Now().Add(24*time.Hour))

		// Create target category
		var catID uuid.UUID
		err := pool.QueryRow(ctx, `
			INSERT INTO beneficiary_categories (name, variant, is_priority)
			VALUES ('War Category Test', 'university', true)
			RETURNING id
		`).Scan(&catID)
		if err != nil {
			t.Fatalf("failed to insert beneficiary category: %v", err)
		}
		defer func() {
			_, _ = pool.Exec(ctx, "DELETE FROM donation_target WHERE donation_id = $1", donationID)
			_, _ = pool.Exec(ctx, "DELETE FROM hero_categories WHERE hero_id = $1", heroID)
			_, _ = pool.Exec(ctx, "DELETE FROM beneficiary_categories WHERE id = $1", catID)
		}()

		// Add target to donation
		_, err = pool.Exec(ctx, "INSERT INTO donation_target (donation_id, beneficiary_category_id) VALUES ($1, $2)", donationID, catID)
		if err != nil {
			t.Fatalf("failed to insert donation_target: %v", err)
		}

		claims := &auth.Claims{UserID: heroID, Email: "hero4@hero.war.test", IsHero: true}

		// Hero does not belong to category -> 403 Forbidden
		req1 := httptest.NewRequest(http.MethodPost, "/api/v1/donations/"+donationID.String()+"/claim", nil)
		req1.SetPathValue("id", donationID.String())
		req1 = req1.WithContext(middleware.WithClaims(req1.Context(), claims))
		rec1 := httptest.NewRecorder()
		h.ClaimDonationHandler(rec1, req1)
		if rec1.Code != http.StatusForbidden {
			t.Fatalf("expected 403 Forbidden for category mismatch, got %d", rec1.Code)
		}

		// Associate hero with category
		_, err = pool.Exec(ctx, "INSERT INTO hero_categories (hero_id, beneficiary_category_id) VALUES ($1, $2)", heroID, catID)
		if err != nil {
			t.Fatalf("failed to insert hero_categories: %v", err)
		}

		// Hero now belongs to category -> 201 Created
		req2 := httptest.NewRequest(http.MethodPost, "/api/v1/donations/"+donationID.String()+"/claim", nil)
		req2.SetPathValue("id", donationID.String())
		req2 = req2.WithContext(middleware.WithClaims(req2.Context(), claims))
		rec2 := httptest.NewRecorder()
		h.ClaimDonationHandler(rec2, req2)
		if rec2.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created after category enrollment, got %d: %s", rec2.Code, rec2.Body.String())
		}
	})

	t.Run("5. Cancellation within 15m grace period avoids late penalty", func(t *testing.T) {
		heroID := createTestHero(t, pool, "hero5@hero.war.test", "Hero Five")
		// Event is in 6 hours (inside 12h window)
		donationID, _ := createTestDonation(t, pool, 10, 5, 5, "open", time.Now().Add(6*time.Hour))
		claims := &auth.Claims{UserID: heroID, Email: "hero5@hero.war.test", IsHero: true}

		// Claim slot
		reqClaim := httptest.NewRequest(http.MethodPost, "/api/v1/donations/"+donationID.String()+"/claim", nil)
		reqClaim.SetPathValue("id", donationID.String())
		reqClaim = reqClaim.WithContext(middleware.WithClaims(reqClaim.Context(), claims))
		recClaim := httptest.NewRecorder()
		h.ClaimDonationHandler(recClaim, reqClaim)
		if recClaim.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created, got %d", recClaim.Code)
		}

		// Cancel immediately (within 15m of claim creation)
		reqCancel := httptest.NewRequest(http.MethodPost, "/api/v1/donations/"+donationID.String()+"/cancel-claim", nil)
		reqCancel.SetPathValue("id", donationID.String())
		reqCancel = reqCancel.WithContext(middleware.WithClaims(reqCancel.Context(), claims))
		recCancel := httptest.NewRecorder()
		h.CancelClaimHandler(recCancel, reqCancel)

		if recCancel.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on cancel, got %d: %s", recCancel.Code, recCancel.Body.String())
		}

		var resp map[string]any
		_ = json.Unmarshal(recCancel.Body.Bytes(), &resp)
		data := resp["data"].(map[string]any)
		if data["cancelled_late"] != false {
			t.Errorf("expected cancelled_late=false due to 15m grace period, got %v", data["cancelled_late"])
		}

		// Ensure no ban record was inserted
		var banCount int
		_ = pool.QueryRow(ctx, "SELECT COUNT(*) FROM user_bans WHERE user_id = $1", heroID).Scan(&banCount)
		if banCount != 0 {
			t.Errorf("expected 0 user_bans, got %d", banCount)
		}

		// Ensure remain was incremented back
		var remain int32
		_ = pool.QueryRow(ctx, "SELECT remain FROM donations WHERE id = $1", donationID).Scan(&remain)
		if remain != 5 {
			t.Errorf("expected remain=5, got %d", remain)
		}
	})

	t.Run("6. Late cancellation (>15m after claim, <12h to event) creates ban and blocks subsequent claims", func(t *testing.T) {
		heroID := createTestHero(t, pool, "hero6@hero.war.test", "Hero Six")
		donationID, _ := createTestDonation(t, pool, 10, 5, 5, "open", time.Now().Add(6*time.Hour))
		claims := &auth.Claims{UserID: heroID, Email: "hero6@hero.war.test", IsHero: true}

		// Claim slot
		reqClaim := httptest.NewRequest(http.MethodPost, "/api/v1/donations/"+donationID.String()+"/claim", nil)
		reqClaim.SetPathValue("id", donationID.String())
		reqClaim = reqClaim.WithContext(middleware.WithClaims(reqClaim.Context(), claims))
		recClaim := httptest.NewRecorder()
		h.ClaimDonationHandler(recClaim, reqClaim)
		if recClaim.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created, got %d", recClaim.Code)
		}

		// Backdate attendance created_at to 30 minutes ago (outside grace period)
		_, err := pool.Exec(ctx, "UPDATE hero_attendances SET created_at = NOW() - INTERVAL '30 minutes' WHERE donation_id = $1 AND hero_id = $2", donationID, heroID)
		if err != nil {
			t.Fatalf("failed to backdate attendance: %v", err)
		}

		// Cancel claim -> late penalty fires!
		reqCancel := httptest.NewRequest(http.MethodPost, "/api/v1/donations/"+donationID.String()+"/cancel-claim", nil)
		reqCancel.SetPathValue("id", donationID.String())
		reqCancel = reqCancel.WithContext(middleware.WithClaims(reqCancel.Context(), claims))
		recCancel := httptest.NewRecorder()
		h.CancelClaimHandler(recCancel, reqCancel)

		if recCancel.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on cancel, got %d: %s", recCancel.Code, recCancel.Body.String())
		}

		var resp map[string]any
		_ = json.Unmarshal(recCancel.Body.Bytes(), &resp)
		data := resp["data"].(map[string]any)
		if data["cancelled_late"] != true {
			t.Errorf("expected cancelled_late=true, got %v", data["cancelled_late"])
		}

		// Verify ban record created
		var banCount int
		_ = pool.QueryRow(ctx, "SELECT COUNT(*) FROM user_bans WHERE user_id = $1 AND scope = 'hero_war' AND lifted_at IS NULL", heroID).Scan(&banCount)
		if banCount != 1 {
			t.Fatalf("expected 1 active user_ban, got %d", banCount)
		}

		// Attempt to claim another event -> 403 Forbidden
		nextDonationID, _ := createTestDonation(t, pool, 10, 5, 5, "open", time.Now().Add(48*time.Hour))
		reqNext := httptest.NewRequest(http.MethodPost, "/api/v1/donations/"+nextDonationID.String()+"/claim", nil)
		reqNext.SetPathValue("id", nextDonationID.String())
		reqNext = reqNext.WithContext(middleware.WithClaims(reqNext.Context(), claims))
		recNext := httptest.NewRecorder()
		h.ClaimDonationHandler(recNext, reqNext)

		if recNext.Code != http.StatusForbidden {
			t.Fatalf("expected 403 Forbidden while banned, got %d: %s", recNext.Code, recNext.Body.String())
		}
		if !strings.Contains(recNext.Body.String(), "WAR_002") {
			t.Fatalf("expected WAR_002 error code while banned, got %s", recNext.Body.String())
		}

		// Complete 2 subsequent eligible donations (baseline is event_at)
		don1, _ := createTestDonation(t, pool, 10, 5, 5, "completed", time.Now().Add(12*time.Hour))
		don2, _ := createTestDonation(t, pool, 10, 5, 5, "completed", time.Now().Add(24*time.Hour))
		defer func() {
			_, _ = pool.Exec(ctx, "DELETE FROM donations WHERE id IN ($1, $2)", don1, don2)
		}()

		// Now 2 eligible events have completed after the late-cancelled event -> Ban is dynamically satisfied!
		reqPostBan := httptest.NewRequest(http.MethodPost, "/api/v1/donations/"+nextDonationID.String()+"/claim", nil)
		reqPostBan.SetPathValue("id", nextDonationID.String())
		reqPostBan = reqPostBan.WithContext(middleware.WithClaims(reqPostBan.Context(), claims))
		recPostBan := httptest.NewRecorder()
		h.ClaimDonationHandler(recPostBan, reqPostBan)

		if recPostBan.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created after ban served, got %d: %s", recPostBan.Code, recPostBan.Body.String())
		}
	})

	t.Run("7. List Hero Reservations", func(t *testing.T) {
		heroID := createTestHero(t, pool, "hero7@hero.war.test", "Hero Seven")
		donationID, _ := createTestDonation(t, pool, 10, 5, 5, "open", time.Now().Add(24*time.Hour))
		claims := &auth.Claims{UserID: heroID, Email: "hero7@hero.war.test", IsHero: true}

		// Claim
		reqClaim := httptest.NewRequest(http.MethodPost, "/api/v1/donations/"+donationID.String()+"/claim", nil)
		reqClaim.SetPathValue("id", donationID.String())
		reqClaim = reqClaim.WithContext(middleware.WithClaims(reqClaim.Context(), claims))
		recClaim := httptest.NewRecorder()
		h.ClaimDonationHandler(recClaim, reqClaim)
		if recClaim.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d", recClaim.Code)
		}

		// List
		reqList := httptest.NewRequest(http.MethodGet, "/api/v1/heroes/me/reservations", nil)
		reqList = reqList.WithContext(middleware.WithClaims(reqList.Context(), claims))
		recList := httptest.NewRecorder()
		h.ListReservationsHandler(recList, reqList)

		if recList.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", recList.Code, recList.Body.String())
		}

		var resp map[string]any
		if err := json.Unmarshal(recList.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal list reservations response: %v", err)
		}
		reservations := resp["data"].([]any)
		if len(reservations) != 1 {
			t.Fatalf("expected 1 reservation, got %d", len(reservations))
		}
		meta, ok := resp["meta"].(map[string]any)
		if !ok {
			t.Fatalf("expected meta object in paginated response, got %T", resp["meta"])
		}
		if meta["page"] != float64(1) || meta["limit"] != float64(20) || meta["total"] != float64(1) || meta["total_pages"] != float64(1) {
			t.Errorf("unexpected meta for single reservation: %v", meta)
		}

		// Verify snake_case DTO shape and explicit nulls (no raw dbgen PascalCase or pgtype leakage)
		item, ok := reservations[0].(map[string]any)
		if !ok {
			t.Fatalf("expected reservation object, got %T", reservations[0])
		}
		for _, requiredKey := range []string{"id", "donation_id", "hero_id", "status", "cancelled_late", "created_at", "updated_at", "event_at", "address", "donation_status", "sponsor_name"} {
			if _, exists := item[requiredKey]; !exists {
				t.Errorf("expected snake_case key %q in HeroReservationResponse", requiredKey)
			}
		}
		for _, forbiddenKey := range []string{"ID", "Valid", "DonationStatus", "SponsorName"} {
			if _, exists := item[forbiddenKey]; exists {
				t.Errorf("unexpected raw dbgen key %q in HeroReservationResponse", forbiddenKey)
			}
		}
		for _, nullableKey := range []string{"cancelled_at", "attended_at", "notes", "maps", "message"} {
			val, exists := item[nullableKey]
			if !exists {
				t.Errorf("expected nullable key %q to be explicitly emitted", nullableKey)
			} else if val != nil {
				t.Errorf("expected nullable key %q to be null, got %v", nullableKey, val)
			}
		}

		// Empty list for a hero with 0 reservations must serialize as [] (not null)
		emptyHeroID := createTestHero(t, pool, "hero7_empty@hero.war.test", "Hero Seven Empty")
		emptyClaims := &auth.Claims{UserID: emptyHeroID, Email: "hero7_empty@hero.war.test", IsHero: true}
		reqEmpty := httptest.NewRequest(http.MethodGet, "/api/v1/heroes/me/reservations", nil)
		reqEmpty = reqEmpty.WithContext(middleware.WithClaims(reqEmpty.Context(), emptyClaims))
		recEmpty := httptest.NewRecorder()
		h.ListReservationsHandler(recEmpty, reqEmpty)

		if recEmpty.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on empty reservations, got %d: %s", recEmpty.Code, recEmpty.Body.String())
		}
		var emptyResp map[string]any
		if err := json.Unmarshal(recEmpty.Body.Bytes(), &emptyResp); err != nil {
			t.Fatalf("failed to unmarshal empty reservations response: %v", err)
		}
		emptyArr, ok := emptyResp["data"].([]any)
		if !ok {
			t.Fatalf("expected non-nil []any for empty reservations, got %T (%v)", emptyResp["data"], emptyResp["data"])
		}
		if len(emptyArr) != 0 {
			t.Errorf("expected 0 reservations, got %d", len(emptyArr))
		}
		emptyMeta, ok := emptyResp["meta"].(map[string]any)
		if !ok {
			t.Fatalf("expected meta object in empty response, got %T", emptyResp["meta"])
		}
		if emptyMeta["page"] != float64(1) || emptyMeta["limit"] != float64(20) || emptyMeta["total"] != float64(0) || emptyMeta["total_pages"] != float64(0) {
			t.Errorf("unexpected meta for empty reservations: %v", emptyMeta)
		}
	})

	t.Run("8. Cancel claim on cancelled donation skips late penalty", func(t *testing.T) {
		heroID := createTestHero(t, pool, "hero8_cancel@hero.war.test", "Hero Eight")
		donationID, _ := createTestDonation(t, pool, 10, 5, 5, "open", time.Now().Add(2*time.Hour))
		claims := &auth.Claims{UserID: heroID, Email: "hero8_cancel@hero.war.test", IsHero: true}

		// Claim slot
		reqClaim := httptest.NewRequest(http.MethodPost, "/api/v1/donations/"+donationID.String()+"/claim", nil)
		reqClaim.SetPathValue("id", donationID.String())
		reqClaim = reqClaim.WithContext(middleware.WithClaims(reqClaim.Context(), claims))
		recClaim := httptest.NewRecorder()
		h.ClaimDonationHandler(recClaim, reqClaim)
		if recClaim.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created on claim, got %d", recClaim.Code)
		}

		// Backdate attendance created_at to 30m ago (outside 15m grace period)
		_, _ = pool.Exec(ctx, "UPDATE hero_attendances SET created_at = NOW() - INTERVAL '30 minutes' WHERE donation_id = $1 AND hero_id = $2", donationID, heroID)

		// Staff cancels the donation
		_, _ = pool.Exec(ctx, "UPDATE donations SET status = 'cancelled' WHERE id = $1", donationID)

		// Hero cancels their reservation
		reqCancel := httptest.NewRequest(http.MethodPost, "/api/v1/donations/"+donationID.String()+"/cancel-claim", nil)
		reqCancel.SetPathValue("id", donationID.String())
		reqCancel = reqCancel.WithContext(middleware.WithClaims(reqCancel.Context(), claims))
		recCancel := httptest.NewRecorder()
		h.CancelClaimHandler(recCancel, reqCancel)

		if recCancel.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on cancel claim for cancelled donation, got %d: %s", recCancel.Code, recCancel.Body.String())
		}

		var resp map[string]any
		_ = json.Unmarshal(recCancel.Body.Bytes(), &resp)
		data := resp["data"].(map[string]any)
		if data["cancelled_late"] != false {
			t.Errorf("expected cancelled_late=false for cancelled donation, got %v", data["cancelled_late"])
		}

		var banCount int
		_ = pool.QueryRow(ctx, "SELECT COUNT(*) FROM user_bans WHERE user_id = $1 AND scope = 'hero_war' AND lifted_at IS NULL", heroID).Scan(&banCount)
		if banCount != 0 {
			t.Errorf("expected 0 user_bans created, got %d", banCount)
		}
	})

	t.Run("9. Retroactive auto-lift of existing bans when staff cancel donation", func(t *testing.T) {
		heroID := createTestHero(t, pool, "hero9_retro@hero.war.test", "Hero Nine")
		donationID, _ := createTestDonation(t, pool, 10, 5, 5, "open", time.Now().Add(2*time.Hour))
		claims := &auth.Claims{UserID: heroID, Email: "hero9_retro@hero.war.test", IsHero: true}

		// Claim slot
		reqClaim := httptest.NewRequest(http.MethodPost, "/api/v1/donations/"+donationID.String()+"/claim", nil)
		reqClaim.SetPathValue("id", donationID.String())
		reqClaim = reqClaim.WithContext(middleware.WithClaims(reqClaim.Context(), claims))
		recClaim := httptest.NewRecorder()
		h.ClaimDonationHandler(recClaim, reqClaim)
		if recClaim.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created on claim, got %d", recClaim.Code)
		}

		// Backdate attendance to 30m ago
		_, _ = pool.Exec(ctx, "UPDATE hero_attendances SET created_at = NOW() - INTERVAL '30 minutes' WHERE donation_id = $1 AND hero_id = $2", donationID, heroID)

		// Hero late-cancels claim -> ban created
		reqCancel := httptest.NewRequest(http.MethodPost, "/api/v1/donations/"+donationID.String()+"/cancel-claim", nil)
		reqCancel.SetPathValue("id", donationID.String())
		reqCancel = reqCancel.WithContext(middleware.WithClaims(reqCancel.Context(), claims))
		recCancel := httptest.NewRecorder()
		h.CancelClaimHandler(recCancel, reqCancel)

		if recCancel.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on cancel claim, got %d", recCancel.Code)
		}

		var activeBans int
		_ = pool.QueryRow(ctx, "SELECT COUNT(*) FROM user_bans WHERE user_id = $1 AND donation_id = $2 AND lifted_at IS NULL", heroID, donationID).Scan(&activeBans)
		if activeBans != 1 {
			t.Fatalf("expected 1 active ban before donation cancellation, got %d", activeBans)
		}

		// Staff cancels donation via CancelDonationHandler
		donHandler := NewDonationHandler(pool, &cfg)
		reqCancelDon := httptest.NewRequest(http.MethodPost, "/api/v1/donations/"+donationID.String()+"/cancel", nil)
		reqCancelDon.SetPathValue("id", donationID.String())
		recCancelDon := httptest.NewRecorder()
		donHandler.CancelDonationHandler(recCancelDon, reqCancelDon)

		if recCancelDon.Code != http.StatusOK {
			t.Fatalf("expected 200 OK cancelling donation, got %d: %s", recCancelDon.Code, recCancelDon.Body.String())
		}

		// Verify existing ban was retroactively auto-lifted!
		var liftedNote *string
		var liftedAt *time.Time
		err := pool.QueryRow(ctx, "SELECT note, lifted_at FROM user_bans WHERE user_id = $1 AND donation_id = $2", heroID, donationID).Scan(&liftedNote, &liftedAt)
		if err != nil {
			t.Fatalf("failed to query ban: %v", err)
		}
		if liftedAt == nil {
			t.Errorf("expected ban to be auto-lifted, but lifted_at is nil")
		}
		if liftedNote == nil || *liftedNote != "auto-lifted: donation cancelled by staff" {
			t.Errorf("expected auto-lifted note, got %v", liftedNote)
		}
	})

	t.Run("10. Anti-churn grace period exhaustion on repeated cancellation", func(t *testing.T) {
		// Event in 2h (inside 12h late window)
		donationID, _ := createTestDonation(t, pool, 10, 5, 5, "open", time.Now().Add(2*time.Hour))
		heroID := createTestHero(t, pool, "churn_hero@hero.war.test", "Churn Hero")
		claims := &auth.Claims{UserID: heroID, Email: "churn_hero@hero.war.test", IsHero: true}

		// First claim
		reqClaim1 := httptest.NewRequest(http.MethodPost, "/api/v1/donations/"+donationID.String()+"/claim", nil)
		reqClaim1.SetPathValue("id", donationID.String())
		reqClaim1 = reqClaim1.WithContext(middleware.WithClaims(reqClaim1.Context(), claims))
		recClaim1 := httptest.NewRecorder()
		h.ClaimDonationHandler(recClaim1, reqClaim1)
		if recClaim1.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created on first claim, got %d", recClaim1.Code)
		}

		// First cancellation: within 15m grace period -> penalty waived (cancelled_late=false)
		reqCancel1 := httptest.NewRequest(http.MethodPost, "/api/v1/donations/"+donationID.String()+"/cancel-claim", nil)
		reqCancel1.SetPathValue("id", donationID.String())
		reqCancel1 = reqCancel1.WithContext(middleware.WithClaims(reqCancel1.Context(), claims))
		recCancel1 := httptest.NewRecorder()
		h.CancelClaimHandler(recCancel1, reqCancel1)
		if recCancel1.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on first cancel, got %d", recCancel1.Code)
		}
		var resp1 map[string]any
		_ = json.Unmarshal(recCancel1.Body.Bytes(), &resp1)
		data1 := resp1["data"].(map[string]any)
		if data1["cancelled_late"].(bool) != false {
			t.Fatalf("expected cancelled_late=false on first cancel within grace period")
		}

		// Re-claim the same donation
		reqClaim2 := httptest.NewRequest(http.MethodPost, "/api/v1/donations/"+donationID.String()+"/claim", nil)
		reqClaim2.SetPathValue("id", donationID.String())
		reqClaim2 = reqClaim2.WithContext(middleware.WithClaims(reqClaim2.Context(), claims))
		recClaim2 := httptest.NewRecorder()
		h.ClaimDonationHandler(recClaim2, reqClaim2)
		if recClaim2.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created on re-claim, got %d", recClaim2.Code)
		}

		// Second cancellation: even though done immediately (<15m of new row), grace period is WAIVED!
		reqCancel2 := httptest.NewRequest(http.MethodPost, "/api/v1/donations/"+donationID.String()+"/cancel-claim", nil)
		reqCancel2.SetPathValue("id", donationID.String())
		reqCancel2 = reqCancel2.WithContext(middleware.WithClaims(reqCancel2.Context(), claims))
		recCancel2 := httptest.NewRecorder()
		h.CancelClaimHandler(recCancel2, reqCancel2)
		if recCancel2.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on second cancel, got %d", recCancel2.Code)
		}
		var resp2 map[string]any
		_ = json.Unmarshal(recCancel2.Body.Bytes(), &resp2)
		data2 := resp2["data"].(map[string]any)
		if data2["cancelled_late"].(bool) != true {
			t.Fatalf("expected cancelled_late=true on re-cancel due to anti-churn grace waiver")
		}

		// Verify ban was created
		var bans int
		_ = pool.QueryRow(ctx, "SELECT COUNT(*) FROM user_bans WHERE user_id = $1 AND donation_id = $2 AND lifted_at IS NULL", heroID, donationID).Scan(&bans)
		if bans != 1 {
			t.Errorf("expected 1 active ban created on second churn cancellation, got %d", bans)
		}
	})

	t.Run("11. Custom HeroCancelGracePeriod config override governs penalty exemption", func(t *testing.T) {
		donationID, _ := createTestDonation(t, pool, 10, 5, 5, "open", time.Now().Add(6*time.Hour))

		// Case A: Widened 30m grace period exempts a claim cancelled 20m after creation
		wideCfg := cfg
		wideCfg.War.HeroCancelGracePeriod = 30 * time.Minute
		hWide := NewHeroWarHandler(pool, &wideCfg)

		heroWide := createTestHero(t, pool, "grace_wide@hero.war.test", "Grace Wide Hero")
		claimsWide := &auth.Claims{UserID: heroWide, Email: "grace_wide@hero.war.test", IsHero: true}

		reqClaimWide := httptest.NewRequest(http.MethodPost, "/api/v1/donations/"+donationID.String()+"/claim", nil)
		reqClaimWide.SetPathValue("id", donationID.String())
		reqClaimWide = reqClaimWide.WithContext(middleware.WithClaims(reqClaimWide.Context(), claimsWide))
		recClaimWide := httptest.NewRecorder()
		hWide.ClaimDonationHandler(recClaimWide, reqClaimWide)
		if recClaimWide.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created, got %d", recClaimWide.Code)
		}

		_, err := pool.Exec(ctx, "UPDATE hero_attendances SET created_at = NOW() - INTERVAL '20 minutes' WHERE donation_id = $1 AND hero_id = $2", donationID, heroWide)
		if err != nil {
			t.Fatalf("failed to backdate attendance: %v", err)
		}

		reqCancelWide := httptest.NewRequest(http.MethodPost, "/api/v1/donations/"+donationID.String()+"/cancel-claim", nil)
		reqCancelWide.SetPathValue("id", donationID.String())
		reqCancelWide = reqCancelWide.WithContext(middleware.WithClaims(reqCancelWide.Context(), claimsWide))
		recCancelWide := httptest.NewRecorder()
		hWide.CancelClaimHandler(recCancelWide, reqCancelWide)
		if recCancelWide.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", recCancelWide.Code, recCancelWide.Body.String())
		}
		var wideResp map[string]any
		_ = json.Unmarshal(recCancelWide.Body.Bytes(), &wideResp)
		if wideResp["data"].(map[string]any)["cancelled_late"] != false {
			t.Errorf("expected cancelled_late=false with 30m grace period override at 20m, got %v", wideResp["data"])
		}

		// Case B: Tightened 5m grace period penalizes a claim cancelled 10m after creation
		tightCfg := cfg
		tightCfg.War.HeroCancelGracePeriod = 5 * time.Minute
		hTight := NewHeroWarHandler(pool, &tightCfg)

		heroTight := createTestHero(t, pool, "grace_tight@hero.war.test", "Grace Tight Hero")
		claimsTight := &auth.Claims{UserID: heroTight, Email: "grace_tight@hero.war.test", IsHero: true}

		reqClaimTight := httptest.NewRequest(http.MethodPost, "/api/v1/donations/"+donationID.String()+"/claim", nil)
		reqClaimTight.SetPathValue("id", donationID.String())
		reqClaimTight = reqClaimTight.WithContext(middleware.WithClaims(reqClaimTight.Context(), claimsTight))
		recClaimTight := httptest.NewRecorder()
		hTight.ClaimDonationHandler(recClaimTight, reqClaimTight)
		if recClaimTight.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created, got %d", recClaimTight.Code)
		}

		_, err = pool.Exec(ctx, "UPDATE hero_attendances SET created_at = NOW() - INTERVAL '10 minutes' WHERE donation_id = $1 AND hero_id = $2", donationID, heroTight)
		if err != nil {
			t.Fatalf("failed to backdate attendance: %v", err)
		}

		reqCancelTight := httptest.NewRequest(http.MethodPost, "/api/v1/donations/"+donationID.String()+"/cancel-claim", nil)
		reqCancelTight.SetPathValue("id", donationID.String())
		reqCancelTight = reqCancelTight.WithContext(middleware.WithClaims(reqCancelTight.Context(), claimsTight))
		recCancelTight := httptest.NewRecorder()
		hTight.CancelClaimHandler(recCancelTight, reqCancelTight)
		if recCancelTight.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", recCancelTight.Code, recCancelTight.Body.String())
		}
		var tightResp map[string]any
		_ = json.Unmarshal(recCancelTight.Body.Bytes(), &tightResp)
		if tightResp["data"].(map[string]any)["cancelled_late"] != true {
			t.Errorf("expected cancelled_late=true with 5m grace period override at 10m, got %v", tightResp["data"])
		}
	})

	t.Run("12. List Hero Reservations Pagination & Status Filter", func(t *testing.T) {
		heroID := createTestHero(t, pool, "hero12@hero.war.test", "Hero Twelve")
		claims := &auth.Claims{UserID: heroID, Email: "hero12@hero.war.test", IsHero: true}

		don1, _ := createTestDonation(t, pool, 10, 5, 5, "open", time.Now().Add(24*time.Hour))
		don2, _ := createTestDonation(t, pool, 10, 5, 5, "open", time.Now().Add(48*time.Hour))
		don3, _ := createTestDonation(t, pool, 10, 5, 5, "open", time.Now().Add(72*time.Hour))

		for _, dID := range []uuid.UUID{don1, don2, don3} {
			reqClaim := httptest.NewRequest(http.MethodPost, "/api/v1/donations/"+dID.String()+"/claim", nil)
			reqClaim.SetPathValue("id", dID.String())
			reqClaim = reqClaim.WithContext(middleware.WithClaims(reqClaim.Context(), claims))
			recClaim := httptest.NewRecorder()
			h.ClaimDonationHandler(recClaim, reqClaim)
			if recClaim.Code != http.StatusCreated {
				t.Fatalf("expected 201 claiming %s, got %d: %s", dID, recClaim.Code, recClaim.Body.String())
			}
		}

		// Cancel middle reservation (don2) so we have 2 registered + 1 cancelled
		reqCancel := httptest.NewRequest(http.MethodPost, "/api/v1/donations/"+don2.String()+"/cancel-claim", nil)
		reqCancel.SetPathValue("id", don2.String())
		reqCancel = reqCancel.WithContext(middleware.WithClaims(reqCancel.Context(), claims))
		recCancel := httptest.NewRecorder()
		h.CancelClaimHandler(recCancel, reqCancel)
		if recCancel.Code != http.StatusOK {
			t.Fatalf("expected 200 cancelling %s, got %d: %s", don2, recCancel.Code, recCancel.Body.String())
		}

		// Page 1, limit 2 -> don3, don2 (ordered by d.event_at DESC)
		reqP1 := httptest.NewRequest(http.MethodGet, "/api/v1/heroes/me/reservations?page=1&limit=2", nil)
		reqP1 = reqP1.WithContext(middleware.WithClaims(reqP1.Context(), claims))
		recP1 := httptest.NewRecorder()
		h.ListReservationsHandler(recP1, reqP1)
		if recP1.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on page 1, got %d: %s", recP1.Code, recP1.Body.String())
		}
		var respP1 map[string]any
		_ = json.Unmarshal(recP1.Body.Bytes(), &respP1)
		dataP1 := respP1["data"].([]any)
		metaP1 := respP1["meta"].(map[string]any)
		if len(dataP1) != 2 {
			t.Fatalf("expected 2 reservations on page 1, got %d", len(dataP1))
		}
		if dataP1[0].(map[string]any)["donation_id"] != don3.String() || dataP1[1].(map[string]any)["donation_id"] != don2.String() {
			t.Errorf("unexpected page 1 ordering: got [%v, %v], want [%v, %v]",
				dataP1[0].(map[string]any)["donation_id"], dataP1[1].(map[string]any)["donation_id"], don3, don2)
		}
		if metaP1["page"] != float64(1) || metaP1["limit"] != float64(2) || metaP1["total"] != float64(3) || metaP1["total_pages"] != float64(2) {
			t.Errorf("unexpected page 1 meta: %v", metaP1)
		}

		// Page 2, limit 2 -> don1
		reqP2 := httptest.NewRequest(http.MethodGet, "/api/v1/heroes/me/reservations?page=2&limit=2", nil)
		reqP2 = reqP2.WithContext(middleware.WithClaims(reqP2.Context(), claims))
		recP2 := httptest.NewRecorder()
		h.ListReservationsHandler(recP2, reqP2)
		if recP2.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on page 2, got %d: %s", recP2.Code, recP2.Body.String())
		}
		var respP2 map[string]any
		_ = json.Unmarshal(recP2.Body.Bytes(), &respP2)
		dataP2 := respP2["data"].([]any)
		metaP2 := respP2["meta"].(map[string]any)
		if len(dataP2) != 1 || dataP2[0].(map[string]any)["donation_id"] != don1.String() {
			t.Errorf("expected [don1] on page 2, got %v", dataP2)
		}
		if metaP2["page"] != float64(2) || metaP2["limit"] != float64(2) || metaP2["total"] != float64(3) || metaP2["total_pages"] != float64(2) {
			t.Errorf("unexpected page 2 meta: %v", metaP2)
		}

		// Status filter: registered (2 items)
		reqReg := httptest.NewRequest(http.MethodGet, "/api/v1/heroes/me/reservations?status=registered", nil)
		reqReg = reqReg.WithContext(middleware.WithClaims(reqReg.Context(), claims))
		recReg := httptest.NewRecorder()
		h.ListReservationsHandler(recReg, reqReg)
		if recReg.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on status=registered, got %d", recReg.Code)
		}
		var respReg map[string]any
		_ = json.Unmarshal(recReg.Body.Bytes(), &respReg)
		if len(respReg["data"].([]any)) != 2 || respReg["meta"].(map[string]any)["total"] != float64(2) {
			t.Errorf("expected 2 registered reservations, got data=%d meta=%v", len(respReg["data"].([]any)), respReg["meta"])
		}

		// Status filter: cancelled (1 item)
		reqCancelled := httptest.NewRequest(http.MethodGet, "/api/v1/heroes/me/reservations?status=cancelled", nil)
		reqCancelled = reqCancelled.WithContext(middleware.WithClaims(reqCancelled.Context(), claims))
		recCancelled := httptest.NewRecorder()
		h.ListReservationsHandler(recCancelled, reqCancelled)
		if recCancelled.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on status=cancelled, got %d", recCancelled.Code)
		}
		var respCancelled map[string]any
		_ = json.Unmarshal(recCancelled.Body.Bytes(), &respCancelled)
		cancelledData := respCancelled["data"].([]any)
		if len(cancelledData) != 1 || cancelledData[0].(map[string]any)["donation_id"] != don2.String() {
			t.Errorf("expected 1 cancelled reservation (%s), got %v", don2, cancelledData)
		}

		// Invalid status filter -> 400 VAL_001
		reqBad := httptest.NewRequest(http.MethodGet, "/api/v1/heroes/me/reservations?status=bogus", nil)
		reqBad = reqBad.WithContext(middleware.WithClaims(reqBad.Context(), claims))
		recBad := httptest.NewRecorder()
		h.ListReservationsHandler(recBad, reqBad)
		if recBad.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request on invalid status, got %d", recBad.Code)
		}
		var respBad map[string]any
		_ = json.Unmarshal(recBad.Body.Bytes(), &respBad)
		if respBad["code"] != "VAL_001" {
			t.Errorf("expected VAL_001 code, got %v", respBad["code"])
		}
	})

	t.Run("13. Reject CancelClaim after attendance is marked attended (400 VAL_002)", func(t *testing.T) {
		heroID := createTestHero(t, pool, "hero13_attended@hero.war.test", "Hero Thirteen")
		donationID, _ := createTestDonation(t, pool, 10, 5, 5, "open", time.Now().Add(30*time.Minute))
		claims := &auth.Claims{UserID: heroID, Email: "hero13_attended@hero.war.test", IsHero: true}

		reqClaim := httptest.NewRequest(http.MethodPost, "/api/v1/donations/"+donationID.String()+"/claim", nil)
		reqClaim.SetPathValue("id", donationID.String())
		reqClaim = reqClaim.WithContext(middleware.WithClaims(reqClaim.Context(), claims))
		recClaim := httptest.NewRecorder()
		h.ClaimDonationHandler(recClaim, reqClaim)
		if recClaim.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created, got %d: %s", recClaim.Code, recClaim.Body.String())
		}

		// Mark attendance as attended prior to event_at (simulating pre-event check-in window)
		if _, err := pool.Exec(ctx, "UPDATE hero_attendances SET status = 'attended', attended_at = NOW() WHERE donation_id = $1 AND hero_id = $2", donationID, heroID); err != nil {
			t.Fatalf("failed to mark attendance attended: %v", err)
		}

		reqCancel := httptest.NewRequest(http.MethodPost, "/api/v1/donations/"+donationID.String()+"/cancel-claim", nil)
		reqCancel.SetPathValue("id", donationID.String())
		reqCancel = reqCancel.WithContext(middleware.WithClaims(reqCancel.Context(), claims))
		recCancel := httptest.NewRecorder()
		h.CancelClaimHandler(recCancel, reqCancel)

		if recCancel.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request cancelling attended reservation, got %d: %s", recCancel.Code, recCancel.Body.String())
		}
		var errResp map[string]any
		_ = json.Unmarshal(recCancel.Body.Bytes(), &errResp)
		if errResp["code"] != "VAL_002" {
			t.Errorf("expected VAL_002 error code, got %v", errResp["code"])
		}

		var remain int32
		_ = pool.QueryRow(ctx, "SELECT remain FROM donations WHERE id = $1", donationID).Scan(&remain)
		if remain != 4 {
			t.Errorf("expected donation remain to stay 4, got %d", remain)
		}
		var status string
		_ = pool.QueryRow(ctx, "SELECT status FROM hero_attendances WHERE donation_id = $1 AND hero_id = $2", donationID, heroID).Scan(&status)
		if status != "attended" {
			t.Errorf("expected attendance status to stay 'attended', got %q", status)
		}
	})
}
