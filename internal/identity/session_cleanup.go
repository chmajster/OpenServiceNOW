package identity

import (
	"context"
	"fmt"
	"time"
)

type SessionCleanupStore interface {
	CleanupSessions(context.Context, time.Time, int) (int64, error)
}

type SessionCleanup struct {
	store            SessionCleanupStore
	batch            int
	revokedRetention time.Duration
	now              func() time.Time
}

func NewSessionCleanup(store SessionCleanupStore, batch int, revokedRetention time.Duration) *SessionCleanup {
	return &SessionCleanup{
		store:            store,
		batch:            batch,
		revokedRetention: revokedRetention,
		now:              func() time.Time { return time.Now().UTC() },
	}
}

func (c *SessionCleanup) RunOnce(ctx context.Context) (int64, error) {
	if c.batch < 1 {
		return 0, fmt.Errorf("session cleanup batch must be positive")
	}
	if c.revokedRetention < 0 {
		return 0, fmt.Errorf("session revoked retention must not be negative")
	}
	revokedBefore := c.now().Add(-c.revokedRetention)
	return c.store.CleanupSessions(ctx, revokedBefore, c.batch)
}
