package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/berbagibitesjogja/core-system/internal/config"
	"github.com/berbagibitesjogja/core-system/internal/response"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestValidateCreateDonationRequest(t *testing.T) {
	validSponsorID := uuid.NewString()
	validEventAt := time.Now().Add(24 * time.Hour).Format(time.RFC3339)
	validCategoryID := uuid.NewString()

	t.Run("Valid request passes validation", func(t *testing.T) {
		heroQuota := 10
		req := CreateDonationRequest{
			SponsorID: validSponsorID,
			EventAt:   validEventAt,
			Quota:     20,
			HeroQuota: &heroQuota,
			Foods: []FoodItemInput{
				{FoodItemID: 1, Quantity: 5.5, Unit: "kg"},
				{FoodItemID: 2, Quantity: 10, Unit: "portion"},
			},
			BeneficiaryCategoryIDs: []string{validCategoryID},
		}

		sponsorID, eventTime, quota, hQuota, hidePub, hideSpon, errs := ValidateCreateDonationRequest(req)
		if len(errs) > 0 {
			t.Fatalf("expected no validation errors, got: %+v", errs)
		}
		if sponsorID.String() != validSponsorID {
			t.Errorf("expected sponsorID %s, got %s", validSponsorID, sponsorID.String())
		}
		if eventTime.IsZero() {
			t.Error("expected non-zero eventTime")
		}
		if quota != 20 || hQuota != 10 {
			t.Errorf("expected quota 20 heroQuota 10, got %d %d", quota, hQuota)
		}
		if !hidePub {
			t.Error("expected hideFromPublic default true")
		}
		if hideSpon {
			t.Error("expected hideSponsor default false")
		}
	})

	t.Run("Empty sponsor ID rejected", func(t *testing.T) {
		req := CreateDonationRequest{
			SponsorID:              "",
			EventAt:                validEventAt,
			Quota:                  20,
			Foods:                  []FoodItemInput{{FoodItemID: 1, Quantity: 1, Unit: "kg"}},
			BeneficiaryCategoryIDs: []string{validCategoryID},
		}
		_, _, _, _, _, _, errs := ValidateCreateDonationRequest(req)
		if errs["sponsor_id"] != "Sponsor ID is required" {
			t.Errorf("expected 'Sponsor ID is required', got %s", errs["sponsor_id"])
		}
	})

	t.Run("Invalid sponsor UUID format rejected", func(t *testing.T) {
		req := CreateDonationRequest{
			SponsorID:              "not-a-uuid",
			EventAt:                validEventAt,
			Quota:                  20,
			Foods:                  []FoodItemInput{{FoodItemID: 1, Quantity: 1, Unit: "kg"}},
			BeneficiaryCategoryIDs: []string{validCategoryID},
		}
		_, _, _, _, _, _, errs := ValidateCreateDonationRequest(req)
		if errs["sponsor_id"] != "Invalid sponsor ID format" {
			t.Errorf("expected 'Invalid sponsor ID format', got %s", errs["sponsor_id"])
		}
	})

	t.Run("Invalid event datetime format rejected", func(t *testing.T) {
		req := CreateDonationRequest{
			SponsorID:              validSponsorID,
			EventAt:                "2026-11-12 12:00:00",
			Quota:                  20,
			Foods:                  []FoodItemInput{{FoodItemID: 1, Quantity: 1, Unit: "kg"}},
			BeneficiaryCategoryIDs: []string{validCategoryID},
		}
		_, _, _, _, _, _, errs := ValidateCreateDonationRequest(req)
		if errs["event_at"] != "Event datetime must be in valid RFC3339 format" {
			t.Errorf("expected RFC3339 error, got %s", errs["event_at"])
		}
	})

	t.Run("Quota zero or negative rejected", func(t *testing.T) {
		req := CreateDonationRequest{
			SponsorID:              validSponsorID,
			EventAt:                validEventAt,
			Quota:                  0,
			Foods:                  []FoodItemInput{{FoodItemID: 1, Quantity: 1, Unit: "kg"}},
			BeneficiaryCategoryIDs: []string{validCategoryID},
		}
		_, _, _, _, _, _, errs := ValidateCreateDonationRequest(req)
		if errs["quota"] != "Quota must be greater than zero" {
			t.Errorf("expected 'Quota must be greater than zero', got %s", errs["quota"])
		}
	})

	t.Run("Hero quota greater than total quota rejected", func(t *testing.T) {
		hQuota := 30
		req := CreateDonationRequest{
			SponsorID:              validSponsorID,
			EventAt:                validEventAt,
			Quota:                  20,
			HeroQuota:              &hQuota,
			Foods:                  []FoodItemInput{{FoodItemID: 1, Quantity: 1, Unit: "kg"}},
			BeneficiaryCategoryIDs: []string{validCategoryID},
		}
		_, _, _, _, _, _, errs := ValidateCreateDonationRequest(req)
		if errs["hero_quota"] != "Hero quota cannot exceed total quota" {
			t.Errorf("expected 'Hero quota cannot exceed total quota', got %s", errs["hero_quota"])
		}
	})

	t.Run("Hero quota negative rejected", func(t *testing.T) {
		hQuota := -5
		req := CreateDonationRequest{
			SponsorID:              validSponsorID,
			EventAt:                validEventAt,
			Quota:                  20,
			HeroQuota:              &hQuota,
			Foods:                  []FoodItemInput{{FoodItemID: 1, Quantity: 1, Unit: "kg"}},
			BeneficiaryCategoryIDs: []string{validCategoryID},
		}
		_, _, _, _, _, _, errs := ValidateCreateDonationRequest(req)
		if errs["hero_quota"] != "Hero quota cannot be negative" {
			t.Errorf("expected 'Hero quota cannot be negative', got %s", errs["hero_quota"])
		}
	})

	t.Run("Address exceeding 500 characters rejected", func(t *testing.T) {
		longAddress := strings.Repeat("A", 501)
		req := CreateDonationRequest{
			SponsorID:              validSponsorID,
			EventAt:                validEventAt,
			Quota:                  20,
			Address:                &longAddress,
			Foods:                  []FoodItemInput{{FoodItemID: 1, Quantity: 1, Unit: "kg"}},
			BeneficiaryCategoryIDs: []string{validCategoryID},
		}
		_, _, _, _, _, _, errs := ValidateCreateDonationRequest(req)
		if errs["address"] != "Address cannot exceed 500 characters" {
			t.Errorf("expected 'Address cannot exceed 500 characters', got %s", errs["address"])
		}
	})

	t.Run("Empty foods list rejected", func(t *testing.T) {
		req := CreateDonationRequest{
			SponsorID:              validSponsorID,
			EventAt:                validEventAt,
			Quota:                  20,
			Foods:                  []FoodItemInput{},
			BeneficiaryCategoryIDs: []string{validCategoryID},
		}
		_, _, _, _, _, _, errs := ValidateCreateDonationRequest(req)
		if errs["foods"] != "At least one food item is required" {
			t.Errorf("expected 'At least one food item is required', got %s", errs["foods"])
		}
	})

	t.Run("Food item invalid unit rejected", func(t *testing.T) {
		req := CreateDonationRequest{
			SponsorID: validSponsorID,
			EventAt:   validEventAt,
			Quota:     20,
			Foods: []FoodItemInput{
				{FoodItemID: 1, Quantity: 5, Unit: "boxes"},
			},
			BeneficiaryCategoryIDs: []string{validCategoryID},
		}
		_, _, _, _, _, _, errs := ValidateCreateDonationRequest(req)
		if !strings.Contains(errs["foods"], "Invalid food unit") {
			t.Errorf("expected invalid food unit error, got %s", errs["foods"])
		}
	})

	t.Run("Food item non-positive quantity rejected", func(t *testing.T) {
		req := CreateDonationRequest{
			SponsorID: validSponsorID,
			EventAt:   validEventAt,
			Quota:     20,
			Foods: []FoodItemInput{
				{FoodItemID: 1, Quantity: 0, Unit: "kg"},
			},
			BeneficiaryCategoryIDs: []string{validCategoryID},
		}
		_, _, _, _, _, _, errs := ValidateCreateDonationRequest(req)
		if errs["foods"] != "Food item quantity must be greater than zero" {
			t.Errorf("expected 'Food item quantity must be greater than zero', got %s", errs["foods"])
		}
	})

	t.Run("Duplicate food item IDs rejected", func(t *testing.T) {
		req := CreateDonationRequest{
			SponsorID: validSponsorID,
			EventAt:   validEventAt,
			Quota:     20,
			Foods: []FoodItemInput{
				{FoodItemID: 1, Quantity: 5, Unit: "kg"},
				{FoodItemID: 1, Quantity: 2, Unit: "kg"},
			},
			BeneficiaryCategoryIDs: []string{validCategoryID},
		}
		_, _, _, _, _, _, errs := ValidateCreateDonationRequest(req)
		if !strings.Contains(errs["foods"], "Duplicate food item ID") {
			t.Errorf("expected duplicate food item error, got %s", errs["foods"])
		}
	})

	t.Run("Empty targets list allowed in creation (optional general food drive)", func(t *testing.T) {
		req := CreateDonationRequest{
			SponsorID:              validSponsorID,
			EventAt:                validEventAt,
			Quota:                  20,
			Foods:                  []FoodItemInput{{FoodItemID: 1, Quantity: 1, Unit: "kg"}},
			BeneficiaryCategoryIDs: []string{},
		}
		_, _, _, _, _, _, errs := ValidateCreateDonationRequest(req)
		if len(errs) != 0 {
			t.Errorf("expected empty targets to be allowed, got errors %v", errs)
		}
	})

	t.Run("Invalid target UUID format rejected", func(t *testing.T) {
		req := CreateDonationRequest{
			SponsorID:              validSponsorID,
			EventAt:                validEventAt,
			Quota:                  20,
			Foods:                  []FoodItemInput{{FoodItemID: 1, Quantity: 1, Unit: "kg"}},
			BeneficiaryCategoryIDs: []string{"invalid-uuid"},
		}
		_, _, _, _, _, _, errs := ValidateCreateDonationRequest(req)
		if errs["beneficiary_category_ids"] != "Invalid beneficiary category ID format" {
			t.Errorf("expected 'Invalid beneficiary category ID format', got %s", errs["beneficiary_category_ids"])
		}
	})

	t.Run("Duplicate target category UUIDs rejected", func(t *testing.T) {
		req := CreateDonationRequest{
			SponsorID:              validSponsorID,
			EventAt:                validEventAt,
			Quota:                  20,
			Foods:                  []FoodItemInput{{FoodItemID: 1, Quantity: 1, Unit: "kg"}},
			BeneficiaryCategoryIDs: []string{validCategoryID, validCategoryID},
		}
		_, _, _, _, _, _, errs := ValidateCreateDonationRequest(req)
		if !strings.Contains(errs["beneficiary_category_ids"], "Duplicate beneficiary category ID") {
			t.Errorf("expected duplicate target error, got %s", errs["beneficiary_category_ids"])
		}
	})
}

func TestBuildPublicSponsorResponse(t *testing.T) {
	sponsorID := uuid.New()
	logoID := uuid.New()
	logoPg := pgtype.UUID{Bytes: logoID, Valid: true}
	sponsorName := "PT Indofood CBP Sukses Makmur"

	t.Run("Unhidden sponsor renders name and logo only", func(t *testing.T) {
		resp := BuildPublicSponsorResponse(sponsorID, sponsorName, logoPg, false, false)
		if resp.ID == nil || *resp.ID != sponsorID.String() {
			t.Errorf("expected sponsor ID %s, got %v", sponsorID.String(), resp.ID)
		}
		if resp.Name != sponsorName {
			t.Errorf("expected sponsor name %s, got %s", sponsorName, resp.Name)
		}
		if resp.Logo == nil || *resp.Logo != logoID.String() {
			t.Errorf("expected logo %s, got %v", logoID.String(), resp.Logo)
		}
	})

	t.Run("Hidden at donation level masks sponsor to 'Generous sponsor' and nil logo", func(t *testing.T) {
		resp := BuildPublicSponsorResponse(sponsorID, sponsorName, logoPg, false, true)
		if resp.ID != nil {
			t.Errorf("expected nil sponsor ID for masked donation, got %v", resp.ID)
		}
		if resp.Name != "Generous sponsor" {
			t.Errorf("expected 'Generous sponsor', got %s", resp.Name)
		}
		if resp.Logo != nil {
			t.Errorf("expected nil logo for masked donation, got %v", resp.Logo)
		}
	})

	t.Run("Hidden at sponsor level masks sponsor to 'Generous sponsor' and nil logo", func(t *testing.T) {
		resp := BuildPublicSponsorResponse(sponsorID, sponsorName, logoPg, true, false)
		if resp.ID != nil {
			t.Errorf("expected nil sponsor ID for masked sponsor, got %v", resp.ID)
		}
		if resp.Name != "Generous sponsor" {
			t.Errorf("expected 'Generous sponsor', got %s", resp.Name)
		}
		if resp.Logo != nil {
			t.Errorf("expected nil logo for masked sponsor, got %v", resp.Logo)
		}
	})
}

func TestBuildStaffSponsorResponse(t *testing.T) {
	sponsorID := uuid.New()
	logoID := uuid.New()
	logoPg := pgtype.UUID{Bytes: logoID, Valid: true}
	emailPg := pgtype.Text{String: "corp@indofood.com", Valid: true}
	phonePg := pgtype.Text{String: "6281234567890", Valid: true}
	addressPg := pgtype.Text{String: "Jl. Sudirman No. 1, Jakarta", Valid: true}

	resp := BuildStaffSponsorResponse(sponsorID, "PT Indofood", logoPg, false, true, true, emailPg, phonePg, addressPg)

	if resp.ID != sponsorID.String() {
		t.Errorf("expected ID %s, got %s", sponsorID.String(), resp.ID)
	}
	if !resp.IsDeleted {
		t.Errorf("expected is_deleted true, got %v", resp.IsDeleted)
	}
	if resp.Email == nil || *resp.Email != "corp@indofood.com" {
		t.Errorf("expected email 'corp@indofood.com', got %v", resp.Email)
	}
	if resp.Phone == nil || *resp.Phone != "6281234567890" {
		t.Errorf("expected phone '6281234567890', got %v", resp.Phone)
	}
	if resp.HideDonations != true {
		t.Errorf("expected hide_donations true, got %v", resp.HideDonations)
	}
	if resp.Address == nil || *resp.Address != "Jl. Sudirman No. 1, Jakarta" {
		t.Errorf("expected address 'Jl. Sudirman No. 1, Jakarta', got %v", resp.Address)
	}
}

func TestNumericConversion(t *testing.T) {
	testVal := 12.75
	num, err := Float64ToNumeric(testVal)
	if err != nil {
		t.Fatalf("Float64ToNumeric failed: %v", err)
	}

	converted := NumericToFloat64(num)
	if converted != testVal {
		t.Errorf("expected %f, got %f", testVal, converted)
	}
}

