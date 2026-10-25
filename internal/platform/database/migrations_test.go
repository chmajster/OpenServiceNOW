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

func TestWorkspaceDownMigrationsRefuseDestructiveDataLoss(t *testing.T) {
	for _, name := range []string{
		"000124_workspace_views.down.sql",
		"000125_workspace_exports.down.sql",
		"000126_workspace_export_items.down.sql",
	} {
		data, err := os.ReadFile(filepath.Join("../../../migrations", name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		statement := strings.ToUpper(string(data))
		if !strings.Contains(statement, "SIGNAL SQLSTATE '45000'") {
			t.Errorf("%s does not stop a potentially destructive downgrade", name)
		}
	}
}

func TestMajorIncidentDownMigrationsProtectTenantOwnedData(t *testing.T) {
	for version, name := range map[int]string{
		130: "000130_incident_major_cases.down.sql",
		131: "000131_incident_major_decisions.down.sql",
		132: "000132_incident_major_teams.down.sql",
		133: "000133_incident_major_links.down.sql",
		134: "000134_incident_major_updates.down.sql",
		135: "000135_incident_major_reviews.down.sql",
		136: "000136_incident_major_actions.down.sql",
	} {
		data, err := os.ReadFile(filepath.Join("../../../migrations", name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if !strings.Contains(strings.ToUpper(string(data)), "SIGNAL SQLSTATE '45000'") {
			t.Errorf("%d %s does not stop a destructive downgrade", version, name)
		}
	}
}
