package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/berbagibitesjogja/core-system/internal/auth"
	"github.com/berbagibitesjogja/core-system/internal/config"
	"github.com/berbagibitesjogja/core-system/internal/middleware"
	"github.com/berbagibitesjogja/core-system/internal/response"
	"github.com/google/uuid"
)

func setStaffAuth(r *http.Request) *http.Request {
	claims := &auth.Claims{
		UserID: uuid.MustParse("00000000-0000-0000-0000-000000000001"),
		Role:   "super",
		Email:  "super@berbagibitesjogja.com",
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

		body, _ := json.Marshal(CreateSponsorRequest{
			Name:    testName,
			Variant: &variant,
			Address: &address,
			Email:   &email,
			Phone:   &phone,
			Hidden:  &hidden,
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
		tooLongName := "This name is definitely going to exceed the fifty characters limit allowed for sponsors"
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
