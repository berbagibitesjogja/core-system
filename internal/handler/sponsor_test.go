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

	"github.com/berbagibitesjogja/core-system/internal/auth"
	"github.com/berbagibitesjogja/core-system/internal/config"
	"github.com/berbagibitesjogja/core-system/internal/middleware"
	"github.com/berbagibitesjogja/core-system/internal/response"
	"github.com/google/uuid"
)

func setStaffAuth(r *http.Request) *http.Request {
	claims := &auth.Claims{
		UserID:  uuid.MustParse("00000000-0000-0000-0000-000000000001"),
		Role:    "super",
		IsStaff: true,
		Email:   "super@berbagibitesjogja.com",
	}
	ctx := middleware.WithClaims(r.Context(), claims)
	return r.WithContext(ctx)
}

func setHeroAuth(r *http.Request) *http.Request {
	claims := &auth.Claims{
		UserID:  uuid.MustParse("00000000-0000-0000-0000-000000000002"),
		IsHero:  true,
		IsStaff: false,
		Email:   "hero@example.com",
	}
	ctx := middleware.WithClaims(r.Context(), claims)
	return r.WithContext(ctx)
}

func TestSponsorCRUD(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	cfg := config.LoadConfig()
	h := NewSponsorHandler(pool, &cfg)

	// Clean up any test sponsors created during this test
	testName := "Test Company Sponsor " + uuid.NewString()[:8]
	defer func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM sponsors WHERE name LIKE 'Test %'")
	}()

	var createdID string

	t.Run("Create Sponsor (Happy Path with Phone Normalization)", func(t *testing.T) {
		variant := "company"
		address := "Jl. Malioboro No. 123, Yogyakarta"
		email := "sponsor@example.com"
		phone := "0812-3456-7890" // Trigger should normalize to 6281234567890
		hidden := false
		hideDonations := false

		body, _ := json.Marshal(CreateSponsorRequest{
			Name:          testName,
			Variant:       &variant,
			Address:       &address,
			Email:         &email,
			Phone:         &phone,
			Hidden:        &hidden,
			HideDonations: &hideDonations,
		})

		req := httptest.NewRequest(http.MethodPost, "/api/v1/sponsors", bytes.NewReader(body))
		req = setStaffAuth(req)
		rec := httptest.NewRecorder()

		h.CreateSponsorHandler(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		var resp response.Response
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		dataBytes, _ := json.Marshal(resp.Data)
		var spResp SponsorResponse
		_ = json.Unmarshal(dataBytes, &spResp)

		if spResp.Name != testName {
			t.Errorf("expected name %s, got %s", testName, spResp.Name)
		}
		if spResp.Variant != "company" {
			t.Errorf("expected variant 'company', got %s", spResp.Variant)
		}
		if spResp.Phone == nil || *spResp.Phone != "6281234567890" {
			t.Errorf("expected normalized phone '6281234567890', got %v", spResp.Phone)
		}
		if spResp.HideDonations != false {
			t.Errorf("expected hide_donations false, got %v", spResp.HideDonations)
		}

		createdID = spResp.ID
	})

	t.Run("Create Sponsor Duplicate Active Name Collision (400 VAL_002)", func(t *testing.T) {
		body, _ := json.Marshal(CreateSponsorRequest{
			Name: testName,
		})

		req := httptest.NewRequest(http.MethodPost, "/api/v1/sponsors", bytes.NewReader(body))
		req = setStaffAuth(req)
		rec := httptest.NewRecorder()

		h.CreateSponsorHandler(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d", rec.Code)
		}

		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.Code != "VAL_002" {
			t.Errorf("expected code VAL_002, got %s", resp.Code)
		}

		errMap, ok := resp.Errors.(map[string]any)
		if !ok || errMap["name"] != "Sponsor name is already registered" {
			t.Errorf("expected error 'Sponsor name is already registered', got %+v", resp.Errors)
		}
	})

	t.Run("Create Sponsor Foreign Key Violation for Non-Existent Logo (400 VAL_002)", func(t *testing.T) {
		nonExistentLogo := uuid.NewString()
		body, _ := json.Marshal(CreateSponsorRequest{
			Name: "Test Sponsor Logo FK",
			Logo: &nonExistentLogo,
		})

		req := httptest.NewRequest(http.MethodPost, "/api/v1/sponsors", bytes.NewReader(body))
		req = setStaffAuth(req)
		rec := httptest.NewRecorder()

		h.CreateSponsorHandler(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d", rec.Code)
		}

		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.Code != "VAL_002" {
			t.Errorf("expected code VAL_002, got %s", resp.Code)
		}

		errMap, ok := resp.Errors.(map[string]any)
		if !ok || errMap["logo"] != "Referenced logo file does not exist" {
			t.Errorf("expected error 'Referenced logo file does not exist', got %+v", resp.Errors)
		}
	})

	t.Run("Create Sponsor Validation Failures (400 VAL_001)", func(t *testing.T) {
		invalidVariant := "non-profit"
		tooLongName := strings.Repeat("A", 151)
		invalidEmail := "not-an-email"

		body, _ := json.Marshal(CreateSponsorRequest{
			Name:    tooLongName,
			Variant: &invalidVariant,
			Email:   &invalidEmail,
		})

		req := httptest.NewRequest(http.MethodPost, "/api/v1/sponsors", bytes.NewReader(body))
		req = setStaffAuth(req)
		rec := httptest.NewRecorder()

		h.CreateSponsorHandler(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d", rec.Code)
		}

		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.Code != "VAL_001" {
			t.Errorf("expected code VAL_001, got %s", resp.Code)
		}
	})

	t.Run("Get Sponsor by ID (Public Access)", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/sponsors/"+createdID, nil)
		req.SetPathValue("id", createdID)
		rec := httptest.NewRecorder()

		h.GetSponsorByIDHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rec.Code)
		}

		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		dataBytes, _ := json.Marshal(resp.Data)
		var spResp SponsorResponse
		_ = json.Unmarshal(dataBytes, &spResp)

		if spResp.ID != createdID {
			t.Errorf("expected ID %s, got %s", createdID, spResp.ID)
		}
	})

	t.Run("Get Sponsor by Non-Existent ID returns 404", func(t *testing.T) {
		randomID := uuid.NewString()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/sponsors/"+randomID, nil)
		req.SetPathValue("id", randomID)
		rec := httptest.NewRecorder()

		h.GetSponsorByIDHandler(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 Not Found, got %d", rec.Code)
		}
	})

	t.Run("Update Sponsor (Safe PATCH without Resetting Other Fields)", func(t *testing.T) {
		newAddress := "Jl. Kaliurang KM 5, Sleman"
		body, _ := json.Marshal(UpdateSponsorRequest{
			Address: &newAddress,
		})

		req := httptest.NewRequest(http.MethodPatch, "/api/v1/sponsors/"+createdID, bytes.NewReader(body))
		req.SetPathValue("id", createdID)
		req = setStaffAuth(req)
		rec := httptest.NewRecorder()

		h.UpdateSponsorHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		dataBytes, _ := json.Marshal(resp.Data)
		var spResp SponsorResponse
		_ = json.Unmarshal(dataBytes, &spResp)

		// Address must be updated
		if spResp.Address == nil || *spResp.Address != newAddress {
			t.Errorf("expected updated address %s, got %v", newAddress, spResp.Address)
		}
		// Name and variant must be preserved (safe patch)
		if spResp.Name != testName {
			t.Errorf("expected preserved name %s, got %s", testName, spResp.Name)
		}
		if spResp.Variant != "company" {
			t.Errorf("expected preserved variant 'company', got %s", spResp.Variant)
		}
	})

	t.Run("Soft Delete Sponsor", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/sponsors/"+createdID, nil)
		req.SetPathValue("id", createdID)
		req = setStaffAuth(req)
		rec := httptest.NewRecorder()

		h.DeleteSponsorHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rec.Code)
		}

		// Subsequent Get must return 404
		getReq := httptest.NewRequest(http.MethodGet, "/api/v1/sponsors/"+createdID, nil)
		getReq.SetPathValue("id", createdID)
		getRec := httptest.NewRecorder()
		h.GetSponsorByIDHandler(getRec, getReq)

		if getRec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 after soft delete, got %d", getRec.Code)
		}

		// Subsequent Delete on already deleted record returns 404
		delReq := httptest.NewRequest(http.MethodDelete, "/api/v1/sponsors/"+createdID, nil)
		delReq.SetPathValue("id", createdID)
		delReq = setStaffAuth(delReq)
		delRec := httptest.NewRecorder()
		h.DeleteSponsorHandler(delRec, delReq)

		if delRec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 on already deleted record, got %d", delRec.Code)
		}
	})

	t.Run("Re-create Sponsor with Same Name after Soft Delete (Unique Index respects deleted_at)", func(t *testing.T) {
		body, _ := json.Marshal(CreateSponsorRequest{
			Name: testName,
		})

		req := httptest.NewRequest(http.MethodPost, "/api/v1/sponsors", bytes.NewReader(body))
		req = setStaffAuth(req)
		rec := httptest.NewRecorder()

		h.CreateSponsorHandler(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created for re-creating soft-deleted name, got %d. Body: %s", rec.Code, rec.Body.String())
		}
	})
}

