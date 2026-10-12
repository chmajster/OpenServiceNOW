package identity

import (
	"errors"
	"time"
)

var (
	ErrNotFound              = errors.New("identity not found")
	ErrInvalidCredentials    = errors.New("invalid credentials")
	ErrUnauthenticated       = errors.New("unauthenticated")
	ErrLastPlatformAdmin     = errors.New("tenant must retain at least one active platform administrator")
	ErrPlatformAdminRequired = errors.New("platform administrator authority required")
	ErrAuthBusy              = errors.New("authentication password verifier is busy")
	ErrSetupCompleted        = errors.New("installation is already complete")
	ErrSetupInvalid          = errors.New("invalid installation settings")
	ErrAccountInvalid        = errors.New("invalid account settings")
	ErrAccountConflict       = errors.New("account version conflict")
	ErrAccountResultUnknown  = errors.New("account transaction result is unknown")
)

type User struct {
	ID                     uint64
	Email                  string
	DisplayName            string
	PasswordHash           string
	Version                uint64
	CredentialVersion      uint64
	PasswordChangeRequired bool
}

type Tenant struct {
	ID   uint64
	Slug string
	Name string
}

type SessionIdentity struct {
	SessionID              uint64
	UserID                 uint64
	TenantID               uint64
	Email                  string
	DisplayName            string
	TenantSlug             string
	TenantName             string
	CSRFHash               [32]byte
	ExpiresAt              time.Time
	Version                uint64
	CredentialVersion      uint64
	PasswordChangeRequired bool
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
