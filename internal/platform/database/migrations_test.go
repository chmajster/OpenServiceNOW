package database

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMigrationsContainSingleStatement(t *testing.T) {
	files, err := filepath.Glob("../../../migrations/*.sql")
	if err != nil {
		t.Fatalf("glob migrations: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("no migration files found")
	}

	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		sql := strings.TrimSpace(string(data))
		if sql == "" {
			t.Errorf("%s is empty", path)
			continue
		}
		if count := strings.Count(sql, ";"); count != 1 {
			t.Errorf("%s contains %d statement terminators; want exactly 1", path, count)
		}
		if !strings.HasSuffix(sql, ";") {
			t.Errorf("%s must end with a semicolon", path)
		}
	}
}