func TestSponsorVisibilityAndFiltering(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	cfg := config.LoadConfig()
	h := NewSponsorHandler(pool, &cfg)

	// Clean up test sponsors
	defer func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM sponsors WHERE name LIKE 'Visibility %'")
	}()

	publicName := fmt.Sprintf("Visibility Public %s", uuid.NewString()[:8])
	hiddenName := fmt.Sprintf("Visibility Hidden %s", uuid.NewString()[:8])

	// Create 1 public sponsor and 1 hidden sponsor
	var hiddenID string
	{
		hiddenTrue := true
		body, _ := json.Marshal(CreateSponsorRequest{
			Name:   hiddenName,
			Hidden: &hiddenTrue,
		})
		req := setStaffAuth(httptest.NewRequest(http.MethodPost, "/api/v1/sponsors", bytes.NewReader(body)))
		rec := httptest.NewRecorder()
		h.CreateSponsorHandler(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("failed to create hidden sponsor: %s", rec.Body.String())
		}
		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		dataBytes, _ := json.Marshal(resp.Data)
		var spResp SponsorResponse
		_ = json.Unmarshal(dataBytes, &spResp)
		hiddenID = spResp.ID
	}
	{
		hiddenFalse := false
		body, _ := json.Marshal(CreateSponsorRequest{
			Name:   publicName,
			Hidden: &hiddenFalse,
		})
		req := setStaffAuth(httptest.NewRequest(http.MethodPost, "/api/v1/sponsors", bytes.NewReader(body)))
		rec := httptest.NewRecorder()
		h.CreateSponsorHandler(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("failed to create public sponsor: %s", rec.Body.String())
		}
	}

	t.Run("Public unauthenticated list hides hidden sponsors", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/sponsors?search=Visibility", nil)
		rec := httptest.NewRecorder()

		h.ListSponsorsHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rec.Code)
		}

		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		dataBytes, _ := json.Marshal(resp.Data)
		var list []SponsorResponse
		_ = json.Unmarshal(dataBytes, &list)

		for _, item := range list {
			if item.Name == hiddenName {
				t.Fatalf("public caller must NOT see hidden sponsor: %s", item.Name)
			}
		}
	})

	t.Run("Public unauthenticated get of hidden sponsor returns 404", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/sponsors/"+hiddenID, nil)
		req.SetPathValue("id", hiddenID)
		rec := httptest.NewRecorder()

		h.GetSponsorByIDHandler(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 Not Found for unauthenticated access to hidden sponsor, got %d", rec.Code)
		}
	})

	t.Run("Authenticated hero cannot see hidden sponsor in list even with ?hidden=true", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/sponsors?search=Visibility&hidden=true", nil)
		req = setHeroAuth(req)
		rec := httptest.NewRecorder()

		h.ListSponsorsHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rec.Code)
		}

		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		dataBytes, _ := json.Marshal(resp.Data)
		var list []SponsorResponse
		_ = json.Unmarshal(dataBytes, &list)

		for _, item := range list {
			if item.Name == hiddenName {
				t.Fatalf("authenticated hero must NOT see hidden sponsor: %s", item.Name)
			}
		}
	})

	t.Run("Authenticated hero get of hidden sponsor returns 404", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/sponsors/"+hiddenID, nil)
		req.SetPathValue("id", hiddenID)
		req = setHeroAuth(req)
		rec := httptest.NewRecorder()

		h.GetSponsorByIDHandler(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 Not Found for hero access to hidden sponsor, got %d", rec.Code)
		}
	})

	t.Run("Authenticated staff get of hidden sponsor returns 200 OK", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/sponsors/"+hiddenID, nil)
		req.SetPathValue("id", hiddenID)
		req = setStaffAuth(req)
		rec := httptest.NewRecorder()

		h.GetSponsorByIDHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK for staff access to hidden sponsor, got %d", rec.Code)
		}
	})

	t.Run("Authenticated staff can query ?hidden=true", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/sponsors?search=Visibility&hidden=true", nil)
		req = setStaffAuth(req)
		rec := httptest.NewRecorder()

		h.ListSponsorsHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rec.Code)
		}

		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		dataBytes, _ := json.Marshal(resp.Data)
		var list []SponsorResponse
		_ = json.Unmarshal(dataBytes, &list)

		foundHidden := false
		for _, item := range list {
			if item.Name == hiddenName {
				foundHidden = true
			}
		}
		if !foundHidden {
			t.Errorf("expected staff to find hidden sponsor when filtering ?hidden=true")
		}
	})

	t.Run("Pagination metadata is correctly calculated", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/sponsors?page=1&limit=1", nil)
		rec := httptest.NewRecorder()

		h.ListSponsorsHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rec.Code)
		}

		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)

		if resp.Meta == nil {
			t.Fatal("expected pagination metadata to be populated")
		}
		if resp.Meta.Limit != 1 || resp.Meta.Page != 1 {
			t.Errorf("expected page 1 limit 1, got page %d limit %d", resp.Meta.Page, resp.Meta.Limit)
		}
	})
}


