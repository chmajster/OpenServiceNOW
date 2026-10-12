package events

import (
	"encoding/json"
	"errors"
	"time"
)

type Status string

const (
	StatusPending    Status = "PENDING"
	StatusDelivering Status = "DELIVERING"
	StatusDelivered  Status = "DELIVERED"
	StatusFailed     Status = "FAILED"
)

var (
	ErrInvalid   = errors.New("invalid event input")
	ErrNotFound  = errors.New("event not found")
	ErrLeaseLost = errors.New("event lease lost")
	ErrConflict  = errors.New("event delivery changed")
)

type Event struct {
	ID                   uint64
	TenantID             uint64
	Type                 string
	Version              uint32
	AggregateType        string
	AggregateID          string
	Payload              json.RawMessage
	Status               Status
	AttemptCount         uint32
	MaxAttempts          uint32
	AvailableAt          time.Time
	LeaseOwner           string
	LeaseExpiresAt       *time.Time
	LastError            string
	IdempotencyKey       string
	CreatedAt            time.Time
	DeliveredAt          *time.Time
	NotificationEligible bool
}

type Delivery struct {
	ID           uint64     `json:"id,string"`
	EventID      uint64     `json:"event_id,string"`
	ConsumerID   string     `json:"consumer_id"`
	Status       string     `json:"status"`
	AttemptCount uint32     `json:"attempt_count"`
	MaxAttempts  uint32     `json:"max_attempts"`
	LastError    string     `json:"last_error"`
	CreatedAt    time.Time  `json:"created_at"`
	FinishedAt   *time.Time `json:"finished_at,omitempty"`
	Event        Event      `json:"-"`
}

type Input struct {
	TenantID       uint64
	Type           string
	Version        uint32
	AggregateType  string
	AggregateID    string
	Payload        json.RawMessage
	MaxAttempts    uint32
	AvailableAt    time.Time
	IdempotencyKey string
}

type HeartbeatState struct {
	LeaseValid bool
}
