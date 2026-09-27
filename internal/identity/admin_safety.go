package identity

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	platformdb "github.com/chmajster/OpenServiceNOW/internal/platform/database"
)

type AdminSafetyRepository struct{}

func NewAdminSafetyRepository() *AdminSafetyRepository {
	return &AdminSafetyRepository{}
}

func (r *AdminSafetyRepository) LockTenant(
	ctx context.Context,
	exec platformdb.Executor,
	tenantID uint64,
) error {
	var id uint64
	err := exec.QueryRowContext(ctx, `
		SELECT id
		FROM tenants
		WHERE id = ?
		FOR UPDATE
	`, tenantID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("lock tenant admin invariant: %w", err)
	}
	return nil
}

func (r *AdminSafetyRepository) IsEffectivePlatformAdmin(
	ctx context.Context,
	exec platformdb.Executor,
	tenantID, userID uint64,
) (bool, error) {
	var allowed bool
	err := exec.QueryRowContext(ctx, `
		SELECT EXISTS(
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
					  AND r.system_role = TRUE
				)
			  )
		)
	`, tenantID, userID).Scan(&allowed)
	if err != nil {
		return false, fmt.Errorf("check effective platform administrator: %w", err)
	}
	return allowed, nil
}

func (r *AdminSafetyRepository) HasSystemRoleBinding(
	ctx context.Context,
	exec platformdb.Executor,
	tenantID, userID uint64,
) (bool, error) {
	var bound bool
	err := exec.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1
			FROM tenant_memberships tm
			WHERE tm.tenant_id = ?
			  AND tm.user_id = ?
			  AND (
				EXISTS (
					SELECT 1
					FROM user_role_assignments ura
					JOIN roles r ON r.id = ura.role_id AND r.tenant_id = ura.tenant_id
					WHERE ura.tenant_id = tm.tenant_id
					  AND ura.user_id = tm.user_id
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
					  AND r.system_role = TRUE
				)
			  )
		)
	`, tenantID, userID).Scan(&bound)
	if err != nil {
		return false, fmt.Errorf("check system-role binding: %w", err)
	}
	return bound, nil
}

func (r *AdminSafetyRepository) CountEffectivePlatformAdmins(
	ctx context.Context,
	exec platformdb.Executor,
	tenantID uint64,
) (uint64, error) {
	var count uint64
	err := exec.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM tenant_memberships tm
		JOIN users u ON u.id = tm.user_id AND u.active = TRUE
		WHERE tm.tenant_id = ?
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
		  )
	`, tenantID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count effective platform administrators: %w", err)
	}
	return count, nil
}
