//go:build integration

package integration

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/chmajster/OpenServiceNOW/internal/audit"
	"github.com/chmajster/OpenServiceNOW/internal/identity"
	"github.com/chmajster/OpenServiceNOW/internal/platform/config"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/rbac"
	"github.com/chmajster/OpenServiceNOW/internal/tenants"
	"github.com/chmajster/OpenServiceNOW/internal/users"
)

func TestTenantIsolationAndOptimisticLocking(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db, err := database.OpenMySQL(ctx, cfg.MySQL)
	if err != nil {
		t.Fatalf("database.OpenMySQL() error = %v", err)
	}
	defer db.Close()

	suffix := time.Now().UTC().Format("150405.000000000")
	tenantASlug := "isolation-a-" + strings.ReplaceAll(suffix, ".", "")
	tenantBSlug := "isolation-b-" + strings.ReplaceAll(suffix, ".", "")
	adminAEmail := fmt.Sprintf("admin-a-%s@example.test", strings.ReplaceAll(suffix, ".", ""))
	adminBEmail := fmt.Sprintf("admin-b-%s@example.test", strings.ReplaceAll(suffix, ".", ""))

	tx := database.NewTxManager(db)
	auditRepo := audit.NewMySQLRepository()
	identityRepo := identity.NewMySQLRepository(db)
	bootstrap := identity.NewBootstrapper(identityRepo, tx, auditRepo)

	adminA, err := bootstrap.Run(ctx, identity.BootstrapInput{
		TenantSlug: tenantASlug, TenantName: "Isolation A",
		AdminEmail: adminAEmail, AdminName: "Admin A",
		AdminPassword: "integration-password-a",
	})
	if err != nil {
		t.Fatalf("bootstrap tenant A: %v", err)
	}

	if _, err := bootstrap.Run(ctx, identity.BootstrapInput{
		TenantSlug: tenantASlug, TenantName: "Unexpected Rename",
		AdminEmail: adminAEmail, AdminName: "Different Name",
		AdminPassword: "wrong-existing-password",
	}); err == nil {
		t.Fatal("re-bootstrap with wrong existing password must fail")
	}
	rebootstrap, err := bootstrap.Run(ctx, identity.BootstrapInput{
		TenantSlug: tenantASlug, TenantName: "Unexpected Rename",
		AdminEmail: adminAEmail, AdminName: "Different Name",
		AdminPassword: "integration-password-a",
	})
	if err != nil {
		t.Fatalf("re-bootstrap tenant A with matching password: %v", err)
	}
	if rebootstrap.TenantID != adminA.TenantID || rebootstrap.UserID != adminA.UserID || rebootstrap.UserCreated {
		t.Fatalf("unexpected re-bootstrap result: %+v", rebootstrap)
	}
	var persistedTenantName string
	if err := db.QueryRowContext(ctx, "SELECT name FROM tenants WHERE id = ?", adminA.TenantID).Scan(&persistedTenantName); err != nil {
		t.Fatalf("read tenant name after re-bootstrap: %v", err)
	}
	if persistedTenantName != "Isolation A" {
		t.Fatalf("tenant name changed during re-bootstrap: %q", persistedTenantName)
	}

	adminB, err := bootstrap.Run(ctx, identity.BootstrapInput{
		TenantSlug: tenantBSlug, TenantName: "Isolation B",
		AdminEmail: adminBEmail, AdminName: "Admin B",
		AdminPassword: "integration-password-b",
	})
	if err != nil {
		t.Fatalf("bootstrap tenant B: %v", err)
	}

	futurePermission := "future.permission." + strings.ReplaceAll(suffix, ".", "")
	if _, err := db.ExecContext(ctx, `
		INSERT INTO permissions (permission_key, description)
		VALUES (?, 'integration dynamic permission')
	`, futurePermission); err != nil {
		t.Fatalf("insert future permission: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = db.ExecContext(cleanupCtx, "DELETE FROM permissions WHERE permission_key = ?", futurePermission)
	})

	allowed, err := identityRepo.HasPermission(ctx, adminA.TenantID, adminA.UserID, futurePermission)
	if err != nil {
		t.Fatalf("check dynamic platform admin permission: %v", err)
	}
	if !allowed {
		t.Fatal("platform_admin must automatically receive new catalog permissions")
	}
	allowed, err = identityRepo.HasPermission(ctx, adminA.TenantID, adminA.UserID, futurePermission+".missing")
	if err != nil {
		t.Fatalf("check unknown permission: %v", err)
	}
	if allowed {
		t.Fatal("platform_admin must not receive permission keys absent from the catalog")
	}
	effectivePermissions, err := identityRepo.ListPermissions(ctx, adminA.TenantID, adminA.UserID)
	if err != nil {
		t.Fatalf("list dynamic platform admin permissions: %v", err)
	}
	if !containsString(effectivePermissions, futurePermission) {
		t.Fatalf("dynamic permission %q missing from platform_admin permission list", futurePermission)
	}

	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = db.ExecContext(
			cleanupCtx,
			"DELETE FROM audit_events WHERE tenant_id IN (?, ?)",
			adminA.TenantID,
			adminB.TenantID,
		)
		_, _ = db.ExecContext(
			cleanupCtx,
			"DELETE FROM sessions WHERE tenant_id IN (?, ?)",
			adminA.TenantID,
			adminB.TenantID,
		)
		_, _ = db.ExecContext(cleanupCtx, "DELETE FROM tenants WHERE id IN (?, ?)", adminA.TenantID, adminB.TenantID)
		_, _ = db.ExecContext(cleanupCtx, "DELETE FROM users WHERE id IN (?, ?)", adminA.UserID, adminB.UserID)
	})

	usersRepo := users.NewMySQLRepository(db)
	usersPageA, err := usersRepo.List(ctx, adminA.TenantID, 0, 50, "")
	if err != nil {
		t.Fatalf("list tenant A users: %v", err)
	}
	if containsUser(usersPageA.Users, adminB.UserID) {
		t.Fatalf("tenant B user %d leaked into tenant A listing", adminB.UserID)
	}
	if !containsUser(usersPageA.Users, adminA.UserID) {
		t.Fatalf("tenant A admin %d missing from tenant A listing", adminA.UserID)
	}

	rbacRepo := rbac.NewMySQLRepository(db)
	adminSafety := identity.NewAdminSafetyRepository()
	rbacService := rbac.NewService(rbacRepo, tx, auditRepo, adminSafety)
	actorA := rbac.Actor{TenantID: adminA.TenantID, UserID: adminA.UserID, RequestID: "integration"}
	groupID, err := rbacService.CreateGroup(ctx, actorA, rbac.CreateGroupInput{Name: "isolation-group"})
	if err != nil {
		t.Fatalf("create tenant A group: %v", err)
	}
	if err := rbacService.AddGroupMember(ctx, actorA, groupID, adminB.UserID); !errors.Is(err, rbac.ErrNotFound) {
		t.Fatalf("cross-tenant group membership error = %v, want ErrNotFound", err)
	}

	rolesPage, err := rbacService.ListRoles(ctx, adminA.TenantID, 0, 50, "")
	if err != nil {
		t.Fatalf("list tenant A roles: %v", err)
	}
	var platformAdminRoleID uint64
	var platformAdminRole rbac.Role
	for _, role := range rolesPage.Roles {
		if role.Name == "platform_admin" {
			platformAdminRoleID = role.ID
			platformAdminRole = role
			break
		}
	}
	if platformAdminRoleID == 0 {
		t.Fatal("platform_admin role not found")
	}
	if !roleContainsPermission(platformAdminRole, futurePermission) {
		t.Fatalf("platform_admin role view missing dynamic permission %q", futurePermission)
	}
	if err := rbacService.SetRolePermissions(
		ctx,
		actorA,
		platformAdminRoleID,
		platformAdminRole.Version,
		[]string{"tenant.read"},
	); !errors.Is(err, rbac.ErrInvalid) {
		t.Fatalf("system role permission update error = %v, want ErrInvalid", err)
	}
	if _, err := rbacService.CreateRole(ctx, actorA, rbac.CreateRoleInput{
		Name: "platform_admin",
	}); !errors.Is(err, rbac.ErrInvalid) {
		t.Fatalf("reserved platform_admin create error = %v, want ErrInvalid", err)
	}
	if err := rbacService.RemoveRoleFromUser(
		ctx,
		rbac.Actor{TenantID: adminA.TenantID, UserID: 999999, RequestID: "integration"},
		platformAdminRoleID,
		adminA.UserID,
	); !errors.Is(err, identity.ErrLastPlatformAdmin) {
		t.Fatalf("last-admin role removal error = %v, want ErrLastPlatformAdmin", err)
	}
	if err := rbacService.RemoveRoleFromUser(ctx, actorA, platformAdminRoleID, adminA.UserID); !errors.Is(err, rbac.ErrInvalid) {
		t.Fatalf("self system-role removal error = %v, want ErrInvalid", err)
	}

	versionedRoleID, err := rbacService.CreateRole(ctx, actorA, rbac.CreateRoleInput{
		Name: "versioned-role-" + strings.ReplaceAll(suffix, ".", ""),
		Permissions: []string{"tenant.read"},
	})
	if err != nil {
		t.Fatalf("create versioned role: %v", err)
	}
	versionedRoles, err := rbacService.ListRoles(ctx, adminA.TenantID, 0, 50, "versioned-role-")
	if err != nil {
		t.Fatalf("list versioned role: %v", err)
	}
	var versionedRole rbac.Role
	for _, role := range versionedRoles.Roles {
		if role.ID == versionedRoleID {
			versionedRole = role
			break
		}
	}
	if versionedRole.ID == 0 {
		t.Fatal("versioned role not found")
	}
	if versionedRole.Version != 1 {
		t.Fatalf("new role version = %d want 1", versionedRole.Version)
	}
	if err := rbacService.SetRolePermissions(
		ctx,
		actorA,
		versionedRole.ID,
		versionedRole.Version,
		[]string{"tenant.read", "tenant.manage"},
	); err != nil {
		t.Fatalf("replace versioned role permissions: %v", err)
	}
	if err := rbacService.SetRolePermissions(
		ctx,
		actorA,
		versionedRole.ID,
		versionedRole.Version,
		[]string{"tenant.read"},
	); !errors.Is(err, rbac.ErrConflict) {
		t.Fatalf("stale role permission update error = %v, want ErrConflict", err)
	}

	usersService := users.NewService(usersRepo, tx, auditRepo, adminSafety)
	if err := usersService.SetMembershipActive(ctx, users.Actor{
		TenantID: adminA.TenantID, UserID: 999999, RequestID: "integration",
	}, adminA.UserID, false); !errors.Is(err, identity.ErrLastPlatformAdmin) {
		t.Fatalf("last-admin membership disable error = %v, want ErrLastPlatformAdmin", err)
	}
	if err := usersService.SetMembershipActive(ctx, users.Actor{
		TenantID: adminA.TenantID, UserID: adminA.UserID, RequestID: "integration",
	}, adminA.UserID, false); !errors.Is(err, users.ErrInvalid) {
		t.Fatalf("self membership disable error = %v, want ErrInvalid", err)
	}

	secondAdminID, err := usersService.Create(ctx, users.Actor{
		TenantID: adminA.TenantID, UserID: adminA.UserID, RequestID: "integration",
	}, users.CreateInput{
		Email: fmt.Sprintf("second-admin-%s@example.test", strings.ReplaceAll(suffix, ".", "")),
		DisplayName: "Second Admin",
		Password: "integration-password-second-admin",
	})
	if err != nil {
		t.Fatalf("create second admin user: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = db.ExecContext(cleanupCtx, "DELETE FROM users WHERE id = ?", secondAdminID)
	})

	if err := rbacService.AssignRoleToUser(ctx, actorA, platformAdminRoleID, secondAdminID); err != nil {
		t.Fatalf("assign platform_admin to second admin: %v", err)
	}

	firstPage, err := usersService.List(ctx, adminA.TenantID, 0, 1, "")
	if err != nil {
		t.Fatalf("list first user page: %v", err)
	}
	if len(firstPage.Users) != 1 || firstPage.NextAfterID == 0 {
		t.Fatalf("first user page = %+v", firstPage)
	}
	secondPage, err := usersService.List(ctx, adminA.TenantID, firstPage.NextAfterID, 1, "")
	if err != nil {
		t.Fatalf("list second user page: %v", err)
	}
	if len(secondPage.Users) != 1 || secondPage.Users[0].ID == firstPage.Users[0].ID {
		t.Fatalf("second user page = %+v", secondPage)
	}
	searchPage, err := usersService.List(ctx, adminA.TenantID, 0, 50, "Second Admin")
	if err != nil {
		t.Fatalf("search users: %v", err)
	}
	if len(searchPage.Users) != 1 || searchPage.Users[0].ID != secondAdminID {
		t.Fatalf("search result = %+v, want second admin %d", searchPage.Users, secondAdminID)
	}

	if err := usersService.SetMembershipActive(ctx, users.Actor{
		TenantID: adminA.TenantID, UserID: secondAdminID, RequestID: "integration",
	}, adminA.UserID, false); err != nil {
		t.Fatalf("disable first admin after second admin exists: %v", err)
	}

	tenantRepo := tenants.NewMySQLRepository(db)
	tenantService := tenants.NewService(tenantRepo, tx, auditRepo)
	current, err := tenantService.Get(ctx, adminA.TenantID)
	if err != nil {
		t.Fatalf("get tenant A: %v", err)
	}
	updated, err := tenantService.UpdateName(ctx, tenants.Actor{
		TenantID: adminA.TenantID, UserID: secondAdminID, RequestID: "integration",
	}, current.Version, "Isolation A Updated")
	if err != nil {
		t.Fatalf("update tenant A: %v", err)
	}
	if updated.Version != current.Version+1 {
		t.Fatalf("tenant version = %d want %d", updated.Version, current.Version+1)
	}
	_, err = tenantService.UpdateName(ctx, tenants.Actor{
		TenantID: adminA.TenantID, UserID: secondAdminID, RequestID: "integration",
	}, current.Version, "Stale Update")
	if !errors.Is(err, tenants.ErrConflict) {
		t.Fatalf("stale tenant update error = %v, want ErrConflict", err)
	}

	auditReader := audit.NewReader(db)
	auditPageOne, err := auditReader.List(ctx, adminA.TenantID, 0, 2)
	if err != nil {
		t.Fatalf("list first audit page: %v", err)
	}
	if len(auditPageOne.Events) != 2 || auditPageOne.NextBeforeID == 0 {
		t.Fatalf(
			"first audit page events=%d next_before_id=%d",
			len(auditPageOne.Events),
			auditPageOne.NextBeforeID,
		)
	}
	auditPageTwo, err := auditReader.List(ctx, adminA.TenantID, auditPageOne.NextBeforeID, 2)
	if err != nil {
		t.Fatalf("list second audit page: %v", err)
	}
	for _, event := range auditPageTwo.Events {
		if event.ID >= auditPageOne.NextBeforeID {
			t.Fatalf(
				"audit cursor returned non-older event id=%d cursor=%d",
				event.ID,
				auditPageOne.NextBeforeID,
			)
		}
	}

	tenantBAudit, err := auditReader.List(ctx, adminB.TenantID, 0, 50)
	if err != nil {
		t.Fatalf("list tenant B audit: %v", err)
	}
	for _, event := range tenantBAudit.Events {
		if event.ObjectID == fmt.Sprint(adminA.TenantID) && event.Action == "tenant.name.update" {
			t.Fatal("tenant A audit event leaked into tenant B audit page")
		}
	}
}

func containsUser(values []users.User, userID uint64) bool {
	for _, user := range values {
		if user.ID == userID {
			return true
		}
	}
	return false
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func roleContainsPermission(role rbac.Role, key string) bool {
	for _, permission := range role.Permissions {
		if permission.Key == key {
			return true
		}
	}
	return false
}
