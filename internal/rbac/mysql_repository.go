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
	if incrementVersion {
		currentKeys, err := r.currentRolePermissionKeys(ctx, exec, roleID)
		if err != nil {
			return err
		}
		if stringSlicesEqual(currentKeys, keys) {
			return errNoChange
		}
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

// Snapshot is read under the same role lock and transaction as the replacement,
// so the audit diff describes the committed transition, including concurrent edits.
func (r *MySQLRepository) RolePermissionSnapshot(ctx context.Context, exec platformdb.Executor, tenantID, roleID uint64) (map[string]any, error) {
	var version uint64
	err := exec.QueryRowContext(ctx, `SELECT version FROM roles WHERE tenant_id = ? AND id = ? FOR UPDATE`, tenantID, roleID).Scan(&version)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("read role audit snapshot: %w", err)
	}
	keys, err := r.currentRolePermissionKeys(ctx, exec, roleID)
	if err != nil {
		return nil, err
	}
	if keys == nil {
		keys = []string{}
	}
	return map[string]any{"permissions": keys, "version": fmt.Sprint(version)}, nil
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
		FROM `+"`groups`"+` g
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

func (r *MySQLRepository) ListGroupMembers(
	ctx context.Context,
	tenantID, groupID, afterUserID uint64,
	limit int,
) (GroupMemberPage, error) {
	exists, err := r.groupExists(ctx, tenantID, groupID)
	if err != nil {
		return GroupMemberPage{}, err
	}
	if !exists {
		return GroupMemberPage{}, ErrNotFound
	}

	rows, err := r.db.QueryContext(ctx, `
		SELECT
			u.id, u.email, u.display_name, u.active, tm.active, gm.created_at
		FROM group_memberships gm
		JOIN tenant_memberships tm
		  ON tm.tenant_id = gm.tenant_id AND tm.user_id = gm.user_id
		JOIN users u ON u.id = gm.user_id
		WHERE gm.tenant_id = ?
		  AND gm.group_id = ?
		  AND gm.user_id > ?
		ORDER BY gm.user_id
		LIMIT ?
	`, tenantID, groupID, afterUserID, limit+1)
	if err != nil {
		return GroupMemberPage{}, fmt.Errorf("list group members: %w", err)
	}
	defer rows.Close()

	members := make([]GroupMember, 0, limit+1)
	for rows.Next() {
		var member GroupMember
		if err := rows.Scan(
			&member.UserID,
			&member.Email,
			&member.DisplayName,
			&member.UserActive,
			&member.MembershipActive,
			&member.JoinedAt,
		); err != nil {
			return GroupMemberPage{}, fmt.Errorf("scan group member: %w", err)
		}
		members = append(members, member)
	}
	if err := rows.Err(); err != nil {
		return GroupMemberPage{}, fmt.Errorf("iterate group members: %w", err)
	}

	page := GroupMemberPage{Members: members}
	if len(members) > limit {
		page.Members = members[:limit]
		page.NextAfterID = page.Members[len(page.Members)-1].UserID
	}
	return page, nil
}

func (r *MySQLRepository) ListUserRoles(
	ctx context.Context,
	tenantID, userID, afterRoleID uint64,
	limit int,
) (AssignedRolePage, error) {
	exists, err := r.tenantMembershipExists(ctx, tenantID, userID)
	if err != nil {
		return AssignedRolePage{}, err
	}
	if !exists {
		return AssignedRolePage{}, ErrNotFound
	}

	rows, err := r.db.QueryContext(ctx, `
		SELECT r.id, r.name, r.system_role, ura.created_at
		FROM user_role_assignments ura
		JOIN roles r ON r.tenant_id = ura.tenant_id AND r.id = ura.role_id
		WHERE ura.tenant_id = ?
		  AND ura.user_id = ?
		  AND ura.role_id > ?
		ORDER BY ura.role_id
		LIMIT ?
	`, tenantID, userID, afterRoleID, limit+1)
	if err != nil {
		return AssignedRolePage{}, fmt.Errorf("list user role assignments: %w", err)
	}
	defer rows.Close()
	return scanAssignedRoles(rows, limit)
}

func (r *MySQLRepository) ListGroupRoles(
	ctx context.Context,
	tenantID, groupID, afterRoleID uint64,
	limit int,
) (AssignedRolePage, error) {
	exists, err := r.groupExists(ctx, tenantID, groupID)
	if err != nil {
		return AssignedRolePage{}, err
	}
	if !exists {
		return AssignedRolePage{}, ErrNotFound
	}

	rows, err := r.db.QueryContext(ctx, `
		SELECT r.id, r.name, r.system_role, gra.created_at
		FROM group_role_assignments gra
		JOIN roles r ON r.tenant_id = gra.tenant_id AND r.id = gra.role_id
		WHERE gra.tenant_id = ?
		  AND gra.group_id = ?
		  AND gra.role_id > ?
		ORDER BY gra.role_id
		LIMIT ?
	`, tenantID, groupID, afterRoleID, limit+1)
	if err != nil {
		return AssignedRolePage{}, fmt.Errorf("list group role assignments: %w", err)
	}
	defer rows.Close()
	return scanAssignedRoles(rows, limit)
}

func scanAssignedRoles(rows *sql.Rows, limit int) (AssignedRolePage, error) {
	roles := make([]AssignedRole, 0, limit+1)
	for rows.Next() {
		var role AssignedRole
		if err := rows.Scan(&role.ID, &role.Name, &role.SystemRole, &role.AssignedAt); err != nil {
			return AssignedRolePage{}, fmt.Errorf("scan role assignment: %w", err)
		}
		roles = append(roles, role)
	}
	if err := rows.Err(); err != nil {
		return AssignedRolePage{}, fmt.Errorf("iterate role assignments: %w", err)
	}
	page := AssignedRolePage{Roles: roles}
	if len(roles) > limit {
		page.Roles = roles[:limit]
		page.NextAfterID = page.Roles[len(page.Roles)-1].ID
	}
	return page, nil
}

func (r *MySQLRepository) groupExists(ctx context.Context, tenantID, groupID uint64) (bool, error) {
	var exists bool
	if err := r.db.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM `+"`groups`"+`
			WHERE tenant_id = ? AND id = ?
		)
	`, tenantID, groupID).Scan(&exists); err != nil {
		return false, fmt.Errorf("check group ownership: %w", err)
	}
	return exists, nil
}

func (r *MySQLRepository) tenantMembershipExists(ctx context.Context, tenantID, userID uint64) (bool, error) {
	var exists bool
	if err := r.db.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM tenant_memberships
			WHERE tenant_id = ? AND user_id = ?
		)
	`, tenantID, userID).Scan(&exists); err != nil {
		return false, fmt.Errorf("check tenant membership ownership: %w", err)
	}
	return exists, nil
}

