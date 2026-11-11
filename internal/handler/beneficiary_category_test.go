package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/berbagibitesjogja/core-system/internal/config"
	"github.com/berbagibitesjogja/core-system/internal/response"
	"github.com/google/uuid"
)

func TestBeneficiaryCategoryCRUD(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	cfg := config.LoadConfig()
	h := NewBeneficiaryCategoryHandler(pool, &cfg)

	testCatName := "Kategori Uji " + uuid.NewString()[:8]
	testDomain := "test-" + uuid.NewString()[:8] + ".ac.id"
	testAddress := "Jl. UGM No. 1, Yogyakarta"
	testMaps := "https://maps.google.com/?q=ugm"
	isPriorityTrue := true

	defer func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM beneficiary_categories WHERE name LIKE 'Kategori Uji %'")
	}()

	var createdID string

	t.Run("Create Beneficiary Category (Happy Path with new fields)", func(t *testing.T) {
		body, _ := json.Marshal(CreateBeneficiaryCategoryRequest{
			Name:        testCatName,
			Address:     &testAddress,
			Maps:        &testMaps,
			Variant:     "university",
			EmailDomain: &testDomain,
			IsPriority:  &isPriorityTrue,
		})

		req := setStaffAuth(httptest.NewRequest(http.MethodPost, "/api/v1/beneficiary-categories", bytes.NewReader(body)))
		rec := httptest.NewRecorder()

		h.CreateBeneficiaryCategoryHandler(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		dataBytes, _ := json.Marshal(resp.Data)
		var catResp BeneficiaryCategoryResponse
		_ = json.Unmarshal(dataBytes, &catResp)

		if catResp.Name != testCatName {
			t.Errorf("expected name %s, got %s", testCatName, catResp.Name)
		}
		if catResp.Variant != "university" {
			t.Errorf("expected variant university, got %s", catResp.Variant)
		}
		if catResp.Address == nil || *catResp.Address != testAddress {
			t.Errorf("expected address %s, got %v", testAddress, catResp.Address)
		}
		if catResp.Maps == nil || *catResp.Maps != testMaps {
			t.Errorf("expected maps %s, got %v", testMaps, catResp.Maps)
		}
		if catResp.EmailDomain == nil || *catResp.EmailDomain != testDomain {
			t.Errorf("expected email_domain %s, got %v", testDomain, catResp.EmailDomain)
		}
		if !catResp.IsPriority {
			t.Errorf("expected is_priority true, got false")
		}
		if catResp.Hidden {
			t.Errorf("default hidden false, got true")
		}

		createdID = catResp.ID
	})

	t.Run("Create Beneficiary Category Case-Insensitive Collision (400 VAL_002)", func(t *testing.T) {
		body, _ := json.Marshal(CreateBeneficiaryCategoryRequest{
			Name:    strings.ToLower(testCatName),
			Variant: "orphanage",
		})

		req := setStaffAuth(httptest.NewRequest(http.MethodPost, "/api/v1/beneficiary-categories", bytes.NewReader(body)))
		rec := httptest.NewRecorder()

		h.CreateBeneficiaryCategoryHandler(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.Code != "VAL_002" {
			t.Errorf("expected code VAL_002, got %s", resp.Code)
		}

		errMap, ok := resp.Errors.(map[string]any)
		if !ok || errMap["name"] != "Beneficiary category already exists" {
			t.Errorf("expected 'Beneficiary category already exists', got %+v", resp.Errors)
		}
	})

	t.Run("Create Beneficiary Category Duplicate Email Domain (400 VAL_002)", func(t *testing.T) {
		body, _ := json.Marshal(CreateBeneficiaryCategoryRequest{
			Name:        testCatName + " Second",
			Variant:     "university",
			EmailDomain: &testDomain,
		})

		req := setStaffAuth(httptest.NewRequest(http.MethodPost, "/api/v1/beneficiary-categories", bytes.NewReader(body)))
		rec := httptest.NewRecorder()

		h.CreateBeneficiaryCategoryHandler(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.Code != "VAL_002" {
			t.Errorf("expected code VAL_002, got %s", resp.Code)
		}

		errMap, ok := resp.Errors.(map[string]any)
		if !ok || errMap["email_domain"] != "Email domain is already registered" {
			t.Errorf("expected 'Email domain is already registered', got %+v", resp.Errors)
		}
	})

	t.Run("Create Beneficiary Category Validation Failures (400 VAL_001)", func(t *testing.T) {
		bodyEmptyName, _ := json.Marshal(CreateBeneficiaryCategoryRequest{
			Name:    "",
			Variant: "orphanage",
		})
		req1 := setStaffAuth(httptest.NewRequest(http.MethodPost, "/api/v1/beneficiary-categories", bytes.NewReader(bodyEmptyName)))
		rec1 := httptest.NewRecorder()
		h.CreateBeneficiaryCategoryHandler(rec1, req1)
		if rec1.Code != http.StatusBadRequest {
			t.Errorf("expected 400 for empty name, got %d", rec1.Code)
		}

		bodyInvalidVariant, _ := json.Marshal(CreateBeneficiaryCategoryRequest{
			Name:    testCatName + " Baru",
			Variant: "invalid_variant_xyz",
		})
		req2 := setStaffAuth(httptest.NewRequest(http.MethodPost, "/api/v1/beneficiary-categories", bytes.NewReader(bodyInvalidVariant)))
		rec2 := httptest.NewRecorder()
		h.CreateBeneficiaryCategoryHandler(rec2, req2)
		if rec2.Code != http.StatusBadRequest {
			t.Errorf("expected 400 for invalid variant, got %d", rec2.Code)
		}

		bodyLongName, _ := json.Marshal(CreateBeneficiaryCategoryRequest{
			Name:    strings.Repeat("A", 151),
			Variant: "orphanage",
		})
		req3 := setStaffAuth(httptest.NewRequest(http.MethodPost, "/api/v1/beneficiary-categories", bytes.NewReader(bodyLongName)))
		rec3 := httptest.NewRecorder()
		h.CreateBeneficiaryCategoryHandler(rec3, req3)
		if rec3.Code != http.StatusBadRequest {
			t.Errorf("expected 400 for oversized name, got %d", rec3.Code)
		}

		orphanDomain := "orphanage.org"
		bodyNonUniDomain, _ := json.Marshal(CreateBeneficiaryCategoryRequest{
			Name:        testCatName + " Orphanage",
			Variant:     "orphanage",
			EmailDomain: &orphanDomain,
		})
		req4 := setStaffAuth(httptest.NewRequest(http.MethodPost, "/api/v1/beneficiary-categories", bytes.NewReader(bodyNonUniDomain)))
		rec4 := httptest.NewRecorder()
		h.CreateBeneficiaryCategoryHandler(rec4, req4)
		if rec4.Code != http.StatusBadRequest {
			t.Errorf("expected 400 for non-university email domain, got %d", rec4.Code)
		}

		upperDomain := "UPPER.AC.ID"
		bodyUpperDomain, _ := json.Marshal(CreateBeneficiaryCategoryRequest{
			Name:        testCatName + " Upper",
			Variant:     "university",
			EmailDomain: &upperDomain,
		})
		req5 := setStaffAuth(httptest.NewRequest(http.MethodPost, "/api/v1/beneficiary-categories", bytes.NewReader(bodyUpperDomain)))
		rec5 := httptest.NewRecorder()
		h.CreateBeneficiaryCategoryHandler(rec5, req5)
		if rec5.Code != http.StatusBadRequest {
			t.Errorf("expected 400 for uppercase email domain, got %d", rec5.Code)
		}

		invalidDomain := "invalidformat"
		bodyInvalidDomain, _ := json.Marshal(CreateBeneficiaryCategoryRequest{
			Name:        testCatName + " Invalid Domain",
			Variant:     "university",
			EmailDomain: &invalidDomain,
		})
		req6 := setStaffAuth(httptest.NewRequest(http.MethodPost, "/api/v1/beneficiary-categories", bytes.NewReader(bodyInvalidDomain)))
		rec6 := httptest.NewRecorder()
		h.CreateBeneficiaryCategoryHandler(rec6, req6)
		if rec6.Code != http.StatusBadRequest {
			t.Errorf("expected 400 for domain without dot, got %d", rec6.Code)
		}
	})

	t.Run("Get Beneficiary Category by ID", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/beneficiary-categories/"+createdID, nil)
		req.SetPathValue("id", createdID)
		rec := httptest.NewRecorder()

		h.GetBeneficiaryCategoryByIDHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		dataBytes, _ := json.Marshal(resp.Data)
		var catResp BeneficiaryCategoryResponse
		_ = json.Unmarshal(dataBytes, &catResp)

		if catResp.ID != createdID || catResp.Name != testCatName {
			t.Errorf("expected ID %s and name %s, got %+v", createdID, testCatName, catResp)
		}
		if catResp.Address == nil || *catResp.Address != testAddress {
			t.Errorf("expected address %s, got %v", testAddress, catResp.Address)
		}
		if catResp.Maps == nil || *catResp.Maps != testMaps {
			t.Errorf("expected maps %s, got %v", testMaps, catResp.Maps)
		}
		if catResp.EmailDomain == nil || *catResp.EmailDomain != testDomain {
			t.Errorf("expected email_domain %s, got %v", testDomain, catResp.EmailDomain)
		}
		if !catResp.IsPriority {
			t.Errorf("expected is_priority true, got false")
		}
	})

	t.Run("Get Beneficiary Category with Invalid UUID Format (400 VAL_001)", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/beneficiary-categories/invalid-uuid", nil)
		req.SetPathValue("id", "invalid-uuid")
		rec := httptest.NewRecorder()

		h.GetBeneficiaryCategoryByIDHandler(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d", rec.Code)
		}
	})

	t.Run("Get Beneficiary Category Non-Existent ID (404 NOT_FOUND)", func(t *testing.T) {
		nonExistent := uuid.NewString()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/beneficiary-categories/"+nonExistent, nil)
		req.SetPathValue("id", nonExistent)
		rec := httptest.NewRecorder()

		h.GetBeneficiaryCategoryByIDHandler(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 Not Found, got %d", rec.Code)
		}
	})

	t.Run("Update Beneficiary Category (Happy Path with new fields)", func(t *testing.T) {
		updatedName := testCatName + " Terupdate"
		updatedAddress := "Jl. Solo KM 10, Yogyakarta"
		updatedMaps := "https://maps.google.com/?q=solo"
		updatedDomain := "updated-" + uuid.NewString()[:8] + ".ac.id"
		body := []byte(fmt.Sprintf(`{"name":"%s","variant":"university","address":"%s","maps":"%s","email_domain":"%s","is_priority":false,"hidden":true}`,
			updatedName, updatedAddress, updatedMaps, updatedDomain))

		req := setStaffAuth(httptest.NewRequest(http.MethodPatch, "/api/v1/beneficiary-categories/"+createdID, bytes.NewReader(body)))
		req.SetPathValue("id", createdID)
		rec := httptest.NewRecorder()

		h.UpdateBeneficiaryCategoryHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		dataBytes, _ := json.Marshal(resp.Data)
		var catResp BeneficiaryCategoryResponse
		_ = json.Unmarshal(dataBytes, &catResp)

		if catResp.Name != updatedName || catResp.Variant != "university" || !catResp.Hidden {
			t.Errorf("expected updated fields, got %+v", catResp)
		}
		if catResp.Address == nil || *catResp.Address != updatedAddress {
			t.Errorf("expected address %s, got %v", updatedAddress, catResp.Address)
		}
		if catResp.Maps == nil || *catResp.Maps != updatedMaps {
			t.Errorf("expected maps %s, got %v", updatedMaps, catResp.Maps)
		}
		if catResp.EmailDomain == nil || *catResp.EmailDomain != updatedDomain {
			t.Errorf("expected email_domain %s, got %v", updatedDomain, catResp.EmailDomain)
		}
		if catResp.IsPriority {
			t.Errorf("expected is_priority false, got true")
		}
	})

	t.Run("Update Beneficiary Category Incompatible Variant with Email Domain (400 VAL_001)", func(t *testing.T) {
		body := []byte(`{"variant":"orphanage"}`)
		req := setStaffAuth(httptest.NewRequest(http.MethodPatch, "/api/v1/beneficiary-categories/"+createdID, bytes.NewReader(body)))
		req.SetPathValue("id", createdID)
		rec := httptest.NewRecorder()

		h.UpdateBeneficiaryCategoryHandler(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d. Body: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("Public Access to Hidden Category (404) vs Hero Access (404) vs Staff Access (200)", func(t *testing.T) {
		pubReq := httptest.NewRequest(http.MethodGet, "/api/v1/beneficiary-categories/"+createdID, nil)
		pubReq.SetPathValue("id", createdID)
		pubRec := httptest.NewRecorder()
		h.GetBeneficiaryCategoryByIDHandler(pubRec, pubReq)

		if pubRec.Code != http.StatusNotFound {
			t.Fatalf("expected public to get 404 for hidden category, got %d", pubRec.Code)
		}

		heroReq := setHeroAuth(httptest.NewRequest(http.MethodGet, "/api/v1/beneficiary-categories/"+createdID, nil))
		heroReq.SetPathValue("id", createdID)
		heroRec := httptest.NewRecorder()
		h.GetBeneficiaryCategoryByIDHandler(heroRec, heroReq)

		if heroRec.Code != http.StatusNotFound {
			t.Fatalf("expected hero to get 404 for hidden category, got %d", heroRec.Code)
		}

		staffReq := setStaffAuth(httptest.NewRequest(http.MethodGet, "/api/v1/beneficiary-categories/"+createdID, nil))
		staffReq.SetPathValue("id", createdID)
		staffRec := httptest.NewRecorder()
		h.GetBeneficiaryCategoryByIDHandler(staffRec, staffReq)

		if staffRec.Code != http.StatusOK {
			t.Fatalf("expected staff to get 200 for hidden category, got %d", staffRec.Code)
		}
	})

	t.Run("Update Beneficiary Category Variant Rejection When Heroes Enrolled (400 VAL_001)", func(t *testing.T) {
		var catID uuid.UUID
		err := pool.QueryRow(context.Background(), `
			INSERT INTO beneficiary_categories (name, variant, is_priority, hidden)
			VALUES ('Kategori Uji Hero Guard', 'university', true, false)
			RETURNING id
		`).Scan(&catID)
		if err != nil {
			t.Fatalf("failed to insert test category: %v", err)
		}
		defer func() {
			_, _ = pool.Exec(context.Background(), "DELETE FROM hero_categories WHERE beneficiary_category_id = $1", catID)
			_, _ = pool.Exec(context.Background(), "DELETE FROM beneficiary_categories WHERE id = $1", catID)
		}()

		var heroUserID uuid.UUID
		err = pool.QueryRow(context.Background(), `
			INSERT INTO users (email, name, is_active, email_verified_at)
			VALUES ('hero.cat.guard@example.com', 'Hero Guard Tester', true, NOW())
			RETURNING id
		`).Scan(&heroUserID)
		if err != nil {
			t.Fatalf("failed to insert test hero user: %v", err)
		}
		defer func() {
			_, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", heroUserID)
		}()

		_, err = pool.Exec(context.Background(), `
			INSERT INTO heroes (user_id) VALUES ($1)
		`, heroUserID)
		if err != nil {
			t.Fatalf("failed to insert hero record: %v", err)
		}

		_, err = pool.Exec(context.Background(), `
			INSERT INTO hero_categories (hero_id, beneficiary_category_id)
			VALUES ($1, $2)
		`, heroUserID, catID)
		if err != nil {
			t.Fatalf("failed to insert hero_category: %v", err)
		}

		updateBody := []byte(`{"variant":"orphanage"}`)
		req := setStaffAuth(httptest.NewRequest(http.MethodPatch, "/api/v1/beneficiary-categories/"+catID.String(), bytes.NewReader(updateBody)))
		req.SetPathValue("id", catID.String())
		rec := httptest.NewRecorder()

		h.UpdateBeneficiaryCategoryHandler(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request when mutating variant with enrolled heroes, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.Code != "VAL_001" {
			t.Errorf("expected error code VAL_001, got %s", resp.Code)
		}
	})

	t.Run("Delete Beneficiary Category (Soft Delete)", func(t *testing.T) {
		req := setStaffAuth(httptest.NewRequest(http.MethodDelete, "/api/v1/beneficiary-categories/"+createdID, nil))
		req.SetPathValue("id", createdID)
		rec := httptest.NewRecorder()

		h.DeleteBeneficiaryCategoryHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		getReq := setStaffAuth(httptest.NewRequest(http.MethodGet, "/api/v1/beneficiary-categories/"+createdID, nil))
		getReq.SetPathValue("id", createdID)
		getRec := httptest.NewRecorder()
		h.GetBeneficiaryCategoryByIDHandler(getRec, getReq)

		if getRec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 after soft-delete, got %d", getRec.Code)
		}

		delReq := setStaffAuth(httptest.NewRequest(http.MethodDelete, "/api/v1/beneficiary-categories/"+createdID, nil))
		delReq.SetPathValue("id", createdID)
		delRec := httptest.NewRecorder()
		h.DeleteBeneficiaryCategoryHandler(delRec, delReq)

		if delRec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 on already deleted record, got %d", delRec.Code)
		}
	})
}

func TestBeneficiaryCategoryListAndTrigramSearch(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	cfg := config.LoadConfig()
	h := NewBeneficiaryCategoryHandler(pool, &cfg)

	t.Run("List Beneficiary Categories with Pagination", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/beneficiary-categories?page=1&limit=10", nil)
		rec := httptest.NewRecorder()

		h.ListBeneficiaryCategoriesHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)

		if resp.Meta == nil {
			t.Fatal("expected pagination metadata to be populated")
		}
		if resp.Meta.Limit != 10 || resp.Meta.Page != 1 {
			t.Errorf("expected page 1 limit 10, got page %d limit %d", resp.Meta.Page, resp.Meta.Limit)
		}
		if resp.Meta.Total < 20 {
			t.Errorf("expected at least 20 seeded categories, got %d", resp.Meta.Total)
		}

		dataBytes, _ := json.Marshal(resp.Data)
		var items []BeneficiaryCategoryResponse
		_ = json.Unmarshal(dataBytes, &items)

		if len(items) != 10 {
			t.Errorf("expected 10 items returned, got %d", len(items))
		}
	})

	t.Run("Filter by Variant (?variant=university)", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/beneficiary-categories?variant=university&limit=50", nil)
		rec := httptest.NewRecorder()

		h.ListBeneficiaryCategoriesHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rec.Code)
		}

		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		dataBytes, _ := json.Marshal(resp.Data)
		var items []BeneficiaryCategoryResponse
		_ = json.Unmarshal(dataBytes, &items)

		if len(items) == 0 {
			t.Fatal("expected university items, got 0")
		}

		for _, item := range items {
			if item.Variant != "university" {
				t.Errorf("expected variant university, got %s", item.Variant)
			}
		}
	})

	t.Run("Filter by Variant (?variant=orphanage)", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/beneficiary-categories?variant=orphanage&limit=50", nil)
		rec := httptest.NewRecorder()

		h.ListBeneficiaryCategoriesHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rec.Code)
		}

		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		dataBytes, _ := json.Marshal(resp.Data)
		var items []BeneficiaryCategoryResponse
		_ = json.Unmarshal(dataBytes, &items)

		if len(items) == 0 {
			t.Fatal("expected orphanage items, got 0")
		}

		for _, item := range items {
			if item.Variant != "orphanage" {
				t.Errorf("expected variant orphanage, got %s", item.Variant)
			}
		}
	})

	t.Run("Trigram Substring Search (?search=panti)", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/beneficiary-categories?search=panti&limit=50", nil)
		rec := httptest.NewRecorder()

		h.ListBeneficiaryCategoriesHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rec.Code)
		}

		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		dataBytes, _ := json.Marshal(resp.Data)
		var items []BeneficiaryCategoryResponse
		_ = json.Unmarshal(dataBytes, &items)

		if len(items) == 0 {
			t.Fatal("expected items matching 'panti', got 0")
		}

		for _, item := range items {
			if !strings.Contains(strings.ToLower(item.Name), "panti") {
				t.Errorf("expected '%s' to contain 'panti'", item.Name)
			}
		}
	})

	t.Run("Search with No Matching Records", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/beneficiary-categories?search=xyznonexistentitem12345", nil)
		rec := httptest.NewRecorder()

		h.ListBeneficiaryCategoriesHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rec.Code)
		}

		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.Meta.Total != 0 {
			t.Errorf("expected total 0, got %d", resp.Meta.Total)
		}
	})
}
