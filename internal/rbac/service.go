package rbac

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/chmajster/OpenServiceNOW/internal/audit"
	"github.com/chmajster/OpenServiceNOW/internal/identity"
	platformdb "github.com/chmajster/OpenServiceNOW/internal/platform/database"
)

type Actor struct {
	TenantID  uint64
	UserID    uint64
	RequestID string
	IPAddress string
}

type Service struct {
	repo        *MySQLRepository
	tx          *platformdb.TxManager
	audit       *audit.MySQLRepository
	adminSafety *identity.AdminSafetyRepository
}

func NewService(
	repo *MySQLRepository,
	tx *platformdb.TxManager,
	auditRepo *audit.MySQLRepository,
	adminSafety *identity.AdminSafetyRepository,
) *Service {
	return &Service{repo: repo, tx: tx, audit: auditRepo, adminSafety: adminSafety}
}

func (s *Service) ListPermissions(ctx context.Context) ([]Permission, error) {
	result, err := s.repo.ListPermissions(ctx)
	if err != nil {
		return nil, err
	}
	if result == nil {
		result = []Permission{}
	}
	return result, nil
}

func (s *Service) ListRoles(
	ctx context.Context,
	tenantID, afterID uint64,
	limit int,
	query string,
) (RolePage, error) {
	query, err := normalizeListInput(limit, query)
	if err != nil {
		return RolePage{}, err
	}
	page, err := s.repo.ListRoles(ctx, tenantID, afterID, limit, query)
	if err != nil {
		return RolePage{}, err
	}
	if page.Roles == nil {
		page.Roles = []Role{}
	}
	return page, nil
}

func (s *Service) CreateRole(ctx context.Context, actor Actor, input CreateRoleInput) (uint64, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Description = strings.TrimSpace(input.Description)
	if err := validateName(input.Name, 150); err != nil {
		return 0, err
	}
	if strings.EqualFold(input.Name, "platform_admin") {
		return 0, fmt.Errorf("%w: role name platform_admin is reserved", ErrInvalid)
	}
	if utf8.RuneCountInString(input.Description) > 500 {
		return 0, fmt.Errorf("%w: description exceeds 500 characters", ErrInvalid)
	}

	permissions, err := normalizePermissionKeys(input.Permissions)
	if err != nil {
		return 0, err
	}
	input.Permissions = permissions

	var roleID uint64
	err = s.tx.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(exec platformdb.Executor) error {
		var createErr error
		roleID, createErr = s.repo.CreateRole(ctx, exec, actor.TenantID, input)
		if createErr != nil {
			return createErr
		}
		if err := s.repo.SetRolePermissions(ctx, exec, actor.TenantID, roleID, 0, false, input.Permissions); err != nil {
			return err
		}
		return s.audit.Append(ctx, exec, audit.Event{
			TenantID:    &actor.TenantID,
			ActorUserID: &actor.UserID,
			Action:      "rbac.role.create",
			ObjectType:  "role",
			ObjectID:    fmt.Sprint(roleID),
			NewValues: map[string]any{
				"name":        input.Name,
				"description": input.Description,
				"permissions": input.Permissions,
			},
			RequestID: actor.RequestID,
			Source:    "api",
			IPAddress: actor.IPAddress,
		})
	})
	if err != nil {
		return 0, err
	}
	return roleID, nil
}

func (s *Service) SetRolePermissions(
	ctx context.Context,
	actor Actor,
	roleID, expectedVersion uint64,
	permissionKeys []string,
) error {
	if expectedVersion == 0 {
		return fmt.Errorf("%w: role version must be greater than zero", ErrInvalid)
	}
	keys, err := normalizePermissionKeys(permissionKeys)
	if err != nil {
		return err
	}
	return s.tx.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(exec platformdb.Executor) error {
		if err := s.repo.SetRolePermissions(
			ctx,
			exec,
			actor.TenantID,
			roleID,
			expectedVersion,
			true,
			keys,
		); err != nil {
			return err
		}
		return s.audit.Append(ctx, exec, audit.Event{
			TenantID:    &actor.TenantID,
			ActorUserID: &actor.UserID,
			Action:      "rbac.role.permissions.set",
			ObjectType:  "role",
			ObjectID:    fmt.Sprint(roleID),
			NewValues:   map[string]any{"permissions": keys, "version": expectedVersion + 1},
			RequestID:   actor.RequestID,
			Source:      "api",
			IPAddress:   actor.IPAddress,
		})
	})
}

