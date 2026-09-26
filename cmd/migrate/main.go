package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"

	"github.com/chmajster/OpenServiceNOW/internal/platform/config"
	platformdb "github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := config.Load()
	if err != nil {
		logger.Error("configuration invalid", "error", err)
		os.Exit(1)
	}
	if len(os.Args) != 2 {
		usage(logger)
	}

	command := os.Args[1]
	switch command {
	case "up", "down", "down-all", "version":
	default:
		usage(logger)
	}

	db, err := platformdb.OpenMySQLForMigrations(context.Background(), cfg.MySQL)
	if err != nil {
		logger.Error("mysql unavailable", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	m, err := platformdb.NewMigrator(db, "file://migrations")
	if err != nil {
		logger.Error("create migrator", "error", err)
		os.Exit(1)
	}
	defer func() { _, _ = m.Close() }()

	switch command {
	case "up":
		err = m.Up()
	case "down":
		err = m.Steps(-1)
	case "down-all":
		err = m.Down()
	case "version":
		printVersion(logger, m)
		return
	}

	if err != nil && !errors.Is(err, migrate.ErrNoChange) {
		logger.Error("migration failed", "command", command, "error", err)
		os.Exit(1)
	}
	fmt.Printf("migration %s complete\n", command)
}

func usage(logger *slog.Logger) {
	logger.Error("usage", "command", "migrate up|down|down-all|version")
	os.Exit(2)
}

func printVersion(logger *slog.Logger, m *migrate.Migrate) {
	version, dirty, err := m.Version()
	if errors.Is(err, migrate.ErrNilVersion) {
		fmt.Println("version: none dirty: false")
		return
	}
	if err != nil {
		logger.Error("read migration version", "error", err)
		os.Exit(1)
	}
	fmt.Printf("version: %d dirty: %t\n", version, dirty)
}
