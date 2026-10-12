package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/chmajster/OpenServiceNOW/internal/activity"
	"github.com/chmajster/OpenServiceNOW/internal/attachments"
	"github.com/chmajster/OpenServiceNOW/internal/events"
	"github.com/chmajster/OpenServiceNOW/internal/identity"
	"github.com/chmajster/OpenServiceNOW/internal/incidents"
	"github.com/chmajster/OpenServiceNOW/internal/jobs"
	"github.com/chmajster/OpenServiceNOW/internal/notifications"
	"github.com/chmajster/OpenServiceNOW/internal/platform/config"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/sla"
	"github.com/chmajster/OpenServiceNOW/internal/systemjobs"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
)

func main() {
	once := flag.Bool("once", false, "claim and process at most one job and one outbox event")
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

	owner := workerIdentity()
	jobRepo := jobs.NewRepository(db)
	jobWorker, err := jobs.NewWorker(
		jobRepo,
		logger,
		owner+"/jobs",
		cfg.Worker.Concurrency,
		cfg.Worker.PollInterval,
		cfg.Worker.LeaseDuration,
		cfg.Worker.RetryDelay,
	)
	if err != nil {
		logger.Error("worker configuration invalid", "error", err)
		os.Exit(1)
	}
	if err := jobWorker.Register(
		systemjobs.SessionCleanupType,
		systemjobs.SessionCleanupHandler(identity.NewMySQLRepository(db)),
	); err != nil {
		logger.Error("register session cleanup job handler", "error", err)
		os.Exit(1)
	}

	eventRepo := events.NewRepository(db)
	dispatcher, err := events.NewDispatcher(
		eventRepo,
		logger,
		owner+"/events",
		cfg.Worker.Concurrency,
		cfg.Worker.PollInterval,
		cfg.Worker.LeaseDuration,
		cfg.Worker.RetryDelay,
	)
	if err != nil {
		logger.Error("event dispatcher configuration invalid", "error", err)
		os.Exit(1)
	}
	projector := activity.NewProjector(database.NewTxManager(db), activity.NewRepository(db))
	slaService := sla.NewService(db)
	if err := slaService.Register(dispatcher, projector); err != nil {
		logger.Error("register SLA consumer failed")
		os.Exit(1)
	}
	if err := jobWorker.Register(sla.JobType, slaService.JobHandler); err != nil {
		logger.Error("register SLA worker failed")
		os.Exit(1)
	}
	notificationService := notifications.NewService(db, cfg.NotificationKey, cfg.SMTPAllowInsecure)
	if err := notificationService.Register(dispatcher); err != nil {
		logger.Error("register notification consumer failed")
		os.Exit(1)
	}
	if err := jobWorker.Register(notifications.SMTPJobType, notificationService.MailHandler); err != nil {
		logger.Error("register SMTP worker failed")
		os.Exit(1)
	}
	if err := workrecords.RegisterActivityHandlers(dispatcher, projector); err != nil {
		logger.Error("register work activity handlers", "error", err)
		os.Exit(1)
	}
	storage, err := attachments.NewLocalStorage(cfg.AttachmentRoot)
	if err != nil {
		logger.Error("attachment storage unavailable", "error", err)
		os.Exit(1)
	}
	if err := jobWorker.Register(attachments.CleanupJobType, attachments.CleanupHandler(storage, attachments.NewRepository(db))); err != nil {
		logger.Error("register attachment cleanup", "error", err)
		os.Exit(1)
	}
	if err := incidents.RegisterEventHandlers(dispatcher, incidents.NewRepository(db), projector); err != nil {
		logger.Error("register incident activity handlers", "error", err)
		os.Exit(1)
	}

	if *once {
		jobProcessed, err := jobWorker.RunOnce(rootCtx)
		if err != nil {
			logger.Error("worker one-shot job failed", "error", err)
			os.Exit(1)
		}
		eventProcessed, err := dispatcher.RunOnce(rootCtx)
		if err != nil {
			logger.Error("worker one-shot event failed", "error", err)
			os.Exit(1)
		}
		logger.Info("worker one-shot complete", "job_processed", jobProcessed, "event_processed", eventProcessed)
		return
	}

	logger.Info("worker started", "concurrency", cfg.Worker.Concurrency)
	runCtx, cancel := context.WithCancel(rootCtx)
	defer cancel()
	errCh := make(chan error, 2)
	go func() { errCh <- jobWorker.Run(runCtx) }()
	go func() { errCh <- dispatcher.Run(runCtx) }()

	select {
	case <-rootCtx.Done():
		cancel()
		<-errCh
		<-errCh
		logger.Info("worker shutdown complete")
	case firstErr := <-errCh:
		if rootCtx.Err() != nil {
			cancel()
			<-errCh
			logger.Info("worker shutdown complete")
			return
		}
		cancel()
		secondErr := <-errCh
		if firstErr == nil {
			firstErr = errors.New("worker subsystem exited unexpectedly")
		}
		logger.Error("worker subsystem failed", "error", firstErr, "secondary_error", secondErr)
		os.Exit(1)
	}
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
