//go:build integration

package integration

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/chmajster/OpenServiceNOW/internal/audit"
	"github.com/chmajster/OpenServiceNOW/internal/identity"
	"github.com/chmajster/OpenServiceNOW/internal/platform/config"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/users"
)

func TestDefaultAdministratorAuthenticationAndPersistence(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := database.OpenMySQL(ctx, cfg.MySQL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	tx := database.NewTxManager(db)
	auditRepo := audit.NewMySQLRepository()
	repo := identity.NewMySQLRepository(db)
	bootstrap := identity.NewBootstrapper(repo, tx, auditRepo)
	input := identity.BootstrapInput{
		TenantSlug: fmt.Sprintf("default-admin-%d", time.Now().UnixNano()), TenantName: "Default administrator test",
		AdminEmail: identity.DefaultAdminEmail, AdminName: "Administrator", AdminPassword: identity.DefaultAdminPassword,
	}
	admin, err := bootstrap.Run(ctx, input)
	if err != nil {
		t.Fatalf("bootstrap default administrator: %v", err)
	}
	if required, err := repo.SetupRequired(ctx); err != nil || required {
		t.Fatalf("headless bootstrap did not close web setup: required=%t err=%v", required, err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_, _ = db.ExecContext(cleanupCtx, "DELETE FROM audit_events WHERE tenant_id = ?", admin.TenantID)
		_, _ = db.ExecContext(cleanupCtx, "DELETE FROM tenants WHERE id = ?", admin.TenantID)
		if admin.UserCreated {
			_, _ = db.ExecContext(cleanupCtx, "DELETE FROM users WHERE id = ?", admin.UserID)
		}
	})
	var originalHash string
	if err := db.QueryRowContext(ctx, "SELECT password_hash FROM users WHERE id = ?", admin.UserID).Scan(&originalHash); err != nil {
		t.Fatal(err)
	}
	if again, err := bootstrap.Run(ctx, input); err != nil || again.UserCreated || again.UserID != admin.UserID {
		t.Fatalf("idempotent bootstrap: result=%+v err=%v", again, err)
	}
	service, err := identity.NewService(repo, tx, auditRepo, cfg.Session, cfg.Auth)
	if err != nil {
		t.Fatal(err)
	}
	for _, identifier := range []string{identity.DefaultAdminLogin, identity.DefaultAdminEmail, " ADMIN "} {
		result, err := service.Login(ctx, identity.LoginInput{Tenant: input.TenantSlug, Email: identifier, Password: identity.DefaultAdminPassword}, identity.RequestMeta{})
		if err != nil || result.Identity.UserID != admin.UserID {
			t.Fatalf("login using %q: result=%+v err=%v", identifier, result.Identity, err)
		}
		if restored, err := service.Authenticate(ctx, result.SessionToken); err != nil || restored.UserID != admin.UserID {
			t.Fatalf("restore database session: identity=%+v err=%v", restored, err)
		}
		if allowed, err := service.HasPermission(ctx, result.Identity, "admin.users.manage"); err != nil || allowed || !result.Identity.PasswordChangeRequired {
			t.Fatalf("default administrator permission: allowed=%t err=%v", allowed, err)
		}
		if err := service.Logout(ctx, result.Identity, identity.RequestMeta{}); err != nil {
			t.Fatal(err)
		}
		if _, err := service.Authenticate(ctx, result.SessionToken); !errors.Is(err, identity.ErrUnauthenticated) {
			t.Fatalf("revoked session error=%v", err)
		}
	}
	for _, candidate := range []identity.LoginInput{
		{Tenant: input.TenantSlug, Email: "admin", Password: "wrong"},
		{Tenant: "missing-tenant", Email: "admin", Password: "admin"},
		{Tenant: input.TenantSlug, Email: "other", Password: "admin"},
	} {
		if _, err := service.Login(ctx, candidate, identity.RequestMeta{}); !errors.Is(err, identity.ErrInvalidCredentials) {
			t.Fatalf("invalid login returned %v", err)
		}
	}
	usersService := users.NewService(users.NewMySQLRepository(db), tx, auditRepo, identity.NewAdminSafetyRepository(), repo, service)
	if _, err := usersService.Create(ctx, users.Actor{TenantID: admin.TenantID, UserID: admin.UserID}, users.CreateInput{
		Email: "short-password@example.test", DisplayName: "Short password", Password: "admin",
	}); !errors.Is(err, users.ErrInvalid) {
		t.Fatalf("ordinary user password policy error=%v", err)
	}
	if !admin.UserCreated {
		return // Never change credentials of an account that existed before this test.
	}
	current, err := service.Login(ctx, identity.LoginInput{Tenant: input.TenantSlug, Email: "admin", Password: "admin"}, identity.RequestMeta{})
	if err != nil {
		t.Fatal(err)
	}
	changed, err := service.ChangePassword(ctx, current.Identity, identity.PasswordInput{CurrentPassword: "admin", NewPassword: "changed-administrator-password", CredentialVersion: current.Identity.CredentialVersion}, identity.RequestMeta{})
	if err != nil {
		t.Fatal(err)
	}
	if allowed, err := service.HasPermission(ctx, changed.Identity, "admin.users.manage"); err != nil || !allowed {
		t.Fatal("changed administrator remains restricted")
	}
	var changedHash string
	if err := db.QueryRowContext(ctx, "SELECT password_hash FROM users WHERE id=?", admin.UserID).Scan(&changedHash); err != nil {
		t.Fatal(err)
	}
	if _, err := bootstrap.Run(ctx, input); err == nil {
		t.Fatal("bootstrap must reject a mismatched existing password")
	}
	var persistedHash string
	if err := db.QueryRowContext(ctx, "SELECT password_hash FROM users WHERE id = ?", admin.UserID).Scan(&persistedHash); err != nil {
		t.Fatal(err)
	}
	if persistedHash != changedHash || persistedHash == originalHash {
		t.Fatal("bootstrap reset the existing administrator password")
	}
	if _, err := service.Login(ctx, identity.LoginInput{Tenant: input.TenantSlug, Email: "admin", Password: "admin"}, identity.RequestMeta{}); !errors.Is(err, identity.ErrInvalidCredentials) {
		t.Fatalf("default password bypasses changed credentials: %v", err)
	}
	input.AdminPassword = "changed-administrator-password"
	input.RequirePasswordChange = true
	if _, err := bootstrap.Run(ctx, input); err != nil {
		t.Fatal(err)
	}
	if restored, err := service.Authenticate(ctx, changed.SessionToken); err != nil || restored.PasswordChangeRequired {
		t.Fatal("re-bootstrap imposed a temporary-password flag on an existing account")
	}
}
