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
	cfg.AppConfig.RateLimit = 10000
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

	// Generate IT staff token (division_id = 1, is_staff = true)
	itStaffID := uuid.New()
	itStaffToken, err := auth.SignAccessToken(itStaffID, "it.test@berbagibitesjogja.com", true, "staff", 1, false, cfg.AppConfig.JWTSecret, 15*time.Minute)
	if err != nil {
		t.Fatalf("failed to sign IT staff token: %v", err)
	}

	// Generate Operational staff token (division_id = 3, is_staff = true)
	opStaffID := uuid.New()
	opStaffToken, err := auth.SignAccessToken(opStaffID, "ops.test@berbagibitesjogja.com", true, "staff", 3, false, cfg.AppConfig.JWTSecret, 15*time.Minute)
	if err != nil {
		t.Fatalf("failed to sign operational staff token: %v", err)
	}

	// Generate Super Admin token (role = "super", universal bypass)
	superAdminID := uuid.New()
	superAdminToken, err := auth.SignAccessToken(superAdminID, "super.test@berbagibitesjogja.com", true, "super", 1, false, cfg.AppConfig.JWTSecret, 15*time.Minute)
	if err != nil {
		t.Fatalf("failed to sign super admin token: %v", err)
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
		{http.MethodPost, "/api/v1/donations"},
		{http.MethodPatch, "/api/v1/donations/00000000-0000-0000-0000-000000000001"},
		{http.MethodDelete, "/api/v1/donations/00000000-0000-0000-0000-000000000001"},
		{http.MethodPost, "/api/v1/donations/00000000-0000-0000-0000-000000000001/publish"},
		{http.MethodPost, "/api/v1/donations/00000000-0000-0000-0000-000000000001/close"},
		{http.MethodPost, "/api/v1/donations/00000000-0000-0000-0000-000000000001/reopen"},
		{http.MethodPost, "/api/v1/donations/00000000-0000-0000-0000-000000000001/complete"},
		{http.MethodPost, "/api/v1/donations/00000000-0000-0000-0000-000000000001/cancel"},
		{http.MethodPatch, "/api/v1/donations/00000000-0000-0000-0000-000000000001/war-slots/1"},
		{http.MethodDelete, "/api/v1/donations/00000000-0000-0000-0000-000000000001/war-slots/1"},
	}

	// Operational-specific mutating endpoints (RequireOperationalStaff)
	operationalEndpoints := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/api/v1/sponsors"},
		{http.MethodPatch, "/api/v1/sponsors/00000000-0000-0000-0000-000000000001"},
		{http.MethodDelete, "/api/v1/sponsors/00000000-0000-0000-0000-000000000001"},
		{http.MethodPost, "/api/v1/food-items"},
		{http.MethodPatch, "/api/v1/food-items/1"},
		{http.MethodDelete, "/api/v1/food-items/1"},
		{http.MethodPost, "/api/v1/donations"},
		{http.MethodPatch, "/api/v1/donations/00000000-0000-0000-0000-000000000001"},
		{http.MethodDelete, "/api/v1/donations/00000000-0000-0000-0000-000000000001"},
		{http.MethodPost, "/api/v1/donations/00000000-0000-0000-0000-000000000001/publish"},
		{http.MethodPost, "/api/v1/donations/00000000-0000-0000-0000-000000000001/close"},
		{http.MethodPost, "/api/v1/donations/00000000-0000-0000-0000-000000000001/reopen"},
		{http.MethodPost, "/api/v1/donations/00000000-0000-0000-0000-000000000001/complete"},
		{http.MethodPost, "/api/v1/donations/00000000-0000-0000-0000-000000000001/cancel"},
		{http.MethodPatch, "/api/v1/donations/00000000-0000-0000-0000-000000000001/war-slots/1"},
		{http.MethodDelete, "/api/v1/donations/00000000-0000-0000-0000-000000000001/war-slots/1"},
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
		req.Header.Set("Authorization", "Bearer "+itStaffToken)
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("expected 403 Forbidden for staff calling GET /api/v1/heroes/me, got %d. Body: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("Non-operational staff (IT staff, division 1) is rejected with 403 on operational mutating endpoints", func(t *testing.T) {
		for _, ep := range operationalEndpoints {
			t.Run(fmt.Sprintf("%s %s", ep.method, ep.path), func(t *testing.T) {
				req := httptest.NewRequest(ep.method, ep.path, nil)
				req.Header.Set("Authorization", "Bearer "+itStaffToken)
				rec := httptest.NewRecorder()

				router.ServeHTTP(rec, req)

				if rec.Code != http.StatusForbidden {
					t.Errorf("expected 403 Forbidden for IT staff on %s %s, got %d. Body: %s", ep.method, ep.path, rec.Code, rec.Body.String())
				}
			})
		}
	})

	t.Run("Operational staff (division 3) passes authorization on operational mutating endpoints", func(t *testing.T) {
		for _, ep := range operationalEndpoints {
			t.Run(fmt.Sprintf("%s %s", ep.method, ep.path), func(t *testing.T) {
				req := httptest.NewRequest(ep.method, ep.path, nil)
				req.Header.Set("Authorization", "Bearer "+opStaffToken)
				rec := httptest.NewRecorder()

				router.ServeHTTP(rec, req)

				if rec.Code == http.StatusForbidden || rec.Code == http.StatusUnauthorized {
					t.Errorf("unexpected auth rejection %d for Operational staff on %s %s. Body: %s", rec.Code, ep.method, ep.path, rec.Body.String())
				}
			})
		}
	})

	t.Run("Super admin passes authorization on operational mutating endpoints", func(t *testing.T) {
		for _, ep := range operationalEndpoints {
			t.Run(fmt.Sprintf("%s %s", ep.method, ep.path), func(t *testing.T) {
				req := httptest.NewRequest(ep.method, ep.path, nil)
				req.Header.Set("Authorization", "Bearer "+superAdminToken)
				rec := httptest.NewRecorder()

				router.ServeHTTP(rec, req)

				if rec.Code == http.StatusForbidden || rec.Code == http.StatusUnauthorized {
					t.Errorf("unexpected auth rejection %d for Super Admin on %s %s. Body: %s", rec.Code, ep.method, ep.path, rec.Body.String())
				}
			})
		}
	})
}
