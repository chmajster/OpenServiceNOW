package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/chmajster/OpenServiceNOW/internal/attachments"
	"github.com/chmajster/OpenServiceNOW/internal/jobs"
	"github.com/chmajster/OpenServiceNOW/internal/platform/config"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/sla"
	"github.com/chmajster/OpenServiceNOW/internal/systemjobs"
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

	jobRepo := jobs.NewRepository(db)
	slaService := sla.NewService(db)
	run := func(ctx context.Context, now time.Time) error {
		bucket := now.UTC().Truncate(cfg.Session.CleanupInterval)
		key := "session-cleanup:" + bucket.Format("20060102T150405.000000000Z")
		job, created, err := systemjobs.EnqueueSessionCleanup(
			ctx,
			jobRepo,
			now,
			cfg.Session.RevokedRetention,
			cfg.Session.CleanupBatch,
			key,
		)
		if err != nil {
			return err
		}
		logger.Info(
			"scheduled durable job",
			"job_id", fmt.Sprint(job.ID),
			"job_type", job.Type,
			"created", created,
			"idempotency_key", key,
		)
		if _, _, err = attachments.EnqueueCleanup(ctx, jobRepo, now); err != nil {
			return err
		}
		_, err = slaService.Schedule(ctx, now)
		return err
	}

	if *once {
		if err := run(rootCtx, time.Now().UTC()); err != nil {
			logger.Error("scheduler iteration failed", "error", err)
			os.Exit(1)
		}
		return
	}

	if err := run(rootCtx, time.Now().UTC()); err != nil {
		logger.Error("initial scheduler iteration failed", "error", err)
	}

	// SLA deadlines are checked without a browser; cleanup keeps its own
	// idempotency bucket even when the scheduler wakes more frequently.
	interval := min(cfg.Session.CleanupInterval, 30*time.Second)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	logger.Info("scheduler started", "interval", interval.String())
	for {
		select {
		case <-rootCtx.Done():
			logger.Info("scheduler shutdown complete")
			return
		case now := <-ticker.C:
			if err := run(rootCtx, now.UTC()); err != nil {
				logger.Error("scheduler iteration failed", "error", err)
			}
		}
	}
}
