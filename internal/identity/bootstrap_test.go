package identity

import (
	"context"
	"strings"
	"testing"
)

func TestBootstrapRejectsOversizedNamesBeforeDatabaseAccess(t *testing.T) {
	bootstrapper := &Bootstrapper{}
	_, err := bootstrapper.Run(context.Background(), BootstrapInput{
		TenantSlug: "default",
		TenantName: strings.Repeat("a", 201),
		AdminEmail: "admin@example.test",
		AdminName: "Administrator",
		AdminPassword: "bootstrap-password-1234",
	})
	if err == nil {
		t.Fatal("expected oversized tenant name error")
	}
}

func TestBootstrapRejectsOversizedEmailBeforeDatabaseAccess(t *testing.T) {
	bootstrapper := &Bootstrapper{}
	_, err := bootstrapper.Run(context.Background(), BootstrapInput{
		TenantSlug: "default",
		TenantName: "Default",
		AdminEmail: strings.Repeat("a", 250) + "@x.test",
		AdminName: "Administrator",
		AdminPassword: "bootstrap-password-1234",
	})
	if err == nil {
		t.Fatal("expected oversized admin email error")
	}
}
