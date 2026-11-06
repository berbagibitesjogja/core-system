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
	invitationHandler := handler.NewInvitationHandler(routerCfg.Pool, routerCfg.Config, m)
	oauthHandler := handler.NewOAuthHandler(routerCfg.Pool, routerCfg.Config, queries)
	sponsorHandler := handler.NewSponsorHandler(routerCfg.Pool, routerCfg.Config)
	foodItemHandler := handler.NewFoodItemHandler(routerCfg.Pool, routerCfg.Config)
	beneficiaryCategoryHandler := handler.NewBeneficiaryCategoryHandler(routerCfg.Pool, routerCfg.Config)
	reimbursementHandler := handler.NewReimbursementHandler(routerCfg.Pool, routerCfg.Config)

	authMiddleware := middleware.Authenticate(routerCfg.Config.AppConfig.JWTSecret)
	optionalAuth := middleware.OptionalAuthenticate(routerCfg.Config.AppConfig.JWTSecret)

	// Public Auth Endpoints
	mux.HandleFunc("POST /api/v1/auth/register", authHandler.RegisterHandler)
	mux.HandleFunc("POST /api/v1/auth/login", authHandler.LoginHandler)
	mux.HandleFunc("POST /api/v1/auth/refresh", authHandler.RefreshTokenHandler)

	// Google OAuth Endpoints
	mux.HandleFunc("GET /api/v1/auth/google/login", oauthHandler.GoogleLoginHandler)
	mux.HandleFunc("GET /api/v1/auth/google/callback", oauthHandler.GoogleCallbackHandler)

	// Authenticated Auth Endpoints
	mux.Handle("POST /api/v1/auth/logout", authMiddleware(http.HandlerFunc(authHandler.LogoutHandler)))

	// Invitation Endpoints
	mux.HandleFunc("GET /api/v1/invitations/validate", invitationHandler.ValidateInvitationHandler)
	mux.Handle("POST /api/v1/invitations", authMiddleware(http.HandlerFunc(invitationHandler.CreateInvitationHandler)))

	// Sponsor Endpoints
	mux.Handle("GET /api/v1/sponsors", optionalAuth(http.HandlerFunc(sponsorHandler.ListSponsorsHandler)))
	mux.Handle("GET /api/v1/sponsors/{id}", optionalAuth(http.HandlerFunc(sponsorHandler.GetSponsorByIDHandler)))
	mux.Handle("POST /api/v1/sponsors", authMiddleware(http.HandlerFunc(sponsorHandler.CreateSponsorHandler)))
	mux.Handle("PATCH /api/v1/sponsors/{id}", authMiddleware(http.HandlerFunc(sponsorHandler.UpdateSponsorHandler)))
	mux.Handle("DELETE /api/v1/sponsors/{id}", authMiddleware(http.HandlerFunc(sponsorHandler.DeleteSponsorHandler)))

	// Food Item Endpoints
	mux.HandleFunc("GET /api/v1/food-items", foodItemHandler.ListFoodItemsHandler)
	mux.HandleFunc("GET /api/v1/food-items/{id}", foodItemHandler.GetFoodItemByIDHandler)
	mux.Handle("POST /api/v1/food-items", authMiddleware(http.HandlerFunc(foodItemHandler.CreateFoodItemHandler)))
	mux.Handle("PATCH /api/v1/food-items/{id}", authMiddleware(http.HandlerFunc(foodItemHandler.UpdateFoodItemHandler)))
	mux.Handle("DELETE /api/v1/food-items/{id}", authMiddleware(http.HandlerFunc(foodItemHandler.DeleteFoodItemHandler)))

	// Beneficiary Category Endpoints
	mux.Handle("GET /api/v1/beneficiary-categories", optionalAuth(http.HandlerFunc(beneficiaryCategoryHandler.ListBeneficiaryCategoriesHandler)))
	mux.Handle("GET /api/v1/beneficiary-categories/{id}", optionalAuth(http.HandlerFunc(beneficiaryCategoryHandler.GetBeneficiaryCategoryByIDHandler)))
	mux.Handle("POST /api/v1/beneficiary-categories", authMiddleware(http.HandlerFunc(beneficiaryCategoryHandler.CreateBeneficiaryCategoryHandler)))
	mux.Handle("PATCH /api/v1/beneficiary-categories/{id}", authMiddleware(http.HandlerFunc(beneficiaryCategoryHandler.UpdateBeneficiaryCategoryHandler)))
	mux.Handle("DELETE /api/v1/beneficiary-categories/{id}", authMiddleware(http.HandlerFunc(beneficiaryCategoryHandler.DeleteBeneficiaryCategoryHandler)))

	// Reimbursement Endpoints
	mux.Handle("POST /api/v1/reimbursements", authMiddleware(http.HandlerFunc(reimbursementHandler.CreateReimbursementHandler)))
	mux.Handle("GET /api/v1/reimbursements", authMiddleware(http.HandlerFunc(reimbursementHandler.ListReimbursementsHandler)))
	mux.Handle("GET /api/v1/reimbursements/{id}", authMiddleware(http.HandlerFunc(reimbursementHandler.GetReimbursementByIDHandler)))
	mux.Handle("PATCH /api/v1/reimbursements/{id}", authMiddleware(http.HandlerFunc(reimbursementHandler.UpdateReimbursementHandler)))
	mux.Handle("DELETE /api/v1/reimbursements/{id}", authMiddleware(http.HandlerFunc(reimbursementHandler.DeleteReimbursementHandler)))

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
		"version": "0.5.0",
	})
}
