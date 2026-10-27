package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/berbagibitesjogja/core-system/internal/config"
	"github.com/berbagibitesjogja/core-system/internal/database"
	"github.com/berbagibitesjogja/core-system/internal/routes"
)

func main() {

	// Config
	cfg := config.LoadConfig()

	// Logger
	var logHandler slog.Handler
	logOptions := &slog.HandlerOptions{Level: cfg.Log.Level}
	if cfg.Log.Format == "text" {
		logHandler = slog.NewTextHandler(os.Stdout, logOptions)
	} else {
		logHandler = slog.NewJSONHandler(os.Stdout, logOptions)
	}
	logger := slog.New(logHandler)
	slog.SetDefault(logger)

	// DB Pool
	dbCtx, cancelDB := context.WithTimeout(context.Background(), cfg.DBConfig.ConnectTimeout)
	defer cancelDB()
	pool, err := database.NewPool(dbCtx, &cfg.DBConfig)
	if err != nil {
		logger.Error("Error connecting to database", "error", err)
		os.Exit(1)
	}
	defer func() {
		logger.Info("Closing database connection pool")
		pool.Close()
	}()
	logger.Info("Connected to database")

	// Hanlder
	router := routes.NewRouter(routes.RouterConfig{
		Pool:   pool,
		Config: &cfg,
	})

	// Server
	buildAddr := ":" + cfg.AppConfig.Port
	server := &http.Server{
		Addr:         buildAddr,
		Handler:      router,
		IdleTimeout:  cfg.AppConfig.IdleTimeout,
		ReadTimeout:  cfg.AppConfig.ReadTimeout,
		WriteTimeout: cfg.AppConfig.WriteTimeout,
	}

	// Graceful Shutdown
	shutdownError := make(chan error)

	go func() {
		quit := make(chan os.Signal, 1)
		signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
		sig := <-quit

		logger.Info("Shutting down server", "signal", sig.String())

		ctx, cancel := context.WithTimeout(context.Background(), cfg.AppConfig.ShutdownTimeout)
		defer cancel()

		shutdownError <- server.Shutdown(ctx)
	}()

	logger.Info("Starting server", "addr", server.Addr)

	err = server.ListenAndServe()
	if !errors.Is(err, http.ErrServerClosed) {
		logger.Error("Server failed to start", "error", err)
		os.Exit(1)
	}

	err = <-shutdownError
	if err != nil {
		logger.Error("Error during server shutdown", "error", err)
		os.Exit(1)
	}

	logger.Info("Server stopped")
}
