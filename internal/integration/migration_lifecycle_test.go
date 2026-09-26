//go:build integration

package integration

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/chmajster/OpenServiceNOW/internal/platform/config"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

func TestMigrationLifecycle(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	db, err := database.OpenMySQLForMigrations(ctx, cfg.MySQL)
	if err != nil {
		t.Fatalf("open migration database: %v", err)
	}
	defer db.Close()

	m, err := database.NewMigrator(db, "file://../../migrations")
	if err != nil {
		t.Fatalf("create migrator: %v", err)
	}
	defer func() { _, _ = m.Close() }()

	assertMigrationVersion(t, m, database.RequiredSchemaVersion)

	if err := m.Steps(-1); err != nil {
		t.Fatalf("rollback one migration: %v", err)
	}
	assertMigrationVersion(t, m, database.RequiredSchemaVersion-1)

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("re-upgrade after single rollback: %v", err)
	}
	assertMigrationVersion(t, m, database.RequiredSchemaVersion)

	if err := m.Down(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("full rollback: %v", err)
	}
	if version, dirty, err := m.Version(); !errors.Is(err, migrate.ErrNilVersion) {
		t.Fatalf("full rollback version=%d dirty=%t err=%v, want no version", version, dirty, err)
	}

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("re-upgrade after full rollback: %v", err)
	}
	assertMigrationVersion(t, m, database.RequiredSchemaVersion)

	if err := database.SchemaReady(ctx, db, cfg.MySQL.PingTimeout); err != nil {
		t.Fatalf("schema readiness after migration lifecycle: %v", err)
	}
}

func assertMigrationVersion(t *testing.T, m *migrate.Migrate, expected uint64) {
	t.Helper()
	version, dirty, err := m.Version()
	if err != nil {
		t.Fatalf("read migration version: %v", err)
	}
	if uint64(version) != expected || dirty {
		t.Fatalf("migration version=%d dirty=%t, want version=%d dirty=false", version, dirty, expected)
	}
}
