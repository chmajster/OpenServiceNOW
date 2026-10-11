package main

import (
	"testing"

	"github.com/chmajster/OpenServiceNOW/internal/identity"
)

func TestBootstrapInputDefaultsAndOverrides(t *testing.T) {
	keys := []string{"BOOTSTRAP_TENANT_SLUG", "BOOTSTRAP_TENANT_NAME", "BOOTSTRAP_ADMIN_EMAIL", "BOOTSTRAP_ADMIN_NAME", "BOOTSTRAP_ADMIN_PASSWORD"}
	for _, key := range keys {
		t.Setenv(key, "")
	}
	want := identity.BootstrapInput{
		TenantSlug: "default", TenantName: "Default tenant",
		AdminEmail: identity.DefaultAdminEmail, AdminName: "Administrator",
		AdminPassword: identity.DefaultAdminPassword,
	}
	if got := bootstrapInput(); got != want {
		t.Fatalf("bootstrapInput() = %+v, want default configuration", got)
	}
	t.Setenv("BOOTSTRAP_TENANT_SLUG", "custom")
	t.Setenv("BOOTSTRAP_TENANT_NAME", "Custom tenant")
	t.Setenv("BOOTSTRAP_ADMIN_EMAIL", "custom@example.test")
	t.Setenv("BOOTSTRAP_ADMIN_NAME", "Custom administrator")
	t.Setenv("BOOTSTRAP_ADMIN_PASSWORD", "custom-bootstrap-password")
	want = identity.BootstrapInput{
		TenantSlug: "custom", TenantName: "Custom tenant",
		AdminEmail: "custom@example.test", AdminName: "Custom administrator",
		AdminPassword: "custom-bootstrap-password",
	}
	if got := bootstrapInput(); got != want {
		t.Fatal("bootstrapInput() did not preserve explicit configuration")
	}
}
