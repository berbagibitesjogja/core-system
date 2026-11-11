package routes

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/berbagibitesjogja/core-system/internal/auth"
	"github.com/berbagibitesjogja/core-system/internal/config"
	"github.com/berbagibitesjogja/core-system/internal/database"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func getTestPool(t *testing.T) *pgxpool.Pool {
	cfg := config.LoadConfig()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	pool, err := database.NewPool(ctx, &cfg.DBConfig)
	if err != nil {
		t.Skipf("skipping test: database not reachable: %v", err)
	}
	return pool
}

func TestActorBoundariesInRoutes(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	cfg := config.LoadConfig()
	router := NewRouter(RouterConfig{
		Pool:   pool,
		Config: &cfg,
	})

	// Generate hero access token (is_staff = false, is_hero = true)
	heroID := uuid.New()
	heroToken, err := auth.SignAccessToken(heroID, "hero.test@example.com", false, "", 0, true, cfg.AppConfig.JWTSecret, 15*time.Minute)
	if err != nil {
		t.Fatalf("failed to sign hero token: %v", err)
	}

	// Generate staff access token (is_staff = true, is_hero = false)
	staffID := uuid.New()
	staffToken, err := auth.SignAccessToken(staffID, "staff.test@berbagibitesjogja.com", true, "staff", 1, false, cfg.AppConfig.JWTSecret, 15*time.Minute)
	if err != nil {
		t.Fatalf("failed to sign staff token: %v", err)
	}

	// Staff-protected endpoints (RequireStaff)
	staffEndpoints := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/api/v1/invitations"},
		{http.MethodPost, "/api/v1/sponsors"},
		{http.MethodPatch, "/api/v1/sponsors/00000000-0000-0000-0000-000000000001"},
		{http.MethodDelete, "/api/v1/sponsors/00000000-0000-0000-0000-000000000001"},
		{http.MethodPost, "/api/v1/food-items"},
		{http.MethodPatch, "/api/v1/food-items/1"},
		{http.MethodDelete, "/api/v1/food-items/1"},
		{http.MethodPost, "/api/v1/beneficiary-categories"},
		{http.MethodPatch, "/api/v1/beneficiary-categories/00000000-0000-0000-0000-000000000001"},
		{http.MethodDelete, "/api/v1/beneficiary-categories/00000000-0000-0000-0000-000000000001"},
		{http.MethodPost, "/api/v1/reimbursements"},
		{http.MethodGet, "/api/v1/reimbursements"},
		{http.MethodGet, "/api/v1/reimbursements/00000000-0000-0000-0000-000000000001"},
		{http.MethodPatch, "/api/v1/reimbursements/00000000-0000-0000-0000-000000000001"},
		{http.MethodDelete, "/api/v1/reimbursements/00000000-0000-0000-0000-000000000001"},
		{http.MethodGet, "/api/v1/heroes/00000000-0000-0000-0000-000000000001/categories"},
		{http.MethodPost, "/api/v1/heroes/00000000-0000-0000-0000-000000000001/categories"},
		{http.MethodDelete, "/api/v1/heroes/00000000-0000-0000-0000-000000000001/categories/00000000-0000-0000-0000-000000000001"},
	}

	t.Run("Hero caller is rejected with 403 Forbidden on all staff endpoints", func(t *testing.T) {
		for _, ep := range staffEndpoints {
			t.Run(fmt.Sprintf("%s %s", ep.method, ep.path), func(t *testing.T) {
				req := httptest.NewRequest(ep.method, ep.path, nil)
				req.Header.Set("Authorization", "Bearer "+heroToken)
				rec := httptest.NewRecorder()

				router.ServeHTTP(rec, req)

				if rec.Code != http.StatusForbidden {
					t.Errorf("expected 403 Forbidden for hero calling %s %s, got %d. Body: %s", ep.method, ep.path, rec.Code, rec.Body.String())
				}
			})
		}
	})

	t.Run("Unauthenticated caller is rejected with 401 Unauthorized on all staff endpoints", func(t *testing.T) {
		for _, ep := range staffEndpoints {
			t.Run(fmt.Sprintf("%s %s", ep.method, ep.path), func(t *testing.T) {
				req := httptest.NewRequest(ep.method, ep.path, nil)
				rec := httptest.NewRecorder()

				router.ServeHTTP(rec, req)

				if rec.Code != http.StatusUnauthorized {
					t.Errorf("expected 401 Unauthorized for unauthenticated call to %s %s, got %d", ep.method, ep.path, rec.Code)
				}
			})
		}
	})

	t.Run("Staff caller is rejected with 403 Forbidden on hero-only endpoints", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/heroes/me", nil)
		req.Header.Set("Authorization", "Bearer "+staffToken)
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("expected 403 Forbidden for staff calling GET /api/v1/heroes/me, got %d. Body: %s", rec.Code, rec.Body.String())
		}
	})
}
