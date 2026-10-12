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
	"github.com/chmajster/OpenServiceNOW/internal/auditapi"
	"github.com/chmajster/OpenServiceNOW/internal/identity"
	"github.com/chmajster/OpenServiceNOW/internal/platform/cache"
	"github.com/chmajster/OpenServiceNOW/internal/platform/config"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/platform/health"
	"github.com/chmajster/OpenServiceNOW/internal/platform/httpserver"
	"github.com/chmajster/OpenServiceNOW/internal/platform/ratelimit"
	"github.com/chmajster/OpenServiceNOW/internal/platform/requestinfo"
	"github.com/chmajster/OpenServiceNOW/internal/rbac"
	"github.com/chmajster/OpenServiceNOW/internal/tenants"
	"github.com/chmajster/OpenServiceNOW/internal/users"
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

	clientIPResolver, err := requestinfo.NewResolver(cfg.TrustedProxyCIDRs)
	if err != nil {
		logger.Error("trusted proxy configuration invalid", "error", err)
		os.Exit(1)
	}

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
	adminSafety := identity.NewAdminSafetyRepository()
	auditReader := audit.NewReader(db)
	auditHandler := auditapi.NewHandler(auditReader)
	identityRepo := identity.NewMySQLRepository(db)
	identityService, err := identity.NewService(identityRepo, txManager, auditRepo, cfg.Session, cfg.Auth)
	if err != nil {
		logger.Error("identity service initialization failed", "error", err)
		os.Exit(1)
	}
	identityMiddleware := identity.NewMiddleware(identityService, cfg.Session)
	identityHandler := identity.NewHandler(identityService, ratelimit.New(redisClient, "opensnow:"), cfg.Session, cfg.Auth)
	setupHandler := identity.NewSetupHandler(
		identity.NewInstaller(identity.NewBootstrapper(identityRepo, txManager, auditRepo), identityService),
		ratelimit.New(redisClient, "opensnow:"), cfg.Session,
	)

	rbacRepo := rbac.NewMySQLRepository(db)
	rbacService := rbac.NewService(rbacRepo, txManager, auditRepo, adminSafety)
	rbacHandler := rbac.NewHandler(rbacService)

	usersRepo := users.NewMySQLRepository(db)
	usersService := users.NewService(usersRepo, txManager, auditRepo, adminSafety, identityRepo, identityService)
	usersHandler := users.NewHandler(usersService)

	tenantsRepo := tenants.NewMySQLRepository(db)
	tenantsService := tenants.NewService(tenantsRepo, txManager, auditRepo)
	tenantsHandler := tenants.NewHandler(tenantsService)

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
		Addr: cfg.HTTPAddr,
		Handler: httpserver.Router(
			logger,
			healthHandler,
			[]func(http.Handler) http.Handler{clientIPResolver.Middleware},
			func(r chi.Router) {
				identity.RegisterSetupRoutes(r, setupHandler)
				identity.RegisterRoutes(r, identityHandler, identityMiddleware, identity.NewAccountHandler(identityService, identityHandler))
			},
			func(r chi.Router) {
				rbac.RegisterRoutes(r, rbacHandler, identityMiddleware)
			},
			func(r chi.Router) {
				users.RegisterRoutes(r, usersHandler, identityMiddleware)
			},
			func(r chi.Router) {
				tenants.RegisterRoutes(r, tenantsHandler, identityMiddleware)
			},
			func(r chi.Router) {
				auditapi.RegisterRoutes(r, auditHandler, identityMiddleware)
			},
		),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      35 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    64 << 10,
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