func TestCreateDonationHandler(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	cfg := config.LoadConfig()
	h := NewDonationHandler(pool, &cfg)

	// Clean up any test records created
	defer func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM donations WHERE message LIKE 'Test Donation %'")
		_, _ = pool.Exec(context.Background(), "DELETE FROM sponsors WHERE name LIKE 'Donation Test Sponsor %'")
	}()

	// 1. Create a test sponsor
	sponsorName := fmt.Sprintf("Donation Test Sponsor %s", uuid.NewString()[:8])
	var sponsorID uuid.UUID
	err := pool.QueryRow(context.Background(), `
		INSERT INTO sponsors (name, variant, address, email, phone, hidden)
		VALUES ($1, 'company', 'Jl. Kaliurang KM 9', 'sponsor@test.com', '6281234567890', false)
		RETURNING id
	`, sponsorName).Scan(&sponsorID)
	if err != nil {
		t.Fatalf("failed to insert test sponsor: %v", err)
	}

	// 2. Fetch an existing food item from seeders
	var foodItemID int32
	err = pool.QueryRow(context.Background(), "SELECT id FROM food_items LIMIT 1").Scan(&foodItemID)
	if err != nil {
		t.Fatalf("failed to query food item from seeders: %v", err)
	}

	// 3. Fetch an existing beneficiary category from seeders
	var categoryID uuid.UUID
	err = pool.QueryRow(context.Background(), "SELECT id FROM beneficiary_categories WHERE variant IN ('university', 'kipk') AND deleted_at IS NULL LIMIT 1").Scan(&categoryID)
	if err != nil {
		t.Fatalf("failed to query beneficiary category from seeders: %v", err)
	}

	testMessage := fmt.Sprintf("Test Donation %s", uuid.NewString()[:8])
	eventAt := time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)
	heroQuota := 15

	t.Run("Create Donation Happy Path (Operational Staff / Super Admin)", func(t *testing.T) {
		reqBody := CreateDonationRequest{
			SponsorID: sponsorID.String(),
			EventAt:   eventAt,
			Quota:     20,
			HeroQuota: &heroQuota,
			Message:   &testMessage,
			Foods: []FoodItemInput{
				{FoodItemID: foodItemID, Quantity: 25.5, Unit: "kg"},
			},
			BeneficiaryCategoryIDs: []string{categoryID.String()},
		}
		bodyBytes, _ := json.Marshal(reqBody)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/donations", bytes.NewReader(bodyBytes))
		req = setStaffAuth(req)
		rec := httptest.NewRecorder()

		h.CreateDonationHandler(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.Status != "success" {
			t.Errorf("expected success status, got %s", resp.Status)
		}

		dataBytes, _ := json.Marshal(resp.Data)
		var donationResp DonationStaffResponse
		_ = json.Unmarshal(dataBytes, &donationResp)

		if donationResp.Status != "draft" {
			t.Errorf("expected initial status 'draft', got %s", donationResp.Status)
		}
		if donationResp.Quota != 20 {
			t.Errorf("expected quota 20, got %d", donationResp.Quota)
		}
		if donationResp.HeroQuota != 15 {
			t.Errorf("expected hero_quota 15, got %d", donationResp.HeroQuota)
		}
		if donationResp.Remain != 15 {
			t.Errorf("expected remain to equal hero_quota (15), got %d", donationResp.Remain)
		}
		if donationResp.Sponsor.ID != sponsorID.String() {
			t.Errorf("expected sponsor ID %s, got %s", sponsorID.String(), donationResp.Sponsor.ID)
		}
		if len(donationResp.Foods) != 1 || donationResp.Foods[0].FoodItemID != foodItemID {
			t.Errorf("expected attached food item %d, got %+v", foodItemID, donationResp.Foods)
		}
		if len(donationResp.Targets) != 1 || donationResp.Targets[0].BeneficiaryCategoryID == nil || *donationResp.Targets[0].BeneficiaryCategoryID != categoryID.String() {
			t.Errorf("expected attached target category %s, got %+v", categoryID.String(), donationResp.Targets)
		}
	})

	t.Run("Create Donation Validation Failure (400 VAL_001)", func(t *testing.T) {
		reqBody := CreateDonationRequest{
			SponsorID:              sponsorID.String(),
			EventAt:                eventAt,
			Quota:                  0, // Invalid quota <= 0
			Foods:                  []FoodItemInput{{FoodItemID: foodItemID, Quantity: 1, Unit: "kg"}},
			BeneficiaryCategoryIDs: []string{categoryID.String()},
		}
		bodyBytes, _ := json.Marshal(reqBody)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/donations", bytes.NewReader(bodyBytes))
		req = setStaffAuth(req)
		rec := httptest.NewRecorder()

		h.CreateDonationHandler(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d", rec.Code)
		}

		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.Code != "VAL_001" {
			t.Errorf("expected code VAL_001, got %s", resp.Code)
		}
	})

	t.Run("Create Donation Non-Existent Sponsor (400 VAL_002)", func(t *testing.T) {
		reqBody := CreateDonationRequest{
			SponsorID:              uuid.NewString(), // Random non-existent sponsor
			EventAt:                eventAt,
			Quota:                  20,
			Foods:                  []FoodItemInput{{FoodItemID: foodItemID, Quantity: 1, Unit: "kg"}},
			BeneficiaryCategoryIDs: []string{categoryID.String()},
		}
		bodyBytes, _ := json.Marshal(reqBody)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/donations", bytes.NewReader(bodyBytes))
		req = setStaffAuth(req)
		rec := httptest.NewRecorder()

		h.CreateDonationHandler(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d", rec.Code)
		}

		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.Code != "VAL_002" {
			t.Errorf("expected code VAL_002, got %s", resp.Code)
		}
	})

	t.Run("Create Donation Non-Existent Food Item (400 VAL_002)", func(t *testing.T) {
		reqBody := CreateDonationRequest{
			SponsorID:              sponsorID.String(),
			EventAt:                eventAt,
			Quota:                  20,
			Foods:                  []FoodItemInput{{FoodItemID: 999999, Quantity: 1, Unit: "kg"}}, // Non-existent food item
			BeneficiaryCategoryIDs: []string{categoryID.String()},
		}
		bodyBytes, _ := json.Marshal(reqBody)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/donations", bytes.NewReader(bodyBytes))
		req = setStaffAuth(req)
		rec := httptest.NewRecorder()

		h.CreateDonationHandler(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d", rec.Code)
		}

		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.Code != "VAL_002" {
			t.Errorf("expected code VAL_002, got %s", resp.Code)
		}
	})

	t.Run("Create Donation Non-Existent Target Category (400 VAL_002)", func(t *testing.T) {
		reqBody := CreateDonationRequest{
			SponsorID:              sponsorID.String(),
			EventAt:                eventAt,
			Quota:                  20,
			Foods:                  []FoodItemInput{{FoodItemID: foodItemID, Quantity: 1, Unit: "kg"}},
			BeneficiaryCategoryIDs: []string{uuid.NewString()}, // Non-existent category
		}
		bodyBytes, _ := json.Marshal(reqBody)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/donations", bytes.NewReader(bodyBytes))
		req = setStaffAuth(req)
		rec := httptest.NewRecorder()

		h.CreateDonationHandler(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d", rec.Code)
		}

		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.Code != "VAL_002" {
			t.Errorf("expected code VAL_002, got %s", resp.Code)
		}
	})

	t.Run("Create Donation Organization-Only Target Enforces hero_quota = 0 (400 VAL_001)", func(t *testing.T) {
		var orgCatID uuid.UUID
		err := pool.QueryRow(context.Background(), "SELECT id FROM beneficiary_categories WHERE variant NOT IN ('university', 'kipk') AND deleted_at IS NULL LIMIT 1").Scan(&orgCatID)
		if err != nil {
			t.Fatalf("failed to query organization beneficiary category: %v", err)
		}

		heroQ := 10
		reqBody := CreateDonationRequest{
			SponsorID:              sponsorID.String(),
			EventAt:                eventAt,
			Quota:                  20,
			HeroQuota:              &heroQ,
			Foods:                  []FoodItemInput{{FoodItemID: foodItemID, Quantity: 1, Unit: "kg"}},
			BeneficiaryCategoryIDs: []string{orgCatID.String()},
		}
		bodyBytes, _ := json.Marshal(reqBody)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/donations", bytes.NewReader(bodyBytes))
		req = setStaffAuth(req)
		rec := httptest.NewRecorder()

		h.CreateDonationHandler(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d", rec.Code)
		}

		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.Code != "VAL_001" {
			t.Errorf("expected code VAL_001, got %s", resp.Code)
		}
	})
}

