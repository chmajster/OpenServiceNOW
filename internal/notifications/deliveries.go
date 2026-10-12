package notifications

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
)

// Job lease exhaustion can happen without re-entering the handler. The durable
// job outcome remains authoritative in that case, rather than a stale SENDING.
const mailStatusSQL = `CASE WHEN d.status IN ('SENDING','QUEUED') AND j.status='FAILED' THEN 'FAILED' WHEN d.status IN ('SENDING','QUEUED') AND j.status='CANCELLED' THEN 'SKIPPED' ELSE d.status END`

func replaceInAppFilter(clause string) string {
	return strings.Replace(clause, " AND n.in_app_enabled=TRUE", "", 1)
}
func (s *Service) MailList(ctx context.Context, tenant, user, before uint64, limit int) (MailPage, error) {
	if limit < 1 || limit > 100 {
		return MailPage{}, ErrInvalid
	}
	query := `SELECT d.id,d.user_id,d.notification_id,d.provider_id,` + mailStatusSQL + `,d.attempt_count,d.version,CASE WHEN d.status IN ('SENDING','QUEUED') AND j.status='FAILED' THEN 'JOB_EXHAUSTED' ELSE d.error_code END,d.created_at,COALESCE(d.finished_at,j.finished_at) FROM notification_mail_deliveries d LEFT JOIN jobs j ON j.id=d.job_id WHERE d.tenant_id=?`
	args := []any{tenant}
	if user > 0 {
		query += ` AND d.user_id=? AND (d.notification_id IS NULL OR EXISTS(SELECT 1` + notificationJoins + ` WHERE n.id=d.notification_id AND `
		a, err := s.currentActor(ctx, s.db, tenant, user)
		if err != nil {
			return MailPage{}, err
		}
		clause, accessArgs := notificationAccess(a)
		clause = replaceInAppFilter(clause)
		query += clause + `))`
		args = append(args, user)
		args = append(args, accessArgs...)
	}
	if before > 0 {
		query += ` AND d.id<?`
		args = append(args, before)
	}
	query += ` ORDER BY d.id DESC LIMIT ?`
	args = append(args, limit+1)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return MailPage{}, err
	}
	defer rows.Close()
	p := MailPage{Deliveries: []MailDelivery{}}
	for rows.Next() {
		var d MailDelivery
		var n sql.Null[uint64]
		var finish sql.NullTime
		if err = rows.Scan(&d.ID, &d.UserID, &n, &d.ProviderID, &d.Status, &d.Attempts, &d.Version, &d.ErrorCode, &d.CreatedAt, &finish); err != nil {
			return p, err
		}
		if n.Valid {
			d.NotificationID = &n.V
		}
		if finish.Valid {
			d.FinishedAt = &finish.Time
		}
		p.Deliveries = append(p.Deliveries, d)
	}
	if err = rows.Err(); err != nil {
		return p, err
	}
	if len(p.Deliveries) > limit {
		p.Deliveries = p.Deliveries[:limit]
		p.NextBeforeID = p.Deliveries[limit-1].ID
	}
	return p, nil
}
func (s *Service) Attempts(ctx context.Context, tenant, user, id uint64) ([]Attempt, error) {
	// This endpoint is administrator-only; user history is the filtered MailList.
	var exists bool
	if err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM notification_mail_deliveries WHERE tenant_id=? AND id=?)`, tenant, id).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrNotFound
	}
	// An exhausted/cancelled job or expired lease cannot still be sending. A
	// crash may prevent the handler from finalizing its last attempt, so expose
	// that outcome as unknown until a later retry persists the reconciliation.
	const lost = `a.status='SENDING' AND (j.id IS NULL OR j.status<>'RUNNING' OR j.lease_expires_at<=UTC_TIMESTAMP(6))`
	rows, err := s.db.QueryContext(ctx, `SELECT a.attempt_number,CASE WHEN `+lost+` THEN 'UNKNOWN' ELSE a.status END,CASE WHEN `+lost+` THEN 'LEASE_LOST' ELSE a.error_code END,a.started_at,CASE WHEN `+lost+` THEN COALESCE(a.finished_at,j.finished_at,j.lease_expires_at) ELSE a.finished_at END FROM notification_mail_attempts a JOIN notification_mail_deliveries d ON d.id=a.delivery_id LEFT JOIN jobs j ON j.id=d.job_id WHERE d.tenant_id=? AND a.delivery_id=? ORDER BY a.attempt_number DESC LIMIT 100`, tenant, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Attempt{}
	for rows.Next() {
		var a Attempt
		var finished sql.NullTime
		if err = rows.Scan(&a.Number, &a.Status, &a.ErrorCode, &a.StartedAt, &finished); err != nil {
			return nil, err
		}
		if finished.Valid {
			a.FinishedAt = &finished.Time
		}
		result = append(result, a)
	}
	return result, rows.Err()
}
func (s *Service) RetryMail(ctx context.Context, tenant, user, id, version uint64) error {
	return s.tx.WithTx(ctx, nil, func(e database.Executor) error {
		// Match the worker's job -> delivery lock order. Verify that the job
		// generation did not change while acquiring locks; never lock a new job
		// after its delivery, which could deadlock with a running worker.
		var expectedJob sql.Null[uint64]
		err := e.QueryRowContext(ctx, `SELECT job_id FROM notification_mail_deliveries WHERE tenant_id=? AND id=?`, tenant, id).Scan(&expectedJob)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		jobStatus := ""
		if expectedJob.Valid {
			if err = e.QueryRowContext(ctx, `SELECT status FROM jobs WHERE id=? FOR UPDATE`, expectedJob.V).Scan(&jobStatus); err != nil {
				return err
			}
		}
		var current uint64
		var status string
		var attempts uint32
		var storedJob sql.Null[uint64]
		err = e.QueryRowContext(ctx, `SELECT version,status,attempt_count,job_id FROM notification_mail_deliveries WHERE tenant_id=? AND id=? FOR UPDATE`, tenant, id).Scan(&current, &status, &attempts, &storedJob)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if storedJob != expectedJob {
			return ErrConflict
		}
		if status == "SENDING" || status == "QUEUED" {
			if jobStatus == "FAILED" {
				status = "FAILED"
			} else if jobStatus == "CANCELLED" {
				status = "SKIPPED"
			}
		}
		if current != version || status != "FAILED" && status != "NOT_CONFIGURED" || attempts >= 100 {
			return ErrConflict
		}
		c, err := s.config(ctx, e, tenant)
		if err != nil {
			return err
		}
		if !c.Enabled {
			return ErrInvalid
		}
		_, err = e.ExecContext(ctx, `UPDATE notification_mail_deliveries SET version=version+1 WHERE id=?`, id)
		if err != nil {
			return err
		}
		if err = s.enqueueMail(ctx, e, tenant, id, version+1); err != nil {
			return err
		}
		return auditChange(ctx, e, tenant, user, "notification.smtp.retried", id)
	})
}