func TestPublicSponsorKeyWhitelist(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	cfg := config.LoadConfig()
	h := NewSponsorHandler(pool, &cfg)

	testName := "Whitelist Sponsor " + uuid.NewString()[:8]
	defer func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM sponsors WHERE name LIKE 'Whitelist %'")
	}()

	variant := "company"
	address := "Jl. Kaliurang KM 5, Yogyakarta"
	email := "contact@whitelist-test.org"
	phone := "0812-9999-8888"
	hidden := false
	hideDonations := false

	body, _ := json.Marshal(CreateSponsorRequest{
		Name:          testName,
		Variant:       &variant,
		Address:       &address,
		Email:         &email,
		Phone:         &phone,
		Hidden:        &hidden,
		HideDonations: &hideDonations,
	})

	createReq := setStaffAuth(httptest.NewRequest(http.MethodPost, "/api/v1/sponsors", bytes.NewReader(body)))
	createRec := httptest.NewRecorder()
	h.CreateSponsorHandler(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("failed to create sponsor: %s", createRec.Body.String())
	}

	var createResp response.Response
	_ = json.Unmarshal(createRec.Body.Bytes(), &createResp)
	createdData, _ := json.Marshal(createResp.Data)
	var spResp SponsorResponse
	_ = json.Unmarshal(createdData, &spResp)
	createdID := spResp.ID

	expectedPublicKeys := map[string]bool{
		"id":      true,
		"name":    true,
		"variant": true,
		"logo":    true,
	}

	assertExactKeys := func(t *testing.T, data map[string]any, roleName string) {
		t.Helper()
		if len(data) != len(expectedPublicKeys) {
			t.Errorf("[%s] expected exactly %d keys, got %d: %v", roleName, len(expectedPublicKeys), len(data), data)
		}
		for k := range data {
			if !expectedPublicKeys[k] {
				t.Errorf("[%s] forbidden key leaked in public response: %s", roleName, k)
			}
		}
	}

	t.Run("Anonymous GET /api/v1/sponsors key whitelist", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/sponsors?search="+url.QueryEscape(testName), nil)
		rec := httptest.NewRecorder()
		h.ListSponsorsHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rec.Code)
		}

		var resp map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal JSON: %v", err)
		}
		items, ok := resp["data"].([]any)
		if !ok || len(items) == 0 {
			t.Fatalf("expected non-empty data array, got: %v", resp["data"])
		}

		found := false
		for _, item := range items {
			itemMap, ok := item.(map[string]any)
			if !ok {
				t.Fatalf("expected map[string]any item, got: %T", item)
			}
			if itemMap["id"] == createdID {
				found = true
				assertExactKeys(t, itemMap, "anonymous-list")
			}
		}
		if !found {
			t.Fatalf("created sponsor %s not found in public list", createdID)
		}
	})

	t.Run("Anonymous GET /api/v1/sponsors/:id key whitelist", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/sponsors/"+createdID, nil)
		req.SetPathValue("id", createdID)
		rec := httptest.NewRecorder()
		h.GetSponsorByIDHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rec.Code)
		}

		var resp map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal JSON: %v", err)
		}
		itemMap, ok := resp["data"].(map[string]any)
		if !ok {
			t.Fatalf("expected map[string]any item, got: %T", resp["data"])
		}
		assertExactKeys(t, itemMap, "anonymous-detail")
	})

	t.Run("Hero GET /api/v1/sponsors/:id key whitelist", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/sponsors/"+createdID, nil)
		req.SetPathValue("id", createdID)
		req = setHeroAuth(req)
		rec := httptest.NewRecorder()
		h.GetSponsorByIDHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rec.Code)
		}

		var resp map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal JSON: %v", err)
		}
		itemMap, ok := resp["data"].(map[string]any)
		if !ok {
			t.Fatalf("expected map[string]any item, got: %T", resp["data"])
		}
		assertExactKeys(t, itemMap, "hero-detail")
	})

	t.Run("Staff GET /api/v1/sponsors/:id retains full fields", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/sponsors/"+createdID, nil)
		req.SetPathValue("id", createdID)
		req = setStaffAuth(req)
		rec := httptest.NewRecorder()
		h.GetSponsorByIDHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rec.Code)
		}

		var resp map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal JSON: %v", err)
		}
		itemMap, ok := resp["data"].(map[string]any)
		if !ok {
			t.Fatalf("expected map[string]any item, got: %T", resp["data"])
		}
		requiredStaffKeys := []string{"email", "phone", "address", "hidden", "hide_donations", "created_at", "updated_at"}
		for _, k := range requiredStaffKeys {
			if _, exists := itemMap[k]; !exists {
				t.Errorf("staff detail missing expected field %s", k)
			}
		}
	})
}

