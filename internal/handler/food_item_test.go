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

func TestFoodItemCRUD(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	cfg := config.LoadConfig()
	h := NewFoodItemHandler(pool, &cfg)

	testItemName := "Makanan Uji " + uuid.NewString()[:8]
	defer func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM food_items WHERE name LIKE 'Makanan Uji %'")
	}()

	var createdID int32

	t.Run("Create Food Item (Happy Path)", func(t *testing.T) {
		body, _ := json.Marshal(CreateFoodItemRequest{
			Name: testItemName,
		})

		req := setStaffAuth(httptest.NewRequest(http.MethodPost, "/api/v1/food-items", bytes.NewReader(body)))
		rec := httptest.NewRecorder()

		h.CreateFoodItemHandler(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		dataBytes, _ := json.Marshal(resp.Data)
		var itemResp FoodItemResponse
		_ = json.Unmarshal(dataBytes, &itemResp)

		if itemResp.Name != testItemName {
			t.Errorf("expected name %s, got %s", testItemName, itemResp.Name)
		}
		if itemResp.ID <= 0 {
			t.Errorf("expected positive ID, got %d", itemResp.ID)
		}

		createdID = itemResp.ID
	})

	t.Run("Create Food Item Case-Insensitive Collision (400 VAL_002)", func(t *testing.T) {
		// Attempt inserting the same name in different casing
		body, _ := json.Marshal(CreateFoodItemRequest{
			Name: strings.ToLower(testItemName),
		})

		req := setStaffAuth(httptest.NewRequest(http.MethodPost, "/api/v1/food-items", bytes.NewReader(body)))
		rec := httptest.NewRecorder()

		h.CreateFoodItemHandler(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.Code != "VAL_002" {
			t.Errorf("expected code VAL_002, got %s", resp.Code)
		}

		errMap, ok := resp.Errors.(map[string]any)
		if !ok || errMap["name"] != "Food item already exists" {
			t.Errorf("expected error 'Food item already exists', got %+v", resp.Errors)
		}
	})

	t.Run("Create Food Item Validation Failures (400 VAL_001)", func(t *testing.T) {
		tooLongName := strings.Repeat("A", 101)
		body, _ := json.Marshal(CreateFoodItemRequest{
			Name: tooLongName,
		})

		req := setStaffAuth(httptest.NewRequest(http.MethodPost, "/api/v1/food-items", bytes.NewReader(body)))
		rec := httptest.NewRecorder()

		h.CreateFoodItemHandler(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d", rec.Code)
		}

		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.Code != "VAL_001" {
			t.Errorf("expected code VAL_001, got %s", resp.Code)
		}
	})

	t.Run("Get Food Item by ID", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/food-items/%d", createdID), nil)
		req.SetPathValue("id", fmt.Sprintf("%d", createdID))
		rec := httptest.NewRecorder()

		h.GetFoodItemByIDHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		dataBytes, _ := json.Marshal(resp.Data)
		var itemResp FoodItemResponse
		_ = json.Unmarshal(dataBytes, &itemResp)

		if itemResp.ID != createdID || itemResp.Name != testItemName {
			t.Errorf("expected ID %d and name %s, got %+v", createdID, testItemName, itemResp)
		}
		if itemResp.CreatedAt == "" || itemResp.UpdatedAt == "" {
			t.Errorf("expected timestamps on detail endpoint, got %+v", itemResp)
		}
	})

	t.Run("Get Food Item with Invalid ID Format (400 VAL_001)", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/food-items/abc", nil)
		req.SetPathValue("id", "abc")
		rec := httptest.NewRecorder()

		h.GetFoodItemByIDHandler(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request for non-numeric ID, got %d", rec.Code)
		}
	})

	t.Run("Get Food Item with Non-Existent ID (404 NOT_FOUND)", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/food-items/999999", nil)
		req.SetPathValue("id", "999999")
		rec := httptest.NewRecorder()

		h.GetFoodItemByIDHandler(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 Not Found, got %d", rec.Code)
		}
	})

	t.Run("Update Food Item (Happy Path)", func(t *testing.T) {
		updatedName := testItemName + " Spesial"
		body, _ := json.Marshal(UpdateFoodItemRequest{
			Name: updatedName,
		})

		req := setStaffAuth(httptest.NewRequest(http.MethodPatch, fmt.Sprintf("/api/v1/food-items/%d", createdID), bytes.NewReader(body)))
		req.SetPathValue("id", fmt.Sprintf("%d", createdID))
		rec := httptest.NewRecorder()

		h.UpdateFoodItemHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		dataBytes, _ := json.Marshal(resp.Data)
		var itemResp FoodItemResponse
		_ = json.Unmarshal(dataBytes, &itemResp)

		if itemResp.Name != updatedName {
			t.Errorf("expected updated name %s, got %s", updatedName, itemResp.Name)
		}
	})

	t.Run("Delete Food Item", func(t *testing.T) {
		req := setStaffAuth(httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/v1/food-items/%d", createdID), nil))
		req.SetPathValue("id", fmt.Sprintf("%d", createdID))
		rec := httptest.NewRecorder()

		h.DeleteFoodItemHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		// Subsequent Get must return 404
		getReq := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/food-items/%d", createdID), nil)
		getReq.SetPathValue("id", fmt.Sprintf("%d", createdID))
		getRec := httptest.NewRecorder()
		h.GetFoodItemByIDHandler(getRec, getReq)

		if getRec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 after delete, got %d", getRec.Code)
		}

		// Subsequent Delete on non-existent record returns 404
		delReq := setStaffAuth(httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/v1/food-items/%d", createdID), nil))
		delReq.SetPathValue("id", fmt.Sprintf("%d", createdID))
		delRec := httptest.NewRecorder()
		h.DeleteFoodItemHandler(delRec, delReq)

		if delRec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 on already deleted record, got %d", delRec.Code)
		}
	})
}

