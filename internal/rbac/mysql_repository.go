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

func (r *MySQLRepository) ListRoles(
	ctx context.Context,
	tenantID, afterID uint64,
	limit int,
	query string,
) (RolePage, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, name, COALESCE(description, ''), system_role, version
		FROM roles
		WHERE tenant_id = ?
		  AND id > ?
		  AND (
			? = ''
			OR LOCATE(?, name) > 0
			OR LOCATE(?, COALESCE(description, '')) > 0
		  )
		ORDER BY id
		LIMIT ?
	`, tenantID, afterID, query, query, query, limit+1)
	if err != nil {
		return RolePage{}, fmt.Errorf("list roles: %w", err)
	}
	defer rows.Close()

	roles := make([]Role, 0, limit+1)
	for rows.Next() {
		var role Role
		if err := rows.Scan(&role.ID, &role.Name, &role.Description, &role.SystemRole, &role.Version); err != nil {
			return RolePage{}, fmt.Errorf("scan role: %w", err)
		}
		role.Permissions = []Permission{}
		roles = append(roles, role)
	}
	if err := rows.Err(); err != nil {
		return RolePage{}, fmt.Errorf("iterate roles: %w", err)
	}

	page := RolePage{Roles: roles}
	if len(roles) > limit {
		page.Roles = roles[:limit]
		page.NextAfterID = page.Roles[len(page.Roles)-1].ID
	}
	if len(page.Roles) == 0 {
		return page, nil
	}

	roleIndex := make(map[uint64]int, len(page.Roles))
	placeholders := strings.TrimRight(strings.Repeat("?,", len(page.Roles)), ",")
	args := make([]any, 0, len(page.Roles)+1)
	args = append(args, tenantID)
	for index, role := range page.Roles {
		roleIndex[role.ID] = index
		args = append(args, role.ID)
	}

	permissionRows, err := r.db.QueryContext(ctx, `
		SELECT r.id, p.id, p.permission_key, COALESCE(p.description, '')
		FROM roles r
		JOIN permissions p
		  ON (
			(r.name = 'platform_admin' AND r.system_role = TRUE)
			OR EXISTS (
				SELECT 1
				FROM role_permissions rp
				WHERE rp.role_id = r.id
				  AND rp.permission_id = p.id
			)
		  )
		WHERE r.tenant_id = ?
		  AND r.id IN (`+placeholders+`)
		ORDER BY r.id, p.permission_key
	`, args...)
	if err != nil {
		return RolePage{}, fmt.Errorf("list role permissions: %w", err)
	}
	defer permissionRows.Close()

	for permissionRows.Next() {
		var roleID uint64
		var permission Permission
		if err := permissionRows.Scan(&roleID, &permission.ID, &permission.Key, &permission.Description); err != nil {
			return RolePage{}, fmt.Errorf("scan role permission: %w", err)
		}
		if index, ok := roleIndex[roleID]; ok {
			page.Roles[index].Permissions = append(page.Roles[index].Permissions, permission)
		}
	}
	if err := permissionRows.Err(); err != nil {
		return RolePage{}, fmt.Errorf("iterate role permissions: %w", err)
	}
	return page, nil
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
	tenantID, roleID, expectedVersion uint64,
	incrementVersion bool,
	keys []string,
) error {
	var systemRole bool
	var currentVersion uint64
	err := exec.QueryRowContext(ctx, `
		SELECT system_role, version
		FROM roles
		WHERE tenant_id = ? AND id = ?
		FOR UPDATE
	`, tenantID, roleID).Scan(&systemRole, &currentVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("check role ownership: %w", err)
	}
	if systemRole {
		return fmt.Errorf("%w: system role permissions are immutable", ErrInvalid)
	}
	if incrementVersion && currentVersion != expectedVersion {
		return ErrConflict
	}

	permissionIDs := make([]uint64, 0, len(keys))
	if len(keys) > 0 {
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

		found := make(map[string]struct{}, len(keys))
		for rows.Next() {
			var id uint64
			var key string
			if err := rows.Scan(&id, &key); err != nil {
				_ = rows.Close()
				return fmt.Errorf("scan resolved permission: %w", err)
			}
			permissionIDs = append(permissionIDs, id)
			found[key] = struct{}{}
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return fmt.Errorf("iterate resolved permissions: %w", err)
		}
		if err := rows.Close(); err != nil {
			return fmt.Errorf("close resolved permissions: %w", err)
		}
		for _, key := range keys {
			if _, ok := found[key]; !ok {
				return fmt.Errorf("%w: unknown permission %q", ErrInvalid, key)
			}
		}
	}

	if _, err := exec.ExecContext(ctx, `DELETE FROM role_permissions WHERE role_id = ?`, roleID); err != nil {
		return fmt.Errorf("clear role permissions: %w", err)
	}
	for _, permissionID := range permissionIDs {
		if _, err := exec.ExecContext(ctx, `
			INSERT INTO role_permissions (role_id, permission_id) VALUES (?, ?)
		`, roleID, permissionID); err != nil {
			return fmt.Errorf("assign role permission: %w", err)
		}
	}

	if incrementVersion {
		result, err := exec.ExecContext(ctx, `
			UPDATE roles
			SET version = version + 1
			WHERE tenant_id = ? AND id = ? AND version = ?
		`, tenantID, roleID, expectedVersion)
		if err != nil {
			return fmt.Errorf("increment role version: %w", err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("read role version update result: %w", err)
		}
		if affected != 1 {
			return ErrConflict
		}
	}
	return nil
}

func (r *MySQLRepository) ListGroups(
	ctx context.Context,
	tenantID, afterID uint64,
	limit int,
	query string,
) (GroupPage, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT
			g.id, g.name, COALESCE(g.description, ''), g.version,
			COUNT(gm.user_id) AS member_count
		FROM groups g
		LEFT JOIN group_memberships gm
		  ON gm.tenant_id = g.tenant_id AND gm.group_id = g.id
		WHERE g.tenant_id = ?
		  AND g.id > ?
		  AND (
			? = ''
			OR LOCATE(?, g.name) > 0
			OR LOCATE(?, COALESCE(g.description, '')) > 0
		  )
		GROUP BY g.id, g.name, g.description, g.version
		ORDER BY g.id
		LIMIT ?
	`, tenantID, afterID, query, query, query, limit+1)
	if err != nil {
		return GroupPage{}, fmt.Errorf("list groups: %w", err)
	}
	defer rows.Close()

	groups := make([]Group, 0, limit+1)
	for rows.Next() {
		var group Group
		if err := rows.Scan(&group.ID, &group.Name, &group.Description, &group.Version, &group.MemberCount); err != nil {
			return GroupPage{}, fmt.Errorf("scan group: %w", err)
		}
		groups = append(groups, group)
	}
	if err := rows.Err(); err != nil {
		return GroupPage{}, fmt.Errorf("iterate groups: %w", err)
	}

	page := GroupPage{Groups: groups}
	if len(groups) > limit {
		page.Groups = groups[:limit]
		page.NextAfterID = page.Groups[len(page.Groups)-1].ID
	}
	return page, nil
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
			return errNoChange
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
	result, err := exec.ExecContext(ctx, `
		DELETE FROM group_memberships
		WHERE tenant_id = ? AND group_id = ? AND user_id = ?
	`, tenantID, groupID, userID)
	if err != nil {
		return fmt.Errorf("remove group member: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read removed group membership result: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
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
			return errNoChange
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
	result, err := exec.ExecContext(ctx, `
		DELETE FROM user_role_assignments
		WHERE tenant_id = ? AND user_id = ? AND role_id = ?
	`, tenantID, userID, roleID)
	if err != nil {
		return fmt.Errorf("remove role from user: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read removed user role result: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
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
			return errNoChange
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
	result, err := exec.ExecContext(ctx, `
		DELETE FROM group_role_assignments
		WHERE tenant_id = ? AND group_id = ? AND role_id = ?
	`, tenantID, groupID, roleID)
	if err != nil {
		return fmt.Errorf("remove role from group: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read removed group role result: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
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
