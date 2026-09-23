package rbac

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	platformdb "github.com/chmajster/OpenServiceNOW/internal/platform/database"
	mysqlDriver "github.com/go-sql-driver/mysql"
)

type MySQLRepository struct {
	db *sql.DB
}

func NewMySQLRepository(db *sql.DB) *MySQLRepository {
	return &MySQLRepository{db: db}
}

func (r *MySQLRepository) ListPermissions(ctx context.Context) ([]Permission, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, permission_key, COALESCE(description, '')
		FROM permissions
		ORDER BY permission_key
	`)
	if err != nil {
		return nil, fmt.Errorf("list permission catalog: %w", err)
	}
	defer rows.Close()

	var result []Permission
	for rows.Next() {
		var item Permission
		if err := rows.Scan(&item.ID, &item.Key, &item.Description); err != nil {
			return nil, fmt.Errorf("scan permission: %w", err)
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate permissions: %w", err)
	}
	return result, nil
}

func (r *MySQLRepository) ListRoles(ctx context.Context, tenantID uint64) ([]Role, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, name, COALESCE(description, ''), system_role, version
		FROM roles
		WHERE tenant_id = ?
		ORDER BY name
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list roles: %w", err)
	}

	roles := make([]Role, 0)
	roleIndex := make(map[uint64]int)
	for rows.Next() {
		var role Role
		if err := rows.Scan(&role.ID, &role.Name, &role.Description, &role.SystemRole, &role.Version); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan role: %w", err)
		}
		role.Permissions = []Permission{}
		roleIndex[role.ID] = len(roles)
		roles = append(roles, role)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, fmt.Errorf("iterate roles: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close roles: %w", err)
	}
	if len(roles) == 0 {
		return roles, nil
	}

	permissionRows, err := r.db.QueryContext(ctx, `
		SELECT r.id, p.id, p.permission_key, COALESCE(p.description, '')
		FROM roles r
		JOIN role_permissions rp ON rp.role_id = r.id
		JOIN permissions p ON p.id = rp.permission_id
		WHERE r.tenant_id = ?
		ORDER BY r.id, p.permission_key
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list role permissions: %w", err)
	}
	defer permissionRows.Close()
	for permissionRows.Next() {
		var roleID uint64
		var permission Permission
		if err := permissionRows.Scan(&roleID, &permission.ID, &permission.Key, &permission.Description); err != nil {
			return nil, fmt.Errorf("scan role permission: %w", err)
		}
		if index, ok := roleIndex[roleID]; ok {
			roles[index].Permissions = append(roles[index].Permissions, permission)
		}
	}
	if err := permissionRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate role permissions: %w", err)
	}
	return roles, nil
}

func (r *MySQLRepository) CreateRole(
	ctx context.Context,
	exec platformdb.Executor,
	tenantID uint64,
	input CreateRoleInput,
) (uint64, error) {
	result, err := exec.ExecContext(ctx, `
		INSERT INTO roles (tenant_id, name, description, system_role)
		VALUES (?, ?, NULLIF(?, ''), FALSE)
	`, tenantID, input.Name, input.Description)
	if err != nil {
		if isDuplicate(err) {
			return 0, ErrConflict
		}
		return 0, fmt.Errorf("create role: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("read role id: %w", err)
	}
	return uint64(id), nil
}

func (r *MySQLRepository) SetRolePermissions(
	ctx context.Context,
	exec platformdb.Executor,
	tenantID, roleID uint64,
	keys []string,
) error {
	var systemRole bool
	err := exec.QueryRowContext(ctx, `
		SELECT system_role
		FROM roles
		WHERE tenant_id = ? AND id = ?
	`, tenantID, roleID).Scan(&systemRole)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("check role ownership: %w", err)
	}
	if systemRole {
		return fmt.Errorf("%w: system role permissions are immutable", ErrInvalid)
	}

	if _, err := exec.ExecContext(ctx, `DELETE FROM role_permissions WHERE role_id = ?`, roleID); err != nil {
		return fmt.Errorf("clear role permissions: %w", err)
	}
	if len(keys) == 0 {
		return nil
	}

	placeholders := strings.TrimRight(strings.Repeat("?,", len(keys)), ",")
	args := make([]any, 0, len(keys))
	for _, key := range keys {
		args = append(args, key)
	}
	rows, err := exec.QueryContext(ctx,
		`SELECT id, permission_key FROM permissions WHERE permission_key IN (`+placeholders+`)`,
		args...,
	)
	if err != nil {
		return fmt.Errorf("resolve permissions: %w", err)
	}
	defer rows.Close()

	permissionIDs := make([]uint64, 0, len(keys))
	found := make(map[string]struct{}, len(keys))
	for rows.Next() {
		var id uint64
		var key string
		if err := rows.Scan(&id, &key); err != nil {
			return fmt.Errorf("scan resolved permission: %w", err)
		}
		permissionIDs = append(permissionIDs, id)
		found[key] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate resolved permissions: %w", err)
	}
	for _, key := range keys {
		if _, ok := found[key]; !ok {
			return fmt.Errorf("%w: unknown permission %q", ErrInvalid, key)
		}
	}
	for _, permissionID := range permissionIDs {
		if _, err := exec.ExecContext(ctx, `
			INSERT INTO role_permissions (role_id, permission_id) VALUES (?, ?)
		`, roleID, permissionID); err != nil {
			return fmt.Errorf("assign role permission: %w", err)
		}
	}
	return nil
}

