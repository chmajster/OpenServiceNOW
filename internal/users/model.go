package users

import (
	"errors"
	"time"
)

var (
	ErrNotFound = errors.New("user membership not found")
	ErrConflict = errors.New("user conflict")
	ErrInvalid  = errors.New("invalid user input")
)

type User struct {
	ID               uint64    `json:"id"`
	Email            string    `json:"email"`
	DisplayName      string    `json:"display_name"`
	UserActive       bool      `json:"user_active"`
	MembershipActive bool      `json:"membership_active"`
	CreatedAt        time.Time `json:"created_at"`
}

type CreateInput struct {
	Email       string
	DisplayName string
	Password    string
}

type Actor struct {
	TenantID  uint64
	UserID    uint64
	RequestID string
	IPAddress string
}
