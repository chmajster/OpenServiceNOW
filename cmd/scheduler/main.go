package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/chmajster/OpenServiceNOW/internal/identity"
	"github.com/chmajster/OpenServiceNOW/internal/platform/config"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
)

func main() {
	once := flag.Bool("once", false, "run one scheduler iteration and exit")
	flag.Parse()

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

	cleanup := identity.NewSessionCleanup(
		identity.NewMySQLRepository(db),
		cfg.Session.CleanupBatch,
		cfg.Session.RevokedRetention,
	)

	lockName := sessionCleanupLockName(cfg.MySQL.Database)

	if *once {
		if err := runCleanupLocked(rootCtx, logger, db, lockName, cleanup); err != nil {
			os.Exit(1)
		}
		return
	}

	if err := runCleanupLocked(rootCtx, logger, db, lockName, cleanup); err != nil {
		logger.Error("initial session cleanup failed", "error", err)
	}

	ticker := time.NewTicker(cfg.Session.CleanupInterval)
	defer ticker.Stop()

	logger.Info("scheduler started", "session_cleanup_interval", cfg.Session.CleanupInterval.String())
	for {
		select {
		case <-rootCtx.Done():
			logger.Info("scheduler shutdown complete")
			return
		case <-ticker.C:
			if err := runCleanupLocked(rootCtx, logger, db, lockName, cleanup); err != nil {
				logger.Error("session cleanup failed", "error", err)
			}
		}
	}
}

func runCleanup(ctx context.Context, logger *slog.Logger, cleanup *identity.SessionCleanup) error {
	started := time.Now()
	affected, err := cleanup.RunOnce(ctx)
	if err != nil {
		logger.Error("session cleanup iteration failed",
			"duration_ms", time.Since(started).Milliseconds(),
			"error", err,
		)
		return err
	}
	logger.Info("session cleanup iteration completed",
		"deleted_sessions", affected,
		"duration_ms", time.Since(started).Milliseconds(),
	)
	return nil
}

func runCleanupLocked(
	ctx context.Context,
	logger *slog.Logger,
	db *sql.DB,
	lockName string,
	cleanup *identity.SessionCleanup,
) error {
	acquired, err := database.WithAdvisoryLock(ctx, db, lockName, func(lockCtx context.Context) error {
		return runCleanup(lockCtx, logger, cleanup)
	})
	if err != nil {
		return err
	}
	if !acquired {
		logger.Info("session cleanup skipped", "reason", "lock_not_acquired")
	}
	return nil
}

func sessionCleanupLockName(databaseName string) string {
	sum := sha256.Sum256([]byte(databaseName))
	return "opensnow:scheduler:session-cleanup:" + hex.EncodeToString(sum[:8])
}
