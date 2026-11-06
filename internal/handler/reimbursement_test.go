package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/berbagibitesjogja/core-system/internal/config"
	"github.com/berbagibitesjogja/core-system/internal/response"
	"github.com/google/uuid"
)

func TestReimbursementCRUD(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	cfg := config.LoadConfig()
	h := NewReimbursementHandler(pool, &cfg)

	// Seed a temporary dummy file in files table to test receipt attachment
	testFileID := uuid.New()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO files (id, storage_bucket, storage_key, mime_type, status)
		VALUES ($1, 'test-bucket', 'receipts/test.jpg', 'image/jpeg', 'uploaded')
		ON CONFLICT DO NOTHING
	`, testFileID)
	if err != nil {
		t.Fatalf("failed to seed test file: %v", err)
	}
	defer func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM reimbursement_files WHERE file_id = $1", testFileID)
		_, _ = pool.Exec(context.Background(), "DELETE FROM files WHERE id = $1", testFileID)
	}()

	testTarget := "08123456" + uuid.NewString()[:6]
	testNotes := "Beli bensin armada " + uuid.NewString()[:6]

	defer func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM reimbursements WHERE target LIKE '08123456%'")
	}()

	var createdID string

	t.Run("Create Reimbursement (Happy Path with Receipt File)", func(t *testing.T) {
		body, _ := json.Marshal(CreateReimbursementRequest{
			Amount:  50000,
			Method:  "GoPay",
			Target:  testTarget,
			Notes:   &testNotes,
			FileIDs: []string{testFileID.String()},
		})

		req := setStaffAuth(httptest.NewRequest(http.MethodPost, "/api/v1/reimbursements", bytes.NewReader(body)))
		rec := httptest.NewRecorder()

		h.CreateReimbursementHandler(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		dataBytes, _ := json.Marshal(resp.Data)
		var rResp ReimbursementResponse
		_ = json.Unmarshal(dataBytes, &rResp)

		if rResp.Amount != 50000 {
			t.Errorf("expected amount 50000, got %d", rResp.Amount)
		}
		if rResp.Method != "GoPay" {
			t.Errorf("expected method GoPay, got %s", rResp.Method)
		}
		if rResp.Target != testTarget {
			t.Errorf("expected target %s, got %s", testTarget, rResp.Target)
		}
		if rResp.Status != "pending" {
			t.Errorf("expected initial status pending, got %s", rResp.Status)
		}
		if len(rResp.FileIDs) != 1 || rResp.FileIDs[0] != testFileID.String() {
			t.Errorf("expected linked file ID %s, got %+v", testFileID.String(), rResp.FileIDs)
		}

		createdID = rResp.ID
	})

	t.Run("Create Reimbursement Validation Failures (400 VAL_001)", func(t *testing.T) {
		// Zero amount
		b1, _ := json.Marshal(CreateReimbursementRequest{
			Amount: 0,
			Method: "GoPay",
			Target: testTarget,
		})
		req1 := setStaffAuth(httptest.NewRequest(http.MethodPost, "/api/v1/reimbursements", bytes.NewReader(b1)))
		rec1 := httptest.NewRecorder()
		h.CreateReimbursementHandler(rec1, req1)
		if rec1.Code != http.StatusBadRequest {
			t.Errorf("expected 400 for zero amount, got %d", rec1.Code)
		}

		// Empty method
		b2, _ := json.Marshal(CreateReimbursementRequest{
			Amount: 10000,
			Method: "",
			Target: testTarget,
		})
		req2 := setStaffAuth(httptest.NewRequest(http.MethodPost, "/api/v1/reimbursements", bytes.NewReader(b2)))
		rec2 := httptest.NewRecorder()
		h.CreateReimbursementHandler(rec2, req2)
		if rec2.Code != http.StatusBadRequest {
			t.Errorf("expected 400 for empty method, got %d", rec2.Code)
		}

		// Empty target
		b3, _ := json.Marshal(CreateReimbursementRequest{
			Amount: 10000,
			Method: "GoPay",
			Target: "",
		})
		req3 := setStaffAuth(httptest.NewRequest(http.MethodPost, "/api/v1/reimbursements", bytes.NewReader(b3)))
		rec3 := httptest.NewRecorder()
		h.CreateReimbursementHandler(rec3, req3)
		if rec3.Code != http.StatusBadRequest {
			t.Errorf("expected 400 for empty target, got %d", rec3.Code)
		}

		// Invalid file UUID
		b4, _ := json.Marshal(CreateReimbursementRequest{
			Amount:  10000,
			Method:  "GoPay",
			Target:  testTarget,
			FileIDs: []string{"invalid-uuid"},
		})
		req4 := setStaffAuth(httptest.NewRequest(http.MethodPost, "/api/v1/reimbursements", bytes.NewReader(b4)))
		rec4 := httptest.NewRecorder()
		h.CreateReimbursementHandler(rec4, req4)
		if rec4.Code != http.StatusBadRequest {
			t.Errorf("expected 400 for invalid file UUID, got %d", rec4.Code)
		}
	})

	t.Run("Get Reimbursement by ID", func(t *testing.T) {
		req := setStaffAuth(httptest.NewRequest(http.MethodGet, "/api/v1/reimbursements/"+createdID, nil))
		req.SetPathValue("id", createdID)
		rec := httptest.NewRecorder()

		h.GetReimbursementByIDHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		dataBytes, _ := json.Marshal(resp.Data)
		var rResp ReimbursementResponse
		_ = json.Unmarshal(dataBytes, &rResp)

		if rResp.ID != createdID || rResp.Target != testTarget {
			t.Errorf("expected ID %s and target %s, got %+v", createdID, testTarget, rResp)
		}
		if len(rResp.FileIDs) != 1 {
			t.Errorf("expected 1 attached file, got %d", len(rResp.FileIDs))
		}
	})

	t.Run("Get Reimbursement with Invalid UUID (400 VAL_001)", func(t *testing.T) {
		req := setStaffAuth(httptest.NewRequest(http.MethodGet, "/api/v1/reimbursements/not-a-uuid", nil))
		req.SetPathValue("id", "not-a-uuid")
		rec := httptest.NewRecorder()

		h.GetReimbursementByIDHandler(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d", rec.Code)
		}
	})

	t.Run("Get Reimbursement Non-Existent ID (404 NOT_FOUND)", func(t *testing.T) {
		nonExistent := uuid.NewString()
		req := setStaffAuth(httptest.NewRequest(http.MethodGet, "/api/v1/reimbursements/"+nonExistent, nil))
		req.SetPathValue("id", nonExistent)
		rec := httptest.NewRecorder()

		h.GetReimbursementByIDHandler(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 Not Found, got %d", rec.Code)
		}
	})

	t.Run("Update Reimbursement (Happy Path)", func(t *testing.T) {
		updatedTarget := testTarget + "99"
		body := []byte(fmt.Sprintf(`{"amount":75000,"target":"%s","status":"completed"}`, updatedTarget))

		req := setStaffAuth(httptest.NewRequest(http.MethodPatch, "/api/v1/reimbursements/"+createdID, bytes.NewReader(body)))
		req.SetPathValue("id", createdID)
		rec := httptest.NewRecorder()

		h.UpdateReimbursementHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		dataBytes, _ := json.Marshal(resp.Data)
		var rResp ReimbursementResponse
		_ = json.Unmarshal(dataBytes, &rResp)

		if rResp.Amount != 75000 || rResp.Target != updatedTarget || rResp.Status != "completed" {
			t.Errorf("expected updated fields, got %+v", rResp)
		}
	})

	t.Run("Delete Reimbursement (Soft Delete)", func(t *testing.T) {
		req := setStaffAuth(httptest.NewRequest(http.MethodDelete, "/api/v1/reimbursements/"+createdID, nil))
		req.SetPathValue("id", createdID)
		rec := httptest.NewRecorder()

		h.DeleteReimbursementHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		// Subsequent Get must return 404
		getReq := setStaffAuth(httptest.NewRequest(http.MethodGet, "/api/v1/reimbursements/"+createdID, nil))
		getReq.SetPathValue("id", createdID)
		getRec := httptest.NewRecorder()
		h.GetReimbursementByIDHandler(getRec, getReq)

		if getRec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 after soft-delete, got %d", getRec.Code)
		}

		// Subsequent Delete on already soft-deleted record returns 404
		delReq := setStaffAuth(httptest.NewRequest(http.MethodDelete, "/api/v1/reimbursements/"+createdID, nil))
		delReq.SetPathValue("id", createdID)
		delRec := httptest.NewRecorder()
		h.DeleteReimbursementHandler(delRec, delReq)

		if delRec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 on already deleted record, got %d", delRec.Code)
		}
	})
}

func TestReimbursementListAndFilters(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	cfg := config.LoadConfig()
	h := NewReimbursementHandler(pool, &cfg)

	t.Run("List Reimbursements with Pagination", func(t *testing.T) {
		req := setStaffAuth(httptest.NewRequest(http.MethodGet, "/api/v1/reimbursements?page=1&limit=10", nil))
		rec := httptest.NewRecorder()

		h.ListReimbursementsHandler(rec, req)

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
	})

	t.Run("Filter by Status (?status=pending)", func(t *testing.T) {
		req := setStaffAuth(httptest.NewRequest(http.MethodGet, "/api/v1/reimbursements?status=pending&limit=10", nil))
		rec := httptest.NewRecorder()

		h.ListReimbursementsHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rec.Code)
		}

		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		dataBytes, _ := json.Marshal(resp.Data)
		var items []ReimbursementResponse
		_ = json.Unmarshal(dataBytes, &items)

		for _, item := range items {
			if item.Status != "pending" {
				t.Errorf("expected status pending, got %s", item.Status)
			}
		}
	})

	t.Run("Search with No Matching Records", func(t *testing.T) {
		req := setStaffAuth(httptest.NewRequest(http.MethodGet, "/api/v1/reimbursements?search=xyznonexistentsearchquery123", nil))
		rec := httptest.NewRecorder()

		h.ListReimbursementsHandler(rec, req)

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
