package incidents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/chmajster/OpenServiceNOW/internal/jobs"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
)

const MaintenanceJobType = "incident.maintenance"

// A durable, idempotent sweep is independent of browser activity. The next
// communication deadline and outbox insert are committed in the same SQL tx.
func EnqueueMaintenance(ctx context.Context, repo *jobs.Repository, now time.Time) error {
	_, _, err := repo.Enqueue(ctx, jobs.EnqueueInput{Type: MaintenanceJobType, Payload: json.RawMessage(`{"version":1}`), AvailableAt: now, MaxAttempts: 10, IdempotencyKey: "incident-maintenance:" + now.UTC().Truncate(time.Minute).Format(time.RFC3339)})
	return err
}

func (s *Service) MaintenanceHandler(ctx context.Context, job jobs.Job) (json.RawMessage, error) {
	var payload struct {
		Version int `json:"version"`
	}
	if job.TenantID != 0 || json.Unmarshal(job.Payload, &payload) != nil || payload.Version != 1 {
		return nil, jobs.ErrInvalid
	}
	now := s.now().UTC().Truncate(time.Microsecond)
	reminders, err := s.ScheduleMajorReminders(ctx, now)
	if err != nil {
		return nil, err
	}
	rows, err := s.repo.db.QueryContext(ctx, `SELECT i.tenant_id,i.id,i.version,w.assigned_to FROM incidents i JOIN work_assignments w ON w.tenant_id=i.tenant_id AND w.object_type='incident' AND w.object_id=CAST(i.id AS CHAR) WHERE i.state='RESOLVED' AND w.assigned_to IS NOT NULL AND CAST(JSON_UNQUOTE(JSON_EXTRACT(i.process_policy,'$.auto_close_hours')) AS UNSIGNED)>0 AND DATE_ADD(i.resolved_at,INTERVAL CAST(JSON_UNQUOTE(JSON_EXTRACT(i.process_policy,'$.auto_close_hours')) AS UNSIGNED) HOUR)<=? AND (i.auto_close_retry_at IS NULL OR i.auto_close_retry_at<=?) ORDER BY COALESCE(i.auto_close_retry_at,i.resolved_at),i.tenant_id,i.id LIMIT 100`, now, now)
	if err != nil {
		return nil, err
	}
	type target struct{ tenant, id, version, user uint64 }
	targets := []target{}
	for rows.Next() {
		var t target
		if err = rows.Scan(&t.tenant, &t.id, &t.version, &t.user); err != nil {
			rows.Close()
			return nil, err
		}
		targets = append(targets, t)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	closed, deferred := 0, 0
	for _, t := range targets {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		// The assigned active user's current resolve/read permissions are reloaded
		// under the tenant lock by Transition; a job does not grant privileges.
		a := Actor{Source: "worker", TenantID: t.tenant, UserID: t.user, RequestID: fmt.Sprintf("job:%d", job.ID), Permissions: map[string]bool{"incident.resolve": true}}
		_, err = s.Transition(ctx, a, t.id, TransitionInput{Version: t.version, State: "CLOSED"})
		if err == nil {
			closed++
			continue
		}
		if !errors.Is(err, ErrConflict) && !errors.Is(err, ErrInvalid) && !errors.Is(err, ErrTransition) && !errors.Is(err, ErrForbidden) && !errors.Is(err, workrecords.ErrForbidden) && !errors.Is(err, workrecords.ErrNotFound) && !errors.Is(err, ErrNotFound) {
			return nil, err
		}
		deferred++
		if _, err = s.repo.db.ExecContext(ctx, `UPDATE incidents SET auto_close_retry_at=? WHERE tenant_id=? AND id=? AND version=? AND state='RESOLVED'`, now.Add(time.Hour), t.tenant, t.id, t.version); err != nil {
			return nil, err
		}
	}
	return json.Marshal(map[string]int{"reminders": reminders, "closed": closed, "deferred": deferred})
}
