package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/chmajster/OpenServiceNOW/internal/platform/cache"
	"github.com/chmajster/OpenServiceNOW/internal/platform/config"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/platform/httpserver"
	"github.com/chmajster/OpenServiceNOW/internal/platform/logging"
	"github.com/chmajster/OpenServiceNOW/internal/platform/migrations"
)

func main() {
	if err := run(); err != nil {
		slog.Error("API terminated", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	logger, err := logging.New(os.Stdout, cfg.LogLevel)
	if err != nil {
		return err
	}
	slog.SetDefault(logger)

	rootCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	db, err := database.Open(rootCtx, cfg.MySQL)
	if err != nil {
		return err
	}
	defer db.Close()

	redisClient, err := cache.Open(rootCtx, cfg.Redis)
	if err != nil {
		return err
	}
	defer redisClient.Close()

	router := httpserver.NewRouter(
		logger,
		cfg.HTTP.BodyLimitBytes,
		cfg.HTTP.HealthTimeout,
		database.HealthChecker{DB: db},
		cache.HealthChecker{Client: redisClient},
		migrations.HealthChecker{DB: db, Dir: cfg.MigrationsDir},
	)
	server := httpserver.NewServer(
		cfg.HTTP.Addr,
		router,
		logger,
		cfg.HTTP.ReadHeaderTimeout,
		cfg.HTTP.ReadTimeout,
		cfg.HTTP.WriteTimeout,
		cfg.HTTP.IdleTimeout,
		cfg.HTTP.ShutdownTimeout,
	)

	return server.Run(rootCtx)
}
