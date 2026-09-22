package identity

import (
	"context"
	"time"
)

type AuthService interface {
	Login(context.Context, LoginInput, RequestMeta) (LoginResult, error)
	Authenticate(context.Context, string) (SessionIdentity, error)
	VerifyCSRF(SessionIdentity, string) bool
	Logout(context.Context, SessionIdentity, RequestMeta) error
	Permissions(context.Context, SessionIdentity) ([]string, error)
	HasPermission(context.Context, SessionIdentity, string) (bool, error)
}

type LoginLimiter interface {
	Allow(context.Context, string, int64, time.Duration) (bool, error)
}
