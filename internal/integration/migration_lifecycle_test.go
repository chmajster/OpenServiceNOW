//go:build integration

package integration

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
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

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	db, err := database.OpenMySQLForMigrations(ctx, cfg.MySQL)
	if err != nil {
		t.Fatalf("open migration database: %v", err)
	}
	defer db.Close()

	migrationsURL, err := lifecycleMigrationSource(t)
	if err != nil {
		t.Fatalf("prepare disposable migration source: %v", err)
	}
	m, err := database.NewMigrator(db, migrationsURL)
	if err != nil {
		t.Fatalf("create migrator: %v", err)
	}
	defer func() { _, _ = m.Close() }()

	assertMigrationVersion(t, m, database.RequiredSchemaVersion)

	// Upgrade an actual pre-fan-out outbox. Enabling notifications must not
	// reinterpret legacy completed, failed or pending events as new mail work.
	if err := m.Steps(37 - int(database.RequiredSchemaVersion)); err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"DELIVERED", "FAILED", "PENDING"} {
		if _, err := db.ExecContext(ctx, `INSERT INTO outbox_events(scope_key,event_type,aggregate_type,aggregate_id,payload,status,delivered_at) VALUES('system','incident.assigned','incident','migration-notification-test','{}',?,IF(?='DELIVERED',UTC_TIMESTAMP(6),NULL))`, status, status); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.Up(); err != nil {
		t.Fatal(err)
	}
	var eligible int
	if err := db.QueryRowContext(ctx, `SELECT SUM(notification_eligible) FROM outbox_events WHERE aggregate_id='migration-notification-test'`).Scan(&eligible); err != nil || eligible != 0 {
		t.Fatalf("historical notification eligibility %d: %v", eligible, err)
	}
	var skipped int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM event_deliveries d JOIN outbox_events e ON e.id=d.event_id WHERE e.aggregate_id='migration-notification-test' AND d.consumer_id='notifications.v1' AND d.status='SKIPPED'`).Scan(&skipped); err != nil || skipped != 2 {
		t.Fatalf("legacy delivery policy %d: %v", skipped, err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO outbox_events(scope_key,event_type,aggregate_type,aggregate_id,payload) VALUES('system','test.new','system','migration-new-test','{}')`); err != nil {
		t.Fatal(err)
	}
	var fresh bool
	if err := db.QueryRowContext(ctx, `SELECT notification_eligible FROM outbox_events WHERE aggregate_id='migration-new-test'`).Scan(&fresh); err != nil || !fresh {
		t.Fatal("new events were disabled", err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM outbox_events WHERE aggregate_id IN ('migration-notification-test','migration-new-test')`); err != nil {
		t.Fatal(err)
	}

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

// The lifecycle test deliberately migrates a disposable integration schema
// fully down and up. The production down migrations for workspace data refuse
// to drop persisted views/exports; use DROP only in this test-only source so
// the older full-schema lifecycle coverage can still run.
func lifecycleMigrationSource(t *testing.T) (string, error) {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("..", "..", "migrations", "*.sql"))
	if err != nil {
		return "", err
	}
	directory, err := os.MkdirTemp("", "opensnow-migrations-")
	if err != nil {
		return "", err
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })
	protected := map[string]string{
		"000139_incident_process.down.sql":               "ALTER TABLE incidents DROP FOREIGN KEY incident_service_fk, DROP INDEX incident_service_queue, DROP COLUMN service_id, DROP COLUMN location, DROP COLUMN template_key, DROP COLUMN template_version, DROP COLUMN process_version, DROP COLUMN process_policy, DROP COLUMN assignment_origin, DROP COLUMN assignment_reason;\n",
		"000140_incident_action_contract.down.sql":       "ALTER TABLE incident_major_actions DROP COLUMN number,DROP COLUMN action_kind;\n",
		"000141_incident_action_numbers.down.sql":        "SELECT 1;\n",
		"000143_incident_nomination_permission.down.sql": "DELETE FROM permissions WHERE permission_key='incident.major.nominate';\n",
		"000148_major_delivery_states.down.sql":          "SELECT 1;\n",
		"000149_incident_action_assignments.down.sql":    "DELETE FROM work_assignments WHERE object_type='incident_action';\n",
		"000150_incident_auto_close_retry.down.sql":      "ALTER TABLE incidents DROP COLUMN auto_close_retry_at;\n",
		"000151_incident_major_batches.down.sql":         "DROP TABLE incident_major_batches;\n",

		"000124_workspace_views.down.sql":          "DROP TABLE IF EXISTS workspace_views;\n",
		"000125_workspace_exports.down.sql":        "DROP TABLE IF EXISTS workspace_exports;\n",
		"000126_workspace_export_items.down.sql":   "DROP TABLE IF EXISTS workspace_export_items;\n",
		"000130_incident_major_cases.down.sql":     "DROP TABLE IF EXISTS incident_major_cases;\n",
		"000131_incident_major_decisions.down.sql": "DROP TABLE IF EXISTS incident_major_decisions;\n",
		"000132_incident_major_teams.down.sql":     "DROP TABLE IF EXISTS incident_major_teams;\n",
		"000133_incident_major_links.down.sql":     "DROP TABLE IF EXISTS incident_major_links;\n",
		"000134_incident_major_updates.down.sql":   "DROP TABLE IF EXISTS incident_major_updates;\n",
		"000135_incident_major_reviews.down.sql":   "DROP TABLE IF EXISTS incident_major_reviews;\n",
		"000136_incident_major_actions.down.sql":   "DROP TABLE IF EXISTS incident_major_actions;\n",
	}
	for _, source := range files {
		name := filepath.Base(source)
		data, err := os.ReadFile(source)
		if err != nil {
			return "", err
		}
		if replacement, ok := protected[name]; ok {
			data = []byte(replacement)
		}
		if err := os.WriteFile(filepath.Join(directory, name), data, 0o600); err != nil {
			return "", err
		}
	}
	absolute, err := filepath.Abs(directory)
	if err != nil {
		return "", err
	}
	path := filepath.ToSlash(absolute)
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return "file://" + path, nil
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
