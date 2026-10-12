package events

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
)

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) Append(ctx context.Context, exec database.Executor, input Input) (uint64, bool, error) {
	input.Type = strings.TrimSpace(input.Type)
	input.AggregateType = strings.TrimSpace(input.AggregateType)
	input.AggregateID = strings.TrimSpace(input.AggregateID)
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	if exec == nil || input.Type == "" || len(input.Type) > 120 ||
		input.AggregateType == "" || len(input.AggregateType) > 100 ||
		input.AggregateID == "" || len(input.AggregateID) > 128 ||
		len(input.IdempotencyKey) > 190 || input.MaxAttempts > 100 {
		return 0, false, ErrInvalid
	}
	if input.Version == 0 {
		input.Version = 1
	}
	if input.Version > 100 {
		return 0, false, ErrInvalid
	}
	if input.MaxAttempts == 0 {
		input.MaxAttempts = 10
	}
	if len(input.Payload) == 0 {
		input.Payload = json.RawMessage(`{}`)
	}
	if !json.Valid(input.Payload) {
		return 0, false, ErrInvalid
	}
	if input.AvailableAt.IsZero() {
		input.AvailableAt = time.Now().UTC()
	}
	scope := scopeKey(input.TenantID)
	tenantValue := nullableTenant(input.TenantID)

	if input.IdempotencyKey == "" {
		result, err := exec.ExecContext(ctx, `
			INSERT INTO outbox_events (
				tenant_id, scope_key, event_type, event_version, aggregate_type, aggregate_id,
				payload, max_attempts, available_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, tenantValue, scope, input.Type, input.Version, input.AggregateType, input.AggregateID,
			[]byte(input.Payload), input.MaxAttempts, input.AvailableAt.UTC())
		if err != nil {
			return 0, false, fmt.Errorf("append outbox event: %w", err)
		}
		id, err := result.LastInsertId()
		if err != nil {
			return 0, false, fmt.Errorf("read outbox event id: %w", err)
		}
		return uint64(id), true, nil
	}

	result, err := exec.ExecContext(ctx, `
		INSERT INTO outbox_events (
			tenant_id, scope_key, event_type, event_version, aggregate_type, aggregate_id,
			payload, max_attempts, available_at, idempotency_key
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE id = LAST_INSERT_ID(id)
	`, tenantValue, scope, input.Type, input.Version, input.AggregateType, input.AggregateID,
		[]byte(input.Payload), input.MaxAttempts, input.AvailableAt.UTC(), input.IdempotencyKey)
	if err != nil {
		return 0, false, fmt.Errorf("append idempotent outbox event: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, false, fmt.Errorf("read outbox append result: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, false, fmt.Errorf("read outbox event id: %w", err)
	}
	return uint64(id), affected == 1, nil
}

func (r *Repository) Claim(ctx context.Context, owner string, lease time.Duration) (Event, bool, error) {
	owner = strings.TrimSpace(owner)
	if owner == "" || len(owner) > 190 || lease < 5*time.Second || lease > time.Hour {
		return Event{}, false, ErrInvalid
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return Event{}, false, fmt.Errorf("begin outbox claim: %w", err)
	}
	defer tx.Rollback()

	var id uint64
	var previousStatus Status
	var attemptCount, maxAttempts uint32
	err = tx.QueryRowContext(ctx, `
		SELECT id, status, attempt_count, max_attempts
		FROM outbox_events
		WHERE available_at <= UTC_TIMESTAMP(6)
		  AND (
			(status = 'PENDING' AND attempt_count < max_attempts)
			OR (status = 'DELIVERING' AND lease_expires_at < UTC_TIMESTAMP(6))
		  )
		ORDER BY available_at, id
		LIMIT 1
		FOR UPDATE SKIP LOCKED
	`).Scan(&id, &previousStatus, &attemptCount, &maxAttempts)
	if errors.Is(err, sql.ErrNoRows) {
		return Event{}, false, nil
	}
	if err != nil {
		return Event{}, false, fmt.Errorf("select outbox event: %w", err)
	}

	now := time.Now().UTC()
	if previousStatus == StatusDelivering && attemptCount >= maxAttempts {
		if _, err := tx.ExecContext(ctx, `
			UPDATE outbox_events
			SET status = 'FAILED',
			    last_error = 'delivery lease expired after maximum attempts',
			    lease_owner = NULL,
			    lease_expires_at = NULL
			WHERE id = ?
		`, id); err != nil {
			return Event{}, false, fmt.Errorf("fail exhausted outbox event: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return Event{}, false, fmt.Errorf("commit exhausted outbox event: %w", err)
		}
		return Event{}, false, nil
	}

	result, err := tx.ExecContext(ctx, `
		UPDATE outbox_events
		SET status = 'DELIVERING',
		    attempt_count = attempt_count + 1,
		    lease_owner = ?,
		    lease_expires_at = ?,
		    last_error = NULL
		WHERE id = ?
	`, owner, now.Add(lease), id)
	if err != nil {
		return Event{}, false, fmt.Errorf("claim outbox event: %w", err)
	}
	if affected, err := result.RowsAffected(); err != nil || affected != 1 {
		if err != nil {
			return Event{}, false, fmt.Errorf("read outbox claim result: %w", err)
		}
		return Event{}, false, ErrLeaseLost
	}
	event, err := scanEvent(tx.QueryRowContext(ctx, eventSelect+` WHERE id = ?`, id))
	if err != nil {
		return Event{}, false, fmt.Errorf("read claimed outbox event: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Event{}, false, fmt.Errorf("commit outbox claim: %w", err)
	}
	return event, true, nil
}

func (r *Repository) Heartbeat(
	ctx context.Context,
	id uint64,
	owner string,
	lease time.Duration,
) (HeartbeatState, error) {
	if id == 0 || owner == "" || lease < 5*time.Second || lease > time.Hour {
		return HeartbeatState{}, ErrInvalid
	}
	result, err := r.db.ExecContext(ctx, `
		UPDATE outbox_events
		SET lease_expires_at = ?
		WHERE id = ?
		  AND status = 'DELIVERING'
		  AND lease_owner = ?
		  AND lease_expires_at > UTC_TIMESTAMP(6)
	`, time.Now().UTC().Add(lease), id, owner)
	if err != nil {
		return HeartbeatState{}, fmt.Errorf("heartbeat outbox event: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return HeartbeatState{}, fmt.Errorf("read outbox heartbeat result: %w", err)
	}
	return HeartbeatState{LeaseValid: affected == 1}, nil
}

func (r *Repository) Complete(ctx context.Context, id uint64, owner string) error {
	if id == 0 || owner == "" {
		return ErrInvalid
	}
	result, err := r.db.ExecContext(ctx, `
		UPDATE outbox_events
		SET status = 'DELIVERED',
		    delivered_at = UTC_TIMESTAMP(6),
		    lease_owner = NULL,
		    lease_expires_at = NULL,
		    last_error = NULL
		WHERE id = ? AND status = 'DELIVERING' AND lease_owner = ?
	`, id, owner)
	if err != nil {
		return fmt.Errorf("complete outbox event: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read outbox completion result: %w", err)
	}
	if affected != 1 {
		return ErrLeaseLost
	}
	return nil
}

func (r *Repository) Fail(
	ctx context.Context,
	id uint64,
	owner string,
	deliveryErr error,
	retryDelay time.Duration,
) error {
	if id == 0 || owner == "" || deliveryErr == nil || retryDelay < 0 || retryDelay > 24*time.Hour {
		return ErrInvalid
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return fmt.Errorf("begin outbox failure update: %w", err)
	}
	defer tx.Rollback()

	var attempts, maxAttempts uint32
	if err := tx.QueryRowContext(ctx, `
		SELECT attempt_count, max_attempts
		FROM outbox_events
		WHERE id = ? AND status = 'DELIVERING' AND lease_owner = ?
		FOR UPDATE
	`, id, owner).Scan(&attempts, &maxAttempts); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrLeaseLost
		}
		return fmt.Errorf("lock failed outbox event: %w", err)
	}
	status := StatusFailed
	availableAt := time.Now().UTC()
	if attempts < maxAttempts {
		status = StatusPending
		availableAt = availableAt.Add(retryDelay)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE outbox_events
		SET status = ?,
		    available_at = ?,
		    lease_owner = NULL,
		    lease_expires_at = NULL,
		    last_error = ?
		WHERE id = ?
	`, status, availableAt, truncate(deliveryErr.Error(), 4000), id); err != nil {
		return fmt.Errorf("fail outbox event: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit failed outbox event: %w", err)
	}
	return nil
}

const eventSelect = `
	SELECT
		id, tenant_id, event_type, event_version, aggregate_type, aggregate_id, payload, status,
		attempt_count, max_attempts, available_at, COALESCE(lease_owner, ''),
		lease_expires_at, COALESCE(last_error, ''), COALESCE(idempotency_key, ''),
		created_at, delivered_at, notification_eligible
	FROM outbox_events
`

type rowScanner interface {
	Scan(...any) error
}

func scanEvent(row rowScanner) (Event, error) {
	var event Event
	var tenantID sql.Null[uint64]
	var payload []byte
	var leaseExpiresAt, deliveredAt sql.NullTime
	if err := row.Scan(
		&event.ID,
		&tenantID,
		&event.Type,
		&event.Version,
		&event.AggregateType,
		&event.AggregateID,
		&payload,
		&event.Status,
		&event.AttemptCount,
		&event.MaxAttempts,
		&event.AvailableAt,
		&event.LeaseOwner,
		&leaseExpiresAt,
		&event.LastError,
		&event.IdempotencyKey,
		&event.CreatedAt,
		&deliveredAt,
		&event.NotificationEligible,
	); err != nil {
		return Event{}, err
	}
	if tenantID.Valid {
		event.TenantID = tenantID.V
	}
	event.Payload = append(json.RawMessage(nil), payload...)
	if leaseExpiresAt.Valid {
		value := leaseExpiresAt.Time
		event.LeaseExpiresAt = &value
	}
	if deliveredAt.Valid {
		value := deliveredAt.Time
		event.DeliveredAt = &value
	}
	return event, nil
}

func scopeKey(tenantID uint64) string {
	if tenantID == 0 {
		return "system"
	}
	return fmt.Sprintf("tenant:%d", tenantID)
}

func nullableTenant(tenantID uint64) any {
	if tenantID == 0 {
		return nil
	}
	return tenantID
}

func truncate(value string, max int) string {
	runes := []rune(strings.ToValidUTF8(value, ""))
	if len(runes) <= max {
		return string(runes)
	}
	return string(runes[:max])
}
