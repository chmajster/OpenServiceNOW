package identity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/chmajster/OpenServiceNOW/internal/audit"
	"github.com/chmajster/OpenServiceNOW/internal/platform/config"
	platformdb "github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/platform/password"
)

type Service struct {
	repo      *MySQLRepository
	tx        *platformdb.TxManager
	audit     *audit.MySQLRepository
	session   config.SessionConfig
	dummyHash string
	now       func() time.Time
}

func NewService(
	repo *MySQLRepository,
	tx *platformdb.TxManager,
	auditRepo *audit.MySQLRepository,
	session config.SessionConfig,
) (*Service, error) {
	dummyHash, err := password.Hash("openservicenow-dummy-password")
	if err != nil {
		return nil, fmt.Errorf("build dummy password hash: %w", err)
	}
	return &Service{
		repo:      repo,
		tx:        tx,
		audit:     auditRepo,
		session:   session,
		dummyHash: dummyHash,
		now:       func() time.Time { return time.Now().UTC() },
	}, nil
}

func (s *Service) Login(ctx context.Context, input LoginInput, meta RequestMeta) (LoginResult, error) {
	tenantSlug := strings.ToLower(strings.TrimSpace(input.Tenant))
	email := strings.ToLower(strings.TrimSpace(input.Email))
	if tenantSlug == "" || email == "" || input.Password == "" {
		return LoginResult{}, ErrInvalidCredentials
	}

	user, tenant, err := s.repo.FindForLogin(ctx, tenantSlug, email)
	if errors.Is(err, ErrNotFound) {
		_, _ = password.Verify(s.dummyHash, input.Password)
		return LoginResult{}, ErrInvalidCredentials
	}
	if err != nil {
		return LoginResult{}, err
	}

	valid, err := password.Verify(user.PasswordHash, input.Password)
	if err != nil {
		return LoginResult{}, fmt.Errorf("verify password hash: %w", err)
	}
	if !valid {
		return LoginResult{}, ErrInvalidCredentials
	}

	sessionToken, sessionHash, err := newToken()
	if err != nil {
		return LoginResult{}, err
	}
	csrfToken, csrfHash, err := newToken()
	if err != nil {
		return LoginResult{}, err
	}
	expiresAt := s.now().Add(s.session.TTL)

	var sessionID uint64
	err = s.tx.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(exec platformdb.Executor) error {
		var createErr error
		sessionID, createErr = s.repo.CreateSession(
			ctx, exec, tenant.ID, user.ID, sessionHash, csrfHash, expiresAt,
			meta.IPAddress, truncate(meta.UserAgent, 512),
		)
		if createErr != nil {
			return createErr
		}
		return s.audit.Append(ctx, exec, audit.Event{
			TenantID:    &tenant.ID,
			ActorUserID: &user.ID,
			Action:      "auth.login",
			ObjectType:  "user",
			ObjectID:    fmt.Sprint(user.ID),
			RequestID:   meta.RequestID,
			Source:      "api",
			IPAddress:   meta.IPAddress,
		})
	})
	if err != nil {
		return LoginResult{}, err
	}

	return LoginResult{
		Identity: SessionIdentity{
			SessionID:   sessionID,
			UserID:      user.ID,
			TenantID:    tenant.ID,
			Email:       user.Email,
			DisplayName: user.DisplayName,
			TenantSlug:  tenant.Slug,
			TenantName:  tenant.Name,
			CSRFHash:    csrfHash,
			ExpiresAt:   expiresAt,
		},
		SessionToken: sessionToken,
		CSRFToken:    csrfToken,
	}, nil
}

func (s *Service) Authenticate(ctx context.Context, sessionToken string) (SessionIdentity, error) {
	if sessionToken == "" {
		return SessionIdentity{}, ErrUnauthenticated
	}
	hash := sha256.Sum256([]byte(sessionToken))
	identity, err := s.repo.FindSession(ctx, hash)
	if errors.Is(err, ErrNotFound) {
		return SessionIdentity{}, ErrUnauthenticated
	}
	if err != nil {
		return SessionIdentity{}, err
	}
	return identity, nil
}

func (s *Service) VerifyCSRF(identity SessionIdentity, token string) bool {
	if token == "" {
		return false
	}
	hash := sha256.Sum256([]byte(token))
	return subtle.ConstantTimeCompare(hash[:], identity.CSRFHash[:]) == 1
}

func (s *Service) Logout(ctx context.Context, identity SessionIdentity, meta RequestMeta) error {
	return s.tx.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(exec platformdb.Executor) error {
		if err := s.repo.RevokeSession(ctx, exec, identity.SessionID, identity.TenantID, identity.UserID); err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}
		return s.audit.Append(ctx, exec, audit.Event{
			TenantID:    &identity.TenantID,
			ActorUserID: &identity.UserID,
			Action:      "auth.logout",
			ObjectType:  "session",
			ObjectID:    fmt.Sprint(identity.SessionID),
			RequestID:   meta.RequestID,
			Source:      "api",
			IPAddress:   meta.IPAddress,
		})
	})
}

func (s *Service) Permissions(ctx context.Context, identity SessionIdentity) ([]string, error) {
	return s.repo.ListPermissions(ctx, identity.TenantID, identity.UserID)
}

func (s *Service) HasPermission(ctx context.Context, identity SessionIdentity, permission string) (bool, error) {
	return s.repo.HasPermission(ctx, identity.TenantID, identity.UserID, permission)
}

func newToken() (string, [32]byte, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", [32]byte{}, fmt.Errorf("generate session token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	return token, sha256.Sum256([]byte(token)), nil
}

func truncate(value string, max int) string {
	if len(value) <= max {
		return value
	}
	return value[:max]
}
