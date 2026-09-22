package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/chmajster/OpenServiceNOW/internal/audit"
	"github.com/chmajster/OpenServiceNOW/internal/identity"
	"github.com/chmajster/OpenServiceNOW/internal/platform/cache"
	"github.com/chmajster/OpenServiceNOW/internal/platform/config"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/platform/health"
	"github.com/chmajster/OpenServiceNOW/internal/platform/httpserver"
	"github.com/chmajster/OpenServiceNOW/internal/platform/ratelimit"
	"github.com/go-chi/chi/v5"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := config.Load()
	if err != nil {
		logger.Error("configuration invalid", "error", err)
		os.Exit(1)
	}

	rootCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	db, err := database.OpenMySQL(rootCtx, cfg.MySQL)
	if err != nil {
		logger.Error("mysql unavailable", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	redisClient, err := cache.OpenRedis(rootCtx, cfg.Redis)
	if err != nil {
		logger.Error("redis unavailable", "error", err)
		os.Exit(1)
	}
	defer redisClient.Close()

	txManager := database.NewTxManager(db)
	auditRepo := audit.NewMySQLRepository()
	identityRepo := identity.NewMySQLRepository(db)
	identityService, err := identity.NewService(identityRepo, txManager, auditRepo, cfg.Session)
	if err != nil {
		logger.Error("identity service initialization failed", "error", err)
		os.Exit(1)
	}
	identityMiddleware := identity.NewMiddleware(identityService, cfg.Session)
	identityHandler := identity.NewHandler(identityService, ratelimit.New(redisClient, "opensnow:"), cfg.Session, cfg.Auth)

	healthHandler := health.New(
		func(ctx context.Context) error {
			if err := database.Ping(ctx, db, cfg.MySQL.PingTimeout); err != nil {
				return err
			}
			return database.SchemaReady(ctx, db, cfg.MySQL.PingTimeout)
		},
		func(ctx context.Context) error { return cache.Ping(ctx, redisClient, cfg.Redis.PingTimeout) },
	)
	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           httpserver.Router(logger, healthHandler, func(r chi.Router) {
			identity.RegisterRoutes(r, identityHandler, identityMiddleware)
		}),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("http server starting", "addr", cfg.HTTPAddr, "env", cfg.AppEnv)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case <-rootCtx.Done():
		logger.Info("shutdown signal received")
	case err := <-errCh:
		if err != nil {
			logger.Error("http server failed", "error", err)
			os.Exit(1)
		}
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
		os.Exit(1)
	}
	logger.Info("shutdown complete")
}
