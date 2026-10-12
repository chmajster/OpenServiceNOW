package identity

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	platformdb "github.com/chmajster/OpenServiceNOW/internal/platform/database"
)

type MySQLRepository struct {
	db *sql.DB
}

func NewMySQLRepository(db *sql.DB) *MySQLRepository {
	return &MySQLRepository{db: db}
}

func (r *MySQLRepository) FindForLogin(ctx context.Context, tenantSlug, email string) (User, Tenant, error) {
	var user User
	var tenant Tenant
	err := r.db.QueryRowContext(ctx, `
		SELECT
			u.id, u.email, u.display_name, u.password_hash, u.version, u.credential_version, u.password_change_required,
			t.id, t.slug, t.name
		FROM users u
		JOIN tenant_memberships tm ON tm.user_id = u.id AND tm.active = TRUE
		JOIN tenants t ON t.id = tm.tenant_id AND t.status = 'ACTIVE'
		WHERE t.slug = ? AND u.email = ? AND u.active = TRUE
		LIMIT 1
	`, tenantSlug, email).Scan(
		&user.ID, &user.Email, &user.DisplayName, &user.PasswordHash,
		&user.Version, &user.CredentialVersion, &user.PasswordChangeRequired,
		&tenant.ID, &tenant.Slug, &tenant.Name,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, Tenant{}, ErrNotFound
	}
	if err != nil {
		return User{}, Tenant{}, fmt.Errorf("find login identity: %w", err)
	}
	return user, tenant, nil
}

