package identity

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/mail"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/chmajster/OpenServiceNOW/internal/audit"
	platformdb "github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/platform/password"
)

var tenantSlugPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,98}[a-z0-9])?$`)

var platformAdminPermissions = []struct {
	Key         string
	Description string
}{
	{Key: "admin.access", Description: "Access administrative APIs and interfaces"},
	{Key: "admin.users.manage", Description: "Manage users and tenant memberships"},
	{Key: "admin.rbac.manage", Description: "Manage groups, roles and permissions"},
	{Key: "admin.audit.read", Description: "Read tenant audit events"},
	{Key: "tenant.read", Description: "Read tenant configuration"},
	{Key: "tenant.manage", Description: "Manage tenant configuration"},
	{Key: "assets.read", Description: "Read IT assets in the tenant"},
	{Key: "assets.manage", Description: "Create, update and archive IT assets"},
}

type BootstrapInput struct {
	TenantSlug            string
	TenantName            string
	AdminEmail            string
	AdminName             string
	AdminPassword         string
	RequirePasswordChange bool
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
	return b.run(ctx, input, false, RequestMeta{RequestID: "bootstrap"})
}

func (b *Bootstrapper) run(ctx context.Context, input BootstrapInput, initialSetup bool, meta RequestMeta) (BootstrapResult, error) {
	input.TenantSlug = strings.ToLower(strings.TrimSpace(input.TenantSlug))
	input.TenantName = strings.TrimSpace(input.TenantName)
	input.AdminEmail = normalizeLoginEmail(input.AdminEmail)
	input.AdminName = strings.TrimSpace(input.AdminName)

	validate := validateBootstrapSettings
	if initialSetup {
		validate = validateBootstrapInput
	}
	if err := validate(input); err != nil {
		return BootstrapResult{}, fmt.Errorf("%w: %v", ErrSetupInvalid, err)
	}

	var result BootstrapResult
	err := b.tx.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable}, func(exec platformdb.Executor) error {
		required, err := b.repo.LockInstallation(ctx, exec)
		if err != nil {
			return err
		}
		if initialSetup && !required {
			return ErrSetupCompleted
		}
		tenantID, err := b.repo.EnsureTenant(ctx, exec, input.TenantSlug, input.TenantName)
		if err != nil {
			return err
		}
		tenantStatus, err := b.repo.TenantStatusForUpdate(ctx, exec, tenantID)
		if err != nil {
			return err
		}
		if tenantStatus != "ACTIVE" {
			return fmt.Errorf("bootstrap tenant %q is not active", input.TenantSlug)
		}

		userID, created, existingPasswordHash, err := b.repo.EnsureUser(
			ctx,
			exec,
			input.AdminEmail,
			input.AdminName,
			func() (string, error) {
				if err := validateBootstrapInput(input); err != nil {
					return "", fmt.Errorf("%w: %v", ErrSetupInvalid, err)
				}
				return hashBootstrapPassword(input)
			},
			input.RequirePasswordChange || (input.AdminEmail == DefaultAdminEmail && input.AdminPassword == DefaultAdminPassword),
		)
		if err != nil {
			return err
		}
		if !created {
			active, activeErr := b.repo.UserActiveForUpdate(ctx, exec, userID)
			if activeErr != nil {
				return activeErr
			}
			if !active {
				return errors.New("bootstrap admin account already exists but is globally disabled")
			}
			valid, verifyErr := password.Verify(existingPasswordHash, input.AdminPassword)
			if verifyErr != nil {
				return fmt.Errorf("verify existing bootstrap admin password: %w", verifyErr)
			}
			if !valid {
				return errors.New("bootstrap admin already exists and the supplied password does not match")
			}
		}
		if err := b.repo.EnsureMembership(ctx, exec, tenantID, userID); err != nil {
			return err
		}

		roleID, err := b.repo.EnsureRole(ctx, exec, tenantID, "platform_admin", "Tenant platform administrator")
		if err != nil {
			return err
		}
		for _, permission := range platformAdminPermissions {
			permissionID, err := b.repo.EnsurePermission(ctx, exec, permission.Key, permission.Description)
			if err != nil {
				return err
			}
			if err := b.repo.EnsureRolePermission(ctx, exec, roleID, permissionID); err != nil {
				return err
			}
		}
		if err := b.repo.EnsureUserRole(ctx, exec, tenantID, userID, roleID); err != nil {
			return err
		}

		action, source := "bootstrap.platform_admin", "bootstrap"
		if initialSetup {
			action, source = "setup.platform_admin", "setup"
		}
		if err := b.audit.Append(ctx, exec, audit.Event{
			TenantID:    &tenantID,
			ActorUserID: &userID,
			Action:      action,
			ObjectType:  "tenant",
			ObjectID:    fmt.Sprint(tenantID),
			NewValues: map[string]any{
				"tenant_slug":  input.TenantSlug,
				"admin_email":  input.AdminEmail,
				"user_created": created,
			},
			RequestID: meta.RequestID,
			Source:    source,
			IPAddress: meta.IPAddress,
		}); err != nil {
			return err
		}
		if err := b.repo.CompleteInstallation(ctx, exec); err != nil {
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

func validateBootstrapInput(input BootstrapInput) error {
	if err := validateBootstrapSettings(input); err != nil {
		return err
	}
	if !(input.AdminEmail == DefaultAdminEmail && input.AdminPassword == DefaultAdminPassword) {
		return password.Validate(input.AdminPassword)
	}
	return nil
}

func validateBootstrapSettings(input BootstrapInput) error {
	if !tenantSlugPattern.MatchString(input.TenantSlug) {
		return errors.New("organization identifier must use lowercase letters, digits or hyphens and contain at most 100 characters")
	}
	if input.TenantName == "" || input.AdminName == "" || input.AdminEmail == "" {
		return errors.New("organization name, administrator name and login are required")
	}
	if utf8.RuneCountInString(input.TenantName) > 200 {
		return errors.New("organization name exceeds 200 characters")
	}
	if utf8.RuneCountInString(input.AdminName) > 200 {
		return errors.New("administrator name exceeds 200 characters")
	}
	if len(input.AdminEmail) > 254 {
		return errors.New("administrator email exceeds 254 bytes")
	}
	address, err := mail.ParseAddress(input.AdminEmail)
	if err != nil || address.Address != input.AdminEmail {
		return errors.New("administrator login must be admin or a valid email address")
	}
	if len(input.AdminPassword) > password.MaxPasswordBytes {
		return fmt.Errorf("administrator password exceeds %d bytes", password.MaxPasswordBytes)
	}
	if input.AdminPassword == "" {
		return errors.New("administrator password is required")
	}
	return nil
}

func hashBootstrapPassword(input BootstrapInput) (string, error) {
	if input.AdminEmail == DefaultAdminEmail && input.AdminPassword == DefaultAdminPassword {
		return password.HashWithMinimumLength(input.AdminPassword, len(DefaultAdminPassword))
	}
	return password.Hash(input.AdminPassword)
}
