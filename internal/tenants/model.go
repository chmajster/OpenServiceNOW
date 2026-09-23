package tenants

import (
	"errors"
	"time"
)

var (
	ErrNotFound = errors.New("tenant not found")
	ErrConflict = errors.New("tenant version conflict")
	ErrInvalid  = errors.New("invalid tenant input")
)

type Tenant struct {
	ID        uint64    `json:"id,string"`
	Slug      string    `json:"slug"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	Version   uint64    `json:"version"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Actor struct {
	TenantID  uint64
	UserID    uint64
	RequestID string
	IPAddress string
}
