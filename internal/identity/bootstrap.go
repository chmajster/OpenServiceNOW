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
}

type BootstrapInput struct {
	TenantSlug    string
	TenantName    string
	AdminEmail    string
	AdminName     string
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
	if utf8.RuneCountInString(input.TenantName) > 200 {
		return BootstrapResult{}, errors.New("bootstrap tenant name exceeds 200 characters")
	}
	if utf8.RuneCountInString(input.AdminName) > 200 {
		return BootstrapResult{}, errors.New("bootstrap admin name exceeds 200 characters")
	}
	if len(input.AdminEmail) > 254 {
		return BootstrapResult{}, errors.New("bootstrap admin email exceeds 254 bytes")
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
			passwordHash,
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

		if err := b.audit.Append(ctx, exec, audit.Event{
			TenantID:    &tenantID,
			ActorUserID: &userID,
			Action:      "bootstrap.platform_admin",
			ObjectType:  "tenant",
			ObjectID:    fmt.Sprint(tenantID),
			NewValues: map[string]any{
				"tenant_slug":  input.TenantSlug,
				"admin_email":  input.AdminEmail,
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
