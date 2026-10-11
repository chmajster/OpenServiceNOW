package jobs

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
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
	if input.Type == "" || len(input.Type) > 120 || len(input.IdempotencyKey) > 190 ||
		input.MaxAttempts > 100 || (input.TenantID == 0 && input.CreatedBy != nil) {
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
	if input.CreatedBy != nil {
		var found int
		err := r.db.QueryRowContext(ctx, `
			SELECT 1
			FROM tenant_memberships tm
			JOIN users u ON u.id = tm.user_id
			WHERE tm.tenant_id = ?
			  AND tm.user_id = ?
			  AND tm.active = TRUE
			  AND u.active = TRUE
		`, input.TenantID, *input.CreatedBy).Scan(&found)
		if errors.Is(err, sql.ErrNoRows) {
			return Job{}, false, ErrInvalid
		}
		if err != nil {
			return Job{}, false, fmt.Errorf("validate job creator: %w", err)
		}
	}

	scope := scopeKey(input.TenantID)
	tenantValue := nullableTenant(input.TenantID)
	var id uint64
	created := true
	if input.IdempotencyKey == "" {
		result, err := r.db.ExecContext(ctx, `
			INSERT INTO jobs (
				tenant_id, scope_key, job_type, payload, max_attempts, available_at, created_by
			) VALUES (?, ?, ?, ?, ?, ?, ?)
		`, tenantValue, scope, input.Type, []byte(input.Payload), input.MaxAttempts, input.AvailableAt.UTC(), nullableUint64(input.CreatedBy))
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
			INSERT INTO jobs (
				tenant_id, scope_key, job_type, payload, max_attempts, available_at, idempotency_key, created_by
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			ON DUPLICATE KEY UPDATE id = LAST_INSERT_ID(id)
		`, tenantValue, scope, input.Type, []byte(input.Payload), input.MaxAttempts, input.AvailableAt.UTC(), input.IdempotencyKey, nullableUint64(input.CreatedBy))
		if err != nil {
			return Job{}, false, fmt.Errorf("enqueue idempotent job: %w", err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return Job{}, false, fmt.Errorf("read enqueue result: %w", err)
		}
		created = affected == 1
		lastID, err := result.LastInsertId()
		if err != nil {
			return Job{}, false, fmt.Errorf("read enqueued job id: %w", err)
		}
		id = uint64(lastID)
	}
	job, err := r.Get(ctx, input.TenantID, id)
	return job, created, err
}

func (r *Repository) Get(ctx context.Context, tenantID, id uint64) (Job, error) {
	if id == 0 {
		return Job{}, ErrInvalid
	}
	job, err := scanJob(r.db.QueryRowContext(ctx, jobSelect+` WHERE scope_key = ? AND id = ?`, scopeKey(tenantID), id))
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
	var previousAttempt, maxAttempts uint32
	err = tx.QueryRowContext(ctx, `
		SELECT id, status, attempt_count, max_attempts
		FROM jobs
		WHERE cancel_requested = FALSE
		  AND available_at <= UTC_TIMESTAMP(6)
		  AND (
			(status IN ('QUEUED','RETRYING') AND attempt_count < max_attempts)
			OR (status = 'RUNNING' AND lease_expires_at < UTC_TIMESTAMP(6))
		  )
		ORDER BY available_at, id
		LIMIT 1
		FOR UPDATE SKIP LOCKED
	`).Scan(&id, &previousStatus, &previousAttempt, &maxAttempts)
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
		if previousAttempt >= maxAttempts {
			if _, err := tx.ExecContext(ctx, `
				UPDATE jobs
				SET status = 'FAILED',
				    error_message = 'worker lease expired after maximum attempts',
				    lease_owner = NULL,
				    lease_expires_at = NULL,
				    finished_at = ?,
				    updated_at = ?
				WHERE id = ?
			`, now, now, id); err != nil {
				return Job{}, false, fmt.Errorf("fail exhausted expired job: %w", err)
			}
			if err := tx.Commit(); err != nil {
				return Job{}, false, fmt.Errorf("commit exhausted expired job: %w", err)
			}
			return Job{}, false, nil
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
	return r.finish(ctx, id, owner, StatusCompleted, resultJSON, "")
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
	return r.finish(ctx, id, owner, StatusCancelled, nil, "cancelled")
}

func (r *Repository) RequestCancel(ctx context.Context, tenantID, id uint64) error {
	if id == 0 {
		return ErrInvalid
	}
	result, err := r.db.ExecContext(ctx, `
		UPDATE jobs
		SET cancel_requested = TRUE,
		    finished_at = CASE WHEN status IN ('QUEUED','RETRYING') THEN UTC_TIMESTAMP(6) ELSE finished_at END,
		    status = CASE WHEN status IN ('QUEUED','RETRYING') THEN 'CANCELLED' ELSE status END,
		    updated_at = UTC_TIMESTAMP(6)
		WHERE scope_key = ?
		  AND id = ?
		  AND status IN ('QUEUED','RETRYING','RUNNING')
	`, scopeKey(tenantID), id)
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

func (r *Repository) UpdateProgress(ctx context.Context, id uint64, owner string, progress uint8) error {
	if id == 0 || owner == "" || progress > 100 {
		return ErrInvalid
	}
	result, err := r.db.ExecContext(ctx, `
		UPDATE jobs
		SET progress = ?, updated_at = UTC_TIMESTAMP(6)
		WHERE id = ? AND status = 'RUNNING' AND lease_owner = ?
	`, progress, id, owner)
	if err != nil {
		return fmt.Errorf("update job progress: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read progress update result: %w", err)
	}
	if affected != 1 {
		return ErrLeaseLost
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
) error {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return fmt.Errorf("begin job completion: %w", err)
	}
	defer tx.Rollback()

	var attempt uint32
	var cancelRequested bool
	if err := tx.QueryRowContext(ctx, `
		SELECT attempt_count, cancel_requested
		FROM jobs
		WHERE id = ? AND status = 'RUNNING' AND lease_owner = ?
		FOR UPDATE
	`, id, owner).Scan(&attempt, &cancelRequested); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrLeaseLost
		}
		return fmt.Errorf("lock running job: %w", err)
	}

	if status == StatusCompleted && cancelRequested {
		status = StatusCancelled
		resultJSON = nil
		message = "cancelled"
	}
	progress := uint8(0)
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

func (r *Repository) AppendLog(
	ctx context.Context,
	jobID uint64,
	owner, level, message string,
	fields json.RawMessage,
) error {
	owner = strings.TrimSpace(owner)
	level = strings.ToUpper(strings.TrimSpace(level))
	message = strings.TrimSpace(message)
	if jobID == 0 || owner == "" || message == "" || len([]rune(message)) > 1000 {
		return ErrInvalid
	}
	switch level {
	case "DEBUG", "INFO", "WARN", "ERROR":
	default:
		return ErrInvalid
	}
	var fieldsValue any
	if len(fields) > 0 {
		if !json.Valid(fields) {
			return ErrInvalid
		}
		fieldsValue = []byte(fields)
	}
	result, err := r.db.ExecContext(ctx, `
		INSERT INTO job_logs (job_id, attempt_number, level, message, fields)
		SELECT id, attempt_count, ?, ?, ?
		FROM jobs
		WHERE id = ? AND status = 'RUNNING' AND lease_owner = ?
	`, level, message, fieldsValue, jobID, owner)
	if err != nil {
		return fmt.Errorf("append job log: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read job log append result: %w", err)
	}
	if affected != 1 {
		return ErrLeaseLost
	}
	return nil
}

func (r *Repository) ListLogs(
	ctx context.Context,
	tenantID, jobID, beforeID uint64,
	limit int,
) (LogPage, error) {
	if jobID == 0 || limit < 1 || limit > 500 {
		return LogPage{}, ErrInvalid
	}
	query := `
		SELECT jl.id, jl.job_id, jl.attempt_number, jl.level, jl.message, jl.fields, jl.created_at
		FROM job_logs jl
		JOIN jobs j ON j.id = jl.job_id
		WHERE j.scope_key = ? AND j.id = ?
	`
	args := []any{scopeKey(tenantID), jobID}
	if beforeID > 0 {
		query += " AND jl.id < ?"
		args = append(args, beforeID)
	}
	query += " ORDER BY jl.id DESC LIMIT ?"
	args = append(args, limit+1)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return LogPage{}, fmt.Errorf("list job logs: %w", err)
	}
	defer rows.Close()
	entries := make([]LogEntry, 0, limit+1)
	for rows.Next() {
		var item LogEntry
		var fields []byte
		if err := rows.Scan(
			&item.ID, &item.JobID, &item.AttemptNumber, &item.Level,
			&item.Message, &fields, &item.CreatedAt,
		); err != nil {
			return LogPage{}, fmt.Errorf("scan job log: %w", err)
		}
		if len(fields) > 0 {
			item.Fields = append(json.RawMessage(nil), fields...)
		}
		entries = append(entries, item)
	}
	if err := rows.Err(); err != nil {
		return LogPage{}, fmt.Errorf("iterate job logs: %w", err)
	}
	page := LogPage{Entries: entries}
	if len(entries) > limit {
		page.Entries = entries[:limit]
		page.NextBeforeID = page.Entries[len(page.Entries)-1].ID
	}
	return page, nil
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
	var tenantID, createdBy sql.Null[uint64]
	var payload, result []byte
	var leaseExpiresAt, startedAt, finishedAt sql.NullTime
	if err := row.Scan(
		&job.ID,
		&tenantID,
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
	if tenantID.Valid {
		job.TenantID = tenantID.V
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
		value := createdBy.V
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