func (r *MySQLRepository) ListGroups(ctx context.Context, tenantID uint64) ([]Group, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT
			g.id, g.name, COALESCE(g.description, ''), g.version,
			COUNT(gm.user_id) AS member_count
		FROM groups g
		LEFT JOIN group_memberships gm
			ON gm.tenant_id = g.tenant_id AND gm.group_id = g.id
		WHERE g.tenant_id = ?
		GROUP BY g.id, g.name, g.description, g.version
		ORDER BY g.name
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list groups: %w", err)
	}
	defer rows.Close()

	var groups []Group
	for rows.Next() {
		var group Group
		if err := rows.Scan(&group.ID, &group.Name, &group.Description, &group.Version, &group.MemberCount); err != nil {
			return nil, fmt.Errorf("scan group: %w", err)
		}
		groups = append(groups, group)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate groups: %w", err)
	}
	return groups, nil
}

func (r *MySQLRepository) CreateGroup(
	ctx context.Context,
	exec platformdb.Executor,
	tenantID uint64,
	input CreateGroupInput,
) (uint64, error) {
	result, err := exec.ExecContext(ctx, `
		INSERT INTO groups (tenant_id, name, description)
		VALUES (?, ?, NULLIF(?, ''))
	`, tenantID, input.Name, input.Description)
	if err != nil {
		if isDuplicate(err) {
			return 0, ErrConflict
		}
		return 0, fmt.Errorf("create group: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("read group id: %w", err)
	}
	return uint64(id), nil
}

func (r *MySQLRepository) AddGroupMember(
	ctx context.Context,
	exec platformdb.Executor,
	tenantID, groupID, userID uint64,
) error {
	result, err := exec.ExecContext(ctx, `
		INSERT INTO group_memberships (tenant_id, group_id, user_id)
		SELECT ?, g.id, tm.user_id
		FROM groups g
		JOIN tenant_memberships tm ON tm.tenant_id = g.tenant_id AND tm.user_id = ? AND tm.active = TRUE
		WHERE g.tenant_id = ? AND g.id = ?
	`, tenantID, userID, tenantID, groupID)
	if err != nil {
		if isDuplicate(err) {
			return nil
		}
		return fmt.Errorf("add group member: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read group membership result: %w", err)
	}
	if affected == 0 {
		var exists bool
		if err := exec.QueryRowContext(ctx, `
			SELECT EXISTS(
				SELECT 1 FROM group_memberships
				WHERE tenant_id = ? AND group_id = ? AND user_id = ?
			)
		`, tenantID, groupID, userID).Scan(&exists); err != nil {
			return fmt.Errorf("verify group membership: %w", err)
		}
		if !exists {
			return ErrNotFound
		}
	}
	return nil
}

func (r *MySQLRepository) RemoveGroupMember(
	ctx context.Context,
	exec platformdb.Executor,
	tenantID, groupID, userID uint64,
) error {
	_, err := exec.ExecContext(ctx, `
		DELETE FROM group_memberships
		WHERE tenant_id = ? AND group_id = ? AND user_id = ?
	`, tenantID, groupID, userID)
	if err != nil {
		return fmt.Errorf("remove group member: %w", err)
	}
	return nil
}

func (r *MySQLRepository) AssignRoleToUser(
	ctx context.Context,
	exec platformdb.Executor,
	tenantID, roleID, userID uint64,
) error {
	result, err := exec.ExecContext(ctx, `
		INSERT INTO user_role_assignments (tenant_id, user_id, role_id)
		SELECT ?, tm.user_id, r.id
		FROM roles r
		JOIN tenant_memberships tm ON tm.tenant_id = r.tenant_id AND tm.user_id = ? AND tm.active = TRUE
		WHERE r.tenant_id = ? AND r.id = ?
	`, tenantID, userID, tenantID, roleID)
	if err != nil {
		if isDuplicate(err) {
			return nil
		}
		return fmt.Errorf("assign role to user: %w", err)
	}
	if affected, err := result.RowsAffected(); err != nil {
		return fmt.Errorf("read role assignment result: %w", err)
	} else if affected == 0 {
		var exists bool
		if err := exec.QueryRowContext(ctx, `
			SELECT EXISTS(
				SELECT 1 FROM user_role_assignments
				WHERE tenant_id = ? AND user_id = ? AND role_id = ?
			)
		`, tenantID, userID, roleID).Scan(&exists); err != nil {
			return fmt.Errorf("verify user role assignment: %w", err)
		}
		if !exists {
			return ErrNotFound
		}
	}
	return nil
}

func (r *MySQLRepository) RemoveRoleFromUser(
	ctx context.Context,
	exec platformdb.Executor,
	tenantID, roleID, userID uint64,
) error {
	_, err := exec.ExecContext(ctx, `
		DELETE FROM user_role_assignments
		WHERE tenant_id = ? AND user_id = ? AND role_id = ?
	`, tenantID, userID, roleID)
	if err != nil {
		return fmt.Errorf("remove role from user: %w", err)
	}
	return nil
}

func (r *MySQLRepository) AssignRoleToGroup(
	ctx context.Context,
	exec platformdb.Executor,
	tenantID, roleID, groupID uint64,
) error {
	result, err := exec.ExecContext(ctx, `
		INSERT INTO group_role_assignments (tenant_id, group_id, role_id)
		SELECT ?, g.id, r.id
		FROM groups g
		JOIN roles r ON r.tenant_id = g.tenant_id AND r.id = ?
		WHERE g.tenant_id = ? AND g.id = ?
	`, tenantID, roleID, tenantID, groupID)
	if err != nil {
		if isDuplicate(err) {
			return nil
		}
		return fmt.Errorf("assign role to group: %w", err)
	}
	if affected, err := result.RowsAffected(); err != nil {
		return fmt.Errorf("read group role assignment result: %w", err)
	} else if affected == 0 {
		var exists bool
		if err := exec.QueryRowContext(ctx, `
			SELECT EXISTS(
				SELECT 1 FROM group_role_assignments
				WHERE tenant_id = ? AND group_id = ? AND role_id = ?
			)
		`, tenantID, groupID, roleID).Scan(&exists); err != nil {
			return fmt.Errorf("verify group role assignment: %w", err)
		}
		if !exists {
			return ErrNotFound
		}
	}
	return nil
}

func (r *MySQLRepository) RemoveRoleFromGroup(
	ctx context.Context,
	exec platformdb.Executor,
	tenantID, roleID, groupID uint64,
) error {
	_, err := exec.ExecContext(ctx, `
		DELETE FROM group_role_assignments
		WHERE tenant_id = ? AND group_id = ? AND role_id = ?
	`, tenantID, groupID, roleID)
	if err != nil {
		return fmt.Errorf("remove role from group: %w", err)
	}
	return nil
}

func isDuplicate(err error) bool {
	var mysqlErr *mysqlDriver.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == 1062
}

func (r *MySQLRepository) IsSystemRole(
	ctx context.Context,
	exec platformdb.Executor,
	tenantID, roleID uint64,
) (bool, error) {
	var systemRole bool
	err := exec.QueryRowContext(ctx, `
		SELECT system_role
		FROM roles
		WHERE tenant_id = ? AND id = ?
	`, tenantID, roleID).Scan(&systemRole)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrNotFound
	}
	if err != nil {
		return false, fmt.Errorf("read system role flag: %w", err)
	}
	return systemRole, nil
}