func TestFoodItemListAndTrigramSearch(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	cfg := config.LoadConfig()
	h := NewFoodItemHandler(pool, &cfg)

	t.Run("List Food Items with Pagination", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/food-items?page=1&limit=10", nil)
		rec := httptest.NewRecorder()

		h.ListFoodItemsHandler(rec, req)

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
		if resp.Meta.Total < 300 {
			t.Errorf("expected at least 300 seeded items, got %d", resp.Meta.Total)
		}

		dataBytes, _ := json.Marshal(resp.Data)
		var items []FoodItemListItemResponse
		_ = json.Unmarshal(dataBytes, &items)

		if len(items) != 10 {
			t.Errorf("expected 10 items returned, got %d", len(items))
		}

		// Verify created_at and updated_at are not in raw json
		if strings.Contains(rec.Body.String(), "created_at") || strings.Contains(rec.Body.String(), "updated_at") {
			t.Errorf("expected created_at and updated_at to be stripped from list response")
		}
	})

	t.Run("Trigram Substring Search (?search=ayam)", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/food-items?search=ayam&limit=50", nil)
		rec := httptest.NewRecorder()

		h.ListFoodItemsHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		dataBytes, _ := json.Marshal(resp.Data)
		var items []FoodItemListItemResponse
		_ = json.Unmarshal(dataBytes, &items)

		if len(items) == 0 {
			t.Fatal("expected items matching 'ayam', got 0")
		}

		for _, item := range items {
			if !strings.Contains(strings.ToLower(item.Name), "ayam") {
				t.Errorf("expected '%s' to contain 'ayam'", item.Name)
			}
		}
	})

	t.Run("Search with No Matching Records", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/food-items?search=xyznonexistentitem12345", nil)
		rec := httptest.NewRecorder()

		h.ListFoodItemsHandler(rec, req)

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