func (s *Service) ListGroups(
	ctx context.Context,
	tenantID, afterID uint64,
	limit int,
	query string,
) (GroupPage, error) {
	query, err := normalizeListInput(limit, query)
	if err != nil {
		return GroupPage{}, err
	}
	page, err := s.repo.ListGroups(ctx, tenantID, afterID, limit, query)
	if err != nil {
		return GroupPage{}, err
	}
	if page.Groups == nil {
		page.Groups = []Group{}
	}
	return page, nil
}

func (s *Service) CreateGroup(ctx context.Context, actor Actor, input CreateGroupInput) (uint64, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Description = strings.TrimSpace(input.Description)
	if err := validateName(input.Name, 150); err != nil {
		return 0, err
	}
	if utf8.RuneCountInString(input.Description) > 500 {
		return 0, fmt.Errorf("%w: description exceeds 500 characters", ErrInvalid)
	}

	var groupID uint64
	err := s.tx.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(exec platformdb.Executor) error {
		var createErr error
		groupID, createErr = s.repo.CreateGroup(ctx, exec, actor.TenantID, input)
		if createErr != nil {
			return createErr
		}
		return s.audit.Append(ctx, exec, audit.Event{
			TenantID:    &actor.TenantID,
			ActorUserID: &actor.UserID,
			Action:      "rbac.group.create",
			ObjectType:  "group",
			ObjectID:    fmt.Sprint(groupID),
			NewValues: map[string]any{
				"name":        input.Name,
				"description": input.Description,
			},
			RequestID: actor.RequestID,
			Source:    "api",
			IPAddress: actor.IPAddress,
		})
	})
	if err != nil {
		return 0, err
	}
	return groupID, nil
}

func (s *Service) AddGroupMember(ctx context.Context, actor Actor, groupID, userID uint64) error {
	return s.writeAssignment(
		ctx,
		actor,
		"rbac.group.member.add",
		"group",
		groupID,
		map[string]any{"user_id": userID},
		func(exec platformdb.Executor) error {
			return s.repo.AddGroupMember(ctx, exec, actor.TenantID, groupID, userID)
		},
	)
}

func (s *Service) RemoveGroupMember(ctx context.Context, actor Actor, groupID, userID uint64) error {
	return s.writeAssignment(
		ctx,
		actor,
		"rbac.group.member.remove",
		"group",
		groupID,
		map[string]any{"user_id": userID},
		func(exec platformdb.Executor) error {
			return s.withAdminSafety(ctx, exec, actor.TenantID, func() error {
				return s.repo.RemoveGroupMember(ctx, exec, actor.TenantID, groupID, userID)
			})
		},
	)
}

func (s *Service) AssignRoleToUser(ctx context.Context, actor Actor, roleID, userID uint64) error {
	return s.writeAssignment(
		ctx,
		actor,
		"rbac.user.role.add",
		"user",
		userID,
		map[string]any{"role_id": roleID},
		func(exec platformdb.Executor) error {
			return s.repo.AssignRoleToUser(ctx, exec, actor.TenantID, roleID, userID)
		},
	)
}

