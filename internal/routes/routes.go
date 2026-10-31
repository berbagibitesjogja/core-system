package routes

import (
	"net/http"

	"github.com/berbagibitesjogja/core-system/internal/config"
	"github.com/berbagibitesjogja/core-system/internal/database"
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

	// Auth and Mailer handlers
	m := mailer.New(routerCfg.Config.SMTP)
	authHandler := handler.NewAuthHandler(routerCfg.Pool, routerCfg.Config)
	invitationHandler := handler.NewInvitationHandler(routerCfg.Pool, routerCfg.Config, m)

	authMiddleware := middleware.Authenticate(routerCfg.Config.AppConfig.JWTSecret)

	// Public Auth Endpoints
	mux.HandleFunc("POST /api/v1/auth/register", authHandler.RegisterHandler)
	mux.HandleFunc("POST /api/v1/auth/login", authHandler.LoginHandler)
	mux.HandleFunc("POST /api/v1/auth/refresh", authHandler.RefreshTokenHandler)

	// Authenticated Auth Endpoints
	mux.Handle("POST /api/v1/auth/logout", authMiddleware(http.HandlerFunc(authHandler.LogoutHandler)))

	// Invitation Endpoints
	mux.HandleFunc("GET /api/v1/invitations/validate", invitationHandler.ValidateInvitationHandler)
	mux.Handle("POST /api/v1/invitations", authMiddleware(http.HandlerFunc(invitationHandler.CreateInvitationHandler)))

	rateLimit := routerCfg.Config.AppConfig.RateLimit
	corsMiddleware := middleware.CORSWithConfig(routerCfg.Config.AppConfig.CORSOrigins, routerCfg.Config.AppConfig.CORSMaxAge)

	// Reordered: RequestID and Logger wrap RateLimit so that 429 rejections are assigned ref_id and logged in slog
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
		"version": "0.1.0",
	})
}
