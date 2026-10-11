package systemjobs

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/chmajster/OpenServiceNOW/internal/jobs"
)

type fakeCleanupStore struct {
	before  time.Time
	batch   int
	deleted int64
	err     error
}

func (f *fakeCleanupStore) CleanupSessions(_ context.Context, before time.Time, batch int) (int64, error) {
	f.before = before
	f.batch = batch
	return f.deleted, f.err
}

func TestSessionCleanupHandler(t *testing.T) {
	now := time.Date(2026, 9, 26, 20, 0, 0, 0, time.UTC)
	payload, err := json.Marshal(SessionCleanupPayload{RevokedBefore: now.Add(-time.Hour), Batch: 250})
	if err != nil {
		t.Fatal(err)
	}
	store := &fakeCleanupStore{deleted: 12}
	result, err := SessionCleanupHandler(store)(context.Background(), jobs.Job{Payload: payload})
	if err != nil {
		t.Fatalf("handler error = %v", err)
	}
	if store.batch != 250 || !store.before.Equal(now.Add(-time.Hour)) {
		t.Fatalf("cleanup args before=%v batch=%d", store.before, store.batch)
	}
	if string(result) != `{"deleted_sessions":12}` {
		t.Fatalf("result = %s", result)
	}
}

func TestSessionCleanupHandlerPropagatesStoreError(t *testing.T) {
	expected := errors.New("db unavailable")
	payload := json.RawMessage(`{"revoked_before":"2026-09-26T19:00:00Z","batch":10}`)
	store := &fakeCleanupStore{err: expected}
	_, err := SessionCleanupHandler(store)(context.Background(), jobs.Job{Payload: payload})
	if !errors.Is(err, expected) {
		t.Fatalf("error = %v", err)
	}
}
