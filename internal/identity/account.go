package identity

import (
	"context"
	"crypto/subtle"
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

// UserForUpdate is the shared serialization point for global credentials and
// session creation. Callers lock their current tenant before this user row.
func (r *MySQLRepository) UserForUpdate(ctx context.Context, exec platformdb.Executor, userID uint64) (User, error) {
	var user User
	err := exec.QueryRowContext(ctx, `SELECT id,email,display_name,password_hash,version,credential_version,password_change_required
		FROM users WHERE id=? AND active=TRUE FOR UPDATE`, userID).Scan(
		&user.ID, &user.Email, &user.DisplayName, &user.PasswordHash, &user.Version, &user.CredentialVersion, &user.PasswordChangeRequired)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrUnauthenticated
	}
	return user, err
}

func (r *MySQLRepository) ValidateLoginForUpdate(ctx context.Context, exec platformdb.Executor, tenantID, userID, credentialVersion uint64) (User, error) {
	status, err := r.TenantStatusForUpdate(ctx, exec, tenantID)
	if err != nil {
		return User{}, err
	}
	if status != "ACTIVE" {
		return User{}, ErrInvalidCredentials
	}
	user, err := r.UserForUpdate(ctx, exec, userID)
	if errors.Is(err, ErrUnauthenticated) {
		return User{}, ErrInvalidCredentials
	}
	if err != nil {
		return User{}, err
	}
	if user.CredentialVersion != credentialVersion {
		return User{}, ErrInvalidCredentials
	}
	var active bool
	err = exec.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM tenant_memberships WHERE tenant_id=? AND user_id=? AND active=TRUE)`, tenantID, userID).Scan(&active)
	if err != nil {
		return User{}, err
	}
	if !active {
		return User{}, ErrInvalidCredentials
	}
	return user, nil
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
		var csrf []byte
		err = exec.QueryRowContext(ctx, `SELECT s.csrf_hash FROM sessions s
			JOIN tenant_memberships tm ON tm.tenant_id=s.tenant_id AND tm.user_id=s.user_id AND tm.active=TRUE
			WHERE s.id=? AND s.user_id=? AND s.tenant_id=? AND s.revoked_at IS NULL AND s.expires_at>UTC_TIMESTAMP(6) FOR UPDATE`,
			identity.SessionID, identity.UserID, identity.TenantID).Scan(&csrf)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrUnauthenticated
		}
		if err != nil {
			return err
		}
		if subtle.ConstantTimeCompare(csrf, identity.CSRFHash[:]) != 1 {
			return ErrUnauthenticated
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
		if _, err := exec.ExecContext(ctx, `UPDATE users SET display_name=?,version=version+1 WHERE id=?`, name, identity.UserID); err != nil {
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
		if _, err := exec.ExecContext(ctx, `UPDATE users SET password_hash=?,credential_version=credential_version+1,version=version+1,password_change_required=FALSE WHERE id=?`, hash, identity.UserID); err != nil {
			return err
		}
		if _, err := exec.ExecContext(ctx, `UPDATE sessions SET revoked_at=UTC_TIMESTAMP(6) WHERE user_id=? AND id<>? AND revoked_at IS NULL`, identity.UserID, identity.SessionID); err != nil {
			return err
		}
		if _, err := exec.ExecContext(ctx, `UPDATE sessions SET token_hash=?,csrf_hash=?,expires_at=? WHERE id=? AND user_id=?`, tokenHash[:], csrfHash[:], result.Identity.ExpiresAt, identity.SessionID, identity.UserID); err != nil {
			return err
		}
		result.Identity.Version = current.Version + 1
		result.Identity.CredentialVersion = current.CredentialVersion + 1
		result.Identity.PasswordChangeRequired = false
		result.Identity.DisplayName = current.DisplayName
		return s.accountAudit(ctx, exec, identity, meta, "account.password.change", "user", identity.UserID, nil, map[string]any{"other_sessions_revoked": true, "credential_version": fmt.Sprint(current.CredentialVersion + 1)})
	})
	if err != nil {
		return LoginResult{}, err
	}
	return result, nil
}

func (s *Service) ListOwnSessions(ctx context.Context, identity SessionIdentity, afterID uint64, limit int) (AccountSessions, error) {
	if limit < 1 || limit > 100 {
		return AccountSessions{}, ErrAccountInvalid
	}
	rows, err := s.repo.db.QueryContext(ctx, `SELECT s.id,s.tenant_id,t.slug,t.name,s.created_at,s.expires_at,COALESCE(s.ip_address,''),COALESCE(s.user_agent,'')
		FROM sessions s JOIN tenants t ON t.id=s.tenant_id AND t.status='ACTIVE'
		JOIN tenant_memberships tm ON tm.tenant_id=s.tenant_id AND tm.user_id=s.user_id AND tm.active=TRUE
		WHERE s.user_id=? AND s.revoked_at IS NULL AND s.expires_at>UTC_TIMESTAMP(6) AND s.id>? ORDER BY s.id LIMIT ?`, identity.UserID, afterID, limit+1)
	if err != nil {
		return AccountSessions{}, err
	}
	defer rows.Close()
	page := AccountSessions{Sessions: []AccountSession{}}
	for rows.Next() {
		var item AccountSession
		if err := rows.Scan(&item.ID, &item.TenantID, &item.TenantSlug, &item.TenantName, &item.CreatedAt, &item.ExpiresAt, &item.IPAddress, &item.UserAgent); err != nil {
			return AccountSessions{}, err
		}
		item.Current = item.ID == identity.SessionID
		page.Sessions = append(page.Sessions, item)
	}
	if err := rows.Err(); err != nil {
		return AccountSessions{}, err
	}
	if len(page.Sessions) > limit {
		page.Sessions = page.Sessions[:limit]
		page.NextAfterID = page.Sessions[limit-1].ID
	}
	return page, nil
}

// A zero target means all other sessions; ownership always comes from identity.
func (s *Service) RevokeOwnSessions(ctx context.Context, identity SessionIdentity, target uint64, meta RequestMeta) error {
	return s.accountTx(ctx, identity, func(exec platformdb.Executor, user User) error {
		if user.PasswordChangeRequired {
			return ErrAccountInvalid
		}
		query := `UPDATE sessions SET revoked_at=UTC_TIMESTAMP(6) WHERE user_id=? AND revoked_at IS NULL AND id<>?`
		args := []any{identity.UserID, identity.SessionID}
		if target != 0 {
			var owned bool
			if err := exec.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sessions WHERE id=? AND user_id=?)`, target, identity.UserID).Scan(&owned); err != nil {
				return err
			}
			if !owned {
				return ErrNotFound
			}
			query = `UPDATE sessions SET revoked_at=UTC_TIMESTAMP(6) WHERE user_id=? AND revoked_at IS NULL AND id=?`
			args[1] = target
		}
		result, err := exec.ExecContext(ctx, query, args...)
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
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