func TestGetDonationByIDHandler(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	cfg := config.LoadConfig()
	h := NewDonationHandler(pool, &cfg)

	// Clean up any test records created
	defer func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM donations WHERE message LIKE 'GetTest %'")
		_, _ = pool.Exec(context.Background(), "DELETE FROM sponsors WHERE name LIKE 'GetTest Sponsor %'")
		_, _ = pool.Exec(context.Background(), "DELETE FROM beneficiary_categories WHERE name LIKE 'GetTest Cat %'")
	}()

	// 1. Create test sponsors: unhidden and hidden
	var unhiddenSponsorID, hiddenSponsorID, hideDonationsSponsorID uuid.UUID
	err := pool.QueryRow(context.Background(), `
		INSERT INTO sponsors (name, variant, address, email, phone, hidden, hide_donations)
		VALUES ('GetTest Sponsor Unhidden', 'company', 'Jl. Kaliurang KM 5', 'unhidden@sponsor.com', '6281111111111', false, false)
		RETURNING id
	`).Scan(&unhiddenSponsorID)
	if err != nil {
		t.Fatalf("failed to insert unhidden sponsor: %v", err)
	}

	err = pool.QueryRow(context.Background(), `
		INSERT INTO sponsors (name, variant, address, email, phone, hidden)
		VALUES ('GetTest Sponsor Hidden', 'company', 'Jl. Kaliurang KM 10', 'hidden@sponsor.com', '6282222222222', true)
		RETURNING id
	`).Scan(&hiddenSponsorID)
	if err != nil {
		t.Fatalf("failed to insert hidden sponsor: %v", err)
	}

	err = pool.QueryRow(context.Background(), `
		INSERT INTO sponsors (name, variant, address, email, phone, hidden, hide_donations)
		VALUES ('GetTest Sponsor HideDonations', 'company', 'Jl. Kaliurang KM 15', 'hidedonations@sponsor.com', '6283333333333', false, true)
		RETURNING id
	`).Scan(&hideDonationsSponsorID)
	if err != nil {
		t.Fatalf("failed to insert hideDonations sponsor: %v", err)
	}

	// 2. Fetch a food item from seeders
	var foodItemID int32
	err = pool.QueryRow(context.Background(), "SELECT id FROM food_items LIMIT 1").Scan(&foodItemID)
	if err != nil {
		t.Fatalf("failed to query food item from seeders: %v", err)
	}

	// 3. Create test categories: unhidden and hidden
	var unhiddenCatID, hiddenCatID uuid.UUID
	err = pool.QueryRow(context.Background(), `
		INSERT INTO beneficiary_categories (name, variant, is_priority, hidden)
		VALUES ('GetTest Cat Unhidden', 'university', false, false)
		RETURNING id
	`).Scan(&unhiddenCatID)
	if err != nil {
		t.Fatalf("failed to insert unhidden cat: %v", err)
	}

	err = pool.QueryRow(context.Background(), `
		INSERT INTO beneficiary_categories (name, variant, is_priority, hidden)
		VALUES ('GetTest Cat Hidden', 'shelter', false, true)
		RETURNING id
	`).Scan(&hiddenCatID)
	if err != nil {
		t.Fatalf("failed to insert hidden cat: %v", err)
	}

	// Helper to create test donations
	createTestDonation := func(sID uuid.UUID, status string, hideFromPub, hideSpon bool, eventAt time.Time, quota, heroQuota, remain int32) uuid.UUID {
		var donationID uuid.UUID
		err := pool.QueryRow(context.Background(), `
			INSERT INTO donations (sponsor_id, created_by, event_at, quota, hero_quota, remain, address, maps, message, status, hide_from_public, hide_sponsor)
			VALUES ($1, '00000000-0000-0000-0000-000000000001', $2, $3, $4, $5, 'Jl. Test 123', 'https://maps.example.com', 'GetTest Message', $6, $7, $8)
			RETURNING id
		`, sID, eventAt, quota, heroQuota, remain, status, hideFromPub, hideSpon).Scan(&donationID)
		if err != nil {
			t.Fatalf("failed to insert test donation: %v", err)
		}

		// Attach food
		_, err = pool.Exec(context.Background(), `
			INSERT INTO foods (donation_id, food_item_id, quantity, unit)
			VALUES ($1, $2, 10.00, 'portion')
		`, donationID, foodItemID)
		if err != nil {
			t.Fatalf("failed to insert test food: %v", err)
		}

		// Attach unhidden category and hidden category
		_, err = pool.Exec(context.Background(), `
			INSERT INTO donation_target (donation_id, beneficiary_category_id)
			VALUES ($1, $2), ($1, $3)
		`, donationID, unhiddenCatID, hiddenCatID)
		if err != nil {
			t.Fatalf("failed to insert test targets: %v", err)
		}

		return donationID
	}

	t.Run("Invalid donation ID format returns 400 VAL_001", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/donations/invalid-uuid", nil)
		req.SetPathValue("id", "invalid-uuid")
		rec := httptest.NewRecorder()

		h.GetDonationByIDHandler(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d", rec.Code)
		}
		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.Code != "VAL_001" {
			t.Errorf("expected code VAL_001, got %s", resp.Code)
		}
	})

	t.Run("Non-existent donation ID returns 404 NOT_FOUND", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/donations/"+uuid.NewString(), nil)
		req.SetPathValue("id", uuid.NewString())
		rec := httptest.NewRecorder()

		h.GetDonationByIDHandler(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 Not Found, got %d", rec.Code)
		}
	})

	t.Run("Draft donation access: Staff sees all, Hero/Public gets 404", func(t *testing.T) {
		draftID := createTestDonation(unhiddenSponsorID, "draft", false, false, time.Now().Add(24*time.Hour), 20, 10, 10)

		// 1. Staff access
		req := httptest.NewRequest(http.MethodGet, "/api/v1/donations/"+draftID.String(), nil)
		req.SetPathValue("id", draftID.String())
		req = setStaffAuth(req)
		rec := httptest.NewRecorder()

		h.GetDonationByIDHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK for staff, got %d. Body: %s", rec.Code, rec.Body.String())
		}
		var staffResp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &staffResp)
		dataBytes, _ := json.Marshal(staffResp.Data)
		var donationStaff DonationStaffResponse
		_ = json.Unmarshal(dataBytes, &donationStaff)

		if donationStaff.Status != "draft" {
			t.Errorf("expected status 'draft', got %s", donationStaff.Status)
		}
		if donationStaff.Sponsor.Email == nil || *donationStaff.Sponsor.Email != "unhidden@sponsor.com" {
			t.Errorf("expected staff to view sponsor email, got %v", donationStaff.Sponsor.Email)
		}
		if donationStaff.Sponsor.Phone == nil || *donationStaff.Sponsor.Phone != "6281111111111" {
			t.Errorf("expected staff to view sponsor phone, got %v", donationStaff.Sponsor.Phone)
		}
		if len(donationStaff.Targets) != 2 {
			t.Errorf("expected staff to view 2 targets (including hidden), got %d", len(donationStaff.Targets))
		}

		// 2. Hero access -> 404
		heroReq := httptest.NewRequest(http.MethodGet, "/api/v1/donations/"+draftID.String(), nil)
		heroReq.SetPathValue("id", draftID.String())
		heroReq = setHeroAuth(heroReq)
		heroRec := httptest.NewRecorder()

		h.GetDonationByIDHandler(heroRec, heroReq)

		if heroRec.Code != http.StatusNotFound {
			t.Errorf("expected 404 for hero on draft donation, got %d", heroRec.Code)
		}

		// 3. Public unauthenticated access -> 404
		pubReq := httptest.NewRequest(http.MethodGet, "/api/v1/donations/"+draftID.String(), nil)
		pubReq.SetPathValue("id", draftID.String())
		pubRec := httptest.NewRecorder()

		h.GetDonationByIDHandler(pubRec, pubReq)

		if pubRec.Code != http.StatusNotFound {
			t.Errorf("expected 404 for public on draft donation, got %d", pubRec.Code)
		}
	})

	t.Run("Hidden donation (hide_from_public = true): Staff sees, Hero/Public gets 404", func(t *testing.T) {
		hiddenID := createTestDonation(unhiddenSponsorID, "open", true, false, time.Now().Add(24*time.Hour), 20, 10, 10)

		// 1. Staff access -> 200 OK
		req := httptest.NewRequest(http.MethodGet, "/api/v1/donations/"+hiddenID.String(), nil)
		req.SetPathValue("id", hiddenID.String())
		req = setStaffAuth(req)
		rec := httptest.NewRecorder()

		h.GetDonationByIDHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK for staff on hidden donation, got %d", rec.Code)
		}

		// 2. Hero access -> 404
		heroReq := httptest.NewRequest(http.MethodGet, "/api/v1/donations/"+hiddenID.String(), nil)
		heroReq.SetPathValue("id", hiddenID.String())
		heroReq = setHeroAuth(heroReq)
		heroRec := httptest.NewRecorder()

		h.GetDonationByIDHandler(heroRec, heroReq)

		if heroRec.Code != http.StatusNotFound {
			t.Errorf("expected 404 for hero on hidden donation, got %d", heroRec.Code)
		}

		// 3. Public access -> 404
		pubReq := httptest.NewRequest(http.MethodGet, "/api/v1/donations/"+hiddenID.String(), nil)
		pubReq.SetPathValue("id", hiddenID.String())
		pubRec := httptest.NewRecorder()

		h.GetDonationByIDHandler(pubRec, pubReq)

		if pubRec.Code != http.StatusNotFound {
			t.Errorf("expected 404 for public on hidden donation, got %d", pubRec.Code)
		}
	})

	t.Run("Cancelled donation: Staff sees, Hero/Public gets 404", func(t *testing.T) {
		cancelledID := createTestDonation(unhiddenSponsorID, "cancelled", false, false, time.Now().Add(24*time.Hour), 20, 10, 10)

		// Staff -> 200
		req := httptest.NewRequest(http.MethodGet, "/api/v1/donations/"+cancelledID.String(), nil)
		req.SetPathValue("id", cancelledID.String())
		req = setStaffAuth(req)
		rec := httptest.NewRecorder()
		h.GetDonationByIDHandler(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("expected 200 for staff on cancelled donation, got %d", rec.Code)
		}

		// Public -> 404
		pubReq := httptest.NewRequest(http.MethodGet, "/api/v1/donations/"+cancelledID.String(), nil)
		pubReq.SetPathValue("id", cancelledID.String())
		pubRec := httptest.NewRecorder()
		h.GetDonationByIDHandler(pubRec, pubReq)
		if pubRec.Code != http.StatusNotFound {
			t.Errorf("expected 404 for public on cancelled donation, got %d", pubRec.Code)
		}
	})

	t.Run("Public / Hero views unhidden open donation with leak prevention", func(t *testing.T) {
		openID := createTestDonation(unhiddenSponsorID, "open", false, false, time.Now().Add(24*time.Hour), 20, 10, 8)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/donations/"+openID.String(), nil)
		req.SetPathValue("id", openID.String())
		rec := httptest.NewRecorder()

		h.GetDonationByIDHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK for public view, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		dataBytes, _ := json.Marshal(resp.Data)
		var donationPub DonationPublicResponse
		_ = json.Unmarshal(dataBytes, &donationPub)

		if donationPub.Sponsor.Name != "GetTest Sponsor Unhidden" {
			t.Errorf("expected unhidden sponsor name, got %s", donationPub.Sponsor.Name)
		}
		if donationPub.Sponsor.ID == nil || *donationPub.Sponsor.ID != unhiddenSponsorID.String() {
			t.Errorf("expected unhidden sponsor ID, got %v", donationPub.Sponsor.ID)
		}
		if donationPub.IsFull {
			t.Error("expected is_full: false when remain > 0 and status is open")
		}
		// Category leak guardrail: unhidden categories shown with details, hidden categories collapsed into "Hidden institution"
		if len(donationPub.Targets) != 2 {
			t.Fatalf("expected 2 target categories for public (1 unhidden + 1 collapsed hidden institution), got %d", len(donationPub.Targets))
		}
		if donationPub.Targets[0].BeneficiaryCategoryID == nil || *donationPub.Targets[0].BeneficiaryCategoryID != unhiddenCatID.String() {
			t.Errorf("expected category ID %s, got %v", unhiddenCatID.String(), donationPub.Targets[0].BeneficiaryCategoryID)
		}
		if donationPub.Targets[1].BeneficiaryCategoryID != nil || donationPub.Targets[1].Name != "Hidden institution" {
			t.Errorf("expected masked Hidden institution, got %+v", donationPub.Targets[1])
		}
	})

	t.Run("Masking: Public / Hero views donation with hide_sponsor = true", func(t *testing.T) {
		maskedDonationID := createTestDonation(unhiddenSponsorID, "open", false, true, time.Now().Add(24*time.Hour), 20, 10, 5)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/donations/"+maskedDonationID.String(), nil)
		req.SetPathValue("id", maskedDonationID.String())
		rec := httptest.NewRecorder()

		h.GetDonationByIDHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rec.Code)
		}

		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		dataBytes, _ := json.Marshal(resp.Data)
		var donationPub DonationPublicResponse
		_ = json.Unmarshal(dataBytes, &donationPub)

		if donationPub.Sponsor.Name != "Generous sponsor" {
			t.Errorf("expected masked sponsor name 'Generous sponsor', got %s", donationPub.Sponsor.Name)
		}
		if donationPub.Sponsor.ID != nil {
			t.Errorf("expected nil sponsor ID for masked donation, got %v", donationPub.Sponsor.ID)
		}
		if donationPub.Sponsor.Logo != nil {
			t.Errorf("expected nil sponsor Logo for masked donation, got %v", donationPub.Sponsor.Logo)
		}
	})

	t.Run("Masking: Public / Hero views donation with sponsor.hidden = true", func(t *testing.T) {
		hiddenSponsorDonationID := createTestDonation(hiddenSponsorID, "open", false, false, time.Now().Add(24*time.Hour), 20, 10, 5)

		// Public view -> masked to "Generous sponsor"
		req := httptest.NewRequest(http.MethodGet, "/api/v1/donations/"+hiddenSponsorDonationID.String(), nil)
		req.SetPathValue("id", hiddenSponsorDonationID.String())
		rec := httptest.NewRecorder()

		h.GetDonationByIDHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rec.Code)
		}

		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		dataBytes, _ := json.Marshal(resp.Data)
		var donationPub DonationPublicResponse
		_ = json.Unmarshal(dataBytes, &donationPub)

		if donationPub.Sponsor.Name != "Generous sponsor" {
			t.Errorf("expected masked sponsor name 'Generous sponsor', got %s", donationPub.Sponsor.Name)
		}
		if donationPub.Sponsor.ID != nil {
			t.Errorf("expected nil sponsor ID for hidden sponsor, got %v", donationPub.Sponsor.ID)
		}

		// Staff view -> unmasked real sponsor name
		staffReq := httptest.NewRequest(http.MethodGet, "/api/v1/donations/"+hiddenSponsorDonationID.String(), nil)
		staffReq.SetPathValue("id", hiddenSponsorDonationID.String())
		staffReq = setStaffAuth(staffReq)
		staffRec := httptest.NewRecorder()

		h.GetDonationByIDHandler(staffRec, staffReq)

		if staffRec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK for staff, got %d", staffRec.Code)
		}
		var staffResp response.Response
		_ = json.Unmarshal(staffRec.Body.Bytes(), &staffResp)
		sDataBytes, _ := json.Marshal(staffResp.Data)
		var donationStaff DonationStaffResponse
		_ = json.Unmarshal(sDataBytes, &donationStaff)

		if donationStaff.Sponsor.Name != "GetTest Sponsor Hidden" {
			t.Errorf("expected staff to see real sponsor name, got %s", donationStaff.Sponsor.Name)
		}
		if !donationStaff.Sponsor.Hidden {
			t.Error("expected staff sponsor.hidden: true")
		}
	})

	t.Run("Masking: Public / Hero views donation with sponsor.hide_donations = true", func(t *testing.T) {
		dID := createTestDonation(hideDonationsSponsorID, "open", false, false, time.Now().Add(24*time.Hour), 20, 10, 5)

		// Public view -> masked to \"Generous sponsor\"
		req := httptest.NewRequest(http.MethodGet, "/api/v1/donations/"+dID.String(), nil)
		req.SetPathValue("id", dID.String())
		rec := httptest.NewRecorder()

		h.GetDonationByIDHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rec.Code)
		}

		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		dataBytes, _ := json.Marshal(resp.Data)
		var donationPub DonationPublicResponse
		_ = json.Unmarshal(dataBytes, &donationPub)

		if donationPub.Sponsor.Name != "Generous sponsor" {
			t.Errorf("expected masked sponsor name \x27Generous sponsor\x27, got %s", donationPub.Sponsor.Name)
		}
		if donationPub.Sponsor.ID != nil {
			t.Errorf("expected nil sponsor ID for hide_donations sponsor, got %v", donationPub.Sponsor.ID)
		}

		// Staff view -> unmasked real sponsor name and hide_donations = true
		staffReq := httptest.NewRequest(http.MethodGet, "/api/v1/donations/"+dID.String(), nil)
		staffReq.SetPathValue("id", dID.String())
		staffReq = setStaffAuth(staffReq)
		staffRec := httptest.NewRecorder()

		h.GetDonationByIDHandler(staffRec, staffReq)

		if staffRec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK for staff, got %d", staffRec.Code)
		}
		var staffResp response.Response
		_ = json.Unmarshal(staffRec.Body.Bytes(), &staffResp)
		sDataBytes, _ := json.Marshal(staffResp.Data)
		var donationStaff DonationStaffResponse
		_ = json.Unmarshal(sDataBytes, &donationStaff)

		if donationStaff.Sponsor.Name != "GetTest Sponsor HideDonations" {
			t.Errorf("expected staff to see real sponsor name, got %s", donationStaff.Sponsor.Name)
		}
		if donationStaff.Sponsor.Hidden != false {
			t.Error("expected staff sponsor.hidden: false")
		}
		if donationStaff.Sponsor.HideDonations != true {
			t.Error("expected staff sponsor.hide_donations: true")
		}
	})

	t.Run("Auto-Close / Full Badge: remain = 0 or status = 'closed' displays is_full: true", func(t *testing.T) {
		fullDonationID := createTestDonation(unhiddenSponsorID, "open", false, false, time.Now().Add(24*time.Hour), 20, 10, 0)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/donations/"+fullDonationID.String(), nil)
		req.SetPathValue("id", fullDonationID.String())
		rec := httptest.NewRecorder()

		h.GetDonationByIDHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rec.Code)
		}
		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		dataBytes, _ := json.Marshal(resp.Data)
		var donationPub DonationPublicResponse
		_ = json.Unmarshal(dataBytes, &donationPub)

		if !donationPub.IsFull {
			t.Error("expected is_full: true when remain == 0")
		}

		closedDonationID := createTestDonation(unhiddenSponsorID, "closed", false, false, time.Now().Add(24*time.Hour), 20, 10, 5)

		cReq := httptest.NewRequest(http.MethodGet, "/api/v1/donations/"+closedDonationID.String(), nil)
		cReq.SetPathValue("id", closedDonationID.String())
		cRec := httptest.NewRecorder()

		h.GetDonationByIDHandler(cRec, cReq)

		if cRec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK for closed donation, got %d", cRec.Code)
		}
		var cResp response.Response
		_ = json.Unmarshal(cRec.Body.Bytes(), &cResp)
		cDataBytes, _ := json.Marshal(cResp.Data)
		var cDonationPub DonationPublicResponse
		_ = json.Unmarshal(cDataBytes, &cDonationPub)

		if cDonationPub.IsFull {
			t.Error("expected is_full: false for closed donation with spots remaining (remain = 5)")
		}
	})

	t.Run("Past donation window: completed within 72h is visible, older than 72h returns 404 to public", func(t *testing.T) {
		// Completed 24h ago -> within 72h window
		recentCompletedID := createTestDonation(unhiddenSponsorID, "completed", false, false, time.Now().Add(-24*time.Hour), 20, 10, 0)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/donations/"+recentCompletedID.String(), nil)
		req.SetPathValue("id", recentCompletedID.String())
		rec := httptest.NewRecorder()

		h.GetDonationByIDHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK for recent completed donation, got %d", rec.Code)
		}

		// Completed 96h ago -> older than 72h window
		oldCompletedID := createTestDonation(unhiddenSponsorID, "completed", false, false, time.Now().Add(-96*time.Hour), 20, 10, 0)

		// Public -> 404
		oldReq := httptest.NewRequest(http.MethodGet, "/api/v1/donations/"+oldCompletedID.String(), nil)
		oldReq.SetPathValue("id", oldCompletedID.String())
		oldRec := httptest.NewRecorder()

		h.GetDonationByIDHandler(oldRec, oldReq)

		if oldRec.Code != http.StatusNotFound {
			t.Errorf("expected 404 for public on completed donation older than 72h, got %d", oldRec.Code)
		}

		// Staff -> 200 OK
		staffReq := httptest.NewRequest(http.MethodGet, "/api/v1/donations/"+oldCompletedID.String(), nil)
		staffReq.SetPathValue("id", oldCompletedID.String())
		staffReq = setStaffAuth(staffReq)
		staffRec := httptest.NewRecorder()

		h.GetDonationByIDHandler(staffRec, staffReq)

		if staffRec.Code != http.StatusOK {
			t.Errorf("expected 200 OK for staff on old completed donation, got %d", staffRec.Code)
		}
	})
}

func TestListDonationsHandler(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	cfg := config.LoadConfig()
	h := NewDonationHandler(pool, &cfg)

	// Clean up any test records created
	defer func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM donations WHERE message LIKE 'ListTest %'")
		_, _ = pool.Exec(context.Background(), "DELETE FROM sponsors WHERE name LIKE 'ListTest Sponsor %'")
		_, _ = pool.Exec(context.Background(), "DELETE FROM beneficiary_categories WHERE name LIKE 'ListTest Cat %'")
	}()

	// 1. Create test sponsors: unhidden and hidden
	var unhiddenSponsorID, hiddenSponsorID uuid.UUID
	err := pool.QueryRow(context.Background(), `
		INSERT INTO sponsors (name, variant, address, email, phone, hidden, hide_donations)
		VALUES ('ListTest Sponsor Unhidden', 'company', 'Jl. Kaliurang KM 7', 'unhidden@listtest.com', '62811112222', false, false)
		RETURNING id
	`).Scan(&unhiddenSponsorID)
	if err != nil {
		t.Fatalf("failed to insert unhidden sponsor: %v", err)
	}

	err = pool.QueryRow(context.Background(), `
		INSERT INTO sponsors (name, variant, address, email, phone, hidden)
		VALUES ('ListTest Sponsor Hidden', 'company', 'Jl. Gejayan No. 45', 'hidden@listtest.com', '62833334444', true)
		RETURNING id
	`).Scan(&hiddenSponsorID)
	if err != nil {
		t.Fatalf("failed to insert hidden sponsor: %v", err)
	}

	// 2. Fetch a food item from seeders
	var foodItemID int32
	err = pool.QueryRow(context.Background(), "SELECT id FROM food_items LIMIT 1").Scan(&foodItemID)
	if err != nil {
		t.Fatalf("failed to query food item from seeders: %v", err)
	}

	// 3. Create test categories: unhidden and hidden
	var unhiddenCatID, hiddenCatID uuid.UUID
	err = pool.QueryRow(context.Background(), `
		INSERT INTO beneficiary_categories (name, variant, is_priority, hidden)
		VALUES ('ListTest Cat Unhidden', 'university', false, false)
		RETURNING id
	`).Scan(&unhiddenCatID)
	if err != nil {
		t.Fatalf("failed to insert unhidden cat: %v", err)
	}

	err = pool.QueryRow(context.Background(), `
		INSERT INTO beneficiary_categories (name, variant, is_priority, hidden)
		VALUES ('ListTest Cat Hidden', 'shelter', false, true)
		RETURNING id
	`).Scan(&hiddenCatID)
	if err != nil {
		t.Fatalf("failed to insert hidden cat: %v", err)
	}

	// Helper to create test donations
	createTestDonation := func(sID uuid.UUID, status string, hideFromPub, hideSpon bool, eventAt time.Time, quota, heroQuota, remain int32, address string) uuid.UUID {
		var donationID uuid.UUID
		err := pool.QueryRow(context.Background(), `
			INSERT INTO donations (sponsor_id, created_by, event_at, quota, hero_quota, remain, address, maps, message, status, hide_from_public, hide_sponsor)
			VALUES ($1, '00000000-0000-0000-0000-000000000001', $2, $3, $4, $5, $6, 'https://maps.example.com', 'ListTest Message', $7, $8, $9)
			RETURNING id
		`, sID, eventAt, quota, heroQuota, remain, address, status, hideFromPub, hideSpon).Scan(&donationID)
		if err != nil {
			t.Fatalf("failed to insert test donation: %v", err)
		}

		// Attach food
		_, err = pool.Exec(context.Background(), `
			INSERT INTO foods (donation_id, food_item_id, quantity, unit)
			VALUES ($1, $2, 10.00, 'portion')
		`, donationID, foodItemID)
		if err != nil {
			t.Fatalf("failed to insert test food: %v", err)
		}

		// Attach unhidden category and hidden category
		_, err = pool.Exec(context.Background(), `
			INSERT INTO donation_target (donation_id, beneficiary_category_id)
			VALUES ($1, $2), ($1, $3)
		`, donationID, unhiddenCatID, hiddenCatID)
		if err != nil {
			t.Fatalf("failed to insert test targets: %v", err)
		}

		return donationID
	}

	// Create diverse donations for list testing
	now := time.Now()
	// Draft donation
	_ = createTestDonation(unhiddenSponsorID, "draft", false, false, now.Add(24*time.Hour), 20, 10, 10, "Jl. Magelang KM 5")
	// Hidden open donation
	_ = createTestDonation(unhiddenSponsorID, "open", true, false, now.Add(36*time.Hour), 20, 10, 10, "Jl. Solo KM 9")
	// Unhidden open donation with unhidden sponsor
	_ = createTestDonation(unhiddenSponsorID, "open", false, false, now.Add(12*time.Hour), 30, 15, 15, "Jl. Kaliurang KM 8")
	// Closed donation (auto-closed, remain = 0)
	_ = createTestDonation(unhiddenSponsorID, "closed", false, false, now.Add(18*time.Hour), 20, 10, 0, "Jl. Palagan No. 12")
	// Open donation with hidden sponsor
	_ = createTestDonation(hiddenSponsorID, "open", false, false, now.Add(48*time.Hour), 20, 10, 10, "Jl. Gejayan No. 50")
	// Open donation with hide_sponsor = true
	_ = createTestDonation(unhiddenSponsorID, "open", false, true, now.Add(60*time.Hour), 20, 10, 10, "Jl. Ringroad Utara")
	// Completed donation within past 72h
	_ = createTestDonation(unhiddenSponsorID, "completed", false, false, now.Add(-24*time.Hour), 20, 10, 0, "Jl. Bantul KM 3")
	// Completed donation older than 72h
	_ = createTestDonation(unhiddenSponsorID, "completed", false, false, now.Add(-96*time.Hour), 20, 10, 0, "Jl. Parangtritis KM 10")

	t.Run("Staff lists donations with pagination and search", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/donations?page=1&limit=5", nil)
		req = setStaffAuth(req)
		rec := httptest.NewRecorder()

		h.ListDonationsHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.Meta == nil || resp.Meta.Page != 1 || resp.Meta.Limit != 5 {
			t.Errorf("expected page 1 limit 5 meta, got %+v", resp.Meta)
		}
		if resp.Meta.Total < 8 {
			t.Errorf("expected at least 8 total donations for staff, got %d", resp.Meta.Total)
		}
	})

	t.Run("Staff filter by status=draft returns only draft donations", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/donations?status=draft", nil)
		req = setStaffAuth(req)
		rec := httptest.NewRecorder()

		h.ListDonationsHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rec.Code)
		}

		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		dataBytes, _ := json.Marshal(resp.Data)
		var donations []DonationStaffResponse
		_ = json.Unmarshal(dataBytes, &donations)

		for _, d := range donations {
			if d.Status != "draft" {
				t.Errorf("expected all donations to be draft, found status: %s", d.Status)
			}
		}
	})

	t.Run("Staff search filter matches sponsor name or address", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/donations?search=Gejayan", nil)
		req = setStaffAuth(req)
		rec := httptest.NewRecorder()

		h.ListDonationsHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rec.Code)
		}

		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		dataBytes, _ := json.Marshal(resp.Data)
		var donations []DonationStaffResponse
		_ = json.Unmarshal(dataBytes, &donations)

		if len(donations) == 0 {
			t.Error("expected at least 1 donation matching 'Gejayan'")
		}
	})

	t.Run("Staff invalid status returns 400 VAL_001", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/donations?status=invalid_status", nil)
		req = setStaffAuth(req)
		rec := httptest.NewRecorder()

		h.ListDonationsHandler(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d", rec.Code)
		}
		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.Code != "VAL_001" {
			t.Errorf("expected code VAL_001, got %s", resp.Code)
		}
	})

	t.Run("Public lists active distributions with privacy masking and full badges", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/donations", nil) // Public / default open
		rec := httptest.NewRecorder()

		h.ListDonationsHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		dataBytes, _ := json.Marshal(resp.Data)
		var donations []DonationPublicResponse
		_ = json.Unmarshal(dataBytes, &donations)

		for _, d := range donations {
			if d.Status != "open" && d.Status != "closed" {
				t.Errorf("public active list must only contain open or closed donations, got: %s", d.Status)
			}
			// Category shielding check
			for _, tg := range d.Targets {
				if tg.BeneficiaryCategoryID != nil && *tg.BeneficiaryCategoryID == hiddenCatID.String() {
					t.Errorf("public list leaked hidden category %s", hiddenCatID.String())
				}
			}
			// Full badge check: strictly hero_quota > 0 && remain == 0
			if d.HeroQuota > 0 && d.Remain == 0 {
				if !d.IsFull {
					t.Errorf("expected is_full: true for donation %s (hero_quota: %d, remain: %d)", d.ID, d.HeroQuota, d.Remain)
				}
			} else {
				if d.IsFull {
					t.Errorf("expected is_full: false for donation %s (hero_quota: %d, remain: %d)", d.ID, d.HeroQuota, d.Remain)
				}
			}
		}
	})

	t.Run("Public lists past distributions completed within past days window", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/donations?status=completed", nil)
		rec := httptest.NewRecorder()

		h.ListDonationsHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		dataBytes, _ := json.Marshal(resp.Data)
		var donations []DonationPublicResponse
		_ = json.Unmarshal(dataBytes, &donations)

		cutoff := time.Now().Add(-cfg.Donation.PastDaysWindow)
		for _, d := range donations {
			if d.Status != "completed" {
				t.Errorf("expected completed status, got: %s", d.Status)
			}
			parsedTime, _ := time.Parse(time.RFC3339, d.EventAt)
			if parsedTime.Before(cutoff) {
				t.Errorf("public past list returned donation older than cutoff window: %s", d.EventAt)
			}
		}
	})

	t.Run("Public invalid status filter returns 400 VAL_001", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/donations?status=draft", nil)
		rec := httptest.NewRecorder()

		h.ListDonationsHandler(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d", rec.Code)
		}
		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.Code != "VAL_001" {
			t.Errorf("expected code VAL_001, got %s", resp.Code)
		}
	})
}


