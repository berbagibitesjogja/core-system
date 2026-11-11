package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/berbagibitesjogja/core-system/internal/auth"
	"github.com/berbagibitesjogja/core-system/internal/config"
	"github.com/berbagibitesjogja/core-system/internal/middleware"
	"github.com/berbagibitesjogja/core-system/internal/response"
	"github.com/google/uuid"
)

func TestHeroesHandlers(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	cfg := config.LoadConfig()
	h := NewHeroHandler(pool, &cfg)

	// Pre-create staff user with UGM email to test BecomeHeroHandler + auto-enroll
	staffEmail := "staff.becomes.hero@mail.ugm.ac.id"
	var staffUserID uuid.UUID
	err := pool.QueryRow(context.Background(), `
		INSERT INTO users (email, name, phone_number, is_active, email_verified_at)
		VALUES ($1, 'Staff Hero Candidate', '628777777777', true, NOW())
		RETURNING id
	`, staffEmail).Scan(&staffUserID)
	if err != nil {
		t.Fatalf("failed to insert test user: %v", err)
	}
	defer func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", staffUserID)
	}()

	_, err = pool.Exec(context.Background(), `
		INSERT INTO staff (user_id, division_id, role)
		VALUES ($1, 2, 'staff')
	`, staffUserID)
	if err != nil {
		t.Fatalf("failed to insert staff: %v", err)
	}

	t.Run("POST /api/v1/heroes/me: staff user becomes hero and auto-enrolls into university category", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/heroes/me", nil)
		claims := &auth.Claims{
			UserID:     staffUserID,
			Email:      staffEmail,
			IsStaff:    true,
			Role:       "staff",
			DivisionID: 2,
			IsHero:     false,
		}
		req = req.WithContext(middleware.WithClaims(req.Context(), claims))
		rec := httptest.NewRecorder()

		h.BecomeHeroHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		dataBytes, _ := json.Marshal(resp.Data)
		var tokenResp AuthTokenResponse
		_ = json.Unmarshal(dataBytes, &tokenResp)

		if !tokenResp.User.IsStaff || !tokenResp.User.IsHero {
			t.Fatalf("expected both is_staff=true and is_hero=true, got: %+v", tokenResp.User)
		}

		// Verify heroes row created in DB
		var heroID uuid.UUID
		err := pool.QueryRow(context.Background(), "SELECT user_id FROM heroes WHERE user_id = $1", staffUserID).Scan(&heroID)
		if err != nil || heroID != staffUserID {
			t.Fatalf("expected hero record in DB: %v", err)
		}

		// Verify auto-enrollment into UGM category
		var catName string
		err = pool.QueryRow(context.Background(), `
			SELECT bc.name
			FROM hero_categories hc
			JOIN beneficiary_categories bc ON bc.id = hc.beneficiary_category_id
			WHERE hc.hero_id = $1
		`, staffUserID).Scan(&catName)
		if err != nil || catName != "Universitas Gadjah Mada" {
			t.Fatalf("expected auto-enrollment into UGM: %v (cat: %s)", err, catName)
		}
	})

	t.Run("GET /api/v1/heroes/me: hero retrieves own profile and categories", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/heroes/me", nil)
		claims := &auth.Claims{
			UserID:     staffUserID,
			Email:      staffEmail,
			IsStaff:    true,
			Role:       "staff",
			DivisionID: 2,
			IsHero:     true,
		}
		req = req.WithContext(middleware.WithClaims(req.Context(), claims))
		rec := httptest.NewRecorder()

		h.GetMyHeroProfileHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		dataBytes, _ := json.Marshal(resp.Data)
		var profile HeroProfileResponse
		_ = json.Unmarshal(dataBytes, &profile)

		if profile.Email != staffEmail || profile.ID != staffUserID.String() {
			t.Fatalf("unexpected profile: %+v", profile)
		}
		if len(profile.Categories) == 0 || profile.Categories[0].Name != "Universitas Gadjah Mada" {
			t.Fatalf("expected UGM in categories: %+v", profile.Categories)
		}
	})

	t.Run("GET /api/v1/heroes/{id}/categories: staff lists a hero's categories", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/heroes/"+staffUserID.String()+"/categories", nil)
		req.SetPathValue("id", staffUserID.String())
		rec := httptest.NewRecorder()

		h.ListHeroCategoriesHandler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d. Body: %s", rec.Code, rec.Body.String())
		}

		var resp response.Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		dataBytes, _ := json.Marshal(resp.Data)
		var categories []HeroCategoryItem
		_ = json.Unmarshal(dataBytes, &categories)

		if len(categories) == 0 {
			t.Fatal("expected at least 1 category for hero")
		}
	})

	t.Run("POST & DELETE /api/v1/heroes/{id}/categories: staff assigns KIPK priority category then removes it", func(t *testing.T) {
		// Find KIPK category ID
		var kipkID uuid.UUID
		err := pool.QueryRow(context.Background(), "SELECT id FROM beneficiary_categories WHERE variant = 'kipk' LIMIT 1").Scan(&kipkID)
		if err != nil {
			t.Fatalf("failed to query KIPK category: %v", err)
		}

		// 1. Assign KIPK category
		addPayload := AddHeroCategoryRequest{CategoryID: kipkID.String()}
		addBody, _ := json.Marshal(addPayload)
		addReq := httptest.NewRequest(http.MethodPost, "/api/v1/heroes/"+staffUserID.String()+"/categories", bytes.NewReader(addBody))
		addReq.SetPathValue("id", staffUserID.String())
		addRec := httptest.NewRecorder()

		h.AddHeroCategoryHandler(addRec, addReq)

		if addRec.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created on adding KIPK category, got %d. Body: %s", addRec.Code, addRec.Body.String())
		}

		// Verify hero now has 2 categories
		var count int
		_ = pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM hero_categories WHERE hero_id = $1", staffUserID).Scan(&count)
		if count != 2 {
			t.Fatalf("expected 2 categories for hero, got %d", count)
		}

		// 2. Remove KIPK category
		delReq := httptest.NewRequest(http.MethodDelete, "/api/v1/heroes/"+staffUserID.String()+"/categories/"+kipkID.String(), nil)
		delReq.SetPathValue("id", staffUserID.String())
		delReq.SetPathValue("category_id", kipkID.String())
		delRec := httptest.NewRecorder()

		h.RemoveHeroCategoryHandler(delRec, delReq)

		if delRec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on removing category, got %d", delRec.Code)
		}

		// Verify count back to 1
		_ = pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM hero_categories WHERE hero_id = $1", staffUserID).Scan(&count)
		if count != 1 {
			t.Fatalf("expected 1 category remaining for hero, got %d", count)
		}
	})

	t.Run("POST /api/v1/heroes/{id}/categories rejects non-university/non-KIPK variant with 400", func(t *testing.T) {
		// Find a community/orphanage category
		var nonEligibleID uuid.UUID
		err := pool.QueryRow(context.Background(), "SELECT id FROM beneficiary_categories WHERE variant NOT IN ('university', 'kipk') LIMIT 1").Scan(&nonEligibleID)
		if err != nil {
			t.Fatalf("failed to query non-university category: %v", err)
		}

		payload := AddHeroCategoryRequest{CategoryID: nonEligibleID.String()}
		bodyBytes, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/heroes/"+staffUserID.String()+"/categories", bytes.NewReader(bodyBytes))
		req.SetPathValue("id", staffUserID.String())
		rec := httptest.NewRecorder()

		h.AddHeroCategoryHandler(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request for non-eligible category variant, got %d. Body: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("GET /api/v1/heroes/{id}/categories returns 404 for non-existent hero", func(t *testing.T) {
		randomID := uuid.New().String()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/heroes/"+randomID+"/categories", nil)
		req.SetPathValue("id", randomID)
		rec := httptest.NewRecorder()

		h.ListHeroCategoriesHandler(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 Not Found for non-existent hero, got %d. Body: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("DELETE /api/v1/heroes/{id}/categories/{category_id} returns 404 for non-existent hero", func(t *testing.T) {
		randomID := uuid.New().String()
		randomCatID := uuid.New().String()
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/heroes/"+randomID+"/categories/"+randomCatID, nil)
		req.SetPathValue("id", randomID)
		req.SetPathValue("category_id", randomCatID)
		rec := httptest.NewRecorder()

		h.RemoveHeroCategoryHandler(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 Not Found for non-existent hero, got %d. Body: %s", rec.Code, rec.Body.String())
		}
	})
}
