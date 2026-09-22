package migrations

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOrdersAndPairsMigrations(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{
		"000002_second.down.sql",
		"000001_first.up.sql",
		"000002_second.up.sql",
		"000001_first.down.sql",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("SELECT 1;"), 0o600); err != nil {
			t.Fatalf("write migration: %v", err)
		}
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got) != 2 || got[0].Version != 1 || got[1].Version != 2 {
		t.Fatalf("unexpected order: %+v", got)
	}
}

func TestLoadRejectsUnpairedMigration(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "000001_first.up.sql"), []byte("SELECT 1;"), 0o600); err != nil {
		t.Fatalf("write migration: %v", err)
	}
	if _, err := Load(dir); err == nil {
		t.Fatal("expected error for unpaired migration")
	}
}
