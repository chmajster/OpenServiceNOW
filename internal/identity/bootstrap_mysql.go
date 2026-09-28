package identity

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	platformdb "github.com/chmajster/OpenServiceNOW/internal/platform/database"
)

func (r *MySQLRepository) EnsureTenant(ctx context.Context, exec platformdb.Executor, slug, name string) (uint64, error) {
	result, err := exec.ExecContext(ctx, `
		INSERT INTO tenants (slug, name)
		VALUES (?, ?)
		ON DUPLICATE KEY UPDATE id = LAST_INSERT_ID(id)
	`, slug, name)
	if err != nil {
		return 0, fmt.Errorf("ensure tenant: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("read tenant id: %w", err)
	}
	return uint64(id), nil
}

func (r *MySQLRepository) EnsureUser(
	ctx context.Context,
	exec platformdb.Executor,
	email, name, passwordHash string,
) (uint64, bool, string, error) {
	var existingID uint64
	var existingPasswordHash string
	err := exec.QueryRowContext(
		ctx,
		`SELECT id, password_hash FROM users WHERE email = ? FOR UPDATE`,
		email,
	).Scan(&existingID, &existingPasswordHash)
	if err == nil {
		return existingID, false, existingPasswordHash, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, false, "", fmt.Errorf("find existing user: %w", err)
	}

	result, err := exec.ExecContext(ctx, `
		INSERT INTO users (email, display_name, password_hash)
		VALUES (?, ?, ?)
	`, email, name, passwordHash)
	if err != nil {
		return 0, false, "", fmt.Errorf("create user: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, false, "", fmt.Errorf("read new user id: %w", err)
	}
	return uint64(id), true, "", nil
}

func (r *MySQLRepository) EnsureMembership(ctx context.Context, exec platformdb.Executor, tenantID, userID uint64) error {
	_, err := exec.ExecContext(ctx, `
		INSERT INTO tenant_memberships (tenant_id, user_id, active)
		VALUES (?, ?, TRUE)
		ON DUPLICATE KEY UPDATE active = TRUE
	`, tenantID, userID)
	if err != nil {
		return fmt.Errorf("ensure tenant membership: %w", err)
	}
	return nil
}

func (r *MySQLRepository) EnsureRole(
	ctx context.Context,
	exec platformdb.Executor,
	tenantID uint64,
	name, description string,
) (uint64, error) {
	result, err := exec.ExecContext(ctx, `
		INSERT INTO roles (tenant_id, name, description, system_role)
		VALUES (?, ?, ?, TRUE)
		ON DUPLICATE KEY UPDATE id = LAST_INSERT_ID(id), description = ?, system_role = TRUE
	`, tenantID, name, description, description)
	if err != nil {
		return 0, fmt.Errorf("ensure role: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("read role id: %w", err)
	}
	return uint64(id), nil
}

func (r *MySQLRepository) EnsurePermission(
	ctx context.Context,
	exec platformdb.Executor,
	key, description string,
) (uint64, error) {
	result, err := exec.ExecContext(ctx, `
		INSERT INTO permissions (permission_key, description)
		VALUES (?, ?)
		ON DUPLICATE KEY UPDATE id = LAST_INSERT_ID(id), description = ?
	`, key, description, description)
	if err != nil {
		return 0, fmt.Errorf("ensure permission: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("read permission id: %w", err)
	}
	return uint64(id), nil
}

func (r *MySQLRepository) EnsureRolePermission(
	ctx context.Context,
	exec platformdb.Executor,
	roleID, permissionID uint64,
) error {
	_, err := exec.ExecContext(ctx, `
		INSERT INTO role_permissions (role_id, permission_id) VALUES (?, ?)
		ON DUPLICATE KEY UPDATE role_id = role_id
	`, roleID, permissionID)
	if err != nil {
		return fmt.Errorf("ensure role permission: %w", err)
	}
	return nil
}

func (r *MySQLRepository) EnsureUserRole(
	ctx context.Context,
	exec platformdb.Executor,
	tenantID, userID, roleID uint64,
) error {
	_, err := exec.ExecContext(ctx, `
		INSERT INTO user_role_assignments (tenant_id, user_id, role_id)
		VALUES (?, ?, ?)
		ON DUPLICATE KEY UPDATE role_id = role_id
	`, tenantID, userID, roleID)
	if err != nil {
		return fmt.Errorf("ensure user role: %w", err)
	}
	return nil
}

func (r *MySQLRepository) TenantStatusForUpdate(
	ctx context.Context,
	exec platformdb.Executor,
	tenantID uint64,
) (string, error) {
	var status string
	err := exec.QueryRowContext(ctx, `
		SELECT status
		FROM tenants
		WHERE id = ?
		FOR UPDATE
	`, tenantID).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("read bootstrap tenant status: %w", err)
	}
	return status, nil
}

func (r *MySQLRepository) UserActiveForUpdate(
	ctx context.Context,
	exec platformdb.Executor,
	userID uint64,
) (bool, error) {
	var active bool
	err := exec.QueryRowContext(ctx, `
		SELECT active
		FROM users
		WHERE id = ?
		FOR UPDATE
	`, userID).Scan(&active)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrNotFound
	}
	if err != nil {
		return false, fmt.Errorf("read bootstrap user state: %w", err)
	}
	return active, nil
}
