//go:build integration

package integration

import (
	"context"
	"github.com/chmajster/OpenServiceNOW/internal/platform/config"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Run only against the explicitly named disposable migration schema. The
// rejected production downgrade is intentionally left dirty for inspection.
func TestModule08ProtectedDowngrade(t *testing.T) {
	if !strings.HasPrefix(os.Getenv("MYSQL_DATABASE"), "module08_migrations") {
		t.Skip("requires a dedicated module08_migrations schema")
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := database.OpenMySQLForMigrations(ctx, cfg.MySQL)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	directory, err := filepath.Abs("../../migrations")
	if err != nil {
		t.Fatal(err)
	}
	m, err := database.NewMigrator(db, "file://"+filepath.ToSlash(directory))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = m.Close() }()
	if err = m.Steps(-1); err == nil || !strings.Contains(err.Error(), "Export durable major incident batch outcomes") {
		t.Fatalf("unsafe downgrade accepted: %v", err)
	}
	version, dirty, err := m.Version()
	if err != nil || !dirty || version != 150 {
		t.Fatalf("failed downgrade state %d %v %v", version, dirty, err)
	}
}
