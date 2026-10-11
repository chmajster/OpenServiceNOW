package main

import (
	"context"
	"log/slog"
	"os"
	"strconv"

	"github.com/chmajster/OpenServiceNOW/internal/audit"
	"github.com/chmajster/OpenServiceNOW/internal/identity"
	"github.com/chmajster/OpenServiceNOW/internal/platform/config"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := config.Load()
	if err != nil {
		logger.Error("configuration invalid", "error", err)
		os.Exit(1)
	}

	input := bootstrapInput()

	db, err := database.OpenMySQL(context.Background(), cfg.MySQL)
	if err != nil {
		logger.Error("mysql unavailable", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	bootstrapper := identity.NewBootstrapper(
		identity.NewMySQLRepository(db),
		database.NewTxManager(db),
		audit.NewMySQLRepository(),
	)
	result, err := bootstrapper.Run(context.Background(), input)
	if err != nil {
		logger.Error("bootstrap failed", "error", err)
		os.Exit(1)
	}

	logger.Info("bootstrap complete",
		"tenant_id", strconv.FormatUint(result.TenantID, 10),
		"user_id", strconv.FormatUint(result.UserID, 10),
		"role_id", strconv.FormatUint(result.RoleID, 10),
		"user_created", result.UserCreated,
	)
}

func bootstrapInput() identity.BootstrapInput {
	return identity.BootstrapInput{
		TenantSlug:    envOrDefault("BOOTSTRAP_TENANT_SLUG", "default"),
		TenantName:    envOrDefault("BOOTSTRAP_TENANT_NAME", "Default tenant"),
		AdminEmail:    envOrDefault("BOOTSTRAP_ADMIN_EMAIL", identity.DefaultAdminEmail),
		AdminName:     envOrDefault("BOOTSTRAP_ADMIN_NAME", "Administrator"),
		AdminPassword: envOrDefault("BOOTSTRAP_ADMIN_PASSWORD", identity.DefaultAdminPassword),
	}
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
