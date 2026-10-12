//go:build integration

package integration

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chmajster/OpenServiceNOW/internal/audit"
	"github.com/chmajster/OpenServiceNOW/internal/identity"
	"github.com/chmajster/OpenServiceNOW/internal/platform/config"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/platform/password"
)

func TestWebInstallerAtomicCreationAndPermanentClosure(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	db, err := database.OpenMySQL(ctx, cfg.MySQL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	// This suite runs against a disposable, migrated integration database.
	var count int
	if err := db.QueryRowContext(ctx, "SELECT (SELECT COUNT(*) FROM users) + (SELECT COUNT(*) FROM tenants)").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("installer integration test requires an empty disposable database")
	}
	if _, err := db.ExecContext(ctx, "UPDATE installation_state SET completed_at=NULL WHERE id=1"); err != nil {
		t.Fatal(err)
	}
	repo := identity.NewMySQLRepository(db)
	tx := database.NewTxManager(db)
	auditRepo := audit.NewMySQLRepository()
	bootstrap := identity.NewBootstrapper(repo, tx, auditRepo)
	auth, err := identity.NewService(repo, tx, auditRepo, cfg.Session, cfg.Auth)
	if err != nil {
		t.Fatal(err)
	}
	installer := identity.NewInstaller(bootstrap, auth)
	assertRequired := func(want bool) {
		t.Helper()
		required, err := installer.Required(ctx)
		if err != nil || required != want {
			t.Fatalf("required=%t want=%t err=%v", required, want, err)
		}
	}
	assertRequired(true)
	input := identity.BootstrapInput{TenantSlug: "web-installer", TenantName: "Web installer", AdminEmail: "owner@example.test", AdminName: "Owner", AdminPassword: "installer-owner-password"}
	tables := []string{"users", "tenants", "roles", "permissions", "user_role_assignments", "tenant_memberships", "role_permissions", "audit_events"}
	baseline := make(map[string]int)
	for _, table := range tables {
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		baseline[table] = count
	}
	// A real SQL failure in the final audit insert must roll back every earlier write.
	if _, err := installer.Create(ctx, input, identity.RequestMeta{RequestID: strings.Repeat("x", 129)}); err == nil {
		t.Fatal("expected audit SQL failure")
	}
	for _, table := range tables {
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != baseline[table] {
			t.Fatalf("partial installation changed %s from %d to %d rows", table, baseline[table], count)
		}
	}
	assertRequired(true)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	start := make(chan struct{})
	for range 2 {
		wg.Go(func() {
			<-start
			_, err := installer.Create(ctx, input, identity.RequestMeta{RequestID: "installer-concurrency"})
			results <- err
		})
	}
	close(start)
	wg.Wait()
	close(results)
	success, conflict := 0, 0
	for err := range results {
		switch {
		case err == nil:
			success++
		case errors.Is(err, identity.ErrSetupCompleted):
			conflict++
		default:
			t.Fatalf("concurrent installation: %v", err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("success=%d conflict=%d", success, conflict)
	}
	assertRequired(false)
	var userID, tenantID uint64
	var hash string
	if err := db.QueryRowContext(ctx, "SELECT id,password_hash FROM users WHERE email=?", input.AdminEmail).Scan(&userID, &hash); err != nil {
		t.Fatal(err)
	}
	if valid, err := password.Verify(hash, input.AdminPassword); err != nil || !valid {
		t.Fatalf("password valid=%t err=%v", valid, err)
	}
	if err := db.QueryRowContext(ctx, "SELECT id FROM tenants WHERE slug=?", input.TenantSlug).Scan(&tenantID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = db.ExecContext(cleanupCtx, "DELETE FROM audit_events WHERE tenant_id=?", tenantID)
		_, _ = db.ExecContext(cleanupCtx, "DELETE FROM tenants WHERE id=?", tenantID)
		_, _ = db.ExecContext(cleanupCtx, "DELETE FROM users WHERE id=?", userID)
	})
	login, err := auth.Login(ctx, identity.LoginInput{Tenant: input.TenantSlug, Email: input.AdminEmail, Password: input.AdminPassword}, identity.RequestMeta{})
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"admin.access", "admin.users.manage", "admin.rbac.manage", "admin.audit.read", "tenant.read", "tenant.manage"} {
		if allowed, err := auth.HasPermission(ctx, login.Identity, key); err != nil || !allowed {
			t.Fatalf("permission %s allowed=%t err=%v", key, allowed, err)
		}
	}
	var events int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM audit_events WHERE action='setup.platform_admin' AND tenant_id=?", tenantID).Scan(&events); err != nil || events != 1 {
		t.Fatalf("events=%d err=%v", events, err)
	}
	var values string
	if err := db.QueryRowContext(ctx, "SELECT new_values FROM audit_events WHERE action='setup.platform_admin' AND tenant_id=?", tenantID).Scan(&values); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(values, input.AdminPassword) || strings.Contains(values, hash) {
		t.Fatal("audit contains credentials")
	}
	// A new service instance sees the same closed state after an application restart.
	installer = identity.NewInstaller(identity.NewBootstrapper(identity.NewMySQLRepository(db), tx, auditRepo), auth)
	assertRequired(false)
	if _, err := installer.Create(ctx, input, identity.RequestMeta{}); !errors.Is(err, identity.ErrSetupCompleted) {
		t.Fatalf("replay error=%v", err)
	}
	if _, err := bootstrap.Run(ctx, input); err != nil {
		t.Fatalf("headless bootstrap after web setup: %v", err)
	}
	var unchangedHash string
	if err := db.QueryRowContext(ctx, "SELECT password_hash FROM users WHERE id=?", userID).Scan(&unchangedHash); err != nil || unchangedHash != hash {
		t.Fatal("bootstrap changed the installed password")
	}
	if _, err := db.ExecContext(ctx, "DELETE FROM audit_events WHERE tenant_id=?", tenantID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "DELETE FROM tenants WHERE id=?", tenantID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "DELETE FROM users WHERE id=?", userID); err != nil {
		t.Fatal(err)
	}
	assertRequired(false)
	// Missing installation metadata must fail closed rather than exposing setup.
	if _, err := db.ExecContext(ctx, "DELETE FROM installation_state WHERE id=1"); err != nil {
		t.Fatal(err)
	}
	if required, err := installer.Required(ctx); err == nil || required {
		t.Fatalf("missing marker required=%t err=%v", required, err)
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO installation_state(id,completed_at) VALUES(1,UTC_TIMESTAMP(6))"); err != nil {
		t.Fatal(err)
	}
}