func TestValidateUpdateDonationRequest(t *testing.T) {
	t.Run("Valid partial update fields", func(t *testing.T) {
		sponsorID := uuid.NewString()
		eventAt := time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)
		quota := 50
		heroQuota := 25
		addr := "Jl. Magelang KM 5"
		catID := uuid.NewString()

		req := UpdateDonationRequest{
			SponsorID:              &sponsorID,
			EventAt:                &eventAt,
			Quota:                  &quota,
			HeroQuota:              &heroQuota,
			Address:                &addr,
			Foods:                  []FoodItemInput{{FoodItemID: 1, Quantity: 10, Unit: "portion"}},
			BeneficiaryCategoryIDs: []string{catID},
		}

		sUUID, eTime, errs := ValidateUpdateDonationRequest(req)
		if len(errs) > 0 {
			t.Fatalf("unexpected validation errors: %+v", errs)
		}
		if sUUID == nil || sUUID.String() != sponsorID {
			t.Errorf("expected sponsor UUID %s, got %v", sponsorID, sUUID)
		}
		if eTime == nil {
			t.Error("expected non-nil event time")
		}
	})

	t.Run("Empty sponsor ID returns error", func(t *testing.T) {
		empty := "   "
		req := UpdateDonationRequest{SponsorID: &empty}
		_, _, errs := ValidateUpdateDonationRequest(req)
		if errs["sponsor_id"] != "Sponsor ID cannot be empty" {
			t.Errorf("expected empty sponsor_id error, got %v", errs["sponsor_id"])
		}
	})

	t.Run("Invalid sponsor UUID format returns error", func(t *testing.T) {
		invalid := "not-a-uuid"
		req := UpdateDonationRequest{SponsorID: &invalid}
		_, _, errs := ValidateUpdateDonationRequest(req)
		if errs["sponsor_id"] != "Invalid sponsor ID format" {
			t.Errorf("expected invalid format error, got %v", errs["sponsor_id"])
		}
	})

	t.Run("Empty event datetime returns error", func(t *testing.T) {
		empty := ""
		req := UpdateDonationRequest{EventAt: &empty}
		_, _, errs := ValidateUpdateDonationRequest(req)
		if errs["event_at"] != "Event datetime cannot be empty" {
			t.Errorf("expected empty event_at error, got %v", errs["event_at"])
		}
	})

	t.Run("Invalid event datetime format returns error", func(t *testing.T) {
		invalid := "2026-11-12 12:00:00"
		req := UpdateDonationRequest{EventAt: &invalid}
		_, _, errs := ValidateUpdateDonationRequest(req)
		if errs["event_at"] != "Event datetime must be in valid RFC3339 format" {
			t.Errorf("expected invalid RFC3339 format error, got %v", errs["event_at"])
		}
	})

	t.Run("Quota <= 0 returns error", func(t *testing.T) {
		zero := 0
		req := UpdateDonationRequest{Quota: &zero}
		_, _, errs := ValidateUpdateDonationRequest(req)
		if errs["quota"] != "Quota must be greater than zero" {
			t.Errorf("expected quota > 0 error, got %v", errs["quota"])
		}
	})

	t.Run("HeroQuota < 0 returns error", func(t *testing.T) {
		neg := -1
		req := UpdateDonationRequest{HeroQuota: &neg}
		_, _, errs := ValidateUpdateDonationRequest(req)
		if errs["hero_quota"] != "Hero quota cannot be negative" {
			t.Errorf("expected negative hero quota error, got %v", errs["hero_quota"])
		}
	})

	t.Run("Address > 500 characters returns error", func(t *testing.T) {
		longAddr := strings.Repeat("A", 501)
		req := UpdateDonationRequest{Address: &longAddr}
		_, _, errs := ValidateUpdateDonationRequest(req)
		if errs["address"] != "Address cannot exceed 500 characters" {
			t.Errorf("expected address length error, got %v", errs["address"])
		}
	})

	t.Run("Empty foods slice returns error", func(t *testing.T) {
		req := UpdateDonationRequest{Foods: []FoodItemInput{}}
		_, _, errs := ValidateUpdateDonationRequest(req)
		if errs["foods"] != "At least one food item is required" {
			t.Errorf("expected non-empty foods error, got %v", errs["foods"])
		}
	})

	t.Run("Invalid food item fields return error", func(t *testing.T) {
		req := UpdateDonationRequest{Foods: []FoodItemInput{{FoodItemID: 0, Quantity: 1, Unit: "portion"}}}
		_, _, errs := ValidateUpdateDonationRequest(req)
		if errs["foods"] != "Invalid food item ID" {
			t.Errorf("expected invalid food item ID error, got %v", errs["foods"])
		}

		req2 := UpdateDonationRequest{Foods: []FoodItemInput{{FoodItemID: 1, Quantity: 0, Unit: "portion"}}}
		_, _, errs2 := ValidateUpdateDonationRequest(req2)
		if errs2["foods"] != "Food item quantity must be greater than zero" {
			t.Errorf("expected quantity > 0 error, got %v", errs2["foods"])
		}

		req3 := UpdateDonationRequest{Foods: []FoodItemInput{{FoodItemID: 1, Quantity: 1, Unit: "invalid_unit"}}}
		_, _, errs3 := ValidateUpdateDonationRequest(req3)
		if !strings.Contains(errs3["foods"], "Invalid food unit") {
			t.Errorf("expected invalid food unit error, got %v", errs3["foods"])
		}
	})

	t.Run("Empty beneficiary category slice allowed in update (clears targets)", func(t *testing.T) {
		req := UpdateDonationRequest{BeneficiaryCategoryIDs: []string{}}
		_, _, errs := ValidateUpdateDonationRequest(req)
		if _, ok := errs["beneficiary_category_ids"]; ok {
			t.Errorf("expected empty targets to be allowed, got error %v", errs["beneficiary_category_ids"])
		}
	})

	t.Run("Invalid beneficiary category UUID returns error", func(t *testing.T) {
		req := UpdateDonationRequest{BeneficiaryCategoryIDs: []string{"invalid-uuid"}}
		_, _, errs := ValidateUpdateDonationRequest(req)
		if errs["beneficiary_category_ids"] != "Invalid beneficiary category ID format" {
			t.Errorf("expected invalid target UUID error, got %v", errs["beneficiary_category_ids"])
		}
	})
}

