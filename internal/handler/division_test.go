package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/berbagibitesjogja/core-system/internal/config"
	"github.com/berbagibitesjogja/core-system/internal/response"
)

func TestListDivisionsHandler(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	cfg := config.LoadConfig()
	h := NewDivisionHandler(pool, &cfg)

	var softDeletedID int32
	err := pool.QueryRow(context.Background(), `
		INSERT INTO divisions (name, deleted_at)
		VALUES ('Temp Soft Deleted', NOW())
		RETURNING id
	`).Scan(&softDeletedID)
	if err != nil {
		t.Fatalf("failed to insert soft-deleted test division: %v", err)
	}
	defer func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM divisions WHERE id = $1`, softDeletedID)
	}()

	req := setStaffAuth(httptest.NewRequest(http.MethodGet, "/api/v1/divisions", nil))
	rec := httptest.NewRecorder()

	h.ListDivisionsHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d. Body: %s", rec.Code, rec.Body.String())
	}

	var envelope map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("failed to parse raw JSON: %v", err)
	}
	if _, hasMeta := envelope["meta"]; hasMeta {
		t.Fatalf("expected unpaginated response without meta, got meta=%v", envelope["meta"])
	}

	var resp response.Response
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if resp.Status != "success" {
		t.Errorf("expected status 'success', got %q", resp.Status)
	}

	rawItems, ok := resp.Data.([]any)
	if !ok {
		t.Fatalf("expected data to be JSON array, got %T", resp.Data)
	}
	if len(rawItems) < 4 {
		t.Fatalf("expected at least 4 seeded divisions, got %d", len(rawItems))
	}

	var prevID int32
	for _, entry := range rawItems {
		obj, ok := entry.(map[string]any)
		if !ok {
			t.Fatalf("expected division item object, got %T", entry)
		}
		if len(obj) != 2 {
			t.Fatalf("expected lean object with 2 keys (id, name), got %v", obj)
		}
		if _, hasCreatedAt := obj["created_at"]; hasCreatedAt {
			t.Fatalf("unexpected created_at in lean response: %v", obj)
		}
		if _, hasUpdatedAt := obj["updated_at"]; hasUpdatedAt {
			t.Fatalf("unexpected updated_at in lean response: %v", obj)
		}
		idFloat, ok := obj["id"].(float64)
		if !ok {
			t.Fatalf("expected numeric id, got %T", obj["id"])
		}
		id := int32(idFloat)
		if id == softDeletedID {
			t.Fatalf("soft-deleted division %d should not be returned", softDeletedID)
		}
		if id <= prevID {
			t.Fatalf("expected divisions ordered by id ASC, got %d after %d", id, prevID)
		}
		prevID = id
	}
}
