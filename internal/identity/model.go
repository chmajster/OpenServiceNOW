package identity

import (
	"errors"
	"time"
)

var (
	ErrNotFound           = errors.New("identity not found")
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrUnauthenticated    = errors.New("unauthenticated")
	ErrLastPlatformAdmin = errors.New("tenant must retain at least one active platform administrator")
)

type User struct {
	ID           uint64
	Email        string
	DisplayName  string
	PasswordHash string
}

type Tenant struct {
	ID   uint64
	Slug string
	Name string
}

type SessionIdentity struct {
	SessionID   uint64
	UserID      uint64
	TenantID    uint64
	Email       string
	DisplayName string
	TenantSlug  string
	TenantName  string
	CSRFHash    [32]byte
	ExpiresAt   time.Time
}

type LoginInput struct {
	Tenant   string
	Email    string
	Password string
}

type RequestMeta struct {
	RequestID string
	IPAddress string
	UserAgent string
}

type LoginResult struct {
	Identity     SessionIdentity
	SessionToken string
	CSRFToken    string
}
