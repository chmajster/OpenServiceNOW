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

	"encoding/json"
	"github.com/chmajster/OpenServiceNOW/internal/activity"
	"github.com/chmajster/OpenServiceNOW/internal/attachments"
	"github.com/chmajster/OpenServiceNOW/internal/audit"
	"github.com/chmajster/OpenServiceNOW/internal/auditapi"
	"github.com/chmajster/OpenServiceNOW/internal/catalog"
	"github.com/chmajster/OpenServiceNOW/internal/cmdb"
	"github.com/chmajster/OpenServiceNOW/internal/events"
	"github.com/chmajster/OpenServiceNOW/internal/identity"
	"github.com/chmajster/OpenServiceNOW/internal/incidents"
	"github.com/chmajster/OpenServiceNOW/internal/notifications"
	"github.com/chmajster/OpenServiceNOW/internal/platform/cache"
	"github.com/chmajster/OpenServiceNOW/internal/platform/config"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/platform/health"
	"github.com/chmajster/OpenServiceNOW/internal/platform/httpserver"
	"github.com/chmajster/OpenServiceNOW/internal/platform/ratelimit"
	"github.com/chmajster/OpenServiceNOW/internal/platform/requestinfo"
	"github.com/chmajster/OpenServiceNOW/internal/rbac"
	"github.com/chmajster/OpenServiceNOW/internal/recordlists"
	"github.com/chmajster/OpenServiceNOW/internal/recordviews"
	"github.com/chmajster/OpenServiceNOW/internal/requests"
	"github.com/chmajster/OpenServiceNOW/internal/sla"
	"github.com/chmajster/OpenServiceNOW/internal/tenants"
	"github.com/chmajster/OpenServiceNOW/internal/users"
	"github.com/chmajster/OpenServiceNOW/internal/workflow"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
	"github.com/chmajster/OpenServiceNOW/internal/workspace"
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

	outboxRepo := events.NewRepository(db)
	incidentService := incidents.NewService(incidents.NewRepository(db), txManager,
		workrecords.NewAllocator(txManager), workrecords.NewService(workrecords.NewRepository(db), txManager, outboxRepo),
		auditRepo, outboxRepo, activity.NewRepository(db))
	storage, err := attachments.NewLocalStorage(cfg.AttachmentRoot)
	if err != nil {
		logger.Error("attachment storage unavailable", "error", err)
		os.Exit(1)
	}
	files, err := attachments.NewEventingService(attachments.NewRepository(db), storage, attachments.MaxFileBytes, txManager, outboxRepo)
	if err != nil {
		logger.Error("attachment service unavailable", "error", err)
		os.Exit(1)
	}
	incidentService.WithAttachments(files)
	incidentHandler := incidents.NewHandler(incidentService, identityService)
	requestService := requests.NewService(db, files)
	requestHandler := requests.NewHandler(requestService, catalog.NewService(db), identityService)
	cmdbService := cmdb.NewService(db, files, map[string]cmdb.RecordDomain{"incidents": incidents.NewCMDBDomain(incidentService), "requests": requests.NewCMDBDomain(requestService)})
	cmdbHandler := cmdb.NewHandler(cmdbService, identityService)
	workflowService := workflow.NewService(db, requestService)
	workflowHandler := workflow.NewHandler(workflowService, identityService)
	requestViewDomain := func(tasks bool) recordviews.Domain {
		return recordviews.Domain{Read: func(a workrecords.Principal) bool {
			return a.Has("request.manage") || a.Has("request.fulfill") || (!tasks && a.Has("request.read_own"))
		}, Validate: func(a workrecords.Principal, raw json.RawMessage) error {
			_, err := recordlists.ValidateConfig(raw, requests.ListSchema(a.UserID, tasks), requests.ListSorts, requests.ListGroups(tasks), requests.ListColumns(tasks))
			return err
		}}
	}
	viewHandler := &recordviews.Handler{Permissions: identityService, Service: &recordviews.Service{DB: db, Domains: map[string]recordviews.Domain{
		"requests": requestViewDomain(false), "fulfillment": requestViewDomain(true),
		"cmdb": {Read: func(a workrecords.Principal) bool { return a.Has("cmdb.read") || a.Has("cmdb.read_own") }, Validate: func(a workrecords.Principal, raw json.RawMessage) error {
			_, err := recordlists.ValidateConfig(raw, cmdb.ListSchema(), cmdb.ListSorts, cmdb.ListGroups, cmdb.ListColumns)
			return err
		}},
		"incidents": {Read: func(a workrecords.Principal) bool { return a.Has("incident.read") || a.Has("incident.read_own") }, Validate: func(a workrecords.Principal, raw json.RawMessage) error {
			_, err := recordlists.ValidateConfig(raw, incidents.ListSchema(a.UserID), incidents.ListSorts, incidents.ListGroups, incidents.ListColumns)
			return err
		}},
	}}}
	workspaceHandler := &workspace.Handler{Service: &workspace.Service{Incidents: incidentService, Requests: requestService, Workflow: workflowService}, Permissions: identityService}
	notificationHandler := notifications.NewHandler(notifications.NewService(db, cfg.NotificationKey, cfg.SMTPAllowInsecure), outboxRepo)

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
			func(r chi.Router) {
				incidents.RegisterRoutes(r, incidentHandler, identityMiddleware)
			},
			func(r chi.Router) { notifications.RegisterRoutes(r, notificationHandler, identityMiddleware) },
			func(r chi.Router) { requests.RegisterRoutes(r, requestHandler, identityMiddleware) },
			func(r chi.Router) { workflow.RegisterRoutes(r, workflowHandler, identityMiddleware) },
			func(r chi.Router) { workspace.RegisterRoutes(r, workspaceHandler, identityMiddleware) },
			func(r chi.Router) { cmdb.RegisterRoutes(r, cmdbHandler, identityMiddleware) },
			func(r chi.Router) { recordviews.RegisterRoutes(r, viewHandler, identityMiddleware) },
			func(r chi.Router) {
				sla.RegisterRoutes(r, sla.NewHandler(sla.NewService(db), identityService), identityMiddleware)
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
