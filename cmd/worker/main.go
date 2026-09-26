package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/chmajster/OpenServiceNOW/internal/identity"
	"github.com/chmajster/OpenServiceNOW/internal/jobs"
	"github.com/chmajster/OpenServiceNOW/internal/platform/config"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/systemjobs"
)

func main() {
	once := flag.Bool("once", false, "claim and process at most one durable job")
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

	repo := jobs.NewRepository(db)
	worker, err := jobs.NewWorker(
		repo,
		logger,
		workerIdentity(),
		cfg.Worker.Concurrency,
		cfg.Worker.PollInterval,
		cfg.Worker.LeaseDuration,
		cfg.Worker.RetryDelay,
	)
	if err != nil {
		logger.Error("worker configuration invalid", "error", err)
		os.Exit(1)
	}
	if err := worker.Register(
		systemjobs.SessionCleanupType,
		systemjobs.SessionCleanupHandler(identity.NewMySQLRepository(db)),
	); err != nil {
		logger.Error("register session cleanup job handler", "error", err)
		os.Exit(1)
	}

	if *once {
		processed, err := worker.RunOnce(rootCtx)
		if err != nil {
			logger.Error("worker one-shot failed", "error", err)
			os.Exit(1)
		}
		logger.Info("worker one-shot complete", "processed", processed)
		return
	}

	logger.Info("worker started", "concurrency", cfg.Worker.Concurrency)
	if err := worker.Run(rootCtx); err != nil {
		logger.Error("worker failed", "error", err)
		os.Exit(1)
	}
	logger.Info("worker shutdown complete")
}

func workerIdentity() string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "unknown-host"
	}
	var random [8]byte
	if _, err := rand.Read(random[:]); err == nil {
		return fmt.Sprintf("%s:%d:%s", host, os.Getpid(), hex.EncodeToString(random[:]))
	}
	return fmt.Sprintf("%s:%d:%d", host, os.Getpid(), time.Now().UnixNano())
}