func (r *MySQLRepository) CreateGroup(
	ctx context.Context,
	exec platformdb.Executor,
	tenantID uint64,
	input CreateGroupInput,
) (uint64, error) {
	result, err := exec.ExecContext(ctx, `
		INSERT INTO `+"`groups`"+` (tenant_id, name, description)
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
		FROM `+"`groups`"+` g
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
		FROM `+"`groups`"+` g
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

func (r *MySQLRepository) GroupHasSystemRole(
	ctx context.Context,
	exec platformdb.Executor,
	tenantID, groupID uint64,
) (bool, error) {
	var hasSystemRole bool
	err := exec.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1
			FROM group_role_assignments gra
			JOIN roles r
			  ON r.tenant_id = gra.tenant_id
			 AND r.id = gra.role_id
			WHERE gra.tenant_id = ?
			  AND gra.group_id = ?
			  AND r.system_role = TRUE
		)
	`, tenantID, groupID).Scan(&hasSystemRole)
	if err != nil {
		return false, fmt.Errorf("check group system role: %w", err)
	}
	return hasSystemRole, nil
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

func (r *MySQLRepository) currentRolePermissionKeys(
	ctx context.Context,
	exec platformdb.Executor,
	roleID uint64,
) ([]string, error) {
	rows, err := exec.QueryContext(ctx, `
		SELECT p.permission_key
		FROM role_permissions rp
		JOIN permissions p ON p.id = rp.permission_id
		WHERE rp.role_id = ?
		ORDER BY p.permission_key
	`, roleID)
	if err != nil {
		return nil, fmt.Errorf("list current role permissions: %w", err)
	}
	defer rows.Close()

	keys := make([]string, 0)
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, fmt.Errorf("scan current role permission: %w", err)
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate current role permissions: %w", err)
	}
	return keys, nil
}

func stringSlicesEqual(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
