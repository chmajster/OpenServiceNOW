package main

import (
	"context"
	"log/slog"
	"os"
	"strings"

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

	input := identity.BootstrapInput{
		TenantSlug:    os.Getenv("BOOTSTRAP_TENANT_SLUG"),
		TenantName:    os.Getenv("BOOTSTRAP_TENANT_NAME"),
		AdminEmail:    os.Getenv("BOOTSTRAP_ADMIN_EMAIL"),
		AdminName:     os.Getenv("BOOTSTRAP_ADMIN_NAME"),
		AdminPassword: os.Getenv("BOOTSTRAP_ADMIN_PASSWORD"),
	}
	if strings.TrimSpace(input.AdminPassword) == "" {
		logger.Error("BOOTSTRAP_ADMIN_PASSWORD is required and must be provided through the environment")
		os.Exit(2)
	}

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
		"tenant_id", result.TenantID,
		"user_id", result.UserID,
		"role_id", result.RoleID,
		"user_created", result.UserCreated,
	)
}
