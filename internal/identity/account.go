package identity

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/chmajster/OpenServiceNOW/internal/audit"
	platformdb "github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/platform/password"
)

type ProfileInput struct {
	DisplayName string `json:"display_name"`
	Version     uint64 `json:"version,string"`
}

type PasswordInput struct {
	CurrentPassword   string `json:"current_password"`
	NewPassword       string `json:"new_password"`
	CredentialVersion uint64 `json:"credential_version,string"`
}

type AccountSession struct {
	ID         uint64    `json:"id,string"`
	Current    bool      `json:"current"`
	TenantID   uint64    `json:"tenant_id,string"`
	TenantSlug string    `json:"tenant_slug"`
	TenantName string    `json:"tenant_name"`
	CreatedAt  time.Time `json:"created_at"`
	ExpiresAt  time.Time `json:"expires_at"`
	IPAddress  string    `json:"ip_address"`
	UserAgent  string    `json:"user_agent"`
}

type AccountSessions struct {
	Sessions    []AccountSession `json:"sessions"`
	NextAfterID uint64           `json:"next_after_id,string,omitempty"`
}

func (s *Service) accountTx(ctx context.Context, identity SessionIdentity, action func(platformdb.Executor, User) error) error {
	return s.tx.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(exec platformdb.Executor) error {
		status, err := s.repo.TenantStatusForUpdate(ctx, exec, identity.TenantID)
		if err != nil {
			return err
		}
		if status != "ACTIVE" {
			return ErrUnauthenticated
		}
		user, err := s.repo.UserForUpdate(ctx, exec, identity.UserID)
		if err != nil {
			return err
		}
		if err := s.repo.CheckAccountSession(ctx, exec, identity); err != nil {
			return err
		}
		return action(exec, user)
	})
}

// HashPassword shares the bounded Argon2 pool with login and account operations.
func (s *Service) HashPassword(plain string) (string, error) {
	var hash string
	err := s.withPasswordCheck(func() error { var err error; hash, err = password.Hash(plain); return err })
	return hash, err
}

func (s *Service) UpdateProfile(ctx context.Context, identity SessionIdentity, input ProfileInput, meta RequestMeta) error {
	name := strings.TrimSpace(input.DisplayName)
	if !utf8.ValidString(name) || utf8.RuneCountInString(name) < 1 || utf8.RuneCountInString(name) > 200 || input.Version == 0 {
		return fmt.Errorf("%w: display name must contain between 1 and 200 characters", ErrAccountInvalid)
	}
	return s.accountTx(ctx, identity, func(exec platformdb.Executor, user User) error {
		if user.PasswordChangeRequired {
			return ErrAccountInvalid
		}
		if user.Version != input.Version {
			return ErrAccountConflict
		}
		if user.DisplayName == name {
			return nil
		}
		if err := s.repo.UpdateAccountName(ctx, exec, identity.UserID, name); err != nil {
			return err
		}
		return s.accountAudit(ctx, exec, identity, meta, "account.profile.update", "user", identity.UserID,
			map[string]any{"display_name": user.DisplayName}, map[string]any{"display_name": name})
	})
}

func (s *Service) ChangePassword(ctx context.Context, identity SessionIdentity, input PasswordInput, meta RequestMeta) (LoginResult, error) {
	if err := password.Validate(input.NewPassword); err != nil {
		return LoginResult{}, fmt.Errorf("%w: %v", ErrAccountInvalid, err)
	}
	if input.CurrentPassword == "" || len(input.CurrentPassword) > password.MaxPasswordBytes || input.NewPassword == input.CurrentPassword || input.CredentialVersion == 0 {
		return LoginResult{}, fmt.Errorf("%w: new password must differ from the current password", ErrAccountInvalid)
	}
	user, _, err := s.repo.FindForLogin(ctx, identity.TenantSlug, identity.Email)
	if err != nil {
		return LoginResult{}, err
	}
	if user.CredentialVersion != input.CredentialVersion {
		return LoginResult{}, ErrAccountConflict
	}
	var hash string
	err = s.withPasswordCheck(func() error {
		valid, err := password.Verify(user.PasswordHash, input.CurrentPassword)
		if err != nil {
			return err
		}
		if !valid {
			return ErrInvalidCredentials
		}
		hash, err = password.Hash(input.NewPassword)
		return err
	})
	if err != nil {
		return LoginResult{}, err
	}
	token, tokenHash, err := newToken()
	if err != nil {
		return LoginResult{}, err
	}
	csrf, csrfHash, err := newToken()
	if err != nil {
		return LoginResult{}, err
	}
	result := LoginResult{Identity: identity, SessionToken: token, CSRFToken: csrf}
	result.Identity.CSRFHash = csrfHash
	result.Identity.ExpiresAt = s.now().Add(s.session.TTL)
	err = s.accountTx(ctx, identity, func(exec platformdb.Executor, current User) error {
		if current.CredentialVersion != input.CredentialVersion || current.CredentialVersion != user.CredentialVersion {
			return ErrAccountConflict
		}
		if err := s.repo.UpdateAccountPassword(ctx, exec, identity.UserID, hash); err != nil {
			return err
		}
		if _, err := s.repo.RevokeAccountSessions(ctx, exec, identity, 0); err != nil {
			return err
		}
		if err := s.repo.RotateAccountSession(ctx, exec, identity, tokenHash, csrfHash, result.Identity.ExpiresAt); err != nil {
			return err
		}
		result.Identity.Version = current.Version + 1
		result.Identity.CredentialVersion = current.CredentialVersion + 1
		result.Identity.PasswordChangeRequired = false
		result.Identity.DisplayName = current.DisplayName
		return s.accountAudit(ctx, exec, identity, meta, "account.password.change", "user", identity.UserID, nil, map[string]any{"other_sessions_revoked": true, "credential_version": fmt.Sprint(current.CredentialVersion + 1)})
	})
	if err != nil {
		if errors.Is(err, platformdb.ErrCommitUncertain) {
			return LoginResult{}, ErrAccountResultUnknown
		}
		return LoginResult{}, err
	}
	return result, nil
}

func (s *Service) RevokeOwnSessions(ctx context.Context, identity SessionIdentity, target uint64, meta RequestMeta) error {
	return s.accountTx(ctx, identity, func(exec platformdb.Executor, user User) error {
		if user.PasswordChangeRequired {
			return ErrAccountInvalid
		}
		count, err := s.repo.RevokeAccountSessions(ctx, exec, identity, target)
		if err != nil {
			return err
		}
		if count == 0 {
			return nil
		}
		return s.accountAudit(ctx, exec, identity, meta, "account.sessions.revoke", "session", target, nil, map[string]any{"revoked_count": fmt.Sprint(count), "all_other_sessions": target == 0})
	})
}

func (s *Service) accountAudit(ctx context.Context, exec platformdb.Executor, identity SessionIdentity, meta RequestMeta, action, object string, id uint64, old, newValues any) error {
	return s.audit.Append(ctx, exec, audit.Event{TenantID: &identity.TenantID, ActorUserID: &identity.UserID, Action: action, ObjectType: object, ObjectID: fmt.Sprint(id), OldValues: old, NewValues: newValues, RequestID: meta.RequestID, Source: "api", IPAddress: meta.IPAddress})
}

func (s *Service) ListOwnSessions(ctx context.Context, identity SessionIdentity, afterID uint64, limit int) (AccountSessions, error) {
	if limit < 1 || limit > 100 {
		return AccountSessions{}, ErrAccountInvalid
	}
	return s.repo.ListOwnSessions(ctx, identity, afterID, limit)
}
