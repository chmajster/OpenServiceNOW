//go:build integration

package integration

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/chmajster/OpenServiceNOW/internal/platform/config"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/platform/password"
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
	if err := m.Up(); err != nil {
		t.Fatal(err)
	}
	// Exercise an actual upgrade from the pre-installation-state schema too.
	if err := m.Steps(24 - int(database.RequiredSchemaVersion)); err != nil {
		t.Fatal(err)
	}
	legacyHash, err := password.Hash("legacy-administrator-password")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO users(email,display_name,password_hash) VALUES('legacy-upgrade@example.test','Legacy administrator',?)", legacyHash); err != nil {
		t.Fatal(err)
	}

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("re-upgrade after single rollback: %v", err)
	}
	assertMigrationVersion(t, m, database.RequiredSchemaVersion)
	var installed bool
	if err := db.QueryRowContext(ctx, "SELECT completed_at IS NOT NULL FROM installation_state WHERE id=1").Scan(&installed); err != nil || !installed {
		t.Fatalf("legacy installation reopened: installed=%t err=%v", installed, err)
	}
	var preservedHash string
	if err := db.QueryRowContext(ctx, "SELECT password_hash FROM users WHERE email='legacy-upgrade@example.test'").Scan(&preservedHash); err != nil || preservedHash != legacyHash {
		t.Fatal("upgrade altered existing credentials")
	}
	var required bool
	var credentialVersion uint64
	if err := db.QueryRowContext(ctx, "SELECT password_change_required,credential_version FROM users WHERE email='legacy-upgrade@example.test'").Scan(&required, &credentialVersion); err != nil || required || credentialVersion != 1 {
		t.Fatal("upgrade imposed a password change on an existing account")
	}

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
	if err := db.QueryRowContext(ctx, "SELECT completed_at IS NULL FROM installation_state WHERE id=1").Scan(&installed); err != nil || !installed {
		t.Fatalf("fresh installation unavailable: required=%t err=%v", installed, err)
	}

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
