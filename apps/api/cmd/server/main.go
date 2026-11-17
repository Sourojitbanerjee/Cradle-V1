package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/cradle-dev/cradle/apps/api/internal/handlers"
	appMiddleware "github.com/cradle-dev/cradle/apps/api/internal/middleware"
	"github.com/cradle-dev/cradle/pkg/storage/postgres"
	"github.com/cradle-dev/cradle/pkg/telemetry"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
)

const AppVersion = "0.1.0-alpha"

func main() {
	logger := telemetry.NewLogger(os.Stdout, slog.LevelInfo)
	slog.SetDefault(logger)

	port := getEnv("PORT", "8080")
	dbURL := os.Getenv("DATABASE_URL")
	cradleAWSAccount := getEnv("CRADLE_AWS_ACCOUNT_ID", "000000000000")

	logger.Info("Starting Cradle Control Plane API",
		slog.String("version", AppVersion),
		slog.String("port", port),
	)

	var pool *pgxpool.Pool
	if dbURL != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		var err error
		pool, err = postgres.NewPool(ctx, postgres.Config{URL: dbURL})
		cancel()
		if err != nil {
			logger.Error("Failed to connect to PostgreSQL database", slog.String("error", err.Error()))
			os.Exit(1)
		}
		defer pool.Close()
		logger.Info("Successfully established PostgreSQL connection pool")

		// Run pending embedded schema migrations automatically
		migrator := postgres.NewMigrator(pool)
		if err := migrator.Up(context.Background()); err != nil {
			logger.Error("Database migration failed", slog.String("error", err.Error()))
			os.Exit(1)
		}
		logger.Info("Database migrations verified and up-to-date")
	} else {
		logger.Warn("DATABASE_URL not set; running in memory/mock mode without database connection")
	}

	r := setupRouter(pool, logger, cradleAWSAccount)

	server := &http.Server{
		Addr:         ":" + port,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Server run context with graceful shutdown
	serverCtx, serverStopCtx := context.WithCancel(context.Background())

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)

	go func() {
		<-sig
		logger.Info("Shutdown signal received; gracefully terminating connections...")

		shutdownCtx, shutdownCancel := context.WithTimeout(serverCtx, 15*time.Second)
		defer shutdownCancel()

		go func() {
			<-shutdownCtx.Done()
			if errors.Is(shutdownCtx.Err(), context.DeadlineExceeded) {
				logger.Error("Graceful shutdown timed out; forcing exit")
				os.Exit(1)
			}
		}()

		if err := server.Shutdown(shutdownCtx); err != nil {
			logger.Error("Server shutdown failed", slog.String("error", err.Error()))
		}
		serverStopCtx()
	}()

	logger.Info(fmt.Sprintf("Cradle API Server listening on http://localhost:%s", port))
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("HTTP Server encountered fatal error", slog.String("error", err.Error()))
		os.Exit(1)
	}

	<-serverCtx.Done()
	logger.Info("Cradle API Server shutdown complete")
}

func setupRouter(pool *pgxpool.Pool, logger *slog.Logger, cradleAWSAccount string) http.Handler {
	r := chi.NewRouter()

	// Base Middleware
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(appMiddleware.RequestLogger(logger))
	r.Use(middleware.Recoverer)

	// Health Endpoints
	healthHandler := handlers.NewHealthHandler(pool, AppVersion)
	r.Get("/healthz", healthHandler.Liveness)
	r.Get("/readyz", healthHandler.Readiness)

	// API v1 Routes
	accountHandler := handlers.NewAccountHandler(pool, cradleAWSAccount)
	resourceHandler := handlers.NewResourceHandler(pool)

	r.Route("/api/v1", func(api chi.Router) {
		api.Use(appMiddleware.TenantMiddleware)

		// Cloud Accounts Management
		api.Route("/accounts", func(acc chi.Router) {
			acc.Get("/external-id", accountHandler.GenerateExternalID)
			acc.Post("/", accountHandler.RegisterAccount)
		})

		// Canonical Resources & Dependency Graph
		api.Route("/resources", func(res chi.Router) {
			res.Get("/", resourceHandler.ListResources)
			res.Get("/{id}/graph", resourceHandler.GetResourceGraph)
		})
	})

	return r
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}