func TestUpdateDonationHandler(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	cfg := config.LoadConfig()
	h := NewDonationHandler(pool, &cfg)

	// Clean up any test records created
	defer func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM donations WHERE message LIKE 'UpdateTest %'")
		_, _ = pool.Exec(context.Background(), "DELETE FROM sponsors WHERE name LIKE 'UpdateTest Sponsor %'")
		_, _ = pool.Exec(context.Background(), "DELETE FROM beneficiary_categories WHERE name LIKE 'UpdateTest Cat %'")
	}()

	// 1. Create test sponsors
	var sponsorID, newSponsorID uuid.UUID
	err := pool.QueryRow(context.Background(), `
		INSERT INTO sponsors (name, variant, address, email, phone, hidden)
		VALUES ('UpdateTest Sponsor Primary', 'company', 'Jl. Kaliurang KM 5', 'primary@updatetest.com', '628111111111', false)
		RETURNING id
	`).Scan(&sponsorID)
	if err != nil {
		t.Fatalf("failed to insert primary test sponsor: %v", err)
	}

	err = pool.QueryRow(context.Background(), `
		INSERT INTO sponsors (name, variant, address, email, phone, hidden)
		VALUES ('UpdateTest Sponsor Secondary', 'company', 'Jl. Gejayan No. 10', 'secondary@updatetest.com', '628222222222', false)
		RETURNING id
	`).Scan(&newSponsorID)
	if err != nil {
		t.Fatalf("failed to insert secondary test sponsor: %v", err)
	}

	// 2. Fetch seed food items
	var foodItemID1, foodItemID2 int32
	rows, err := pool.Query(context.Background(), "SELECT id FROM food_items LIMIT 2")
	if err != nil {
		t.Fatalf("failed to query food items: %v", err)
	}
	defer rows.Close()
	if rows.Next() {
		_ = rows.Scan(&foodItemID1)
	}
	if rows.Next() {
		_ = rows.Scan(&foodItemID2)
	}

	// 3. Create test categories (student/hero category vs organization category)
	var uniCatID, orgCatID uuid.UUID
	err = pool.QueryRow(context.Background(), `
		INSERT INTO beneficiary_categories (name, variant, is_priority, hidden)
		VALUES ('UpdateTest Cat University', 'university', false, false)
		RETURNING id
	`).Scan(&uniCatID)
	if err != nil {
		t.Fatalf("failed to insert university category: %v", err)
	}

	err = pool.QueryRow(context.Background(), `
		INSERT INTO beneficiary_categories (name, variant, is_priority, hidden)
		VALUES ('UpdateTest Cat Shelter', 'shelter', false, false)
		RETURNING id
	`).Scan(&orgCatID)
	if err != nil {
		t.Fatalf("failed to insert shelter category: %v", err)
	}

	// Helper to create initial donation
	createDonation := func(status string, quota, heroQuota, remain int32, catID uuid.UUID) uuid.UUID {
		var dID uuid.UUID
		err := pool.QueryRow(context.Background(), `
			INSERT INTO donations (sponsor_id, created_by, event_at, quota, hero_quota, remain, address, maps, message, status, hide_from_public, hide_sponsor)
			VALUES ($1, '00000000-0000-0000-0000-000000000001', NOW() + INTERVAL '2 days', $2, $3, $4, 'Jl. Kaliurang KM 5', 'https://maps.example.com', 'UpdateTest Initial', $5, false, false)
			RETURNING id
		`, sponsorID, quota, heroQuota, remain, status).Scan(&dID)
		if err != nil {
			t.Fatalf("failed to insert initial donation: %v", err)
		}

		_, _ = pool.Exec(context.Background(), `
			INSERT INTO foods (donation_id, food_item_id, quantity, unit)
			VALUES ($1, $2, 10.00, 'portion')
		`, dID, foodItemID1)

		_, _ = pool.Exec(context.Background(), `
			INSERT INTO donation_target (donation_id, beneficiary_category_id)
			VALUES ($1, $2)
		`, dID, catID)

		return dID
	}

	t.Run("Update donation invalid UUID format returns 400 VAL_001", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/donations/invalid-uuid", strings.NewReader(`{}`))
		req.SetPathValue("id", "invalid-uuid")
		req = setStaffAuth(req)
		rec := httptest.NewRecorder()

		h.UpdateDonationHandler(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d", rec.Code)
		}
		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.Code != "VAL_001" {
			t.Errorf("expected VAL_001, got %s", resp.Code)
		}
	})

	t.Run("Update donation non-existent ID returns 404 NOT_FOUND", func(t *testing.T) {
		nonExistentID := uuid.NewString()
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/donations/"+nonExistentID, strings.NewReader(`{}`))
		req.SetPathValue("id", nonExistentID)
		req = setStaffAuth(req)
		rec := httptest.NewRecorder()

		h.UpdateDonationHandler(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 Not Found, got %d", rec.Code)
		}
	})

	t.Run("State Guard: completed donation is read-only (400 VAL_001)", func(t *testing.T) {
		completedID := createDonation("completed", 20, 10, 0, uniCatID)
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/donations/"+completedID.String(), strings.NewReader(`{"quota": 30}`))
		req.SetPathValue("id", completedID.String())
		req = setStaffAuth(req)
		rec := httptest.NewRecorder()

		h.UpdateDonationHandler(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d. Body: %s", rec.Code, rec.Body.String())
		}
		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.Code != "VAL_001" {
			t.Errorf("expected VAL_001, got %s", resp.Code)
		}
	})

	t.Run("State Guard: cancelled donation is read-only (400 VAL_001)", func(t *testing.T) {
		cancelledID := createDonation("cancelled", 20, 10, 5, uniCatID)
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/donations/"+cancelledID.String(), strings.NewReader(`{"quota": 30}`))
		req.SetPathValue("id", cancelledID.String())
		req = setStaffAuth(req)
		rec := httptest.NewRecorder()

		h.UpdateDonationHandler(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d. Body: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("Quota invariant: hero_quota cannot exceed total quota (400 VAL_001)", func(t *testing.T) {
		dID := createDonation("draft", 20, 10, 10, uniCatID)
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/donations/"+dID.String(), strings.NewReader(`{"hero_quota": 25}`))
		req.SetPathValue("id", dID.String())
		req = setStaffAuth(req)
		rec := httptest.NewRecorder()

		h.UpdateDonationHandler(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d", rec.Code)
		}
	})

	t.Run("Quota invariant: reducing hero_quota drops remain below zero (400 VAL_001)", func(t *testing.T) {
		// quota=20, hero_quota=10, remain=2 (meaning 8 spots were reserved)
		dID := createDonation("open", 20, 10, 2, uniCatID)
		// Try to set hero_quota=5 (delta = 5 - 10 = -5 => new remain = 2 - 5 = -3 < 0)
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/donations/"+dID.String(), strings.NewReader(`{"hero_quota": 5}`))
		req.SetPathValue("id", dID.String())
		req = setStaffAuth(req)
		rec := httptest.NewRecorder()

		h.UpdateDonationHandler(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d. Body: %s", rec.Code, rec.Body.String())
		}
		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.Code != "VAL_001" {
			t.Errorf("expected VAL_001, got %s", resp.Code)
		}
	})

	t.Run("Auto-Close: reducing hero_quota to reach remain = 0 switches open to closed", func(t *testing.T) {
		// quota=20, hero_quota=10, remain=3
		dID := createDonation("open", 20, 10, 3, uniCatID)
		// Set hero_quota=7 (delta = 7 - 10 = -3 => new remain = 0)
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/donations/"+dID.String(), strings.NewReader(`{"hero_quota": 7}`))
		req.SetPathValue("id", dID.String())
		req = setStaffAuth(req)
		rec := httptest.NewRecorder()

		h.UpdateDonationHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d. Body: %s", rec.Code, rec.Body.String())
		}
		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		dataBytes, _ := json.Marshal(resp.Data)
		var donationResp DonationStaffResponse
		_ = json.Unmarshal(dataBytes, &donationResp)

		if donationResp.Status != "closed" {
			t.Errorf("expected status 'closed', got '%s'", donationResp.Status)
		}
		if donationResp.Remain != 0 {
			t.Errorf("expected remain 0, got %d", donationResp.Remain)
		}
	})

	t.Run("Auto-Reopen: increasing hero_quota switches closed to open", func(t *testing.T) {
		// quota=20, hero_quota=10, remain=0, status=closed
		dID := createDonation("closed", 20, 10, 0, uniCatID)
		// Set hero_quota=15 (delta = 15 - 10 = +5 => new remain = 5)
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/donations/"+dID.String(), strings.NewReader(`{"hero_quota": 15}`))
		req.SetPathValue("id", dID.String())
		req = setStaffAuth(req)
		rec := httptest.NewRecorder()

		h.UpdateDonationHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d. Body: %s", rec.Code, rec.Body.String())
		}
		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		dataBytes, _ := json.Marshal(resp.Data)
		var donationResp DonationStaffResponse
		_ = json.Unmarshal(dataBytes, &donationResp)

		if donationResp.Status != "open" {
			t.Errorf("expected status 'open', got '%s'", donationResp.Status)
		}
		if donationResp.Remain != 5 {
			t.Errorf("expected remain 5, got %d", donationResp.Remain)
		}
	})

	t.Run("Organization-only target rejects hero_quota > 0 with 400 VAL_001", func(t *testing.T) {
		dID := createDonation("draft", 20, 0, 0, uniCatID)
		// Update target to shelter only, while hero_quota is 10
		body := fmt.Sprintf(`{"beneficiary_category_ids": ["%s"], "hero_quota": 10}`, orgCatID.String())
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/donations/"+dID.String(), strings.NewReader(body))
		req.SetPathValue("id", dID.String())
		req = setStaffAuth(req)
		rec := httptest.NewRecorder()

		h.UpdateDonationHandler(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d. Body: %s", rec.Code, rec.Body.String())
		}
		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.Code != "VAL_001" {
			t.Errorf("expected VAL_001, got %s", resp.Code)
		}
	})

	t.Run("Non-existent sponsor returns 400 VAL_002", func(t *testing.T) {
		dID := createDonation("draft", 20, 10, 10, uniCatID)
		body := fmt.Sprintf(`{"sponsor_id": "%s"}`, uuid.NewString())
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/donations/"+dID.String(), strings.NewReader(body))
		req.SetPathValue("id", dID.String())
		req = setStaffAuth(req)
		rec := httptest.NewRecorder()

		h.UpdateDonationHandler(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d", rec.Code)
		}
		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.Code != "VAL_002" {
			t.Errorf("expected VAL_002, got %s", resp.Code)
		}
	})

	t.Run("Non-existent food item returns 400 VAL_002", func(t *testing.T) {
		dID := createDonation("draft", 20, 10, 10, uniCatID)
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/donations/"+dID.String(), strings.NewReader(`{"foods": [{"food_item_id": 999999, "quantity": 5, "unit": "kg"}]}`))
		req.SetPathValue("id", dID.String())
		req = setStaffAuth(req)
		rec := httptest.NewRecorder()

		h.UpdateDonationHandler(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d", rec.Code)
		}
		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.Code != "VAL_002" {
			t.Errorf("expected VAL_002, got %s", resp.Code)
		}
	})

	t.Run("Happy path patch update on draft donation returns 200 OK", func(t *testing.T) {
		dID := createDonation("draft", 20, 10, 10, uniCatID)
		newAddr := "Jl. Solo KM 12, Sleman"
		newMsg := "UpdateTest Updated Message"
		newEventAt := time.Now().Add(72 * time.Hour).UTC().Format(time.RFC3339)
		newQuota := 40
		newHeroQuota := 20

		bodyObj := UpdateDonationRequest{
			SponsorID:              &[]string{newSponsorID.String()}[0],
			EventAt:                &newEventAt,
			Quota:                  &newQuota,
			HeroQuota:              &newHeroQuota,
			Address:                &newAddr,
			Message:                &newMsg,
			Foods:                  []FoodItemInput{{FoodItemID: foodItemID2, Quantity: 30.5, Unit: "pack"}},
			BeneficiaryCategoryIDs: []string{uniCatID.String(), orgCatID.String()},
		}
		bodyBytes, _ := json.Marshal(bodyObj)

		req := httptest.NewRequest(http.MethodPatch, "/api/v1/donations/"+dID.String(), bytes.NewReader(bodyBytes))
		req.SetPathValue("id", dID.String())
		req = setStaffAuth(req)
		rec := httptest.NewRecorder()

		h.UpdateDonationHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		dataBytes, _ := json.Marshal(resp.Data)
		var donationResp DonationStaffResponse
		_ = json.Unmarshal(dataBytes, &donationResp)

		if donationResp.Quota != 40 {
			t.Errorf("expected quota 40, got %d", donationResp.Quota)
		}
		if donationResp.HeroQuota != 20 {
			t.Errorf("expected hero_quota 20, got %d", donationResp.HeroQuota)
		}
		if donationResp.Remain != 20 {
			t.Errorf("expected remain 20 (delta +10), got %d", donationResp.Remain)
		}
		if donationResp.Address == nil || *donationResp.Address != newAddr {
			t.Errorf("expected address '%s', got %v", newAddr, donationResp.Address)
		}
		if donationResp.Message == nil || *donationResp.Message != newMsg {
			t.Errorf("expected message '%s', got %v", newMsg, donationResp.Message)
		}
		if donationResp.Sponsor.ID != newSponsorID.String() {
			t.Errorf("expected new sponsor ID %s, got %s", newSponsorID.String(), donationResp.Sponsor.ID)
		}
		if len(donationResp.Foods) != 1 || donationResp.Foods[0].FoodItemID != foodItemID2 {
			t.Errorf("expected replaced food item %d, got %+v", foodItemID2, donationResp.Foods)
		}
		if len(donationResp.Targets) != 2 {
			t.Errorf("expected 2 attached targets, got %d", len(donationResp.Targets))
		}
	})
}


