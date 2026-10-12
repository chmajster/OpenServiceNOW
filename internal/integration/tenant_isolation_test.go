//go:build integration

package integration

import (
	"context"
	"database/sql"
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
	lockName := "opensnow-integration-" + strings.ReplaceAll(suffix, ".", "")
	outerAcquired, err := database.WithAdvisoryLock(ctx, db, lockName, func(lockCtx context.Context) error {
		innerAcquired, err := database.WithAdvisoryLock(lockCtx, db, lockName, func(context.Context) error {
			return errors.New("second holder must not execute")
		})
		if err != nil {
			return fmt.Errorf("second advisory lock attempt: %w", err)
		}
		if innerAcquired {
			return errors.New("second advisory lock unexpectedly acquired")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("advisory lock integration: %v", err)
	}
	if !outerAcquired {
		t.Fatal("first advisory lock was not acquired")
	}

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
	usersService := users.NewService(usersRepo, tx, auditRepo, adminSafety, identityRepo)
	rbacService := rbac.NewService(rbacRepo, tx, auditRepo, adminSafety)
	actorA := rbac.Actor{TenantID: adminA.TenantID, UserID: adminA.UserID, RequestID: "integration"}
	groupID, err := rbacService.CreateGroup(ctx, actorA, rbac.CreateGroupInput{Name: "isolation-group"})
	if err != nil {
		t.Fatalf("create tenant A group: %v", err)
	}
	if err := rbacService.AddGroupMember(ctx, actorA, groupID, adminB.UserID); !errors.Is(err, rbac.ErrNotFound) {
		t.Fatalf("cross-tenant group membership error = %v, want ErrNotFound", err)
	}
	if err := rbacService.AddGroupMember(ctx, actorA, groupID, adminA.UserID); err != nil {
		t.Fatalf("add tenant A group member: %v", err)
	}
	memberPage, err := rbacService.ListGroupMembers(ctx, adminA.TenantID, groupID, 0, 1)
	if err != nil {
		t.Fatalf("list tenant A group members: %v", err)
	}
	if len(memberPage.Members) != 1 || memberPage.Members[0].UserID != adminA.UserID {
		t.Fatalf("group member page = %+v, want user %d", memberPage, adminA.UserID)
	}
	if _, err := rbacService.ListGroupMembers(ctx, adminB.TenantID, groupID, 0, 50); !errors.Is(err, rbac.ErrNotFound) {
		t.Fatalf("cross-tenant group read error = %v, want ErrNotFound", err)
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
	); !errors.Is(err, identity.ErrPlatformAdminRequired) {
		t.Fatalf("unauthorized system-role removal error = %v, want ErrPlatformAdminRequired", err)
	}
	if err := rbacService.RemoveRoleFromUser(ctx, actorA, platformAdminRoleID, adminA.UserID); !errors.Is(err, rbac.ErrInvalid) {
		t.Fatalf("self system-role removal error = %v, want ErrInvalid", err)
	}

	delegatedUserID, err := usersService.Create(ctx, users.Actor{
		TenantID: adminA.TenantID, UserID: adminA.UserID, RequestID: "integration",
	}, users.CreateInput{
		Email:       fmt.Sprintf("delegated-rbac-%s@example.test", strings.ReplaceAll(suffix, ".", "")),
		DisplayName: "Delegated RBAC Manager",
		Password:    "integration-password-delegated-rbac",
	})
	if err != nil {
		t.Fatalf("create delegated RBAC user: %v", err)
	}
	dormantAdminID, err := usersService.Create(ctx, users.Actor{
		TenantID: adminA.TenantID, UserID: adminA.UserID, RequestID: "integration",
	}, users.CreateInput{
		Email:       fmt.Sprintf("dormant-admin-%s@example.test", strings.ReplaceAll(suffix, ".", "")),
		DisplayName: "Dormant Admin",
		Password:    "integration-password-dormant-admin",
	})
	if err != nil {
		t.Fatalf("create dormant admin candidate: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = db.ExecContext(cleanupCtx, "DELETE FROM users WHERE id IN (?, ?)", delegatedUserID, dormantAdminID)
	})

	delegatedActor := rbac.Actor{
		TenantID:  adminA.TenantID,
		UserID:    delegatedUserID,
		RequestID: "integration",
	}
	if err := rbacService.AssignRoleToUser(
		ctx,
		delegatedActor,
		platformAdminRoleID,
		delegatedUserID,
	); !errors.Is(err, identity.ErrPlatformAdminRequired) {
		t.Fatalf("delegated system-role assignment error = %v, want ErrPlatformAdminRequired", err)
	}

	privilegedGroupID, err := rbacService.CreateGroup(
		ctx,
		actorA,
		rbac.CreateGroupInput{Name: "privileged-group-" + strings.ReplaceAll(suffix, ".", "")},
	)
	if err != nil {
		t.Fatalf("create privileged test group: %v", err)
	}
	if err := rbacService.AssignRoleToGroup(
		ctx,
		delegatedActor,
		privilegedGroupID,
		platformAdminRoleID,
	); !errors.Is(err, identity.ErrPlatformAdminRequired) {
		t.Fatalf("delegated group system-role assignment error = %v, want ErrPlatformAdminRequired", err)
	}
	if err := rbacService.AssignRoleToGroup(ctx, actorA, privilegedGroupID, platformAdminRoleID); err != nil {
		t.Fatalf("platform admin group-role assignment: %v", err)
	}
	if err := rbacService.AddGroupMember(
		ctx,
		delegatedActor,
		privilegedGroupID,
		delegatedUserID,
	); !errors.Is(err, identity.ErrPlatformAdminRequired) {
		t.Fatalf("delegated privileged-group join error = %v, want ErrPlatformAdminRequired", err)
	}
	if err := rbacService.AddGroupMember(ctx, actorA, privilegedGroupID, dormantAdminID); err != nil {
		t.Fatalf("platform admin privileged-group membership: %v", err)
	}
	if err := rbacService.RemoveGroupMember(
		ctx,
		delegatedActor,
		privilegedGroupID,
		dormantAdminID,
	); !errors.Is(err, identity.ErrPlatformAdminRequired) {
		t.Fatalf("delegated privileged-group removal error = %v, want ErrPlatformAdminRequired", err)
	}
	if err := rbacService.RemoveRoleFromGroup(
		ctx,
		delegatedActor,
		privilegedGroupID,
		platformAdminRoleID,
	); !errors.Is(err, identity.ErrPlatformAdminRequired) {
		t.Fatalf("delegated system-role group removal error = %v, want ErrPlatformAdminRequired", err)
	}

	if err := usersService.SetMembershipActive(ctx, users.Actor{
		TenantID: adminA.TenantID, UserID: delegatedUserID, RequestID: "integration",
	}, dormantAdminID, false); !errors.Is(err, identity.ErrPlatformAdminRequired) {
		t.Fatalf("delegated platform-admin disable error = %v, want ErrPlatformAdminRequired", err)
	}
	if err := usersService.SetMembershipActive(ctx, users.Actor{
		TenantID: adminA.TenantID, UserID: adminA.UserID, RequestID: "integration",
	}, dormantAdminID, false); err != nil {
		t.Fatalf("platform admin disables privileged membership: %v", err)
	}
	if err := usersService.SetMembershipActive(ctx, users.Actor{
		TenantID: adminA.TenantID, UserID: delegatedUserID, RequestID: "integration",
	}, dormantAdminID, true); !errors.Is(err, identity.ErrPlatformAdminRequired) {
		t.Fatalf("delegated dormant platform-admin activation error = %v, want ErrPlatformAdminRequired", err)
	}
	if err := usersService.SetMembershipActive(ctx, users.Actor{
		TenantID: adminA.TenantID, UserID: adminA.UserID, RequestID: "integration",
	}, dormantAdminID, true); err != nil {
		t.Fatalf("platform admin reactivates privileged membership: %v", err)
	}

	versionedRoleID, err := rbacService.CreateRole(ctx, actorA, rbac.CreateRoleInput{
		Name:        "versioned-role-" + strings.ReplaceAll(suffix, ".", ""),
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

	updatedRoles, err := rbacService.ListRoles(ctx, adminA.TenantID, 0, 50, "versioned-role-")
	if err != nil {
		t.Fatalf("reload versioned role: %v", err)
	}
	var updatedVersionedRole rbac.Role
	for _, role := range updatedRoles.Roles {
		if role.ID == versionedRole.ID {
			updatedVersionedRole = role
			break
		}
	}
	if updatedVersionedRole.Version != versionedRole.Version+1 {
		t.Fatalf("updated role version = %d want %d", updatedVersionedRole.Version, versionedRole.Version+1)
	}
	if err := rbacService.SetRolePermissions(
		ctx,
		actorA,
		updatedVersionedRole.ID,
		updatedVersionedRole.Version,
		[]string{"tenant.manage", "tenant.read"},
	); err != nil {
		t.Fatalf("idempotent role permission update: %v", err)
	}
	afterNoopRoles, err := rbacService.ListRoles(ctx, adminA.TenantID, 0, 50, "versioned-role-")
	if err != nil {
		t.Fatalf("reload role after no-op permission update: %v", err)
	}
	for _, role := range afterNoopRoles.Roles {
		if role.ID == updatedVersionedRole.ID && role.Version != updatedVersionedRole.Version {
			t.Fatalf("no-op role update changed version from %d to %d", updatedVersionedRole.Version, role.Version)
		}
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

	if err := rbacService.AssignRoleToUser(ctx, actorA, versionedRole.ID, adminA.UserID); err != nil {
		t.Fatalf("assign direct role to tenant A user: %v", err)
	}
	userRolePage, err := rbacService.ListUserRoles(ctx, adminA.TenantID, adminA.UserID, 0, 200)
	if err != nil {
		t.Fatalf("list tenant A user roles: %v", err)
	}
	if !containsAssignedRole(userRolePage.Roles, versionedRole.ID) {
		t.Fatalf("direct role %d missing from %+v", versionedRole.ID, userRolePage.Roles)
	}
	if _, err := rbacService.ListUserRoles(ctx, adminA.TenantID, adminB.UserID, 0, 50); !errors.Is(err, rbac.ErrNotFound) {
		t.Fatalf("foreign membership role read error = %v, want ErrNotFound", err)
	}
	if err := rbacService.RemoveRoleFromUser(ctx, actorA, versionedRole.ID, adminA.UserID); err != nil {
		t.Fatalf("remove direct role from tenant A user: %v", err)
	}
	userRolePage, err = rbacService.ListUserRoles(ctx, adminA.TenantID, adminA.UserID, 0, 200)
	if err != nil {
		t.Fatalf("reload tenant A user roles: %v", err)
	}
	if containsAssignedRole(userRolePage.Roles, versionedRole.ID) {
		t.Fatalf("removed direct role %d still present in %+v", versionedRole.ID, userRolePage.Roles)
	}

	if err := rbacService.AssignRoleToGroup(ctx, actorA, groupID, versionedRole.ID); err != nil {
		t.Fatalf("assign role to tenant A group: %v", err)
	}
	groupRolePage, err := rbacService.ListGroupRoles(ctx, adminA.TenantID, groupID, 0, 200)
	if err != nil {
		t.Fatalf("list tenant A group roles: %v", err)
	}
	if !containsAssignedRole(groupRolePage.Roles, versionedRole.ID) {
		t.Fatalf("group role %d missing from %+v", versionedRole.ID, groupRolePage.Roles)
	}
	if err := rbacService.RemoveRoleFromGroup(ctx, actorA, groupID, versionedRole.ID); err != nil {
		t.Fatalf("remove role from tenant A group: %v", err)
	}
	groupRolePage, err = rbacService.ListGroupRoles(ctx, adminA.TenantID, groupID, 0, 200)
	if err != nil {
		t.Fatalf("reload tenant A group roles: %v", err)
	}
	if containsAssignedRole(groupRolePage.Roles, versionedRole.ID) {
		t.Fatalf("removed group role %d still present in %+v", versionedRole.ID, groupRolePage.Roles)
	}

	if err := rbacService.RemoveGroupMember(ctx, actorA, groupID, adminA.UserID); err != nil {
		t.Fatalf("remove tenant A group member: %v", err)
	}
	memberPage, err = rbacService.ListGroupMembers(ctx, adminA.TenantID, groupID, 0, 50)
	if err != nil {
		t.Fatalf("reload tenant A group members: %v", err)
	}
	if len(memberPage.Members) != 0 {
		t.Fatalf("removed group member still present: %+v", memberPage.Members)
	}

	var adminBPasswordHashBefore string
	if err := db.QueryRowContext(
		ctx,
		"SELECT password_hash FROM users WHERE id = ?",
		adminB.UserID,
	).Scan(&adminBPasswordHashBefore); err != nil {
		t.Fatalf("read admin B password hash before tenant membership: %v", err)
	}

	addedExistingID, err := usersService.AddExistingMembership(
		ctx,
		users.Actor{
			TenantID:  adminA.TenantID,
			UserID:    adminA.UserID,
			RequestID: "integration",
		},
		users.AddExistingInput{Email: adminBEmail},
	)
	if err != nil {
		t.Fatalf("add existing global account to tenant A: %v", err)
	}
	if addedExistingID != adminB.UserID {
		t.Fatalf("added existing user id = %d want %d", addedExistingID, adminB.UserID)
	}

	var adminBPasswordHashAfter string
	if err := db.QueryRowContext(
		ctx,
		"SELECT password_hash FROM users WHERE id = ?",
		adminB.UserID,
	).Scan(&adminBPasswordHashAfter); err != nil {
		t.Fatalf("read admin B password hash after tenant membership: %v", err)
	}
	if adminBPasswordHashAfter != adminBPasswordHashBefore {
		t.Fatal("adding existing tenant membership changed the global account password hash")
	}

	linkedUser, linkedTenant, err := identityRepo.FindForLogin(ctx, tenantASlug, adminBEmail)
	if err != nil {
		t.Fatalf("find linked existing account for tenant A login: %v", err)
	}
	if linkedUser.ID != adminB.UserID || linkedTenant.ID != adminA.TenantID {
		t.Fatalf(
			"linked login identity user=%d tenant=%d want user=%d tenant=%d",
			linkedUser.ID,
			linkedTenant.ID,
			adminB.UserID,
			adminA.TenantID,
		)
	}

	if _, err := usersService.AddExistingMembership(
		ctx,
		users.Actor{
			TenantID:  adminA.TenantID,
			UserID:    adminA.UserID,
			RequestID: "integration",
		},
		users.AddExistingInput{Email: adminBEmail},
	); !errors.Is(err, users.ErrConflict) {
		t.Fatalf("duplicate existing membership error = %v, want ErrConflict", err)
	}

	linkedPage, err := usersService.List(ctx, adminA.TenantID, 0, 50, adminBEmail)
	if err != nil {
		t.Fatalf("search linked existing account in tenant A: %v", err)
	}
	if len(linkedPage.Users) != 1 || linkedPage.Users[0].ID != adminB.UserID {
		t.Fatalf("linked existing account search result = %+v", linkedPage.Users)
	}
	if err := usersService.SetMembershipActive(ctx, users.Actor{
		TenantID: adminA.TenantID, UserID: 999999, RequestID: "integration",
	}, adminA.UserID, false); !errors.Is(err, identity.ErrPlatformAdminRequired) {
		t.Fatalf("unauthorized platform-admin membership disable error = %v, want ErrPlatformAdminRequired", err)
	}
	if err := usersService.SetMembershipActive(ctx, users.Actor{
		TenantID: adminA.TenantID, UserID: adminA.UserID, RequestID: "integration",
	}, adminA.UserID, false); !errors.Is(err, users.ErrInvalid) {
		t.Fatalf("self membership disable error = %v, want ErrInvalid", err)
	}

	secondAdminID, err := usersService.Create(ctx, users.Actor{
		TenantID: adminA.TenantID, UserID: adminA.UserID, RequestID: "integration",
	}, users.CreateInput{
		Email:       fmt.Sprintf("second-admin-%s@example.test", strings.ReplaceAll(suffix, ".", "")),
		DisplayName: "Second Admin",
		Password:    "integration-password-second-admin",
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

	var tokenHash [32]byte
	var csrfHash [32]byte
	copy(tokenHash[:], []byte("integration-session-token-hash-0001"))
	copy(csrfHash[:], []byte("integration-session-csrf-hash-00001"))
	sessionID, err := createIntegrationSession(
		ctx,
		tx,
		identityRepo,
		adminA.TenantID,
		adminA.UserID,
		tokenHash,
		csrfHash,
	)
	if err != nil {
		t.Fatalf("create integration session: %v", err)
	}

	if err := usersService.SetMembershipActive(ctx, users.Actor{
		TenantID: adminA.TenantID, UserID: secondAdminID, RequestID: "integration",
	}, adminA.UserID, false); err != nil {
		t.Fatalf("disable first admin after second admin exists: %v", err)
	}

	var revoked bool
	if err := db.QueryRowContext(ctx, `
		SELECT revoked_at IS NOT NULL
		FROM sessions
		WHERE id = ?
	`, sessionID).Scan(&revoked); err != nil {
		t.Fatalf("read revoked session state: %v", err)
	}
	if !revoked {
		t.Fatal("disabling tenant membership must revoke active sessions")
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
	permissionAudit, err := auditReader.ListFiltered(ctx, adminA.TenantID, 0, 10, audit.Filter{Action: "rbac.role.permissions.set", ObjectID: fmt.Sprint(versionedRoleID)})
	if err != nil || len(permissionAudit.Events) == 0 {
		t.Fatalf("permission audit=%+v err=%v", permissionAudit, err)
	}
	for _, event := range permissionAudit.Events {
		if len(event.OldValues) == 0 || len(event.NewValues) == 0 {
			t.Fatalf("permission replacement audit lacks before/after values: %+v", event)
		}
	}
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

	filter := audit.Filter{Action: "tenant.name.update", ObjectType: "tenant", ObjectID: fmt.Sprint(adminA.TenantID), ActorUserID: secondAdminID, RequestID: "integration", From: time.Now().Add(-time.Hour), To: time.Now().Add(time.Hour)}
	filtered, err := auditReader.ListFiltered(ctx, adminA.TenantID, 0, 1, filter)
	if err != nil || len(filtered.Events) != 1 {
		t.Fatalf("filtered audit=%+v err=%v", filtered, err)
	}
	if page, err := auditReader.ListFiltered(ctx, adminB.TenantID, 0, 1, filter); err != nil || len(page.Events) != 0 {
		t.Fatalf("filtered audit leaked tenant: %+v %v", page, err)
	}
	for _, change := range []func(*audit.Filter){
		func(f *audit.Filter) { f.Action = "missing" }, func(f *audit.Filter) { f.ObjectType = "missing" }, func(f *audit.Filter) { f.ObjectID = "missing" }, func(f *audit.Filter) { f.ActorUserID = adminB.UserID }, func(f *audit.Filter) { f.RequestID = "missing" }, func(f *audit.Filter) { f.From = time.Now().Add(time.Hour) }, func(f *audit.Filter) { f.To = time.Now().Add(-time.Hour) },
	} {
		wrong := filter
		change(&wrong)
		page, err := auditReader.ListFiltered(ctx, adminA.TenantID, 0, 1, wrong)
		if err != nil || len(page.Events) != 0 {
			t.Fatalf("incorrect filter=%+v returned=%+v err=%v", wrong, page, err)
		}
	}
	if page, err := auditReader.ListFiltered(ctx, adminA.TenantID, filtered.Events[0].ID, 1, filter); err != nil || len(page.Events) != 0 {
		t.Fatalf("filtered cursor=%+v %v", page, err)
	}
	if role, err := rbacService.GetRole(ctx, adminA.TenantID, versionedRoleID); err != nil || role.ID != versionedRoleID {
		t.Fatalf("role detail=%+v %v", role, err)
	}
	if _, err := rbacService.GetRole(ctx, adminB.TenantID, versionedRoleID); !errors.Is(err, rbac.ErrNotFound) {
		t.Fatalf("cross-tenant role detail err=%v", err)
	}
	if group, err := rbacService.GetGroup(ctx, adminA.TenantID, groupID); err != nil || group.ID != groupID {
		t.Fatalf("group detail=%+v %v", group, err)
	}
	if _, err := rbacService.GetGroup(ctx, adminB.TenantID, groupID); !errors.Is(err, rbac.ErrNotFound) {
		t.Fatalf("cross-tenant group detail err=%v", err)
	}

	if _, err := db.ExecContext(ctx, "UPDATE tenants SET status = 'DISABLED' WHERE id = ?", adminB.TenantID); err != nil {
		t.Fatalf("disable tenant B for bootstrap test: %v", err)
	}
	if _, err := bootstrap.Run(ctx, identity.BootstrapInput{
		TenantSlug:    tenantBSlug,
		TenantName:    "Isolation B",
		AdminEmail:    adminBEmail,
		AdminName:     "Admin B",
		AdminPassword: "integration-password-b",
	}); err == nil {
		t.Fatal("bootstrap must reject disabled existing tenant")
	}
	if _, err := db.ExecContext(ctx, "UPDATE tenants SET status = 'ACTIVE' WHERE id = ?", adminB.TenantID); err != nil {
		t.Fatalf("restore tenant B status: %v", err)
	}

	if _, err := db.ExecContext(ctx, "UPDATE users SET active = FALSE WHERE id = ?", adminB.UserID); err != nil {
		t.Fatalf("disable admin B for bootstrap test: %v", err)
	}
	if _, err := bootstrap.Run(ctx, identity.BootstrapInput{
		TenantSlug:    tenantBSlug,
		TenantName:    "Isolation B",
		AdminEmail:    adminBEmail,
		AdminName:     "Admin B",
		AdminPassword: "integration-password-b",
	}); err == nil {
		t.Fatal("bootstrap must reject globally disabled existing admin")
	}
	if _, err := db.ExecContext(ctx, "UPDATE users SET active = TRUE WHERE id = ?", adminB.UserID); err != nil {
		t.Fatalf("restore admin B state: %v", err)
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

func createIntegrationSession(
	ctx context.Context,
	tx *database.TxManager,
	repo *identity.MySQLRepository,
	tenantID, userID uint64,
	tokenHash, csrfHash [32]byte,
) (uint64, error) {
	var sessionID uint64
	err := tx.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(exec database.Executor) error {
		var err error
		sessionID, err = repo.CreateSession(
			ctx,
			exec,
			tenantID,
			userID,
			tokenHash,
			csrfHash,
			time.Now().UTC().Add(time.Hour),
			"127.0.0.1",
			"integration-test",
		)
		return err
	})
	return sessionID, err
}

func containsAssignedRole(values []rbac.AssignedRole, roleID uint64) bool {
	for _, role := range values {
		if role.ID == roleID {
			return true
		}
	}
	return false
}