func (r *MySQLRepository) CreateSession(
	ctx context.Context,
	exec platformdb.Executor,
	tenantID, userID uint64,
	tokenHash, csrfHash [32]byte,
	expiresAt time.Time,
	ipAddress, userAgent string,
) (uint64, error) {
	result, err := exec.ExecContext(ctx, `
		INSERT INTO sessions (
			tenant_id, user_id, token_hash, csrf_hash, expires_at, ip_address, user_agent
		) VALUES (?, ?, ?, ?, ?, NULLIF(?, ''), NULLIF(?, ''))
	`,
		tenantID, userID, tokenHash[:], csrfHash[:], expiresAt.UTC(), ipAddress, userAgent,
	)
	if err != nil {
		return 0, fmt.Errorf("create session: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("read session id: %w", err)
	}
	return uint64(id), nil
}

func (r *MySQLRepository) FindSession(ctx context.Context, tokenHash [32]byte) (SessionIdentity, error) {
	var identity SessionIdentity
	var csrf []byte
	err := r.db.QueryRowContext(ctx, `
		SELECT
			s.id, s.user_id, s.tenant_id, s.csrf_hash, s.expires_at,
			u.email, u.display_name, u.version, u.credential_version, u.password_change_required,
			t.slug, t.name
		FROM sessions s
		JOIN users u ON u.id = s.user_id AND u.active = TRUE
		JOIN tenant_memberships tm
			ON tm.tenant_id = s.tenant_id AND tm.user_id = s.user_id AND tm.active = TRUE
		JOIN tenants t ON t.id = s.tenant_id AND t.status = 'ACTIVE'
		WHERE s.token_hash = ?
		  AND s.revoked_at IS NULL
		  AND s.expires_at > UTC_TIMESTAMP(6)
		LIMIT 1
	`, tokenHash[:]).Scan(
		&identity.SessionID,
		&identity.UserID,
		&identity.TenantID,
		&csrf,
		&identity.ExpiresAt,
		&identity.Email,
		&identity.DisplayName,
		&identity.Version, &identity.CredentialVersion, &identity.PasswordChangeRequired,
		&identity.TenantSlug,
		&identity.TenantName,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return SessionIdentity{}, ErrNotFound
	}
	if err != nil {
		return SessionIdentity{}, fmt.Errorf("find session: %w", err)
	}
	if len(csrf) != len(identity.CSRFHash) {
		return SessionIdentity{}, fmt.Errorf("invalid stored CSRF hash length: %d", len(csrf))
	}
	copy(identity.CSRFHash[:], csrf)
	return identity, nil
}

func (r *MySQLRepository) RevokeSession(
	ctx context.Context,
	exec platformdb.Executor,
	sessionID, tenantID, userID uint64,
) error {
	result, err := exec.ExecContext(ctx, `
		UPDATE sessions
		SET revoked_at = UTC_TIMESTAMP(6)
		WHERE id = ? AND tenant_id = ? AND user_id = ? AND revoked_at IS NULL
	`, sessionID, tenantID, userID)
	if err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read revoked session result: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *MySQLRepository) RevokeUserSessions(
	ctx context.Context,
	exec platformdb.Executor,
	tenantID, userID uint64,
) (int64, error) {
	result, err := exec.ExecContext(ctx, `
		UPDATE sessions
		SET revoked_at = UTC_TIMESTAMP(6)
		WHERE tenant_id = ?
		  AND user_id = ?
		  AND revoked_at IS NULL
	`, tenantID, userID)
	if err != nil {
		return 0, fmt.Errorf("revoke user sessions: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("read revoked user sessions result: %w", err)
	}
	return affected, nil
}

func (r *MySQLRepository) ListPermissions(ctx context.Context, tenantID, userID uint64) ([]string, error) {
	return r.ListPermissionsInTx(ctx, r.db, tenantID, userID)
}

// Reuses permission SQL in a domain transaction, including a one-connection pool.
func (r *MySQLRepository) ListPermissionsInTx(ctx context.Context, exec platformdb.Executor, tenantID, userID uint64) ([]string, error) {
	rows, err := exec.QueryContext(ctx, `
		SELECT p.permission_key
		FROM permissions p
		WHERE EXISTS (
			SELECT 1
			FROM tenant_memberships tm
			JOIN users u ON u.id = tm.user_id AND u.active = TRUE
			WHERE tm.tenant_id = ?
			  AND tm.user_id = ?
			  AND tm.active = TRUE
			  AND (
				EXISTS (
					SELECT 1
					FROM user_role_assignments ura
					JOIN roles r ON r.id = ura.role_id AND r.tenant_id = ura.tenant_id
					WHERE ura.tenant_id = tm.tenant_id
					  AND ura.user_id = tm.user_id
					  AND r.name = 'platform_admin'
					  AND r.system_role = TRUE
				)
				OR EXISTS (
					SELECT 1
					FROM group_memberships gm
					JOIN group_role_assignments gra
					  ON gra.tenant_id = gm.tenant_id AND gra.group_id = gm.group_id
					JOIN roles r ON r.id = gra.role_id AND r.tenant_id = gra.tenant_id
					WHERE gm.tenant_id = tm.tenant_id
					  AND gm.user_id = tm.user_id
					  AND r.name = 'platform_admin'
					  AND r.system_role = TRUE
				)
				OR EXISTS (
					SELECT 1
					FROM user_role_assignments ura
					JOIN roles r ON r.id = ura.role_id AND r.tenant_id = ura.tenant_id
					JOIN role_permissions rp ON rp.role_id = r.id
					WHERE ura.tenant_id = tm.tenant_id
					  AND ura.user_id = tm.user_id
					  AND rp.permission_id = p.id
				)
				OR EXISTS (
					SELECT 1
					FROM group_memberships gm
					JOIN group_role_assignments gra
					  ON gra.tenant_id = gm.tenant_id AND gra.group_id = gm.group_id
					JOIN roles r ON r.id = gra.role_id AND r.tenant_id = gra.tenant_id
					JOIN role_permissions rp ON rp.role_id = r.id
					WHERE gm.tenant_id = tm.tenant_id
					  AND gm.user_id = tm.user_id
					  AND rp.permission_id = p.id
				)
			  )
		)
		ORDER BY p.permission_key
	`, tenantID, userID)
	if err != nil {
		return nil, fmt.Errorf("list permissions: %w", err)
	}
	defer rows.Close()

	permissions := make([]string, 0)
	for rows.Next() {
		var permission string
		if err := rows.Scan(&permission); err != nil {
			return nil, fmt.Errorf("scan permission: %w", err)
		}
		permissions = append(permissions, permission)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate permissions: %w", err)
	}
	return permissions, nil
}

func (r *MySQLRepository) HasPermission(ctx context.Context, tenantID, userID uint64, permission string) (bool, error) {
	var allowed bool
	err := r.db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM permissions p
			WHERE p.permission_key = ?
			  AND EXISTS (
				SELECT 1
				FROM tenant_memberships tm
				JOIN users u ON u.id = tm.user_id AND u.active = TRUE
				WHERE tm.tenant_id = ?
				  AND tm.user_id = ?
				  AND tm.active = TRUE
				  AND (
					EXISTS (
						SELECT 1
						FROM user_role_assignments ura
						JOIN roles r ON r.id = ura.role_id AND r.tenant_id = ura.tenant_id
						WHERE ura.tenant_id = tm.tenant_id
						  AND ura.user_id = tm.user_id
						  AND r.name = 'platform_admin'
						  AND r.system_role = TRUE
					)
					OR EXISTS (
						SELECT 1
						FROM group_memberships gm
						JOIN group_role_assignments gra
						  ON gra.tenant_id = gm.tenant_id AND gra.group_id = gm.group_id
						JOIN roles r ON r.id = gra.role_id AND r.tenant_id = gra.tenant_id
						WHERE gm.tenant_id = tm.tenant_id
						  AND gm.user_id = tm.user_id
						  AND r.name = 'platform_admin'
						  AND r.system_role = TRUE
					)
					OR EXISTS (
						SELECT 1
						FROM user_role_assignments ura
						JOIN roles r ON r.id = ura.role_id AND r.tenant_id = ura.tenant_id
						JOIN role_permissions rp ON rp.role_id = r.id
						WHERE ura.tenant_id = tm.tenant_id
						  AND ura.user_id = tm.user_id
						  AND rp.permission_id = p.id
					)
					OR EXISTS (
						SELECT 1
						FROM group_memberships gm
						JOIN group_role_assignments gra
						  ON gra.tenant_id = gm.tenant_id AND gra.group_id = gm.group_id
						JOIN roles r ON r.id = gra.role_id AND r.tenant_id = gra.tenant_id
						JOIN role_permissions rp ON rp.role_id = r.id
						WHERE gm.tenant_id = tm.tenant_id
						  AND gm.user_id = tm.user_id
						  AND rp.permission_id = p.id
					)
				  )
			  )
		)
	`, permission, tenantID, userID).Scan(&allowed)
	if err != nil {
		return false, fmt.Errorf("check permission: %w", err)
	}
	return allowed, nil
}

func (r *MySQLRepository) CleanupSessions(
	ctx context.Context,
	revokedBefore time.Time,
	limit int,
) (int64, error) {
	if limit < 1 || limit > 10000 {
		return 0, fmt.Errorf("cleanup session limit out of range")
	}

	expiredResult, err := r.db.ExecContext(ctx, `
		DELETE FROM sessions
		WHERE expires_at <= UTC_TIMESTAMP(6)
		ORDER BY expires_at, id
		LIMIT ?
	`, limit)
	if err != nil {
		return 0, fmt.Errorf("cleanup expired sessions: %w", err)
	}
	expired, err := expiredResult.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("read expired session cleanup result: %w", err)
	}
	if expired >= int64(limit) {
		return expired, nil
	}

	remaining := limit - int(expired)
	revokedResult, err := r.db.ExecContext(ctx, `
		DELETE FROM sessions
		WHERE revoked_at IS NOT NULL
		  AND revoked_at <= ?
		ORDER BY revoked_at, id
		LIMIT ?
	`, revokedBefore.UTC(), remaining)
	if err != nil {
		return 0, fmt.Errorf("cleanup revoked sessions: %w", err)
	}
	revoked, err := revokedResult.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("read revoked session cleanup result: %w", err)
	}
	return expired + revoked, nil
}