func TestDonationLifecycleActions(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	cfg := config.LoadConfig()
	h := NewDonationHandler(pool, &cfg)

	defer func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM donations WHERE message LIKE 'LifecycleTest %'")
		_, _ = pool.Exec(context.Background(), "DELETE FROM sponsors WHERE name LIKE 'Lifecycle Sponsor %'")
	}()

	sponsorName := fmt.Sprintf("Lifecycle Sponsor %s", uuid.NewString()[:8])
	var sponsorID uuid.UUID
	err := pool.QueryRow(context.Background(), `
		INSERT INTO sponsors (name, variant, address, email, phone, hidden)
		VALUES ($1, 'company', 'Jl. Test KM 1', 'sponsor@lifecycle.com', '62811111111', false)
		RETURNING id
	`, sponsorName).Scan(&sponsorID)
	if err != nil {
		t.Fatalf("failed to insert test sponsor: %v", err)
	}

	var foodItemID int32
	err = pool.QueryRow(context.Background(), "SELECT id FROM food_items LIMIT 1").Scan(&foodItemID)
	if err != nil {
		t.Fatalf("failed to query food item: %v", err)
	}

	var uniCatID uuid.UUID
	err = pool.QueryRow(context.Background(), "SELECT id FROM beneficiary_categories WHERE variant IN ('university', 'kipk') AND deleted_at IS NULL LIMIT 1").Scan(&uniCatID)
	if err != nil {
		t.Fatalf("failed to query university category: %v", err)
	}

	var orgCatID uuid.UUID
	err = pool.QueryRow(context.Background(), "SELECT id FROM beneficiary_categories WHERE variant NOT IN ('university', 'kipk') AND deleted_at IS NULL LIMIT 1").Scan(&orgCatID)
	if err != nil {
		t.Fatalf("failed to query organization category: %v", err)
	}

	createDonation := func(status string, eventAt time.Time, quota, heroQuota, remain int32, withFood bool, catID *uuid.UUID) uuid.UUID {
		var dID uuid.UUID
		err := pool.QueryRow(context.Background(), `
			INSERT INTO donations (sponsor_id, created_by, event_at, quota, hero_quota, remain, address, maps, message, status, hide_from_public, hide_sponsor)
			VALUES ($1, '00000000-0000-0000-0000-000000000001', $2, $3, $4, $5, 'Jl. Test 123', 'https://maps.example.com', 'LifecycleTest ' || gen_random_uuid()::text, $6, false, false)
			RETURNING id
		`, sponsorID, eventAt, quota, heroQuota, remain, status).Scan(&dID)
		if err != nil {
			t.Fatalf("failed to insert test donation: %v", err)
		}

		if withFood {
			_, err = pool.Exec(context.Background(), `
				INSERT INTO foods (donation_id, food_item_id, quantity, unit)
				VALUES ($1, $2, 10.00, 'portion')
			`, dID, foodItemID)
			if err != nil {
				t.Fatalf("failed to insert food: %v", err)
			}
		}

		if catID != nil {
			_, err = pool.Exec(context.Background(), `
				INSERT INTO donation_target (donation_id, beneficiary_category_id)
				VALUES ($1, $2)
			`, dID, *catID)
			if err != nil {
				t.Fatalf("failed to insert target: %v", err)
			}
		}

		return dID
	}

	futureTime := time.Now().Add(24 * time.Hour)
	pastTime := time.Now().Add(-24 * time.Hour)

	t.Run("Invalid donation ID format returns 400", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/donations/not-a-uuid/publish", nil)
		req.SetPathValue("id", "not-a-uuid")
		req = setStaffAuth(req)
		rec := httptest.NewRecorder()

		h.PublishDonationHandler(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", rec.Code)
		}
	})

	t.Run("Non-existent donation ID returns 404", func(t *testing.T) {
		nonExistentID := uuid.NewString()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/donations/"+nonExistentID+"/publish", nil)
		req.SetPathValue("id", nonExistentID)
		req = setStaffAuth(req)
		rec := httptest.NewRecorder()

		h.PublishDonationHandler(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Errorf("expected 404, got %d", rec.Code)
		}
	})

	t.Run("Publish: Non-draft donation rejected", func(t *testing.T) {
		dID := createDonation("open", futureTime, 20, 10, 10, true, &uniCatID)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/donations/"+dID.String()+"/publish", nil)
		req.SetPathValue("id", dID.String())
		req = setStaffAuth(req)
		rec := httptest.NewRecorder()

		h.PublishDonationHandler(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d. Body: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("Publish: Draft with past event_at rejected", func(t *testing.T) {
		dID := createDonation("draft", pastTime, 20, 10, 10, true, &uniCatID)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/donations/"+dID.String()+"/publish", nil)
		req.SetPathValue("id", dID.String())
		req = setStaffAuth(req)
		rec := httptest.NewRecorder()

		h.PublishDonationHandler(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d. Body: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("Publish: Draft without foods rejected", func(t *testing.T) {
		dID := createDonation("draft", futureTime, 20, 10, 10, false, &uniCatID)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/donations/"+dID.String()+"/publish", nil)
		req.SetPathValue("id", dID.String())
		req = setStaffAuth(req)
		rec := httptest.NewRecorder()

		h.PublishDonationHandler(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d. Body: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("Publish: Draft with organization-only target and hero_quota > 0 rejected", func(t *testing.T) {
		dID := createDonation("draft", futureTime, 20, 10, 10, true, &orgCatID)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/donations/"+dID.String()+"/publish", nil)
		req.SetPathValue("id", dID.String())
		req = setStaffAuth(req)
		rec := httptest.NewRecorder()

		h.PublishDonationHandler(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d. Body: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("Publish: Valid draft transitions to open", func(t *testing.T) {
		dID := createDonation("draft", futureTime, 20, 10, 10, true, &uniCatID)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/donations/"+dID.String()+"/publish", nil)
		req.SetPathValue("id", dID.String())
		req = setStaffAuth(req)
		rec := httptest.NewRecorder()

		h.PublishDonationHandler(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		var dbStatus string
		_ = pool.QueryRow(context.Background(), "SELECT status FROM donations WHERE id = $1", dID).Scan(&dbStatus)
		if dbStatus != "open" {
			t.Errorf("expected db status open, got %s", dbStatus)
		}
	})

	t.Run("Publish: Draft with remain == 0 and hero_quota > 0 transitions to closed", func(t *testing.T) {
		dID := createDonation("draft", futureTime, 20, 10, 0, true, &uniCatID)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/donations/"+dID.String()+"/publish", nil)
		req.SetPathValue("id", dID.String())
		req = setStaffAuth(req)
		rec := httptest.NewRecorder()

		h.PublishDonationHandler(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		var dbStatus string
		_ = pool.QueryRow(context.Background(), "SELECT status FROM donations WHERE id = $1", dID).Scan(&dbStatus)
		if dbStatus != "closed" {
			t.Errorf("expected db status closed, got %s", dbStatus)
		}
	})

	t.Run("Close: Non-open donation rejected", func(t *testing.T) {
		dID := createDonation("draft", futureTime, 20, 10, 10, true, &uniCatID)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/donations/"+dID.String()+"/close", nil)
		req.SetPathValue("id", dID.String())
		req = setStaffAuth(req)
		rec := httptest.NewRecorder()

		h.CloseDonationHandler(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d. Body: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("Close: Already closed donation rejected", func(t *testing.T) {
		dID := createDonation("closed", futureTime, 20, 10, 0, true, &uniCatID)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/donations/"+dID.String()+"/close", nil)
		req.SetPathValue("id", dID.String())
		req = setStaffAuth(req)
		rec := httptest.NewRecorder()

		h.CloseDonationHandler(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d. Body: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("Close: Valid open donation transitions to closed", func(t *testing.T) {
		dID := createDonation("open", futureTime, 20, 10, 10, true, &uniCatID)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/donations/"+dID.String()+"/close", nil)
		req.SetPathValue("id", dID.String())
		req = setStaffAuth(req)
		rec := httptest.NewRecorder()

		h.CloseDonationHandler(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		var dbStatus string
		_ = pool.QueryRow(context.Background(), "SELECT status FROM donations WHERE id = $1", dID).Scan(&dbStatus)
		if dbStatus != "closed" {
			t.Errorf("expected db status closed, got %s", dbStatus)
		}
	})

	t.Run("Reopen: Non-closed donation rejected", func(t *testing.T) {
		dID := createDonation("open", futureTime, 20, 10, 10, true, &uniCatID)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/donations/"+dID.String()+"/reopen", nil)
		req.SetPathValue("id", dID.String())
		req = setStaffAuth(req)
		rec := httptest.NewRecorder()

		h.ReopenDonationHandler(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d. Body: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("Reopen: Closed donation with zero remain rejected", func(t *testing.T) {
		dID := createDonation("closed", futureTime, 20, 10, 0, true, &uniCatID)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/donations/"+dID.String()+"/reopen", nil)
		req.SetPathValue("id", dID.String())
		req = setStaffAuth(req)
		rec := httptest.NewRecorder()

		h.ReopenDonationHandler(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d. Body: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("Reopen: Closed donation with past event_at rejected", func(t *testing.T) {
		dID := createDonation("closed", pastTime, 20, 10, 5, true, &uniCatID)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/donations/"+dID.String()+"/reopen", nil)
		req.SetPathValue("id", dID.String())
		req = setStaffAuth(req)
		rec := httptest.NewRecorder()

		h.ReopenDonationHandler(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d. Body: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("Reopen: Valid closed donation transitions to open", func(t *testing.T) {
		dID := createDonation("closed", futureTime, 20, 10, 5, true, &uniCatID)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/donations/"+dID.String()+"/reopen", nil)
		req.SetPathValue("id", dID.String())
		req = setStaffAuth(req)
		rec := httptest.NewRecorder()

		h.ReopenDonationHandler(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		var dbStatus string
		_ = pool.QueryRow(context.Background(), "SELECT status FROM donations WHERE id = $1", dID).Scan(&dbStatus)
		if dbStatus != "open" {
			t.Errorf("expected db status open, got %s", dbStatus)
		}
	})

	t.Run("Complete: Draft or cancelled donation rejected", func(t *testing.T) {
		dID1 := createDonation("draft", futureTime, 20, 10, 10, true, &uniCatID)
		req1 := httptest.NewRequest(http.MethodPost, "/api/v1/donations/"+dID1.String()+"/complete", nil)
		req1.SetPathValue("id", dID1.String())
		req1 = setStaffAuth(req1)
		rec1 := httptest.NewRecorder()
		h.CompleteDonationHandler(rec1, req1)
		if rec1.Code != http.StatusBadRequest {
			t.Errorf("expected 400 for draft, got %d", rec1.Code)
		}

		dID2 := createDonation("cancelled", futureTime, 20, 10, 10, true, &uniCatID)
		req2 := httptest.NewRequest(http.MethodPost, "/api/v1/donations/"+dID2.String()+"/complete", nil)
		req2.SetPathValue("id", dID2.String())
		req2 = setStaffAuth(req2)
		rec2 := httptest.NewRecorder()
		h.CompleteDonationHandler(rec2, req2)
		if rec2.Code != http.StatusBadRequest {
			t.Errorf("expected 400 for cancelled, got %d", rec2.Code)
		}
	})

	t.Run("Complete: Already completed donation rejected", func(t *testing.T) {
		dID := createDonation("completed", pastTime, 20, 10, 0, true, &uniCatID)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/donations/"+dID.String()+"/complete", nil)
		req.SetPathValue("id", dID.String())
		req = setStaffAuth(req)
		rec := httptest.NewRecorder()

		h.CompleteDonationHandler(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d. Body: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("Complete: Valid open and closed donations transition to completed", func(t *testing.T) {
		dIDOpen := createDonation("open", futureTime, 20, 10, 5, true, &uniCatID)
		reqOpen := httptest.NewRequest(http.MethodPost, "/api/v1/donations/"+dIDOpen.String()+"/complete", nil)
		reqOpen.SetPathValue("id", dIDOpen.String())
		reqOpen = setStaffAuth(reqOpen)
		recOpen := httptest.NewRecorder()
		h.CompleteDonationHandler(recOpen, reqOpen)
		if recOpen.Code != http.StatusOK {
			t.Fatalf("expected 200 OK for open, got %d. Body: %s", recOpen.Code, recOpen.Body.String())
		}

		dIDClosed := createDonation("closed", futureTime, 20, 10, 0, true, &uniCatID)
		reqClosed := httptest.NewRequest(http.MethodPost, "/api/v1/donations/"+dIDClosed.String()+"/complete", nil)
		reqClosed.SetPathValue("id", dIDClosed.String())
		reqClosed = setStaffAuth(reqClosed)
		recClosed := httptest.NewRecorder()
		h.CompleteDonationHandler(recClosed, reqClosed)
		if recClosed.Code != http.StatusOK {
			t.Fatalf("expected 200 OK for closed, got %d. Body: %s", recClosed.Code, recClosed.Body.String())
		}
	})

	t.Run("Cancel: Completed donation cannot be cancelled", func(t *testing.T) {
		dID := createDonation("completed", pastTime, 20, 10, 0, true, &uniCatID)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/donations/"+dID.String()+"/cancel", nil)
		req.SetPathValue("id", dID.String())
		req = setStaffAuth(req)
		rec := httptest.NewRecorder()

		h.CancelDonationHandler(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d. Body: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("Cancel: Already cancelled donation rejected", func(t *testing.T) {
		dID := createDonation("cancelled", futureTime, 20, 10, 10, true, &uniCatID)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/donations/"+dID.String()+"/cancel", nil)
		req.SetPathValue("id", dID.String())
		req = setStaffAuth(req)
		rec := httptest.NewRecorder()

		h.CancelDonationHandler(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d. Body: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("Cancel: Draft, open, and closed donations transition to cancelled", func(t *testing.T) {
		for _, status := range []string{"draft", "open", "closed"} {
			dID := createDonation(status, futureTime, 20, 10, 5, true, &uniCatID)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/donations/"+dID.String()+"/cancel", nil)
			req.SetPathValue("id", dID.String())
			req = setStaffAuth(req)
			rec := httptest.NewRecorder()

			h.CancelDonationHandler(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("expected 200 OK for %s, got %d. Body: %s", status, rec.Code, rec.Body.String())
			}

			var dbStatus string
			_ = pool.QueryRow(context.Background(), "SELECT status FROM donations WHERE id = $1", dID).Scan(&dbStatus)
			if dbStatus != "cancelled" {
				t.Errorf("expected db status cancelled for %s, got %s", status, dbStatus)
			}
		}
	})
}


func TestDeleteDonationHandler(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	cfg := config.LoadConfig()
	h := NewDonationHandler(pool, &cfg)

	defer func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM donations WHERE message LIKE 'DeleteTest %'")
		_, _ = pool.Exec(context.Background(), "DELETE FROM sponsors WHERE name LIKE 'Delete Sponsor %'")
	}()

	sponsorName := fmt.Sprintf("Delete Sponsor %s", uuid.NewString()[:8])
	var sponsorID uuid.UUID
	err := pool.QueryRow(context.Background(), `
		INSERT INTO sponsors (name, variant, address, email, phone, hidden)
		VALUES ($1, 'company', 'Jl. Test KM 2', 'sponsor@delete.com', '62822222222', false)
		RETURNING id
	`, sponsorName).Scan(&sponsorID)
	if err != nil {
		t.Fatalf("failed to insert test sponsor: %v", err)
	}

	createDonation := func(status string) uuid.UUID {
		var dID uuid.UUID
		err := pool.QueryRow(context.Background(), `
			INSERT INTO donations (sponsor_id, created_by, event_at, quota, hero_quota, remain, address, maps, message, status, hide_from_public, hide_sponsor)
			VALUES ($1, '00000000-0000-0000-0000-000000000001', NOW() + INTERVAL '1 day', 20, 10, 10, 'Jl. Test 456', 'https://maps.example.com', 'DeleteTest ' || gen_random_uuid()::text, $2, false, false)
			RETURNING id
		`, sponsorID, status).Scan(&dID)
		if err != nil {
			t.Fatalf("failed to insert test donation: %v", err)
		}
		return dID
	}

	t.Run("Invalid donation ID format returns 400", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/donations/not-a-uuid", nil)
		req.SetPathValue("id", "not-a-uuid")
		req = setStaffAuth(req)
		rec := httptest.NewRecorder()

		h.DeleteDonationHandler(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", rec.Code)
		}
	})

	t.Run("Non-existent donation ID returns 404", func(t *testing.T) {
		nonExistentID := uuid.NewString()
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/donations/"+nonExistentID, nil)
		req.SetPathValue("id", nonExistentID)
		req = setStaffAuth(req)
		rec := httptest.NewRecorder()

		h.DeleteDonationHandler(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Errorf("expected 404, got %d", rec.Code)
		}
	})

	t.Run("State Guard: non-draft donations cannot be deleted", func(t *testing.T) {
		for _, status := range []string{"open", "closed", "completed", "cancelled"} {
			dID := createDonation(status)
			req := httptest.NewRequest(http.MethodDelete, "/api/v1/donations/"+dID.String(), nil)
			req.SetPathValue("id", dID.String())
			req = setStaffAuth(req)
			rec := httptest.NewRecorder()

			h.DeleteDonationHandler(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400 for status %s, got %d. Body: %s", status, rec.Code, rec.Body.String())
			}
		}
	})

	t.Run("Draft donation is successfully soft deleted", func(t *testing.T) {
		dID := createDonation("draft")
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/donations/"+dID.String(), nil)
		req.SetPathValue("id", dID.String())
		req = setStaffAuth(req)
		rec := httptest.NewRecorder()

		h.DeleteDonationHandler(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		var deletedAt *time.Time
		err := pool.QueryRow(context.Background(), "SELECT deleted_at FROM donations WHERE id = $1", dID).Scan(&deletedAt)
		if err != nil || deletedAt == nil {
			t.Fatalf("expected deleted_at to be populated, err: %v", err)
		}

		// Re-deleting now returns 404
		req2 := httptest.NewRequest(http.MethodDelete, "/api/v1/donations/"+dID.String(), nil)
		req2.SetPathValue("id", dID.String())
		req2 = setStaffAuth(req2)
		rec2 := httptest.NewRecorder()
		h.DeleteDonationHandler(rec2, req2)
		if rec2.Code != http.StatusNotFound {
			t.Errorf("expected 404 on re-delete, got %d", rec2.Code)
		}
	})
}


func TestSoftDeletedSponsorMasking(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	cfg := config.LoadConfig()
	h := NewDonationHandler(pool, &cfg)

	defer func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM donations WHERE message LIKE 'SoftDeleteTest%'")
		_, _ = pool.Exec(context.Background(), "DELETE FROM sponsors WHERE name LIKE 'SoftDelete Sponsor%'")
		_, _ = pool.Exec(context.Background(), "DELETE FROM beneficiary_categories WHERE name LIKE 'SoftDelete Cat%'")
		_, _ = pool.Exec(context.Background(), "DELETE FROM food_items WHERE name LIKE 'SoftDelete %'")
	}()

	catName := "SoftDelete Cat " + uuid.NewString()[:8]
	var catID uuid.UUID
	err := pool.QueryRow(context.Background(), "INSERT INTO beneficiary_categories (name, variant) VALUES ($1, 'other') RETURNING id", catName).Scan(&catID)
	if err != nil {
		t.Fatalf("failed to insert category: %v", err)
	}

	foodName := "SoftDelete Rice Box " + uuid.NewString()[:8]
	var foodItemID int32
	err = pool.QueryRow(context.Background(), "INSERT INTO food_items (name) VALUES ($1) RETURNING id", foodName).Scan(&foodItemID)
	if err != nil {
		t.Fatalf("failed to insert food item: %v", err)
	}

	sponsorName := "SoftDelete Sponsor " + uuid.NewString()[:8]
	var sponsorID uuid.UUID
	err = pool.QueryRow(context.Background(),
		"INSERT INTO sponsors (name, variant, email, phone, address, hidden, hide_donations) VALUES ($1, 'company', 'soft@sponsor.org', '62811223344', 'Jl. Kaliurang', false, false) RETURNING id",
		sponsorName,
	).Scan(&sponsorID)
	if err != nil {
		t.Fatalf("failed to insert sponsor: %v", err)
	}

	var donationID uuid.UUID
	msg := "SoftDeleteTest Event"
	err = pool.QueryRow(context.Background(),
		"INSERT INTO donations (sponsor_id, created_by, event_at, quota, hero_quota, remain, message, status, hide_from_public, hide_sponsor) VALUES ($1, '00000000-0000-0000-0000-000000000001', NOW() + INTERVAL '24 hours', 20, 10, 10, $2, 'open', false, false) RETURNING id",
		sponsorID, msg,
	).Scan(&donationID)
	if err != nil {
		t.Fatalf("failed to insert donation: %v", err)
	}

	_, _ = pool.Exec(context.Background(), "INSERT INTO foods (donation_id, food_item_id, quantity, unit) VALUES ($1, $2, 5, 'portion')", donationID, foodItemID)
	_, _ = pool.Exec(context.Background(), "INSERT INTO donation_target (donation_id, beneficiary_category_id) VALUES ($1, $2)", donationID, catID)

	// Before soft delete: Public detail sees real name
	{
		req := httptest.NewRequest(http.MethodGet, "/api/v1/donations/"+donationID.String(), nil)
		req.SetPathValue("id", donationID.String())
		rec := httptest.NewRecorder()
		h.GetDonationByIDHandler(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK before delete, got %d", rec.Code)
		}
		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		dataBytes, _ := json.Marshal(resp.Data)
		var pubResp DonationPublicResponse
		_ = json.Unmarshal(dataBytes, &pubResp)
		if pubResp.Sponsor.Name != sponsorName {
			t.Errorf("expected real sponsor name %s before soft delete, got %s", sponsorName, pubResp.Sponsor.Name)
		}
		if pubResp.Sponsor.ID == nil || *pubResp.Sponsor.ID != sponsorID.String() {
			t.Errorf("expected sponsor ID %s before soft delete, got %v", sponsorID.String(), pubResp.Sponsor.ID)
		}
	}

	// Soft-delete the sponsor
	_, err = pool.Exec(context.Background(), "UPDATE sponsors SET deleted_at = NOW() WHERE id = $1", sponsorID)
	if err != nil {
		t.Fatalf("failed to soft-delete sponsor: %v", err)
	}

	// After soft delete: Public detail masks sponsor
	{
		req := httptest.NewRequest(http.MethodGet, "/api/v1/donations/"+donationID.String(), nil)
		req.SetPathValue("id", donationID.String())
		rec := httptest.NewRecorder()
		h.GetDonationByIDHandler(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK after delete, got %d", rec.Code)
		}
		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		dataBytes, _ := json.Marshal(resp.Data)
		var pubResp DonationPublicResponse
		_ = json.Unmarshal(dataBytes, &pubResp)
		if pubResp.Sponsor.Name != "Generous sponsor" {
			t.Errorf("expected masked sponsor name 'Generous sponsor' after soft delete, got %s", pubResp.Sponsor.Name)
		}
		if pubResp.Sponsor.ID != nil {
			t.Errorf("expected nil sponsor ID after soft delete, got %v", pubResp.Sponsor.ID)
		}
		if pubResp.Sponsor.Logo != nil {
			t.Errorf("expected nil sponsor logo after soft delete, got %v", pubResp.Sponsor.Logo)
		}
	}

	// After soft delete: Public list masks sponsor
	{
		req := httptest.NewRequest(http.MethodGet, "/api/v1/donations", nil)
		rec := httptest.NewRecorder()
		h.ListDonationsHandler(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK for public list, got %d", rec.Code)
		}
		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		dataBytes, _ := json.Marshal(resp.Data)
		var list []DonationPublicResponse
		_ = json.Unmarshal(dataBytes, &list)
		found := false
		for _, item := range list {
			if item.ID == donationID.String() {
				found = true
				if item.Sponsor.Name != "Generous sponsor" {
					t.Errorf("expected masked sponsor in public list, got %s", item.Sponsor.Name)
				}
				if item.Sponsor.ID != nil {
					t.Errorf("expected nil sponsor ID in public list, got %v", item.Sponsor.ID)
				}
			}
		}
		if !found {
			t.Errorf("donation %s not found in public list", donationID.String())
		}
	}

	// After soft delete: Staff detail sees real name + is_deleted = true
	{
		req := httptest.NewRequest(http.MethodGet, "/api/v1/donations/"+donationID.String(), nil)
		req.SetPathValue("id", donationID.String())
		req = setStaffAuth(req)
		rec := httptest.NewRecorder()
		h.GetDonationByIDHandler(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK for staff detail, got %d", rec.Code)
		}
		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		dataBytes, _ := json.Marshal(resp.Data)
		var staffResp DonationStaffResponse
		_ = json.Unmarshal(dataBytes, &staffResp)
		if staffResp.Sponsor.Name != sponsorName {
			t.Errorf("expected staff to see real sponsor name %s, got %s", sponsorName, staffResp.Sponsor.Name)
		}
		if !staffResp.Sponsor.IsDeleted {
			t.Errorf("expected staff to see is_deleted = true, got %v", staffResp.Sponsor.IsDeleted)
		}
	}

	// After soft delete: Staff list sees real name + is_deleted = true
	{
		req := httptest.NewRequest(http.MethodGet, "/api/v1/donations/staff", nil)
		req = setStaffAuth(req)
		rec := httptest.NewRecorder()
		h.ListDonationsHandler(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK for staff list, got %d", rec.Code)
		}
		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		dataBytes, _ := json.Marshal(resp.Data)
		var list []DonationStaffResponse
		_ = json.Unmarshal(dataBytes, &list)
		found := false
		for _, item := range list {
			if item.ID == donationID.String() {
				found = true
				if item.Sponsor.Name != sponsorName {
					t.Errorf("expected staff list to see real sponsor name %s, got %s", sponsorName, item.Sponsor.Name)
				}
				if !item.Sponsor.IsDeleted {
					t.Errorf("expected staff list to see is_deleted = true, got %v", item.Sponsor.IsDeleted)
				}
			}
		}
		if !found {
			t.Errorf("donation %s not found in staff list", donationID.String())
		}
	}
}

func TestPhase2CorrectnessAndDomainStateMachine(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	cfg := config.LoadConfig()
	h := NewDonationHandler(pool, &cfg)

	defer func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM donations WHERE message LIKE 'Phase2Test%'")
		_, _ = pool.Exec(context.Background(), "DELETE FROM sponsors WHERE name LIKE 'Phase2 Sponsor%'")
		_, _ = pool.Exec(context.Background(), "DELETE FROM beneficiary_categories WHERE name LIKE 'Phase2 Cat%'")
		_, _ = pool.Exec(context.Background(), "DELETE FROM food_items WHERE name LIKE 'Phase2 Food%'")
	}()

	foodName := "Phase2 Food " + uuid.NewString()[:8]
	var foodItemID int32
	err := pool.QueryRow(context.Background(), "INSERT INTO food_items (name) VALUES ($1) RETURNING id", foodName).Scan(&foodItemID)
	if err != nil {
		t.Fatalf("failed to insert food item: %v", err)
	}

	sponsorName := "Phase2 Sponsor " + uuid.NewString()[:8]
	var sponsorID uuid.UUID
	err = pool.QueryRow(context.Background(),
		"INSERT INTO sponsors (name, variant, hidden, hide_donations) VALUES ($1, 'company', false, false) RETURNING id",
		sponsorName,
	).Scan(&sponsorID)
	if err != nil {
		t.Fatalf("failed to insert sponsor: %v", err)
	}

	var unhiddenCatID, hiddenCatID1, hiddenCatID2, orgCatID uuid.UUID
	err = pool.QueryRow(context.Background(), "INSERT INTO beneficiary_categories (name, variant, hidden) VALUES ($1, 'university', false) RETURNING id", "Phase2 Cat Uni "+uuid.NewString()[:8]).Scan(&unhiddenCatID)
	if err != nil {
		t.Fatalf("failed to insert unhidden cat: %v", err)
	}
	err = pool.QueryRow(context.Background(), "INSERT INTO beneficiary_categories (name, variant, hidden) VALUES ($1, 'orphanage', true) RETURNING id", "Phase2 Cat Hidden 1 "+uuid.NewString()[:8]).Scan(&hiddenCatID1)
	if err != nil {
		t.Fatalf("failed to insert hidden cat 1: %v", err)
	}
	err = pool.QueryRow(context.Background(), "INSERT INTO beneficiary_categories (name, variant, hidden) VALUES ($1, 'nursing_home', true) RETURNING id", "Phase2 Cat Hidden 2 "+uuid.NewString()[:8]).Scan(&hiddenCatID2)
	if err != nil {
		t.Fatalf("failed to insert hidden cat 2: %v", err)
	}
	err = pool.QueryRow(context.Background(), "INSERT INTO beneficiary_categories (name, variant, hidden) VALUES ($1, 'shelter', false) RETURNING id", "Phase2 Cat Org "+uuid.NewString()[:8]).Scan(&orgCatID)
	if err != nil {
		t.Fatalf("failed to insert org cat: %v", err)
	}

	t.Run("B1 Fix: Org-only open donation remains open on unrelated PATCH", func(t *testing.T) {
		var dID uuid.UUID
		err := pool.QueryRow(context.Background(),
			"INSERT INTO donations (sponsor_id, created_by, event_at, quota, hero_quota, remain, message, status, hide_from_public) VALUES ($1, '00000000-0000-0000-0000-000000000001', NOW() + INTERVAL '24 hours', 50, 0, 0, 'Phase2Test Org-Only Open', 'open', false) RETURNING id",
			sponsorID,
		).Scan(&dID)
		if err != nil {
			t.Fatalf("failed to insert donation: %v", err)
		}
		_, _ = pool.Exec(context.Background(), "INSERT INTO foods (donation_id, food_item_id, quantity, unit) VALUES ($1, $2, 10, 'portion')", dID, foodItemID)
		_, _ = pool.Exec(context.Background(), "INSERT INTO donation_target (donation_id, beneficiary_category_id) VALUES ($1, $2)", dID, orgCatID)

		req := httptest.NewRequest(http.MethodPatch, "/api/v1/donations/"+dID.String(), strings.NewReader(`{"message": "Phase2Test Updated Note"}`))
		req.SetPathValue("id", dID.String())
		req = setStaffAuth(req)
		rec := httptest.NewRecorder()

		h.UpdateDonationHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d. Body: %s", rec.Code, rec.Body.String())
		}
		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		dataBytes, _ := json.Marshal(resp.Data)
		var staffResp DonationStaffResponse
		_ = json.Unmarshal(dataBytes, &staffResp)

		if staffResp.Status != "open" {
			t.Errorf("B1 regression! Org-only donation flipped from open to '%s' on unrelated message edit", staffResp.Status)
		}
	})

	t.Run("B1 Fix: Manually closed donation with spots remaining stays closed on message edit", func(t *testing.T) {
		var dID uuid.UUID
		err := pool.QueryRow(context.Background(),
			"INSERT INTO donations (sponsor_id, created_by, event_at, quota, hero_quota, remain, message, status, hide_from_public) VALUES ($1, '00000000-0000-0000-0000-000000000001', NOW() + INTERVAL '24 hours', 50, 20, 10, 'Phase2Test Manually Closed', 'closed', false) RETURNING id",
			sponsorID,
		).Scan(&dID)
		if err != nil {
			t.Fatalf("failed to insert donation: %v", err)
		}

		req := httptest.NewRequest(http.MethodPatch, "/api/v1/donations/"+dID.String(), strings.NewReader(`{"message": "Phase2Test Closed Note Update"}`))
		req.SetPathValue("id", dID.String())
		req = setStaffAuth(req)
		rec := httptest.NewRecorder()

		h.UpdateDonationHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rec.Code)
		}
		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		dataBytes, _ := json.Marshal(resp.Data)
		var staffResp DonationStaffResponse
		_ = json.Unmarshal(dataBytes, &staffResp)

		if staffResp.Status != "closed" {
			t.Errorf("expected status to stay 'closed', got '%s'", staffResp.Status)
		}
	})

	t.Run("B1 Fix: Auto-reopen does not reopen event with past event_at", func(t *testing.T) {
		var dID uuid.UUID
		err := pool.QueryRow(context.Background(),
			"INSERT INTO donations (sponsor_id, created_by, event_at, quota, hero_quota, remain, message, status, hide_from_public) VALUES ($1, '00000000-0000-0000-0000-000000000001', NOW() - INTERVAL '2 hours', 50, 10, 0, 'Phase2Test Past Closed', 'closed', false) RETURNING id",
			sponsorID,
		).Scan(&dID)
		if err != nil {
			t.Fatalf("failed to insert donation: %v", err)
		}

		req := httptest.NewRequest(http.MethodPatch, "/api/v1/donations/"+dID.String(), strings.NewReader(`{"hero_quota": 15}`))
		req.SetPathValue("id", dID.String())
		req = setStaffAuth(req)
		rec := httptest.NewRecorder()

		h.UpdateDonationHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d. Body: %s", rec.Code, rec.Body.String())
		}
		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		dataBytes, _ := json.Marshal(resp.Data)
		var staffResp DonationStaffResponse
		_ = json.Unmarshal(dataBytes, &staffResp)

		if staffResp.Status != "closed" {
			t.Errorf("expected past event to remain 'closed' despite quota increase, got '%s'", staffResp.Status)
		}
		if staffResp.Remain != 5 {
			t.Errorf("expected remain = 5, got %d", staffResp.Remain)
		}
	})

	t.Run("B2 Fix: is_full strictly requires hero_quota > 0 && remain == 0", func(t *testing.T) {
		var orgID uuid.UUID
		err := pool.QueryRow(context.Background(),
			"INSERT INTO donations (sponsor_id, created_by, event_at, quota, hero_quota, remain, message, status, hide_from_public) VALUES ($1, '00000000-0000-0000-0000-000000000001', NOW() + INTERVAL '24 hours', 50, 0, 0, 'Phase2Test Org-Only IsFull', 'open', false) RETURNING id",
			sponsorID,
		).Scan(&orgID)
		if err != nil {
			t.Fatalf("failed to insert org-only donation: %v", err)
		}

		pubReq := httptest.NewRequest(http.MethodGet, "/api/v1/donations/"+orgID.String(), nil)
		pubReq.SetPathValue("id", orgID.String())
		pubRec := httptest.NewRecorder()
		h.GetDonationByIDHandler(pubRec, pubReq)
		if pubRec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", pubRec.Code)
		}
		var pubResp1 response.Response
		_ = json.Unmarshal(pubRec.Body.Bytes(), &pubResp1)
		dataBytes1, _ := json.Marshal(pubResp1.Data)
		var pubDonation1 DonationPublicResponse
		_ = json.Unmarshal(dataBytes1, &pubDonation1)

		if pubDonation1.IsFull {
			t.Errorf("B2 regression! Org-only donation (hero_quota: 0) had is_full: true")
		}

		var fullID uuid.UUID
		err = pool.QueryRow(context.Background(),
			"INSERT INTO donations (sponsor_id, created_by, event_at, quota, hero_quota, remain, message, status, hide_from_public) VALUES ($1, '00000000-0000-0000-0000-000000000001', NOW() + INTERVAL '24 hours', 50, 10, 0, 'Phase2Test Full Hero', 'closed', false) RETURNING id",
			sponsorID,
		).Scan(&fullID)
		if err != nil {
			t.Fatalf("failed to insert full donation: %v", err)
		}

		pubReq2 := httptest.NewRequest(http.MethodGet, "/api/v1/donations/"+fullID.String(), nil)
		pubReq2.SetPathValue("id", fullID.String())
		pubRec2 := httptest.NewRecorder()
		h.GetDonationByIDHandler(pubRec2, pubReq2)
		if pubRec2.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", pubRec2.Code)
		}
		var pubResp2 response.Response
		_ = json.Unmarshal(pubRec2.Body.Bytes(), &pubResp2)
		dataBytes2, _ := json.Marshal(pubResp2.Data)
		var pubDonation2 DonationPublicResponse
		_ = json.Unmarshal(dataBytes2, &pubDonation2)

		if !pubDonation2.IsFull {
			t.Errorf("expected is_full: true for filled hero quota, got false")
		}

		var closedWithSpotsID uuid.UUID
		err = pool.QueryRow(context.Background(),
			"INSERT INTO donations (sponsor_id, created_by, event_at, quota, hero_quota, remain, message, status, hide_from_public) VALUES ($1, '00000000-0000-0000-0000-000000000001', NOW() + INTERVAL '24 hours', 50, 10, 5, 'Phase2Test Closed With Spots', 'closed', false) RETURNING id",
			sponsorID,
		).Scan(&closedWithSpotsID)
		if err != nil {
			t.Fatalf("failed to insert closed donation with spots: %v", err)
		}

		pubReq3 := httptest.NewRequest(http.MethodGet, "/api/v1/donations/"+closedWithSpotsID.String(), nil)
		pubReq3.SetPathValue("id", closedWithSpotsID.String())
		pubRec3 := httptest.NewRecorder()
		h.GetDonationByIDHandler(pubRec3, pubReq3)
		if pubRec3.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", pubRec3.Code)
		}
		var pubResp3 response.Response
		_ = json.Unmarshal(pubRec3.Body.Bytes(), &pubResp3)
		dataBytes3, _ := json.Marshal(pubResp3.Data)
		var pubDonation3 DonationPublicResponse
		_ = json.Unmarshal(dataBytes3, &pubDonation3)

		if pubDonation3.IsFull {
			t.Errorf("B2 regression! Manually closed donation with remain=5 had is_full: true")
		}
	})

	t.Run("B3 / D1: Optional targets on create and update", func(t *testing.T) {
		body := fmt.Sprintf(`{
			"sponsor_id": "%s",
			"event_at": "%s",
			"quota": 30,
			"hero_quota": 20,
			"foods": [{"food_item_id": %d, "quantity": 10, "unit": "portion"}],
			"beneficiary_category_ids": []
		}`, sponsorID.String(), time.Now().Add(24*time.Hour).Format(time.RFC3339), foodItemID)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/donations", strings.NewReader(body))
		req = setStaffAuth(req)
		rec := httptest.NewRecorder()

		h.CreateDonationHandler(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created with empty targets, got %d. Body: %s", rec.Code, rec.Body.String())
		}
		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		dataBytes, _ := json.Marshal(resp.Data)
		var created DonationStaffResponse
		_ = json.Unmarshal(dataBytes, &created)

		if len(created.Targets) != 0 {
			t.Errorf("expected 0 targets on created general food drive, got %d", len(created.Targets))
		}

		body2 := fmt.Sprintf(`{"beneficiary_category_ids": ["%s"], "hero_quota": 0}`, orgCatID.String())
		req2 := httptest.NewRequest(http.MethodPatch, "/api/v1/donations/"+created.ID, strings.NewReader(body2))
		req2.SetPathValue("id", created.ID)
		req2 = setStaffAuth(req2)
		rec2 := httptest.NewRecorder()

		h.UpdateDonationHandler(rec2, req2)
		if rec2.Code != http.StatusOK {
			t.Fatalf("expected 200 OK adding target, got %d. Body: %s", rec2.Code, rec2.Body.String())
		}

		body3 := `{"beneficiary_category_ids": []}`
		req3 := httptest.NewRequest(http.MethodPatch, "/api/v1/donations/"+created.ID, strings.NewReader(body3))
		req3.SetPathValue("id", created.ID)
		req3 = setStaffAuth(req3)
		rec3 := httptest.NewRecorder()

		h.UpdateDonationHandler(rec3, req3)
		if rec3.Code != http.StatusOK {
			t.Fatalf("expected 200 OK clearing targets with empty slice, got %d. Body: %s", rec3.Code, rec3.Body.String())
		}
		var resp3 response.Response
		_ = json.Unmarshal(rec3.Body.Bytes(), &resp3)
		dataBytes3, _ := json.Marshal(resp3.Data)
		var cleared DonationStaffResponse
		_ = json.Unmarshal(dataBytes3, &cleared)

		if len(cleared.Targets) != 0 {
			t.Errorf("expected targets to be cleared to 0 items, got %d", len(cleared.Targets))
		}
	})

	t.Run("B3 / Peer Review 4: Multiple hidden categories collapsed into single Hidden institution item", func(t *testing.T) {
		var multiCatID uuid.UUID
		err := pool.QueryRow(context.Background(),
			"INSERT INTO donations (sponsor_id, created_by, event_at, quota, hero_quota, remain, message, status, hide_from_public) VALUES ($1, '00000000-0000-0000-0000-000000000001', NOW() + INTERVAL '24 hours', 50, 10, 10, 'Phase2Test Multi-Cat Hidden Collapse', 'open', false) RETURNING id",
			sponsorID,
		).Scan(&multiCatID)
		if err != nil {
			t.Fatalf("failed to insert multi-cat donation: %v", err)
		}
		_, _ = pool.Exec(context.Background(), "INSERT INTO foods (donation_id, food_item_id, quantity, unit) VALUES ($1, $2, 10, 'portion')", multiCatID, foodItemID)
		_, _ = pool.Exec(context.Background(), "INSERT INTO donation_target (donation_id, beneficiary_category_id) VALUES ($1, $2), ($1, $3), ($1, $4)", multiCatID, unhiddenCatID, hiddenCatID1, hiddenCatID2)

		staffReq := httptest.NewRequest(http.MethodGet, "/api/v1/donations/"+multiCatID.String(), nil)
		staffReq.SetPathValue("id", multiCatID.String())
		staffReq = setStaffAuth(staffReq)
		staffRec := httptest.NewRecorder()
		h.GetDonationByIDHandler(staffRec, staffReq)
		if staffRec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK for staff, got %d", staffRec.Code)
		}
		var staffResp response.Response
		_ = json.Unmarshal(staffRec.Body.Bytes(), &staffResp)
		staffData, _ := json.Marshal(staffResp.Data)
		var staffDonation DonationStaffResponse
		_ = json.Unmarshal(staffData, &staffDonation)
		if len(staffDonation.Targets) != 3 {
			t.Fatalf("expected staff to view all 3 targets, got %d", len(staffDonation.Targets))
		}

		pubReq := httptest.NewRequest(http.MethodGet, "/api/v1/donations/"+multiCatID.String(), nil)
		pubReq.SetPathValue("id", multiCatID.String())
		pubRec := httptest.NewRecorder()
		h.GetDonationByIDHandler(pubRec, pubReq)
		if pubRec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK for public, got %d", pubRec.Code)
		}
		var pubResp response.Response
		_ = json.Unmarshal(pubRec.Body.Bytes(), &pubResp)
		pubData, _ := json.Marshal(pubResp.Data)
		var pubDonation DonationPublicResponse
		_ = json.Unmarshal(pubData, &pubDonation)

		if len(pubDonation.Targets) != 2 {
			t.Fatalf("expected public to view exactly 2 targets (1 unhidden + 1 collapsed Hidden institution), got %d: %+v", len(pubDonation.Targets), pubDonation.Targets)
		}

		if pubDonation.Targets[0].BeneficiaryCategoryID == nil || *pubDonation.Targets[0].BeneficiaryCategoryID != unhiddenCatID.String() {
			t.Errorf("expected first target to be unhidden category ID %s, got %v", unhiddenCatID.String(), pubDonation.Targets[0].BeneficiaryCategoryID)
		}
		if pubDonation.Targets[1].BeneficiaryCategoryID != nil {
			t.Errorf("expected collapsed hidden target to have null beneficiary_category_id, got %v", *pubDonation.Targets[1].BeneficiaryCategoryID)
		}
		if pubDonation.Targets[1].Name != "Hidden institution" {
			t.Errorf("expected collapsed hidden target name 'Hidden institution', got '%s'", pubDonation.Targets[1].Name)
		}
		if pubDonation.Targets[1].Variant != nil {
			t.Errorf("expected collapsed hidden target to have null variant, got %v", *pubDonation.Targets[1].Variant)
		}

		listReq := httptest.NewRequest(http.MethodGet, "/api/v1/donations", nil)
		listRec := httptest.NewRecorder()
		h.ListDonationsHandler(listRec, listReq)
		if listRec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK for public list, got %d", listRec.Code)
		}
		var listResp response.Response
		_ = json.Unmarshal(listRec.Body.Bytes(), &listResp)
		listData, _ := json.Marshal(listResp.Data)
		var listItems []DonationPublicResponse
		_ = json.Unmarshal(listData, &listItems)

		found := false
		for _, item := range listItems {
			if item.ID == multiCatID.String() {
				found = true
				if len(item.Targets) != 2 {
					t.Fatalf("expected 2 targets in public list for multi-cat donation, got %d: %+v", len(item.Targets), item.Targets)
				}
				if item.Targets[1].BeneficiaryCategoryID != nil || item.Targets[1].Name != "Hidden institution" {
					t.Errorf("expected collapsed 'Hidden institution' in public list, got %+v", item.Targets[1])
				}
			}
		}
		if !found {
			t.Errorf("multi-cat donation %s not found in public list", multiCatID.String())
		}
	})
}

func TestDonationDefensiveHygiene(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	cfg := config.LoadConfig()
	h := NewDonationHandler(pool, &cfg)
	ctx := context.Background()

	// 1. Reject draft publication if sponsor is soft-deleted
	t.Run("Reject publishing draft if sponsor has been soft-deleted", func(t *testing.T) {
		var sponsorID uuid.UUID
		err := pool.QueryRow(ctx, `
			INSERT INTO sponsors (name, email, phone, variant)
			VALUES ('Publish Deleted Sponsor', 'pubdel@example.com', '081234567891', 'individual')
			RETURNING id;
		`).Scan(&sponsorID)
		if err != nil {
			t.Fatalf("failed to insert sponsor: %v", err)
		}
		defer func() {
			_, _ = pool.Exec(ctx, "DELETE FROM donations WHERE sponsor_id = $1", sponsorID)
			_, _ = pool.Exec(ctx, "DELETE FROM sponsors WHERE id = $1", sponsorID)
		}()

		// Create draft donation
		var donationID uuid.UUID
		futureTime := time.Now().Add(48 * time.Hour)
		err = pool.QueryRow(ctx, `
			INSERT INTO donations (
				sponsor_id, event_at, quota, hero_quota, remain,
				address, message, status, hide_from_public, hide_sponsor
			) VALUES (
				$1, $2, 100, 50, 50,
				'Draft Address', 'Draft Message', 'draft', FALSE, FALSE
			) RETURNING id;
		`, sponsorID, futureTime).Scan(&donationID)
		if err != nil {
			t.Fatalf("failed to create draft donation: %v", err)
		}

		var foodItemID int32
		err = pool.QueryRow(ctx, "SELECT id FROM food_items LIMIT 1").Scan(&foodItemID)
		if err != nil {
			err = pool.QueryRow(ctx, "INSERT INTO food_items (name) VALUES ('Test Food Item') RETURNING id").Scan(&foodItemID)
			if err != nil {
				t.Fatalf("failed to get/insert food item: %v", err)
			}
		}

		// Add required food item so only sponsor check blocks publication
		_, err = pool.Exec(ctx, `
			INSERT INTO foods (donation_id, food_item_id, quantity, unit)
			VALUES ($1, $2, 10.0, 'portion');
		`, donationID, foodItemID)
		if err != nil {
			t.Fatalf("failed to insert food item: %v", err)
		}

		// Soft-delete the sponsor
		_, err = pool.Exec(ctx, `UPDATE sponsors SET deleted_at = NOW() WHERE id = $1`, sponsorID)
		if err != nil {
			t.Fatalf("failed to soft delete sponsor: %v", err)
		}

		// Try to publish
		req := httptest.NewRequest(http.MethodPost, "/api/v1/donations/"+donationID.String()+"/publish", nil)
		req.SetPathValue("id", donationID.String())
		req = setStaffAuth(req)
		rec := httptest.NewRecorder()

		h.PublishDonationHandler(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp["code"] != "VAL_002" {
			t.Errorf("expected error code VAL_002, got %v", resp["code"])
		}
		if resp["message"] != "Sponsor has been deleted" {
			t.Errorf("expected message 'Sponsor has been deleted', got %v", resp["message"])
		}
	})

	// 2. Reject past event_at on open donation
	t.Run("Reject updating event_at to the past on an open donation", func(t *testing.T) {
		var sponsorID uuid.UUID
		err := pool.QueryRow(ctx, `
			INSERT INTO sponsors (name, email, phone, variant)
			VALUES ('Past Event Sponsor', 'pastevent@example.com', '081234567892', 'individual')
			RETURNING id;
		`).Scan(&sponsorID)
		if err != nil {
			t.Fatalf("failed to insert sponsor: %v", err)
		}
		defer func() {
			_, _ = pool.Exec(ctx, "DELETE FROM donations WHERE sponsor_id = $1", sponsorID)
			_, _ = pool.Exec(ctx, "DELETE FROM sponsors WHERE id = $1", sponsorID)
		}()

		var donationID uuid.UUID
		futureTime := time.Now().Add(24 * time.Hour)
		err = pool.QueryRow(ctx, `
			INSERT INTO donations (
				sponsor_id, event_at, quota, hero_quota, remain,
				address, message, status, hide_from_public, hide_sponsor
			) VALUES (
				$1, $2, 50, 20, 20,
				'Active Address', 'Active Message', 'open', FALSE, FALSE
			) RETURNING id;
		`, sponsorID, futureTime).Scan(&donationID)
		if err != nil {
			t.Fatalf("failed to create open donation: %v", err)
		}

		// Try to update event_at to past
		pastTimeStr := time.Now().Add(-2 * time.Hour).Format(time.RFC3339)
		patchBody := map[string]any{
			"event_at": pastTimeStr,
		}
		bodyBytes, _ := json.Marshal(patchBody)
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/donations/"+donationID.String(), bytes.NewReader(bodyBytes))
		req.SetPathValue("id", donationID.String())
		req = setStaffAuth(req)
		rec := httptest.NewRecorder()

		h.UpdateDonationHandler(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d", rec.Code)
		}
		var resp map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		errs, ok := resp["errors"].(map[string]any)
		if !ok || errs["event_at"] != "Distribution event cannot be in the past for an open donation" {
			t.Errorf("expected error 'Distribution event cannot be in the past for an open donation', got: %+v", resp)
		}

		// Update to future time succeeds
		newFutureStr := time.Now().Add(72 * time.Hour).Format(time.RFC3339)
		validBody := map[string]any{
			"event_at": newFutureStr,
		}
		validBytes, _ := json.Marshal(validBody)
		req2 := httptest.NewRequest(http.MethodPatch, "/api/v1/donations/"+donationID.String(), bytes.NewReader(validBytes))
		req2.SetPathValue("id", donationID.String())
		req2 = setStaffAuth(req2)
		rec2 := httptest.NewRecorder()

		h.UpdateDonationHandler(rec2, req2)
		if rec2.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", rec2.Code, rec2.Body.String())
		}
	})

	// 3. Search wildcard escaping in donations staff list
	t.Run("Literal % and _ in donation staff search are escaped", func(t *testing.T) {
		var sponsorID uuid.UUID
		err := pool.QueryRow(ctx, `
			INSERT INTO sponsors (name, email, phone, variant)
			VALUES ('Wildcard Donation Sponsor', 'wildcard_don@example.com', '081234567893', 'company')
			RETURNING id;
		`).Scan(&sponsorID)
		if err != nil {
			t.Fatalf("failed to insert sponsor: %v", err)
		}
		defer func() {
			_, _ = pool.Exec(ctx, "DELETE FROM donations WHERE sponsor_id = $1", sponsorID)
			_, _ = pool.Exec(ctx, "DELETE FROM sponsors WHERE id = $1", sponsorID)
		}()

		future := time.Now().Add(24 * time.Hour)
		_, err = pool.Exec(ctx, `
			INSERT INTO donations (sponsor_id, event_at, quota, hero_quota, remain, address, status, hide_from_public, hide_sponsor)
			VALUES
				($1, $2, 10, 5, 5, 'Jalan Solo % KM 5', 'open', FALSE, FALSE),
				($1, $2, 10, 5, 5, 'Jalan Solo 100 KM 5', 'open', FALSE, FALSE),
				($1, $2, 10, 5, 5, 'Jalan Kaliurang a_b', 'open', FALSE, FALSE),
				($1, $2, 10, 5, 5, 'Jalan Kaliurang axb', 'open', FALSE, FALSE);
		`, sponsorID, future)
		if err != nil {
			t.Fatalf("failed to insert wildcard test donations: %v", err)
		}

		// Search literal %
		searchURL := "/api/v1/donations?search=" + url.QueryEscape("Solo %")
		req := httptest.NewRequest(http.MethodGet, searchURL, nil)
		req = setStaffAuth(req)
		rec := httptest.NewRecorder()
		h.ListDonationsHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rec.Code)
		}
		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		dataBytes, _ := json.Marshal(resp.Data)
		var list []DonationStaffResponse
		_ = json.Unmarshal(dataBytes, &list)

		if len(list) != 1 || *list[0].Address != "Jalan Solo % KM 5" {
			t.Errorf("expected 1 donation matching literal %%, got %d", len(list))
		}

		// Search literal _
		searchURL2 := "/api/v1/donations?search=" + url.QueryEscape("Kaliurang a_b")
		req2 := httptest.NewRequest(http.MethodGet, searchURL2, nil)
		req2 = setStaffAuth(req2)
		rec2 := httptest.NewRecorder()
		h.ListDonationsHandler(rec2, req2)

		if rec2.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rec2.Code)
		}
		var resp2 response.Response
		_ = json.Unmarshal(rec2.Body.Bytes(), &resp2)
		dataBytes2, _ := json.Marshal(resp2.Data)
		var list2 []DonationStaffResponse
		_ = json.Unmarshal(dataBytes2, &list2)

		if len(list2) != 1 || *list2[0].Address != "Jalan Kaliurang a_b" {
			t.Errorf("expected 1 donation matching literal _, got %d", len(list2))
		}
	})
}

func TestDonationPayloadParityAndPaginationBoundaries(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	cfg := config.LoadConfig()
	h := NewDonationHandler(pool, &cfg)
	ctx := context.Background()

	t.Run("Payload parity between anonymous visitor and hero caller", func(t *testing.T) {
		var sponsorID uuid.UUID
		err := pool.QueryRow(ctx, `
			INSERT INTO sponsors (name, email, phone, variant, hidden)
			VALUES ('Parity Sponsor', 'parity@example.com', '081234567899', 'company', true)
			RETURNING id;
		`).Scan(&sponsorID)
		if err != nil {
			t.Fatalf("failed to insert sponsor: %v", err)
		}
		defer func() {
			_, _ = pool.Exec(ctx, "DELETE FROM donations WHERE sponsor_id = $1", sponsorID)
			_, _ = pool.Exec(ctx, "DELETE FROM sponsors WHERE id = $1", sponsorID)
		}()

		var donationID uuid.UUID
		futureTime := time.Now().Add(24 * time.Hour)
		err = pool.QueryRow(ctx, `
			INSERT INTO donations (
				sponsor_id, event_at, quota, hero_quota, remain,
				address, message, status, hide_from_public, hide_sponsor
			) VALUES (
				$1, $2, 40, 20, 20,
				'Parity Address', 'Parity Message', 'open', FALSE, FALSE
			) RETURNING id;
		`, sponsorID, futureTime).Scan(&donationID)
		if err != nil {
			t.Fatalf("failed to insert donation: %v", err)
		}

		// 1. Anonymous request
		reqAnon := httptest.NewRequest(http.MethodGet, "/api/v1/donations/"+donationID.String(), nil)
		reqAnon.SetPathValue("id", donationID.String())
		recAnon := httptest.NewRecorder()
		h.GetDonationByIDHandler(recAnon, reqAnon)
		if recAnon.Code != http.StatusOK {
			t.Fatalf("expected 200 for anonymous, got %d", recAnon.Code)
		}

		// 2. Hero request
		reqHero := httptest.NewRequest(http.MethodGet, "/api/v1/donations/"+donationID.String(), nil)
		reqHero.SetPathValue("id", donationID.String())
		reqHero = setHeroAuth(reqHero)
		recHero := httptest.NewRecorder()
		h.GetDonationByIDHandler(recHero, reqHero)
		if recHero.Code != http.StatusOK {
			t.Fatalf("expected 200 for hero, got %d", recHero.Code)
		}

		var respAnon, respHero map[string]any
		_ = json.Unmarshal(recAnon.Body.Bytes(), &respAnon)
		_ = json.Unmarshal(recHero.Body.Bytes(), &respHero)

		dataAnon := respAnon["data"].(map[string]any)
		dataHero := respHero["data"].(map[string]any)

		if len(dataAnon) != len(dataHero) {
			t.Errorf("expected key count parity, got anon=%d hero=%d", len(dataAnon), len(dataHero))
		}
		for k, vAnon := range dataAnon {
			vHero, exists := dataHero[k]
			if !exists {
				t.Errorf("key %s missing in hero response", k)
			}
			if k == "sponsor" {
				sAnon := vAnon.(map[string]any)
				sHero := vHero.(map[string]any)
				if sAnon["name"] != "Generous sponsor" || sHero["name"] != "Generous sponsor" {
					t.Errorf("expected both to mask sponsor to 'Generous sponsor', got anon=%v hero=%v", sAnon["name"], sHero["name"])
				}
				if sAnon["id"] != nil || sHero["id"] != nil {
					t.Errorf("expected both to have nil sponsor ID, got anon=%v hero=%v", sAnon["id"], sHero["id"])
				}
			}
		}
	})

	t.Run("Pagination boundaries: page beyond last page returns empty array", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/donations?page=9999&limit=10", nil)
		rec := httptest.NewRecorder()
		h.ListDonationsHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rec.Code)
		}
		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		dataBytes, _ := json.Marshal(resp.Data)
		var list []DonationPublicResponse
		_ = json.Unmarshal(dataBytes, &list)

		if len(list) != 0 {
			t.Errorf("expected empty array for page 9999, got %d items", len(list))
		}
		if resp.Meta == nil || resp.Meta.Page != 9999 {
			t.Errorf("expected meta.page=9999, got %+v", resp.Meta)
		}
	})

	t.Run("Pagination boundaries: limit above max is capped dynamically", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/donations?page=1&limit=500", nil)
		rec := httptest.NewRecorder()
		h.ListDonationsHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rec.Code)
		}
		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)

		if resp.Meta == nil || resp.Meta.Limit != cfg.AppConfig.MaxLimit {
			t.Errorf("expected meta.limit to be capped at %d, got %+v", cfg.AppConfig.MaxLimit, resp.Meta)
		}
	})
}
