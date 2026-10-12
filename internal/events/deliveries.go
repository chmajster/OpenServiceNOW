package events

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"time"
)

var consumerPattern = regexp.MustCompile(`^[a-z][a-z0-9._-]{2,99}$`)

// Distribute freezes the required recipient set exactly once under the event
// lease. A committed split is never extended implicitly by enabling a module.
func (r *Repository) Distribute(ctx context.Context, id uint64, owner string, consumers []string) error {
	if len(consumers) == 0 || len(consumers) > 32 {
		return ErrInvalid
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var max uint32
	var eligible bool
	if err = tx.QueryRowContext(ctx, `SELECT max_attempts,notification_eligible FROM outbox_events WHERE id=? AND status='DELIVERING' AND lease_owner=? AND lease_expires_at>UTC_TIMESTAMP(6) FOR UPDATE`, id, owner).Scan(&max, &eligible); errors.Is(err, sql.ErrNoRows) {
		return ErrLeaseLost
	}
	if err != nil {
		return err
	}
	for _, consumer := range consumers {
		if !consumerPattern.MatchString(consumer) {
			return ErrInvalid
		}
		status := "PENDING"
		if consumer == "notifications.v1" && !eligible {
			status = "SKIPPED"
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO event_deliveries(event_id,consumer_id,status,max_attempts,available_at,finished_at) VALUES(?,?,?,?,UTC_TIMESTAMP(6),IF(?='SKIPPED',UTC_TIMESTAMP(6),NULL)) ON DUPLICATE KEY UPDATE id=id`, id, consumer, status, max, status)
		if err != nil {
			return err
		}
	}
	// DELIVERED now means distribution committed, not downstream success.
	_, err = tx.ExecContext(ctx, `UPDATE outbox_events SET status='DELIVERED',delivered_at=UTC_TIMESTAMP(6),lease_owner=NULL,lease_expires_at=NULL,last_error=NULL WHERE id=?`, id)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Repository) ClaimDelivery(ctx context.Context, owner string, lease time.Duration) (Delivery, bool, error) {
	if owner == "" || len(owner) > 190 || lease < 5*time.Second || lease > time.Hour {
		return Delivery{}, false, ErrInvalid
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return Delivery{}, false, err
	}
	defer tx.Rollback()
	var d Delivery
	err = tx.QueryRowContext(ctx, `SELECT id,event_id,consumer_id,attempt_count,max_attempts FROM event_deliveries WHERE available_at<=UTC_TIMESTAMP(6) AND (status='PENDING' OR (status='DELIVERING' AND lease_expires_at<UTC_TIMESTAMP(6))) ORDER BY available_at,id LIMIT 1 FOR UPDATE SKIP LOCKED`).Scan(&d.ID, &d.EventID, &d.ConsumerID, &d.AttemptCount, &d.MaxAttempts)
	if errors.Is(err, sql.ErrNoRows) {
		return d, false, nil
	}
	if err != nil {
		return d, false, err
	}
	if d.AttemptCount >= d.MaxAttempts {
		_, err = tx.ExecContext(ctx, `UPDATE event_deliveries SET status='FAILED',last_error='LEASE_EXHAUSTED',lease_owner=NULL,lease_expires_at=NULL,finished_at=UTC_TIMESTAMP(6) WHERE id=?`, d.ID)
		if err == nil {
			err = tx.Commit()
		}
		return d, false, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE event_deliveries SET status='DELIVERING',attempt_count=attempt_count+1,lease_owner=?,lease_expires_at=?,finished_at=NULL WHERE id=?`, owner, time.Now().UTC().Add(lease), d.ID)
	if err != nil {
		return d, false, err
	}
	d.Event, err = scanEvent(tx.QueryRowContext(ctx, eventSelect+` WHERE id=?`, d.EventID))
	if err != nil {
		return d, false, err
	}
	d.AttemptCount++
	err = tx.Commit()
	return d, err == nil, err
}
func (r *Repository) HeartbeatDelivery(ctx context.Context, id uint64, owner string, lease time.Duration) (HeartbeatState, error) {
	result, err := r.db.ExecContext(ctx, `UPDATE event_deliveries SET lease_expires_at=? WHERE id=? AND status='DELIVERING' AND lease_owner=? AND lease_expires_at>UTC_TIMESTAMP(6)`, time.Now().UTC().Add(lease), id, owner)
	if err != nil {
		return HeartbeatState{}, err
	}
	n, err := result.RowsAffected()
	return HeartbeatState{LeaseValid: n == 1}, err
}
func (r *Repository) FinishDelivery(ctx context.Context, id uint64, owner string, deliveryErr error, delay time.Duration) error {
	// Never persist arbitrary downstream exception text: it may contain secrets,
	// message bodies or SMTP responses. Detailed safe codes belong to the module.
	code := ""
	if deliveryErr != nil {
		code = "CONSUMER_FAILED"
	}
	result, err := r.db.ExecContext(ctx, `UPDATE event_deliveries SET status=CASE WHEN ?='' THEN 'DELIVERED' WHEN attempt_count<max_attempts THEN 'PENDING' ELSE 'FAILED' END,last_error=NULLIF(?,''),available_at=?,finished_at=CASE WHEN ?='' OR attempt_count>=max_attempts THEN UTC_TIMESTAMP(6) ELSE NULL END,lease_owner=NULL,lease_expires_at=NULL WHERE id=? AND status='DELIVERING' AND lease_owner=? AND lease_expires_at>UTC_TIMESTAMP(6)`, code, code, time.Now().UTC().Add(delay), code, id, owner)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n != 1 {
		return ErrLeaseLost
	}
	return err
}

type DeliveryPage struct {
	Deliveries   []Delivery `json:"deliveries"`
	NextBeforeID uint64     `json:"next_before_id,string,omitempty"`
}

func (r *Repository) ListDeliveries(ctx context.Context, tenant, before uint64, limit int) (DeliveryPage, error) {
	if limit < 1 || limit > 100 {
		return DeliveryPage{}, ErrInvalid
	}
	rows, err := r.db.QueryContext(ctx, `SELECT d.id,d.event_id,d.consumer_id,d.status,d.attempt_count,d.max_attempts,COALESCE(d.last_error,''),d.created_at,d.finished_at FROM event_deliveries d JOIN outbox_events e ON e.id=d.event_id WHERE e.tenant_id=? AND (?=0 OR d.id<?) ORDER BY d.id DESC LIMIT ?`, tenant, before, before, limit+1)
	if err != nil {
		return DeliveryPage{}, err
	}
	defer rows.Close()
	p := DeliveryPage{Deliveries: []Delivery{}}
	for rows.Next() {
		var d Delivery
		var finished sql.NullTime
		if err = rows.Scan(&d.ID, &d.EventID, &d.ConsumerID, &d.Status, &d.AttemptCount, &d.MaxAttempts, &d.LastError, &d.CreatedAt, &finished); err != nil {
			return p, err
		}
		if finished.Valid {
			d.FinishedAt = &finished.Time
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
func (r *Repository) RetryDelivery(ctx context.Context, tenant, id uint64, attempts uint32) error {
	result, err := r.db.ExecContext(ctx, `UPDATE event_deliveries d JOIN outbox_events e ON e.id=d.event_id SET d.status='PENDING',d.max_attempts=LEAST(d.attempt_count+10,100),d.available_at=UTC_TIMESTAMP(6),d.finished_at=NULL WHERE e.tenant_id=? AND d.id=? AND d.status='FAILED' AND d.attempt_count=? AND d.attempt_count<100`, tenant, id, attempts)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n != 1 {
		return fmt.Errorf("%w: delivery changed or is not failed", ErrInvalid)
	}
	return err
}
