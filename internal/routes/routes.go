package routes

import (
	"net/http"

	"github.com/berbagibitesjogja/core-system/internal/config"
	"github.com/berbagibitesjogja/core-system/internal/database"
	"github.com/berbagibitesjogja/core-system/internal/dbgen"
	"github.com/berbagibitesjogja/core-system/internal/handler"
	"github.com/berbagibitesjogja/core-system/internal/mailer"
	"github.com/berbagibitesjogja/core-system/internal/middleware"
	"github.com/berbagibitesjogja/core-system/internal/response"
	"github.com/jackc/pgx/v5/pgxpool"
)

type RouterConfig struct {
	Pool   *pgxpool.Pool
	Config *config.Config
}

func NewRouter(routerCfg RouterConfig) http.Handler {
	mux := http.NewServeMux()

	// Health check endpoint
	mux.HandleFunc("/health", HealthCheckHandler(routerCfg.Pool))
	mux.HandleFunc("/api/v1", APIv1Handler)

	// Handlers initialization
	m := mailer.New(routerCfg.Config.SMTP)
	queries := dbgen.New(routerCfg.Pool)
	authHandler := handler.NewAuthHandler(routerCfg.Pool, routerCfg.Config)
	heroAuthHandler := handler.NewHeroAuthHandler(routerCfg.Pool, routerCfg.Config, m)
	heroHandler := handler.NewHeroHandler(routerCfg.Pool, routerCfg.Config)
	invitationHandler := handler.NewInvitationHandler(routerCfg.Pool, routerCfg.Config, m)
	oauthHandler := handler.NewOAuthHandler(routerCfg.Pool, routerCfg.Config, queries)
	sponsorHandler := handler.NewSponsorHandler(routerCfg.Pool, routerCfg.Config)
	foodItemHandler := handler.NewFoodItemHandler(routerCfg.Pool, routerCfg.Config)
	beneficiaryCategoryHandler := handler.NewBeneficiaryCategoryHandler(routerCfg.Pool, routerCfg.Config)
	reimbursementHandler := handler.NewReimbursementHandler(routerCfg.Pool, routerCfg.Config)
	staffHandler := handler.NewStaffHandler(routerCfg.Pool, routerCfg.Config)
	donationHandler := handler.NewDonationHandler(routerCfg.Pool, routerCfg.Config)

	authMiddleware := middleware.Authenticate(routerCfg.Config.AppConfig.JWTSecret)
	optionalAuth := middleware.OptionalAuthenticate(routerCfg.Config.AppConfig.JWTSecret)
	staffOnly := func(h http.Handler) http.Handler {
		return authMiddleware(middleware.RequireStaff(h))
	}
	heroOnly := func(h http.Handler) http.Handler {
		return authMiddleware(middleware.RequireHero(h))
	}
	operationalOnly := func(h http.Handler) http.Handler {
		return authMiddleware(middleware.RequireOperationalStaff(routerCfg.Config.AppConfig.DivisionOperational)(h))
	}

	// Public Auth Endpoints
	mux.HandleFunc("POST /api/v1/auth/register", authHandler.RegisterHandler)
	mux.HandleFunc("POST /api/v1/auth/login", authHandler.LoginHandler)
	mux.HandleFunc("POST /api/v1/auth/refresh", authHandler.RefreshTokenHandler)

	// Public Hero Auth Endpoints
	mux.HandleFunc("POST /api/v1/heroes/register", heroAuthHandler.RegisterHandler)
	mux.HandleFunc("POST /api/v1/heroes/verify-email", heroAuthHandler.VerifyEmailHandler)
	mux.HandleFunc("POST /api/v1/heroes/resend-verification", heroAuthHandler.ResendVerificationHandler)

	// Google OAuth Endpoints
	mux.HandleFunc("GET /api/v1/auth/google/login", oauthHandler.GoogleLoginHandler)
	mux.HandleFunc("GET /api/v1/auth/google/callback", oauthHandler.GoogleCallbackHandler)

	// Authenticated Auth Endpoints
	mux.Handle("POST /api/v1/auth/logout", authMiddleware(http.HandlerFunc(authHandler.LogoutHandler)))

	// Invitation Endpoints
	mux.HandleFunc("GET /api/v1/invitations/validate", invitationHandler.ValidateInvitationHandler)
	mux.Handle("POST /api/v1/invitations", staffOnly(http.HandlerFunc(invitationHandler.CreateInvitationHandler)))
	mux.Handle("POST /api/v1/invitations/accept", authMiddleware(http.HandlerFunc(invitationHandler.AcceptInvitationHandler)))

	// Hero Endpoints
	mux.Handle("POST /api/v1/heroes/me", authMiddleware(http.HandlerFunc(heroHandler.BecomeHeroHandler)))
	mux.Handle("GET /api/v1/heroes/me", heroOnly(http.HandlerFunc(heroHandler.GetMyHeroProfileHandler)))
	mux.Handle("GET /api/v1/heroes/{id}/categories", staffOnly(http.HandlerFunc(heroHandler.ListHeroCategoriesHandler)))
	mux.Handle("POST /api/v1/heroes/{id}/categories", staffOnly(http.HandlerFunc(heroHandler.AddHeroCategoryHandler)))
	mux.Handle("DELETE /api/v1/heroes/{id}/categories/{category_id}", staffOnly(http.HandlerFunc(heroHandler.RemoveHeroCategoryHandler)))

	// Staff Endpoints
	mux.Handle("GET /api/v1/staff", staffOnly(http.HandlerFunc(staffHandler.ListStaffHandler)))
	mux.Handle("GET /api/v1/staff/{id}", staffOnly(http.HandlerFunc(staffHandler.GetStaffHandler)))
	mux.Handle("PATCH /api/v1/staff/me", staffOnly(http.HandlerFunc(staffHandler.SelfDemoteStaffHandler)))
	mux.Handle("DELETE /api/v1/staff/me", staffOnly(http.HandlerFunc(staffHandler.ResignStaffHandler)))
	mux.Handle("PATCH /api/v1/staff/{id}", staffOnly(http.HandlerFunc(staffHandler.UpdateStaffHandler)))
	mux.Handle("DELETE /api/v1/staff/{id}", staffOnly(http.HandlerFunc(staffHandler.KickStaffHandler)))

	// Sponsor Endpoints
	mux.Handle("GET /api/v1/sponsors", optionalAuth(http.HandlerFunc(sponsorHandler.ListSponsorsHandler)))
	mux.Handle("GET /api/v1/sponsors/{id}", optionalAuth(http.HandlerFunc(sponsorHandler.GetSponsorByIDHandler)))
	mux.Handle("POST /api/v1/sponsors", operationalOnly(http.HandlerFunc(sponsorHandler.CreateSponsorHandler)))
	mux.Handle("PATCH /api/v1/sponsors/{id}", operationalOnly(http.HandlerFunc(sponsorHandler.UpdateSponsorHandler)))
	mux.Handle("DELETE /api/v1/sponsors/{id}", operationalOnly(http.HandlerFunc(sponsorHandler.DeleteSponsorHandler)))

	// Food Item Endpoints
	mux.HandleFunc("GET /api/v1/food-items", foodItemHandler.ListFoodItemsHandler)
	mux.HandleFunc("GET /api/v1/food-items/{id}", foodItemHandler.GetFoodItemByIDHandler)
	mux.Handle("POST /api/v1/food-items", operationalOnly(http.HandlerFunc(foodItemHandler.CreateFoodItemHandler)))
	mux.Handle("PATCH /api/v1/food-items/{id}", operationalOnly(http.HandlerFunc(foodItemHandler.UpdateFoodItemHandler)))
	mux.Handle("DELETE /api/v1/food-items/{id}", operationalOnly(http.HandlerFunc(foodItemHandler.DeleteFoodItemHandler)))

	// Beneficiary Category Endpoints
	mux.Handle("GET /api/v1/beneficiary-categories", optionalAuth(http.HandlerFunc(beneficiaryCategoryHandler.ListBeneficiaryCategoriesHandler)))
	mux.Handle("GET /api/v1/beneficiary-categories/{id}", optionalAuth(http.HandlerFunc(beneficiaryCategoryHandler.GetBeneficiaryCategoryByIDHandler)))
	mux.Handle("POST /api/v1/beneficiary-categories", staffOnly(http.HandlerFunc(beneficiaryCategoryHandler.CreateBeneficiaryCategoryHandler)))
	mux.Handle("PATCH /api/v1/beneficiary-categories/{id}", staffOnly(http.HandlerFunc(beneficiaryCategoryHandler.UpdateBeneficiaryCategoryHandler)))
	mux.Handle("DELETE /api/v1/beneficiary-categories/{id}", staffOnly(http.HandlerFunc(beneficiaryCategoryHandler.DeleteBeneficiaryCategoryHandler)))

	// Reimbursement Endpoints
	mux.Handle("POST /api/v1/reimbursements", staffOnly(http.HandlerFunc(reimbursementHandler.CreateReimbursementHandler)))
	mux.Handle("GET /api/v1/reimbursements", staffOnly(http.HandlerFunc(reimbursementHandler.ListReimbursementsHandler)))
	mux.Handle("GET /api/v1/reimbursements/{id}", staffOnly(http.HandlerFunc(reimbursementHandler.GetReimbursementByIDHandler)))
	mux.Handle("PATCH /api/v1/reimbursements/{id}", staffOnly(http.HandlerFunc(reimbursementHandler.UpdateReimbursementHandler)))
	mux.Handle("DELETE /api/v1/reimbursements/{id}", staffOnly(http.HandlerFunc(reimbursementHandler.DeleteReimbursementHandler)))

	// Donation Endpoints
	mux.Handle("GET /api/v1/donations", optionalAuth(http.HandlerFunc(donationHandler.ListDonationsHandler)))
	mux.Handle("POST /api/v1/donations", operationalOnly(http.HandlerFunc(donationHandler.CreateDonationHandler)))
	mux.Handle("GET /api/v1/donations/{id}", optionalAuth(http.HandlerFunc(donationHandler.GetDonationByIDHandler)))
	mux.Handle("PATCH /api/v1/donations/{id}", operationalOnly(http.HandlerFunc(donationHandler.UpdateDonationHandler)))
	mux.Handle("DELETE /api/v1/donations/{id}", operationalOnly(http.HandlerFunc(donationHandler.DeleteDonationHandler)))

	// Donation Lifecycle Action Endpoints (Mutation: Operational Staff or Super Admin)
	mux.Handle("POST /api/v1/donations/{id}/publish", operationalOnly(http.HandlerFunc(donationHandler.PublishDonationHandler)))
	mux.Handle("POST /api/v1/donations/{id}/close", operationalOnly(http.HandlerFunc(donationHandler.CloseDonationHandler)))
	mux.Handle("POST /api/v1/donations/{id}/reopen", operationalOnly(http.HandlerFunc(donationHandler.ReopenDonationHandler)))
	mux.Handle("POST /api/v1/donations/{id}/complete", operationalOnly(http.HandlerFunc(donationHandler.CompleteDonationHandler)))
	mux.Handle("POST /api/v1/donations/{id}/cancel", operationalOnly(http.HandlerFunc(donationHandler.CancelDonationHandler)))

	rateLimit := routerCfg.Config.AppConfig.RateLimit
	corsMiddleware := middleware.CORSWithConfig(routerCfg.Config.AppConfig.CORSOrigins, routerCfg.Config.AppConfig.CORSMaxAge)

	// RequestID and Logger wrap RateLimit so that 429 rejections are assigned ref_id and logged in slog
	return corsMiddleware(middleware.SecurityHeaders(middleware.RequestID(middleware.Logger(middleware.RateLimit(rateLimit)(mux)))))
}

func HealthCheckHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		db := database.CheckHealth(r.Context(), pool)
		if db.Status != "healthy" {
			response.ErrorWithRequest(w, r, http.StatusServiceUnavailable, "DB_UNHEALTHY", "Database is unhealthy", db)
			return
		}

		response.JSON(w, http.StatusOK, map[string]any{
			"status":   "healthy",
			"api":      "healthy",
			"database": db,
		})
	}
}

func APIv1Handler(w http.ResponseWriter, r *http.Request) {
	response.JSON(w, http.StatusOK, map[string]string{
		"message": "Berbagi Bites Jogja API v1",
		"version": "0.8.0",
	})
}
