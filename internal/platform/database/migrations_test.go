package database

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestMigrationsContainSingleStatementAndMatchRequiredVersion(t *testing.T) {
	files, err := filepath.Glob("../../../migrations/*.up.sql")
	if err != nil {
		t.Fatalf("glob migrations: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("no up migration files found")
	}

	var highest uint64
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		sqlText := strings.TrimSpace(string(data))
		if sqlText == "" {
			t.Errorf("%s is empty", path)
			continue
		}
		if count := strings.Count(sqlText, ";"); count != 1 {
			t.Errorf("%s contains %d statement terminators; want exactly 1", path, count)
		}
		if !strings.HasSuffix(sqlText, ";") {
			t.Errorf("%s must end with a semicolon", path)
		}

		base := filepath.Base(path)
		versionText, _, ok := strings.Cut(base, "_")
		if !ok {
			t.Errorf("migration %s has invalid filename", base)
			continue
		}
		version, err := strconv.ParseUint(versionText, 10, 64)
		if err != nil {
			t.Errorf("migration %s has invalid version: %v", base, err)
			continue
		}
		if version > highest {
			highest = version
		}
	}

	if highest != RequiredSchemaVersion {
		t.Fatalf("highest migration version = %d, RequiredSchemaVersion = %d", highest, RequiredSchemaVersion)
	}

	downFiles, err := filepath.Glob("../../../migrations/*.down.sql")
	if err != nil {
		t.Fatalf("glob down migrations: %v", err)
	}
	if len(downFiles) != len(files) {
		t.Fatalf("up migrations = %d, down migrations = %d", len(files), len(downFiles))
	}
	for _, path := range downFiles {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		sqlText := strings.TrimSpace(string(data))
		if count := strings.Count(sqlText, ";"); count != 1 || !strings.HasSuffix(sqlText, ";") {
			t.Errorf("%s must contain exactly one terminated SQL statement", path)
		}
	}
}

func TestValidateSchemaVersion(t *testing.T) {
	if err := ValidateSchemaVersion(RequiredSchemaVersion, false); err != nil {
		t.Fatalf("expected current clean schema to be ready: %v", err)
	}
	if err := ValidateSchemaVersion(RequiredSchemaVersion, true); err == nil {
		t.Fatal("expected dirty schema error")
	}
	if err := ValidateSchemaVersion(RequiredSchemaVersion-1, false); err == nil {
		t.Fatal("expected stale schema version error")
	}
}
