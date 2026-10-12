package identity

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"time"

	platformdb "github.com/chmajster/OpenServiceNOW/internal/platform/database"
)

func (r *MySQLRepository) LockMembershipUser(ctx context.Context, exec platformdb.Executor, tenantID, userID uint64) error {
	var id uint64
	err := exec.QueryRowContext(ctx, `SELECT id FROM users WHERE id=? AND EXISTS(SELECT 1 FROM tenant_memberships WHERE user_id=? AND tenant_id=?) FOR UPDATE`, userID, userID, tenantID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

// Callers lock the current tenant first. This row serializes credentials and login.
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

func (r *MySQLRepository) ListOwnSessions(ctx context.Context, identity SessionIdentity, afterID uint64, limit int) (AccountSessions, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT s.id,s.tenant_id,t.slug,t.name,s.created_at,s.expires_at,COALESCE(s.ip_address,''),COALESCE(s.user_agent,'')
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

func (r *MySQLRepository) CheckAccountSession(ctx context.Context, exec platformdb.Executor, identity SessionIdentity) error {
	var csrf []byte
	err := exec.QueryRowContext(ctx, `SELECT s.csrf_hash FROM sessions s
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
	return nil
}

// A zero target means all other sessions; ownership always comes from identity.
func (r *MySQLRepository) RevokeAccountSessions(ctx context.Context, exec platformdb.Executor, identity SessionIdentity, target uint64) (int64, error) {
	query := `UPDATE sessions SET revoked_at=UTC_TIMESTAMP(6) WHERE user_id=? AND revoked_at IS NULL AND id<>?`
	args := []any{identity.UserID, identity.SessionID}
	if target != 0 {
		var owned bool
		if err := exec.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sessions WHERE id=? AND user_id=?)`, target, identity.UserID).Scan(&owned); err != nil {
			return 0, err
		}
		if !owned {
			return 0, ErrNotFound
		}
		query = `UPDATE sessions SET revoked_at=UTC_TIMESTAMP(6) WHERE user_id=? AND revoked_at IS NULL AND id=?`
		args[1] = target
	}
	result, err := exec.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	return count, nil
}

func (r *MySQLRepository) UpdateAccountName(ctx context.Context, exec platformdb.Executor, userID uint64, name string) error {
	_, err := exec.ExecContext(ctx, `UPDATE users SET display_name=?,version=version+1 WHERE id=?`, name, userID)
	return err
}
func (r *MySQLRepository) UpdateAccountPassword(ctx context.Context, exec platformdb.Executor, userID uint64, hash string) error {
	_, err := exec.ExecContext(ctx, `UPDATE users SET password_hash=?,credential_version=credential_version+1,version=version+1,password_change_required=FALSE WHERE id=?`, hash, userID)
	return err
}
func (r *MySQLRepository) RotateAccountSession(ctx context.Context, exec platformdb.Executor, identity SessionIdentity, tokenHash, csrfHash [32]byte, expiresAt time.Time) error {
	_, err := exec.ExecContext(ctx, `UPDATE sessions SET token_hash=?,csrf_hash=?,expires_at=? WHERE id=? AND user_id=?`, tokenHash[:], csrfHash[:], expiresAt, identity.SessionID, identity.UserID)
	return err
}
