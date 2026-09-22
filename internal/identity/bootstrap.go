package identity

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/mail"
	"regexp"
	"strings"

	"github.com/chmajster/OpenServiceNOW/internal/audit"
	platformdb "github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/platform/password"
)

var tenantSlugPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,98}[a-z0-9])?$`)

var platformAdminPermissions = map[string]string{
	"admin.access":        "Access administrative APIs and interfaces",
	"admin.users.manage":  "Manage users and tenant memberships",
	"admin.rbac.manage":   "Manage groups, roles and permissions",
	"admin.audit.read":    "Read tenant audit events",
	"tenant.read":         "Read tenant configuration",
	"tenant.manage":       "Manage tenant configuration",
}

type BootstrapInput struct {
	TenantSlug   string
	TenantName   string
	AdminEmail   string
	AdminName    string
	AdminPassword string
}

type BootstrapResult struct {
	TenantID    uint64
	UserID      uint64
	RoleID      uint64
	UserCreated bool
}

type Bootstrapper struct {
	repo  *MySQLRepository
	tx    *platformdb.TxManager
	audit *audit.MySQLRepository
}

func NewBootstrapper(repo *MySQLRepository, tx *platformdb.TxManager, auditRepo *audit.MySQLRepository) *Bootstrapper {
	return &Bootstrapper{repo: repo, tx: tx, audit: auditRepo}
}

func (b *Bootstrapper) Run(ctx context.Context, input BootstrapInput) (BootstrapResult, error) {
	input.TenantSlug = strings.ToLower(strings.TrimSpace(input.TenantSlug))
	input.TenantName = strings.TrimSpace(input.TenantName)
	input.AdminEmail = strings.ToLower(strings.TrimSpace(input.AdminEmail))
	input.AdminName = strings.TrimSpace(input.AdminName)

	if !tenantSlugPattern.MatchString(input.TenantSlug) {
		return BootstrapResult{}, errors.New("bootstrap tenant slug is invalid")
	}
	if input.TenantName == "" || input.AdminName == "" || input.AdminEmail == "" {
		return BootstrapResult{}, errors.New("bootstrap tenant name, admin name and admin email are required")
	}
	address, err := mail.ParseAddress(input.AdminEmail)
	if err != nil || address.Address != input.AdminEmail {
		return BootstrapResult{}, errors.New("bootstrap admin email is invalid")
	}

	passwordHash, err := password.Hash(input.AdminPassword)
	if err != nil {
		return BootstrapResult{}, fmt.Errorf("hash bootstrap admin password: %w", err)
	}

	var result BootstrapResult
	err = b.tx.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable}, func(exec platformdb.Executor) error {
		tenantID, err := ensureTenant(ctx, exec, input.TenantSlug, input.TenantName)
		if err != nil {
			return err
		}
		userID, created, err := ensureUser(ctx, exec, input.AdminEmail, input.AdminName, passwordHash)
		if err != nil {
			return err
		}
		if err := ensureMembership(ctx, exec, tenantID, userID); err != nil {
			return err
		}

		roleID, err := ensureRole(ctx, exec, tenantID, "platform_admin", "Tenant platform administrator")
		if err != nil {
			return err
		}
		for permissionKey, description := range platformAdminPermissions {
			permissionID, err := ensurePermission(ctx, exec, permissionKey, description)
			if err != nil {
				return err
			}
			if err := ensureRolePermission(ctx, exec, roleID, permissionID); err != nil {
				return err
			}
		}
		if err := ensureUserRole(ctx, exec, tenantID, userID, roleID); err != nil {
			return err
		}

		if err := b.audit.Append(ctx, exec, audit.Event{
			TenantID:    &tenantID,
			ActorUserID: &userID,
			Action:      "bootstrap.platform_admin",
			ObjectType:  "tenant",
			ObjectID:    fmt.Sprint(tenantID),
			NewValues: map[string]any{
				"tenant_slug": input.TenantSlug,
				"admin_email": input.AdminEmail,
				"user_created": created,
			},
			RequestID: "bootstrap",
			Source:    "bootstrap",
		}); err != nil {
			return err
		}

		result = BootstrapResult{
			TenantID: tenantID, UserID: userID, RoleID: roleID, UserCreated: created,
		}
		return nil
	})
	if err != nil {
		return BootstrapResult{}, err
	}
	return result, nil
}

func ensureTenant(ctx context.Context, exec platformdb.Executor, slug, name string) (uint64, error) {
	result, err := exec.ExecContext(ctx, `
		INSERT INTO tenants (slug, name)
		VALUES (?, ?)
		ON DUPLICATE KEY UPDATE id = LAST_INSERT_ID(id), name = ?
	`, slug, name, name)
	if err != nil {
		return 0, fmt.Errorf("ensure tenant: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("read tenant id: %w", err)
	}
	return uint64(id), nil
}

func ensureUser(ctx context.Context, exec platformdb.Executor, email, name, passwordHash string) (uint64, bool, error) {
	var existingID uint64
	err := exec.QueryRowContext(ctx, `SELECT id FROM users WHERE email = ? FOR UPDATE`, email).Scan(&existingID)
	if err == nil {
		return existingID, false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, false, fmt.Errorf("find existing user: %w", err)
	}

	result, err := exec.ExecContext(ctx, `
		INSERT INTO users (email, display_name, password_hash)
		VALUES (?, ?, ?)
	`, email, name, passwordHash)
	if err != nil {
		return 0, false, fmt.Errorf("create user: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, false, fmt.Errorf("read new user id: %w", err)
	}
	return uint64(id), true, nil
}

func ensureMembership(ctx context.Context, exec platformdb.Executor, tenantID, userID uint64) error {
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

func ensureRole(ctx context.Context, exec platformdb.Executor, tenantID uint64, name, description string) (uint64, error) {
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

func ensurePermission(ctx context.Context, exec platformdb.Executor, key, description string) (uint64, error) {
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

func ensureRolePermission(ctx context.Context, exec platformdb.Executor, roleID, permissionID uint64) error {
	_, err := exec.ExecContext(ctx, `
		INSERT INTO role_permissions (role_id, permission_id) VALUES (?, ?)
		ON DUPLICATE KEY UPDATE role_id = role_id
	`, roleID, permissionID)
	if err != nil {
		return fmt.Errorf("ensure role permission: %w", err)
	}
	return nil
}

func ensureUserRole(ctx context.Context, exec platformdb.Executor, tenantID, userID, roleID uint64) error {
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
