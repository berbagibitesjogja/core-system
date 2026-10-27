package routes

import (
	"net/http"

	"github.com/berbagibitesjogja/core-system/internal/config"
	"github.com/berbagibitesjogja/core-system/internal/database"
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

	rateLimit := routerCfg.Config.AppConfig.RateLimit
	corsMiddleware := middleware.CORSWithConfig(routerCfg.Config.AppConfig.CORSOrigins, routerCfg.Config.AppConfig.CORSMaxAge)

	return corsMiddleware(middleware.SecurityHeaders(middleware.RateLimit(rateLimit)(middleware.RequestID(middleware.Logger(mux)))))
}

func HealthCheckHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		db := database.CheckHealth(r.Context(), pool)
		if db.Status != "healthy" {
			response.Error(w, http.StatusServiceUnavailable, "DB_UNHEALTHY", "Database is unhealthy", db)
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
