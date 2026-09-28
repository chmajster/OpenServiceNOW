package identity

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeSessionCleanupStore struct {
	revokedBefore time.Time
	limit         int
	affected      int64
	err           error
}

func (f *fakeSessionCleanupStore) CleanupSessions(
	_ context.Context,
	revokedBefore time.Time,
	limit int,
) (int64, error) {
	f.revokedBefore = revokedBefore
	f.limit = limit
	return f.affected, f.err
}

func TestSessionCleanupRunOnce(t *testing.T) {
	store := &fakeSessionCleanupStore{affected: 7}
	cleanup := NewSessionCleanup(store, 500, 24*time.Hour)
	now := time.Date(2026, 9, 23, 1, 0, 0, 0, time.UTC)
	cleanup.now = func() time.Time { return now }

	affected, err := cleanup.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if affected != 7 {
		t.Fatalf("affected = %d", affected)
	}
	if store.limit != 500 {
		t.Fatalf("limit = %d", store.limit)
	}
	expected := now.Add(-24 * time.Hour)
	if !store.revokedBefore.Equal(expected) {
		t.Fatalf("revokedBefore = %s want %s", store.revokedBefore, expected)
	}
}

func TestSessionCleanupPropagatesStoreError(t *testing.T) {
	expected := errors.New("database down")
	store := &fakeSessionCleanupStore{err: expected}
	cleanup := NewSessionCleanup(store, 100, time.Hour)

	if _, err := cleanup.RunOnce(context.Background()); !errors.Is(err, expected) {
		t.Fatalf("RunOnce() error = %v", err)
	}
}