func (s *Service) RemoveRoleFromUser(ctx context.Context, actor Actor, roleID, userID uint64) error {
	return s.writeAssignment(
		ctx,
		actor,
		"rbac.user.role.remove",
		"user",
		userID,
		map[string]any{"role_id": roleID},
		func(exec platformdb.Executor) error {
			if actor.UserID == userID {
				systemRole, err := s.repo.IsSystemRole(ctx, exec, actor.TenantID, roleID)
				if err != nil {
					return err
				}
				if systemRole {
					return fmt.Errorf("%w: cannot remove own system role", ErrInvalid)
				}
			}
			return s.withAdminSafety(ctx, exec, actor.TenantID, func() error {
				return s.repo.RemoveRoleFromUser(ctx, exec, actor.TenantID, roleID, userID)
			})
		},
	)
}

func (s *Service) AssignRoleToGroup(ctx context.Context, actor Actor, groupID, roleID uint64) error {
	return s.writeAssignment(
		ctx,
		actor,
		"rbac.group.role.add",
		"group",
		groupID,
		map[string]any{"role_id": roleID},
		func(exec platformdb.Executor) error {
			return s.repo.AssignRoleToGroup(ctx, exec, actor.TenantID, roleID, groupID)
		},
	)
}

func (s *Service) RemoveRoleFromGroup(ctx context.Context, actor Actor, groupID, roleID uint64) error {
	return s.writeAssignment(
		ctx,
		actor,
		"rbac.group.role.remove",
		"group",
		groupID,
		map[string]any{"role_id": roleID},
		func(exec platformdb.Executor) error {
			return s.withAdminSafety(ctx, exec, actor.TenantID, func() error {
				return s.repo.RemoveRoleFromGroup(ctx, exec, actor.TenantID, roleID, groupID)
			})
		},
	)
}

func (s *Service) writeAssignment(
	ctx context.Context,
	actor Actor,
	action string,
	objectType string,
	objectID uint64,
	values map[string]any,
	write func(platformdb.Executor) error,
) error {
	return s.tx.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(exec platformdb.Executor) error {
		if err := write(exec); err != nil {
			if errors.Is(err, errNoChange) {
				return nil
			}
			return err
		}
		return s.audit.Append(ctx, exec, audit.Event{
			TenantID:    &actor.TenantID,
			ActorUserID: &actor.UserID,
			Action:      action,
			ObjectType:  objectType,
			ObjectID:    fmt.Sprint(objectID),
			NewValues:   values,
			RequestID:   actor.RequestID,
			Source:      "api",
			IPAddress:   actor.IPAddress,
		})
	})
}

func validateName(value string, max int) error {
	length := utf8.RuneCountInString(value)
	if length < 1 || length > max {
		return fmt.Errorf("%w: name must contain between 1 and %d characters", ErrInvalid, max)
	}
	return nil
}

func normalizePermissionKeys(values []string) ([]string, error) {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, raw := range values {
		key := strings.TrimSpace(raw)
		if key == "" || utf8.RuneCountInString(key) > 160 {
			return nil, fmt.Errorf("%w: permission key is invalid", ErrInvalid)
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, key)
	}
	sort.Strings(result)
	return result, nil
}

func (s *Service) withAdminSafety(
	ctx context.Context,
	exec platformdb.Executor,
	tenantID uint64,
	write func() error,
) error {
	if err := s.adminSafety.LockTenant(ctx, exec, tenantID); err != nil {
		return err
	}
	if err := write(); err != nil {
		return err
	}
	count, err := s.adminSafety.CountEffectivePlatformAdmins(ctx, exec, tenantID)
	if err != nil {
		return err
	}
	if count == 0 {
		return identity.ErrLastPlatformAdmin
	}
	return nil
}

func normalizeListInput(limit int, query string) (string, error) {
	if limit < 1 || limit > 200 {
		return "", fmt.Errorf("%w: limit must be between 1 and 200", ErrInvalid)
	}
	query = strings.TrimSpace(query)
	if utf8.RuneCountInString(query) > 200 {
		return "", fmt.Errorf("%w: query exceeds 200 characters", ErrInvalid)
	}
	return query, nil
}
