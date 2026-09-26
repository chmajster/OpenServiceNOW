package events

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"
)

type fakeStore struct {
	event Event
	claimed bool
	completed bool
	failed error
}

func (f *fakeStore) Claim(context.Context, string, time.Duration) (Event, bool, error) {
	if f.claimed {
		return Event{}, false, nil
	}
	f.claimed = true
	return f.event, true, nil
}

func (f *fakeStore) Heartbeat(context.Context, uint64, string, time.Duration) (HeartbeatState, error) {
	return HeartbeatState{LeaseValid: true}, nil
}

func (f *fakeStore) Complete(context.Context, uint64, string) error {
	f.completed = true
	return nil
}

func (f *fakeStore) Fail(_ context.Context, _ uint64, _ string, err error, _ time.Duration) error {
	f.failed = err
	return nil
}

func newTestDispatcher(t *testing.T, store Store) *Dispatcher {
	t.Helper()
	dispatcher, err := NewDispatcher(
		store,
		slog.New(slog.NewJSONHandler(io.Discard, nil)),
		"test-dispatcher",
		1,
		50*time.Millisecond,
		5*time.Second,
		time.Second,
	)
	if err != nil {
		t.Fatalf("NewDispatcher() error = %v", err)
	}
	return dispatcher
}

func TestDispatcherDeliversRegisteredEvent(t *testing.T) {
	store := &fakeStore{event: Event{ID: 1, Type: "incident.created"}}
	dispatcher := newTestDispatcher(t, store)
	called := false
	if err := dispatcher.Register("incident.created", func(context.Context, Event) error {
		called = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	processed, err := dispatcher.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if !processed || !called || !store.completed {
		t.Fatalf("processed=%v called=%v completed=%v", processed, called, store.completed)
	}
}

func TestDispatcherFailsUnknownEventType(t *testing.T) {
	store := &fakeStore{event: Event{ID: 2, Type: "unknown"}}
	dispatcher := newTestDispatcher(t, store)
	processed, err := dispatcher.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if !processed || store.failed == nil {
		t.Fatalf("processed=%v failed=%v", processed, store.failed)
	}
}

func TestDispatcherPropagatesHandlerFailureToStore(t *testing.T) {
	expected := errors.New("downstream unavailable")
	store := &fakeStore{event: Event{ID: 3, Type: "test.failed"}}
	dispatcher := newTestDispatcher(t, store)
	if err := dispatcher.Register("test.failed", func(context.Context, Event) error { return expected }); err != nil {
		t.Fatal(err)
	}
	if _, err := dispatcher.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if !errors.Is(store.failed, expected) {
		t.Fatalf("store failure = %v", store.failed)
	}
}
