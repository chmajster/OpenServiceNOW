package systemjobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/chmajster/OpenServiceNOW/internal/identity"
	"github.com/chmajster/OpenServiceNOW/internal/jobs"
)

const SessionCleanupType = "system.session_cleanup"

type SessionCleanupPayload struct {
	RevokedBefore time.Time `json:"revoked_before"`
	Batch         int       `json:"batch"`
}

func EnqueueSessionCleanup(
	ctx context.Context,
	repo *jobs.Repository,
	now time.Time,
	retention time.Duration,
	batch int,
	idempotencyKey string,
) (jobs.Job, bool, error) {
	if repo == nil || retention < 0 || batch < 1 || batch > 10000 || idempotencyKey == "" {
		return jobs.Job{}, false, jobs.ErrInvalid
	}
	payload, err := json.Marshal(SessionCleanupPayload{
		RevokedBefore: now.UTC().Add(-retention),
		Batch:         batch,
	})
	if err != nil {
		return jobs.Job{}, false, fmt.Errorf("marshal session cleanup payload: %w", err)
	}
	return repo.Enqueue(ctx, jobs.EnqueueInput{
		Type:           SessionCleanupType,
		Payload:        payload,
		MaxAttempts:    3,
		AvailableAt:    now.UTC(),
		IdempotencyKey: idempotencyKey,
	})
}

func SessionCleanupHandler(store identity.SessionCleanupStore) jobs.Handler {
	return func(ctx context.Context, job jobs.Job) (json.RawMessage, error) {
		if store == nil {
			return nil, errors.New("session cleanup store is nil")
		}
		var payload SessionCleanupPayload
		if err := json.Unmarshal(job.Payload, &payload); err != nil {
			return nil, fmt.Errorf("decode session cleanup payload: %w", err)
		}
		if payload.RevokedBefore.IsZero() || payload.Batch < 1 || payload.Batch > 10000 {
			return nil, jobs.ErrInvalid
		}
		deleted, err := store.CleanupSessions(ctx, payload.RevokedBefore.UTC(), payload.Batch)
		if err != nil {
			return nil, err
		}
		result, err := json.Marshal(map[string]int64{"deleted_sessions": deleted})
		if err != nil {
			return nil, fmt.Errorf("marshal session cleanup result: %w", err)
		}
		return result, nil
	}
}
