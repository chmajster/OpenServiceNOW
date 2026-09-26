package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"
)

type fakeStore struct {
	job Job
	claimed bool
	completeResult json.RawMessage
	failed error
	cancelled bool
	heartbeat HeartbeatState
}

func (f *fakeStore) Claim(context.Context, string, time.Duration) (Job, bool, error) {
	if f.claimed {
		return Job{}, false, nil
	}
	f.claimed = true
	return f.job, true, nil
}

func (f *fakeStore) Heartbeat(context.Context, uint64, string, time.Duration) (HeartbeatState, error) {
	if !f.heartbeat.LeaseValid && !f.heartbeat.CancelRequested {
		return HeartbeatState{LeaseValid: true}, nil
	}
	return f.heartbeat, nil
}

func (f *fakeStore) Complete(_ context.Context, _ uint64, _ string, result json.RawMessage) error {
	f.completeResult = append(json.RawMessage(nil), result...)
	return nil
}

func (f *fakeStore) Fail(_ context.Context, _ uint64, _ string, err error, _ time.Duration) error {
	f.failed = err
	return nil
}

func (f *fakeStore) MarkCancelled(context.Context, uint64, string) error {
	f.cancelled = true
	return nil
}

func newTestWorker(t *testing.T, store Store) *Worker {
	t.Helper()
	worker, err := NewWorker(
		store,
		slog.New(slog.NewJSONHandler(io.Discard, nil)),
		"test-worker",
		1,
		50*time.Millisecond,
		5*time.Second,
		time.Second,
	)
	if err != nil {
		t.Fatalf("NewWorker() error = %v", err)
	}
	return worker
}

func TestWorkerRunOnceCompletesJob(t *testing.T) {
	store := &fakeStore{job: Job{ID: 7, Type: "test.ok"}}
	worker := newTestWorker(t, store)
	if err := worker.Register("test.ok", func(context.Context, Job) (json.RawMessage, error) {
		return json.RawMessage(`{"ok":true}`), nil
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	processed, err := worker.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if !processed {
		t.Fatal("expected a processed job")
	}
	if string(store.completeResult) != `{"ok":true}` {
		t.Fatalf("complete result = %s", store.completeResult)
	}
	if store.failed != nil {
		t.Fatalf("unexpected failure: %v", store.failed)
	}
}

func TestWorkerRunOnceFailsUnknownJobType(t *testing.T) {
	store := &fakeStore{job: Job{ID: 8, Type: "unknown"}}
	worker := newTestWorker(t, store)
	processed, err := worker.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if !processed {
		t.Fatal("expected a processed job")
	}
	if store.failed == nil {
		t.Fatal("unknown job type must fail durably")
	}
}

func TestWorkerRejectsDuplicateHandler(t *testing.T) {
	store := &fakeStore{}
	worker := newTestWorker(t, store)
	handler := func(context.Context, Job) (json.RawMessage, error) { return nil, errors.New("x") }
	if err := worker.Register("duplicate", handler); err != nil {
		t.Fatalf("first Register() error = %v", err)
	}
	if err := worker.Register("duplicate", handler); err == nil {
		t.Fatal("duplicate handler registration must fail")
	}
}
