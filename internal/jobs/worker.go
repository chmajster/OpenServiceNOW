package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

type Store interface {
	Claim(context.Context, string, time.Duration) (Job, bool, error)
	Heartbeat(context.Context, uint64, string, time.Duration) (HeartbeatState, error)
	Complete(context.Context, uint64, string, json.RawMessage) error
	Fail(context.Context, uint64, string, error, time.Duration) error
	MarkCancelled(context.Context, uint64, string) error
}

type Handler func(context.Context, Job) (json.RawMessage, error)

type Worker struct {
	store Store
	logger *slog.Logger
	owner string
	concurrency int
	pollInterval time.Duration
	leaseDuration time.Duration
	retryDelay time.Duration
	handlers map[string]Handler
}

func NewWorker(
	store Store,
	logger *slog.Logger,
	owner string,
	concurrency int,
	pollInterval time.Duration,
	leaseDuration time.Duration,
	retryDelay time.Duration,
) (*Worker, error) {
	if store == nil || logger == nil || owner == "" ||
		concurrency < 1 || concurrency > 64 ||
		pollInterval < 50*time.Millisecond ||
		leaseDuration < 5*time.Second || leaseDuration > time.Hour ||
		retryDelay < 0 || retryDelay > 24*time.Hour {
		return nil, ErrInvalid
	}
	return &Worker{
		store: store,
		logger: logger,
		owner: owner,
		concurrency: concurrency,
		pollInterval: pollInterval,
		leaseDuration: leaseDuration,
		retryDelay: retryDelay,
		handlers: make(map[string]Handler),
	}, nil
}

func (w *Worker) Register(jobType string, handler Handler) error {
	if jobType == "" || len(jobType) > 120 || handler == nil {
		return ErrInvalid
	}
	if _, exists := w.handlers[jobType]; exists {
		return fmt.Errorf("job handler %q already registered", jobType)
	}
	w.handlers[jobType] = handler
	return nil
}

func (w *Worker) Run(ctx context.Context) error {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	errCh := make(chan error, w.concurrency)
	for index := 0; index < w.concurrency; index++ {
		wg.Add(1)
		go func(slot int) {
			defer wg.Done()
			if err := w.runLoop(runCtx, fmt.Sprintf("%s/%d", w.owner, slot)); err != nil &&
				!errors.Is(err, context.Canceled) {
				select {
				case errCh <- err:
				case <-runCtx.Done():
				}
			}
		}(index + 1)
	}
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-ctx.Done():
		cancel()
		<-done
		return nil
	case err := <-errCh:
		cancel()
		<-done
		return err
	case <-done:
		return nil
	}
}

func (w *Worker) RunOnce(ctx context.Context) (bool, error) {
	return w.claimAndProcess(ctx, w.owner+"/once")
}

func (w *Worker) runLoop(ctx context.Context, owner string) error {
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
			processed, err := w.claimAndProcess(ctx, owner)
			if err != nil {
				w.logger.Error("worker iteration failed", "owner", owner, "error", err)
				timer.Reset(w.pollInterval)
				continue
			}
			if processed {
				timer.Reset(0)
			} else {
				timer.Reset(w.pollInterval)
			}
		}
	}
}

func (w *Worker) claimAndProcess(ctx context.Context, owner string) (bool, error) {
	job, ok, err := w.store.Claim(ctx, owner, w.leaseDuration)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, nil
	}
	handler, exists := w.handlers[job.Type]
	if !exists {
		runErr := fmt.Errorf("no handler registered for job type %q", job.Type)
		finalCtx, cancel := finalizeContext(ctx)
		defer cancel()
		if failErr := w.store.Fail(finalCtx, job.ID, owner, runErr, w.retryDelay); failErr != nil {
			return true, errors.Join(runErr, failErr)
		}
		return true, nil
	}
	w.process(ctx, owner, job, handler)
	return true, nil
}

func (w *Worker) process(ctx context.Context, owner string, job Job, handler Handler) {
	jobCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var cancelRequested atomic.Bool
	var leaseLost atomic.Bool
	heartbeatDone := make(chan struct{})
	go func() {
		defer close(heartbeatDone)
		interval := w.leaseDuration / 3
		if interval < time.Second {
			interval = time.Second
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-jobCtx.Done():
				return
			case <-ticker.C:
				state, err := w.store.Heartbeat(jobCtx, job.ID, owner, w.leaseDuration)
				if err != nil {
					w.logger.Error("job heartbeat failed", "job_id", job.ID, "error", err)
					leaseLost.Store(true)
					cancel()
					return
				}
				if !state.LeaseValid {
					leaseLost.Store(true)
					cancel()
					return
				}
				if state.CancelRequested {
					cancelRequested.Store(true)
					cancel()
					return
				}
			}
		}
	}()

	result, runErr := handler(jobCtx, job)
	cancel()
	<-heartbeatDone

	if leaseLost.Load() {
		w.logger.Warn("job lease lost", "job_id", job.ID, "job_type", job.Type)
		return
	}

	finalCtx, finalCancel := finalizeContext(ctx)
	defer finalCancel()

	if cancelRequested.Load() {
		if err := w.store.MarkCancelled(finalCtx, job.ID, owner); err != nil &&
			!errors.Is(err, ErrLeaseLost) {
			w.logger.Error("mark cancelled job failed", "job_id", job.ID, "error", err)
		}
		return
	}
	if runErr != nil {
		if err := w.store.Fail(finalCtx, job.ID, owner, runErr, w.retryDelay); err != nil &&
			!errors.Is(err, ErrLeaseLost) {
			w.logger.Error("fail job update failed", "job_id", job.ID, "error", err)
		}
		return
	}
	if len(result) == 0 {
		result = json.RawMessage(`{}`)
	}
	if !json.Valid(result) {
		runErr = errors.New("job handler returned invalid JSON result")
		if err := w.store.Fail(finalCtx, job.ID, owner, runErr, w.retryDelay); err != nil &&
			!errors.Is(err, ErrLeaseLost) {
			w.logger.Error("invalid result failure update failed", "job_id", job.ID, "error", err)
		}
		return
	}
	if err := w.store.Complete(finalCtx, job.ID, owner, result); err != nil &&
		!errors.Is(err, ErrLeaseLost) {
		w.logger.Error("complete job update failed", "job_id", job.ID, "error", err)
	}
}

func finalizeContext(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(parent), 5*time.Second)
}