func TestSponsorDefensiveHygiene(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	cfg := config.LoadConfig()
	h := NewSponsorHandler(pool, &cfg)
	ctx := context.Background()

	// 1. Strict Email Validation
	t.Run("Reject RFC 5322 formatted email containing name", func(t *testing.T) {
		body := map[string]any{
			"name":  "Strict Email Sponsor",
			"email": "Jane Doe <jane@example.com>",
		}
		bodyBytes, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/sponsors", bytes.NewReader(bodyBytes))
		req = setStaffAuth(req)
		rec := httptest.NewRecorder()

		h.CreateSponsorHandler(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d", rec.Code)
		}
		var resp map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		errs, ok := resp["errors"].(map[string]any)
		if !ok || errs["email"] != "Invalid email format" {
			t.Errorf("expected email validation error, got: %+v", resp)
		}
	})

	t.Run("Reject invalid email format", func(t *testing.T) {
		body := map[string]any{
			"name":  "Invalid Email Sponsor",
			"email": "notanemail@",
		}
		bodyBytes, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/sponsors", bytes.NewReader(bodyBytes))
		req = setStaffAuth(req)
		rec := httptest.NewRecorder()

		h.CreateSponsorHandler(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d", rec.Code)
		}
	})

	// 2. Logo file validation in files table
	t.Run("Reject non-existent logo file UUID", func(t *testing.T) {
		fakeFileID := uuid.NewString()
		body := map[string]any{
			"name": "Missing Logo Sponsor",
			"logo": fakeFileID,
		}
		bodyBytes, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/sponsors", bytes.NewReader(bodyBytes))
		req = setStaffAuth(req)
		rec := httptest.NewRecorder()

		h.CreateSponsorHandler(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d", rec.Code)
		}
		var resp map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		errs, ok := resp["errors"].(map[string]any)
		if !ok || errs["logo"] != "Referenced logo file does not exist" {
			t.Errorf("expected logo error 'Referenced logo file does not exist', got: %+v", resp)
		}
	})

	t.Run("Reject logo file with status uploading", func(t *testing.T) {
		var unuploadedID uuid.UUID
		err := pool.QueryRow(ctx, `
			INSERT INTO files (storage_bucket, storage_key, status, mime_type)
			VALUES ('public', 'sponsors/unuploaded.png', 'uploading', 'image/png')
			RETURNING id;
		`).Scan(&unuploadedID)
		if err != nil {
			t.Fatalf("failed to insert unuploaded file: %v", err)
		}
		defer func() {
			_, _ = pool.Exec(ctx, "DELETE FROM files WHERE id = $1", unuploadedID)
		}()

		body := map[string]any{
			"name": "Unuploaded Logo Sponsor",
			"logo": unuploadedID.String(),
		}
		bodyBytes, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/sponsors", bytes.NewReader(bodyBytes))
		req = setStaffAuth(req)
		rec := httptest.NewRecorder()

		h.CreateSponsorHandler(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d", rec.Code)
		}
		var resp map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		errs, ok := resp["errors"].(map[string]any)
		if !ok || errs["logo"] != "File not found or not uploaded" {
			t.Errorf("expected logo error 'File not found or not uploaded', got: %+v", resp)
		}
	})

	t.Run("Reject logo file in private bucket", func(t *testing.T) {
		var privateID uuid.UUID
		err := pool.QueryRow(ctx, `
			INSERT INTO files (storage_bucket, storage_key, status, mime_type)
			VALUES ('private', 'sponsors/secret.png', 'uploaded', 'image/png')
			RETURNING id;
		`).Scan(&privateID)
		if err != nil {
			t.Fatalf("failed to insert private file: %v", err)
		}
		defer func() {
			_, _ = pool.Exec(ctx, "DELETE FROM files WHERE id = $1", privateID)
		}()

		body := map[string]any{
			"name": "Private Logo Sponsor",
			"logo": privateID.String(),
		}
		bodyBytes, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/sponsors", bytes.NewReader(bodyBytes))
		req = setStaffAuth(req)
		rec := httptest.NewRecorder()

		h.CreateSponsorHandler(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d", rec.Code)
		}
	})

	t.Run("Reject non-image logo file", func(t *testing.T) {
		var pdfID uuid.UUID
		err := pool.QueryRow(ctx, `
			INSERT INTO files (storage_bucket, storage_key, status, mime_type)
			VALUES ('public', 'sponsors/doc.pdf', 'uploaded', 'application/pdf')
			RETURNING id;
		`).Scan(&pdfID)
		if err != nil {
			t.Fatalf("failed to insert pdf file: %v", err)
		}
		defer func() {
			_, _ = pool.Exec(ctx, "DELETE FROM files WHERE id = $1", pdfID)
		}()

		body := map[string]any{
			"name": "PDF Logo Sponsor",
			"logo": pdfID.String(),
		}
		bodyBytes, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/sponsors", bytes.NewReader(bodyBytes))
		req = setStaffAuth(req)
		rec := httptest.NewRecorder()

		h.CreateSponsorHandler(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d", rec.Code)
		}
	})

	t.Run("Accept valid uploaded logo file", func(t *testing.T) {
		var uploadedID uuid.UUID
		err := pool.QueryRow(ctx, `
			INSERT INTO files (storage_bucket, storage_key, status, mime_type)
			VALUES ('public', 'sponsors/uploaded.png', 'uploaded', 'image/png')
			RETURNING id;
		`).Scan(&uploadedID)
		if err != nil {
			t.Fatalf("failed to insert uploaded file: %v", err)
		}
		defer func() {
			_, _ = pool.Exec(ctx, "DELETE FROM sponsors WHERE name = 'Uploaded Logo Sponsor'")
			_, _ = pool.Exec(ctx, "DELETE FROM files WHERE id = $1", uploadedID)
		}()

		body := map[string]any{
			"name":  "Uploaded Logo Sponsor",
			"email": "uploaded@example.com",
			"logo":  uploadedID.String(),
		}
		bodyBytes, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/sponsors", bytes.NewReader(bodyBytes))
		req = setStaffAuth(req)
		rec := httptest.NewRecorder()

		h.CreateSponsorHandler(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	// 3. Search wildcard escaping
	t.Run("Literal % and _ in sponsor search are escaped", func(t *testing.T) {
		_, err := pool.Exec(ctx, `
			INSERT INTO sponsors (name, email, variant)
			VALUES
				('Wildcard 100% Sponsor', 'wild1@example.com', 'individual'),
				('Wildcard 1000 Sponsor', 'wild2@example.com', 'individual'),
				('Wildcard a_b Sponsor', 'wild3@example.com', 'individual'),
				('Wildcard axb Sponsor', 'wild4@example.com', 'individual');
		`)
		if err != nil {
			t.Fatalf("failed to insert wildcard test sponsors: %v", err)
		}
		defer func() {
			_, _ = pool.Exec(ctx, "DELETE FROM sponsors WHERE name LIKE 'Wildcard %'")
		}()

		// Search for literal % (URL-encoded as %25)
		searchURL := "/api/v1/sponsors?search=" + url.QueryEscape("100%")
		req := httptest.NewRequest(http.MethodGet, searchURL, nil)
		req = setStaffAuth(req)
		rec := httptest.NewRecorder()
		h.ListSponsorsHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rec.Code)
		}
		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		dataBytes, _ := json.Marshal(resp.Data)
		var list []SponsorResponse
		_ = json.Unmarshal(dataBytes, &list)

		if len(list) != 1 || list[0].Name != "Wildcard 100% Sponsor" {
			t.Errorf("expected 1 result matching literal 100%%, got %d: %+v", len(list), list)
		}

		// Search for literal _
		searchURL2 := "/api/v1/sponsors?search=" + url.QueryEscape("a_b")
		req2 := httptest.NewRequest(http.MethodGet, searchURL2, nil)
		req2 = setStaffAuth(req2)
		rec2 := httptest.NewRecorder()
		h.ListSponsorsHandler(rec2, req2)

		if rec2.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rec2.Code)
		}
		var resp2 response.Response
		_ = json.Unmarshal(rec2.Body.Bytes(), &resp2)
		dataBytes2, _ := json.Marshal(resp2.Data)
		var list2 []SponsorResponse
		_ = json.Unmarshal(dataBytes2, &list2)

		if len(list2) != 1 || list2[0].Name != "Wildcard a_b Sponsor" {
			t.Errorf("expected 1 result matching literal a_b, got %d: %+v", len(list2), list2)
		}
	})
}
