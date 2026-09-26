package jobs

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

func (r *Repository) Enqueue(ctx context.Context, input EnqueueInput) (Job, bool, error) {
	input.Type = strings.TrimSpace(input.Type)
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	if input.TenantID == 0 || input.Type == "" || len(input.Type) > 120 ||
		len(input.IdempotencyKey) > 190 || input.MaxAttempts > 100 {
		return Job{}, false, ErrInvalid
	}
	if input.MaxAttempts == 0 {
		input.MaxAttempts = 3
	}
	if len(input.Payload) == 0 {
		input.Payload = json.RawMessage(`{}`)
	}
	if !json.Valid(input.Payload) {
		return Job{}, false, ErrInvalid
	}
	if input.AvailableAt.IsZero() {
		input.AvailableAt = time.Now().UTC()
	}

	var id uint64
	created := true
	if input.IdempotencyKey == "" {
		result, err := r.db.ExecContext(ctx, `
			INSERT INTO jobs (
				tenant_id, job_type, payload, max_attempts, available_at, created_by
			) VALUES (?, ?, ?, ?, ?, ?)
		`, input.TenantID, input.Type, []byte(input.Payload), input.MaxAttempts, input.AvailableAt.UTC(), nullableUint64(input.CreatedBy))
		if err != nil {
			return Job{}, false, fmt.Errorf("enqueue job: %w", err)
		}
		lastID, err := result.LastInsertId()
		if err != nil {
			return Job{}, false, fmt.Errorf("read enqueued job id: %w", err)
		}
		id = uint64(lastID)
	} else {
		result, err := r.db.ExecContext(ctx, `
			INSERT IGNORE INTO jobs (
				tenant_id, job_type, payload, max_attempts, available_at, idempotency_key, created_by
			) VALUES (?, ?, ?, ?, ?, ?, ?)
		`, input.TenantID, input.Type, []byte(input.Payload), input.MaxAttempts, input.AvailableAt.UTC(), input.IdempotencyKey, nullableUint64(input.CreatedBy))
		if err != nil {
			return Job{}, false, fmt.Errorf("enqueue idempotent job: %w", err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return Job{}, false, fmt.Errorf("read enqueue result: %w", err)
		}
		created = affected == 1
		if created {
			lastID, err := result.LastInsertId()
			if err != nil {
				return Job{}, false, fmt.Errorf("read enqueued job id: %w", err)
			}
			id = uint64(lastID)
		} else {
			if err := r.db.QueryRowContext(ctx, `
				SELECT id
				FROM jobs
				WHERE tenant_id = ? AND job_type = ? AND idempotency_key = ?
			`, input.TenantID, input.Type, input.IdempotencyKey).Scan(&id); err != nil {
				return Job{}, false, fmt.Errorf("read existing idempotent job: %w", err)
			}
		}
	}
	job, err := r.Get(ctx, input.TenantID, id)
	return job, created, err
}

func (r *Repository) Get(ctx context.Context, tenantID, id uint64) (Job, error) {
	if tenantID == 0 || id == 0 {
		return Job{}, ErrInvalid
	}
	job, err := scanJob(r.db.QueryRowContext(ctx, jobSelect+` WHERE tenant_id = ? AND id = ?`, tenantID, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Job{}, ErrNotFound
	}
	if err != nil {
		return Job{}, fmt.Errorf("get job: %w", err)
	}
	return job, nil
}

func (r *Repository) Claim(ctx context.Context, owner string, lease time.Duration) (Job, bool, error) {
	owner = strings.TrimSpace(owner)
	if owner == "" || len(owner) > 190 || lease < 5*time.Second || lease > time.Hour {
		return Job{}, false, ErrInvalid
	}

	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return Job{}, false, fmt.Errorf("begin job claim: %w", err)
	}
	defer tx.Rollback()

	var id uint64
	var previousStatus Status
	var previousAttempt uint32
	err = tx.QueryRowContext(ctx, `
		SELECT id, status, attempt_count
		FROM jobs
		WHERE cancel_requested = FALSE
		  AND attempt_count < max_attempts
		  AND available_at <= UTC_TIMESTAMP(6)
		  AND (
			status IN ('QUEUED','RETRYING')
			OR (status = 'RUNNING' AND lease_expires_at < UTC_TIMESTAMP(6))
		  )
		ORDER BY available_at, id
		LIMIT 1
		FOR UPDATE SKIP LOCKED
	`).Scan(&id, &previousStatus, &previousAttempt)
	if errors.Is(err, sql.ErrNoRows) {
		return Job{}, false, nil
	}
	if err != nil {
		return Job{}, false, fmt.Errorf("select claimable job: %w", err)
	}

	now := time.Now().UTC()
	if previousStatus == StatusRunning && previousAttempt > 0 {
		if _, err := tx.ExecContext(ctx, `
			UPDATE job_attempts
			SET status = 'LOST', finished_at = ?, error_message = 'worker lease expired'
			WHERE job_id = ? AND attempt_number = ? AND status = 'RUNNING'
		`, now, id, previousAttempt); err != nil {
			return Job{}, false, fmt.Errorf("mark expired job attempt lost: %w", err)
		}
	}

	leaseUntil := now.Add(lease)
	result, err := tx.ExecContext(ctx, `
		UPDATE jobs
		SET status = 'RUNNING',
		    attempt_count = attempt_count + 1,
		    lease_owner = ?,
		    lease_expires_at = ?,
		    started_at = COALESCE(started_at, ?),
		    updated_at = ?
		WHERE id = ?
	`, owner, leaseUntil, now, now, id)
	if err != nil {
		return Job{}, false, fmt.Errorf("claim job: %w", err)
	}
	if affected, err := result.RowsAffected(); err != nil || affected != 1 {
		if err != nil {
			return Job{}, false, fmt.Errorf("read claim result: %w", err)
		}
		return Job{}, false, ErrLeaseLost
	}

	attempt := previousAttempt + 1
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO job_attempts (job_id, attempt_number, worker_id, status, started_at)
		VALUES (?, ?, ?, 'RUNNING', ?)
	`, id, attempt, owner, now); err != nil {
		return Job{}, false, fmt.Errorf("create job attempt: %w", err)
	}

	job, err := scanJob(tx.QueryRowContext(ctx, jobSelect+` WHERE id = ?`, id))
	if err != nil {
		return Job{}, false, fmt.Errorf("read claimed job: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Job{}, false, fmt.Errorf("commit job claim: %w", err)
	}
	return job, true, nil
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
	now := time.Now().UTC()
	result, err := r.db.ExecContext(ctx, `
		UPDATE jobs
		SET lease_expires_at = ?, updated_at = ?
		WHERE id = ?
		  AND status = 'RUNNING'
		  AND lease_owner = ?
		  AND lease_expires_at > UTC_TIMESTAMP(6)
	`, now.Add(lease), now, id, owner)
	if err != nil {
		return HeartbeatState{}, fmt.Errorf("heartbeat job: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return HeartbeatState{}, fmt.Errorf("read heartbeat result: %w", err)
	}
	if affected != 1 {
		return HeartbeatState{LeaseValid: false}, nil
	}
	var cancelRequested bool
	if err := r.db.QueryRowContext(ctx, `
		SELECT cancel_requested
		FROM jobs
		WHERE id = ? AND status = 'RUNNING' AND lease_owner = ?
	`, id, owner).Scan(&cancelRequested); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return HeartbeatState{LeaseValid: false}, nil
		}
		return HeartbeatState{}, fmt.Errorf("read job cancellation state: %w", err)
	}
	return HeartbeatState{LeaseValid: true, CancelRequested: cancelRequested}, nil
}

func (r *Repository) Complete(
	ctx context.Context,
	id uint64,
	owner string,
	resultJSON json.RawMessage,
) error {
	if id == 0 || owner == "" {
		return ErrInvalid
	}
	if len(resultJSON) == 0 {
		resultJSON = json.RawMessage(`{}`)
	}
	if !json.Valid(resultJSON) {
		return ErrInvalid
	}
	return r.finish(ctx, id, owner, StatusCompleted, resultJSON, "", 0)
}

func (r *Repository) Fail(
	ctx context.Context,
	id uint64,
	owner string,
	jobErr error,
	retryDelay time.Duration,
) error {
	if id == 0 || owner == "" || jobErr == nil || retryDelay < 0 || retryDelay > 24*time.Hour {
		return ErrInvalid
	}

	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return fmt.Errorf("begin job failure update: %w", err)
	}
	defer tx.Rollback()

	var attempt, maxAttempts uint32
	var cancelRequested bool
	if err := tx.QueryRowContext(ctx, `
		SELECT attempt_count, max_attempts, cancel_requested
		FROM jobs
		WHERE id = ? AND status = 'RUNNING' AND lease_owner = ?
		FOR UPDATE
	`, id, owner).Scan(&attempt, &maxAttempts, &cancelRequested); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrLeaseLost
		}
		return fmt.Errorf("lock failed job: %w", err)
	}

	message := truncate(jobErr.Error(), 4000)
	now := time.Now().UTC()
	nextStatus := StatusFailed
	var availableAt any = now
	var finishedAt any = now
	attemptStatus := "FAILED"
	if cancelRequested {
		nextStatus = StatusCancelled
		attemptStatus = "CANCELLED"
	} else if attempt < maxAttempts {
		nextStatus = StatusRetrying
		availableAt = now.Add(retryDelay)
		finishedAt = nil
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE jobs
		SET status = ?,
		    error_message = ?,
		    available_at = ?,
		    lease_owner = NULL,
		    lease_expires_at = NULL,
		    finished_at = ?,
		    updated_at = ?
		WHERE id = ? AND status = 'RUNNING' AND lease_owner = ?
	`, nextStatus, message, availableAt, finishedAt, now, id, owner); err != nil {
		return fmt.Errorf("update failed job: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE job_attempts
		SET status = ?, finished_at = ?, error_message = ?
		WHERE job_id = ? AND attempt_number = ? AND worker_id = ? AND status = 'RUNNING'
	`, attemptStatus, now, message, id, attempt, owner); err != nil {
		return fmt.Errorf("finish failed job attempt: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit failed job: %w", err)
	}
	return nil
}

func (r *Repository) MarkCancelled(ctx context.Context, id uint64, owner string) error {
	return r.finish(ctx, id, owner, StatusCancelled, nil, "cancelled", 0)
}

func (r *Repository) RequestCancel(ctx context.Context, tenantID, id uint64) error {
	if tenantID == 0 || id == 0 {
		return ErrInvalid
	}
	result, err := r.db.ExecContext(ctx, `
		UPDATE jobs
		SET cancel_requested = TRUE,
		    status = CASE WHEN status IN ('QUEUED','RETRYING') THEN 'CANCELLED' ELSE status END,
		    finished_at = CASE WHEN status IN ('QUEUED','RETRYING') THEN UTC_TIMESTAMP(6) ELSE finished_at END,
		    updated_at = UTC_TIMESTAMP(6)
		WHERE tenant_id = ?
		  AND id = ?
		  AND status IN ('QUEUED','RETRYING','RUNNING')
	`, tenantID, id)
	if err != nil {
		return fmt.Errorf("request job cancellation: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read cancel result: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Repository) finish(
	ctx context.Context,
	id uint64,
	owner string,
	status Status,
	resultJSON json.RawMessage,
	message string,
	progress uint8,
) error {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return fmt.Errorf("begin job completion: %w", err)
	}
	defer tx.Rollback()

	var attempt uint32
	if err := tx.QueryRowContext(ctx, `
		SELECT attempt_count
		FROM jobs
		WHERE id = ? AND status = 'RUNNING' AND lease_owner = ?
		FOR UPDATE
	`, id, owner).Scan(&attempt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrLeaseLost
		}
		return fmt.Errorf("lock running job: %w", err)
	}

	if status == StatusCompleted {
		progress = 100
	}
	now := time.Now().UTC()
	var resultValue any
	if len(resultJSON) > 0 {
		resultValue = []byte(resultJSON)
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE jobs
		SET status = ?,
		    result = ?,
		    error_message = NULLIF(?, ''),
		    progress = ?,
		    lease_owner = NULL,
		    lease_expires_at = NULL,
		    finished_at = ?,
		    updated_at = ?
		WHERE id = ? AND status = 'RUNNING' AND lease_owner = ?
	`, status, resultValue, truncate(message, 4000), progress, now, now, id, owner)
	if err != nil {
		return fmt.Errorf("finish job: %w", err)
	}
	if affected, err := result.RowsAffected(); err != nil || affected != 1 {
		if err != nil {
			return fmt.Errorf("read job completion result: %w", err)
		}
		return ErrLeaseLost
	}

	attemptStatus := string(status)
	if _, err := tx.ExecContext(ctx, `
		UPDATE job_attempts
		SET status = ?, finished_at = ?, error_message = NULLIF(?, '')
		WHERE job_id = ? AND attempt_number = ? AND worker_id = ? AND status = 'RUNNING'
	`, attemptStatus, now, truncate(message, 4000), id, attempt, owner); err != nil {
		return fmt.Errorf("finish job attempt: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit job completion: %w", err)
	}
	return nil
}

const jobSelect = `
	SELECT
		id, tenant_id, job_type, status, payload, result, COALESCE(error_message, ''),
		progress, attempt_count, max_attempts, available_at, COALESCE(lease_owner, ''),
		lease_expires_at, cancel_requested, COALESCE(idempotency_key, ''), created_by,
		created_at, updated_at, started_at, finished_at
	FROM jobs
`

type rowScanner interface {
	Scan(...any) error
}

func scanJob(row rowScanner) (Job, error) {
	var job Job
	var payload, result []byte
	var leaseExpiresAt, startedAt, finishedAt sql.NullTime
	var createdBy sql.NullInt64
	if err := row.Scan(
		&job.ID,
		&job.TenantID,
		&job.Type,
		&job.Status,
		&payload,
		&result,
		&job.ErrorMessage,
		&job.Progress,
		&job.AttemptCount,
		&job.MaxAttempts,
		&job.AvailableAt,
		&job.LeaseOwner,
		&leaseExpiresAt,
		&job.CancelRequested,
		&job.IdempotencyKey,
		&createdBy,
		&job.CreatedAt,
		&job.UpdatedAt,
		&startedAt,
		&finishedAt,
	); err != nil {
		return Job{}, err
	}
	job.Payload = append(json.RawMessage(nil), payload...)
	if len(result) > 0 {
		job.Result = append(json.RawMessage(nil), result...)
	}
	if leaseExpiresAt.Valid {
		value := leaseExpiresAt.Time
		job.LeaseExpiresAt = &value
	}
	if createdBy.Valid {
		value := uint64(createdBy.Int64)
		job.CreatedBy = &value
	}
	if startedAt.Valid {
		value := startedAt.Time
		job.StartedAt = &value
	}
	if finishedAt.Valid {
		value := finishedAt.Time
		job.FinishedAt = &value
	}
	return job, nil
}

func nullableUint64(value *uint64) any {
	if value == nil {
		return nil
	}
	return *value
}

func truncate(value string, max int) string {
	runes := []rune(strings.ToValidUTF8(value, ""))
	if len(runes) <= max {
		return string(runes)
	}
	return string(runes[:max])
}

var _ database.Executor = (*sql.Tx)(nil)
