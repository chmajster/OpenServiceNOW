package jobs

import (
	"encoding/json"
	"errors"
	"time"
)

type Status string

const (
	StatusQueued    Status = "QUEUED"
	StatusRunning   Status = "RUNNING"
	StatusRetrying  Status = "RETRYING"
	StatusCompleted Status = "COMPLETED"
	StatusFailed    Status = "FAILED"
	StatusCancelled Status = "CANCELLED"
)

var (
	ErrInvalid   = errors.New("invalid job input")
	ErrNotFound  = errors.New("job not found")
	ErrLeaseLost = errors.New("job lease lost")
)

type Job struct {
	ID              uint64
	TenantID        uint64
	Type            string
	Status          Status
	Payload         json.RawMessage
	Result          json.RawMessage
	ErrorMessage    string
	Progress        uint8
	AttemptCount    uint32
	MaxAttempts     uint32
	AvailableAt     time.Time
	LeaseOwner      string
	LeaseExpiresAt  *time.Time
	CancelRequested bool
	IdempotencyKey  string
	CreatedBy       *uint64
	CreatedAt       time.Time
	UpdatedAt       time.Time
	StartedAt       *time.Time
	FinishedAt      *time.Time
}

type EnqueueInput struct {
	TenantID       uint64
	Type           string
	Payload        json.RawMessage
	MaxAttempts    uint32
	AvailableAt    time.Time
	IdempotencyKey string
	CreatedBy      *uint64
}

type HeartbeatState struct {
	LeaseValid      bool
	CancelRequested bool
}

type LogEntry struct {
	ID            uint64          `json:"id,string"`
	JobID         uint64          `json:"job_id,string"`
	AttemptNumber uint32          `json:"attempt_number"`
	Level         string          `json:"level"`
	Message       string          `json:"message"`
	Fields        json.RawMessage `json:"fields,omitempty"`
	CreatedAt     time.Time       `json:"created_at"`
}

type LogPage struct {
	Entries      []LogEntry `json:"entries"`
	NextBeforeID uint64     `json:"next_before_id,string,omitempty"`
}
