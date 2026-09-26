package events

import (
	"encoding/json"
	"errors"
	"time"
)

type Status string

const (
	StatusPending Status = "PENDING"
	StatusDelivering Status = "DELIVERING"
	StatusDelivered Status = "DELIVERED"
	StatusFailed Status = "FAILED"
)

var (
	ErrInvalid = errors.New("invalid event input")
	ErrNotFound = errors.New("event not found")
	ErrLeaseLost = errors.New("event lease lost")
)

type Event struct {
	ID uint64
	TenantID uint64
	Type string
	AggregateType string
	AggregateID string
	Payload json.RawMessage
	Status Status
	AttemptCount uint32
	MaxAttempts uint32
	AvailableAt time.Time
	LeaseOwner string
	LeaseExpiresAt *time.Time
	LastError string
	IdempotencyKey string
	CreatedAt time.Time
	DeliveredAt *time.Time
}

type Input struct {
	TenantID uint64
	Type string
	AggregateType string
	AggregateID string
	Payload json.RawMessage
	MaxAttempts uint32
	AvailableAt time.Time
	IdempotencyKey string
}

type HeartbeatState struct {
	LeaseValid bool
}
