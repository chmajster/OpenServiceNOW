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

const tokenEncodedLength = 43

type Service struct {
	repo           *MySQLRepository
	tx             *platformdb.TxManager
	audit          *audit.MySQLRepository
	session        config.SessionConfig
	dummyHash      string
	passwordChecks chan struct{}
	now            func() time.Time
}

func NewService(
	repo *MySQLRepository,
	tx *platformdb.TxManager,
	auditRepo *audit.MySQLRepository,
	session config.SessionConfig,
	auth config.AuthConfig,
) (*Service, error) {
	if auth.MaxConcurrentPasswordChecks < 1 {
		return nil, errors.New("MaxConcurrentPasswordChecks must be at least 1")
	}
	dummyHash, err := password.Hash("openservicenow-dummy-password")
	if err != nil {
		return nil, fmt.Errorf("build dummy password hash: %w", err)
	}
	return &Service{
		repo:           repo,
		tx:             tx,
		audit:          auditRepo,
		session:        session,
		dummyHash:      dummyHash,
		passwordChecks: make(chan struct{}, auth.MaxConcurrentPasswordChecks),
		now:            func() time.Time { return time.Now().UTC() },
	}, nil
}

func (s *Service) Login(ctx context.Context, input LoginInput, meta RequestMeta) (LoginResult, error) {
	tenantSlug := strings.ToLower(strings.TrimSpace(input.Tenant))
	email := normalizeLoginEmail(input.Email)
	if tenantSlug == "" || email == "" || input.Password == "" {
		return LoginResult{}, ErrInvalidCredentials
	}
	if len(tenantSlug) > 100 || len(email) > 254 || len(input.Password) > password.MaxPasswordBytes {
		return LoginResult{}, ErrInvalidCredentials
	}

	user, tenant, err := s.repo.FindForLogin(ctx, tenantSlug, email)
	if errors.Is(err, ErrNotFound) {
		if err := s.withPasswordCheck(func() error {
			_, verifyErr := password.Verify(s.dummyHash, input.Password)
			return verifyErr
		}); err != nil {
			return LoginResult{}, err
		}
		return LoginResult{}, ErrInvalidCredentials
	}
	if err != nil {
		return LoginResult{}, err
	}

	var valid bool
	err = s.withPasswordCheck(func() error {
		var verifyErr error
		valid, verifyErr = password.Verify(user.PasswordHash, input.Password)
		return verifyErr
	})
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
		current, err := s.repo.ValidateLoginForUpdate(ctx, exec, tenant.ID, user.ID, user.CredentialVersion)
		if err != nil {
			return err
		}
		user = current
		var createErr error
		sessionID, createErr = s.repo.CreateSession(
			ctx, exec, tenant.ID, user.ID, sessionHash, csrfHash, expiresAt,
			meta.IPAddress, truncateText(meta.UserAgent, 512),
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
			Version:     user.Version, CredentialVersion: user.CredentialVersion, PasswordChangeRequired: user.PasswordChangeRequired,
		},
		SessionToken: sessionToken,
		CSRFToken:    csrfToken,
	}, nil
}

func (s *Service) Authenticate(ctx context.Context, sessionToken string) (SessionIdentity, error) {
	if len(sessionToken) != tokenEncodedLength {
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
	if len(token) != tokenEncodedLength {
		return false
	}
	hash := sha256.Sum256([]byte(token))
	return subtle.ConstantTimeCompare(hash[:], identity.CSRFHash[:]) == 1
}

func (s *Service) Logout(ctx context.Context, identity SessionIdentity, meta RequestMeta) error {
	err := s.accountTx(ctx, identity, func(exec platformdb.Executor, _ User) error {
		if err := s.repo.RevokeSession(ctx, exec, identity.SessionID, identity.TenantID, identity.UserID); err != nil {
			if errors.Is(err, ErrNotFound) {
				return nil
			}
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
	if errors.Is(err, ErrUnauthenticated) {
		return nil
	}
	return err
}

func (s *Service) Permissions(ctx context.Context, identity SessionIdentity) ([]string, error) {
	if identity.PasswordChangeRequired {
		return []string{}, nil
	}
	return s.repo.ListPermissions(ctx, identity.TenantID, identity.UserID)
}

func (s *Service) HasPermission(ctx context.Context, identity SessionIdentity, permission string) (bool, error) {
	if identity.PasswordChangeRequired {
		return false, nil
	}
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

func truncateText(value string, max int) string {
	runes := []rune(strings.ToValidUTF8(value, ""))
	if len(runes) <= max {
		return string(runes)
	}
	return string(runes[:max])
}

func (s *Service) withPasswordCheck(check func() error) error {
	select {
	case s.passwordChecks <- struct{}{}:
		defer func() { <-s.passwordChecks }()
		return check()
	default:
		return ErrAuthBusy
	}
}
