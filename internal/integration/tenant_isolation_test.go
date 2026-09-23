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
	adminB, err := bootstrap.Run(ctx, identity.BootstrapInput{
		TenantSlug: tenantBSlug, TenantName: "Isolation B",
		AdminEmail: adminBEmail, AdminName: "Admin B",
		AdminPassword: "integration-password-b",
	})
	if err != nil {
		t.Fatalf("bootstrap tenant B: %v", err)
	}

	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = db.ExecContext(cleanupCtx, "DELETE FROM tenants WHERE id IN (?, ?)", adminA.TenantID, adminB.TenantID)
		_, _ = db.ExecContext(cleanupCtx, "DELETE FROM users WHERE id IN (?, ?)", adminA.UserID, adminB.UserID)
	})

	usersRepo := users.NewMySQLRepository(db)
	usersA, err := usersRepo.List(ctx, adminA.TenantID)
	if err != nil {
		t.Fatalf("list tenant A users: %v", err)
	}
	if containsUser(usersA, adminB.UserID) {
		t.Fatalf("tenant B user %d leaked into tenant A listing", adminB.UserID)
	}
	if !containsUser(usersA, adminA.UserID) {
		t.Fatalf("tenant A admin %d missing from tenant A listing", adminA.UserID)
	}

	rbacRepo := rbac.NewMySQLRepository(db)
	rbacService := rbac.NewService(rbacRepo, tx, auditRepo)
	actorA := rbac.Actor{TenantID: adminA.TenantID, UserID: adminA.UserID, RequestID: "integration"}
	groupID, err := rbacService.CreateGroup(ctx, actorA, rbac.CreateGroupInput{Name: "isolation-group"})
	if err != nil {
		t.Fatalf("create tenant A group: %v", err)
	}
	if err := rbacService.AddGroupMember(ctx, actorA, groupID, adminB.UserID); !errors.Is(err, rbac.ErrNotFound) {
		t.Fatalf("cross-tenant group membership error = %v, want ErrNotFound", err)
	}

	tenantRepo := tenants.NewMySQLRepository(db)
	tenantService := tenants.NewService(tenantRepo, tx, auditRepo)
	current, err := tenantService.Get(ctx, adminA.TenantID)
	if err != nil {
		t.Fatalf("get tenant A: %v", err)
	}
	updated, err := tenantService.UpdateName(ctx, tenants.Actor{
		TenantID: adminA.TenantID, UserID: adminA.UserID, RequestID: "integration",
	}, current.Version, "Isolation A Updated")
	if err != nil {
		t.Fatalf("update tenant A: %v", err)
	}
	if updated.Version != current.Version+1 {
		t.Fatalf("tenant version = %d want %d", updated.Version, current.Version+1)
	}
	_, err = tenantService.UpdateName(ctx, tenants.Actor{
		TenantID: adminA.TenantID, UserID: adminA.UserID, RequestID: "integration",
	}, current.Version, "Stale Update")
	if !errors.Is(err, tenants.ErrConflict) {
		t.Fatalf("stale tenant update error = %v, want ErrConflict", err)
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
