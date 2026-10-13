package workspace

import (
	"encoding/json"
	"errors"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
	"strings"
	"testing"
	"time"
)

func TestMetadataKeysAreCanonicalBoundedAndPrecisionSafe(t *testing.T) {
	keys, err := parseKeys([]string{"/changes/9007199254740993", "/cmdb/18446744073709551615"})
	if err != nil || keys["change"][0] != 9007199254740993 {
		t.Fatal(keys, err)
	}
	for _, input := range [][]string{{"/changes/01"}, {"/changes/0"}, {"/changes/-1"}, {"/changes/18446744073709551616"}, {"/admin/users/1"}, {"/changes/1?tab=history"}, {"/changes/1", "/changes/1"}, {"/changes/1 OR 1=1"}} {
		if _, err := parseKeys(input); !errors.Is(err, ErrInvalid) {
			t.Fatal(input, err)
		}
	}
	many := make([]string, 151)
	if _, err := parseKeys(many); !errors.Is(err, ErrInvalid) {
		t.Fatal("bound", err)
	}
	raw, _ := json.Marshal(workrecords.Reference{ID: 9007199254740993, Version: 9007199254740995})
	if !strings.Contains(string(raw), `"id":"9007199254740993"`) || !strings.Contains(string(raw), `"version":"9007199254740995"`) {
		t.Fatal(string(raw))
	}
}
func TestReportFilterUsesBoundParametersAndCorrectAgeBoundaries(t *testing.T) {
	at := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	period := ReportPeriod{From: at.Add(-time.Hour), To: at.Add(time.Hour)}
	for _, p := range []ReportPeriod{{}, {From: at, To: at}, {From: at, To: at.Add(367 * 24 * time.Hour)}} {
		if ValidatePeriod(p) == nil {
			t.Fatal("invalid period", p)
		}
	}
	scope := workrecords.ReferenceScope{Kind: "problem", ID: "p.id", State: "p.state", CreatedAt: "p.created_at", Active: "p.state NOT IN('CLOSED','CANCELLED')", Where: " WHERE p.tenant_id=?", Args: []any{uint64(42)}}
	scopes := map[string]workrecords.ReferenceScope{"problem": scope}
	for _, bucket := range []string{"0_7", "7_30", "30_plus"} {
		s, err := reportScope(scopes, ReportSourceFilter{Report: "backlog_age", Domain: "problem", Bucket: bucket, Period: period, AsOf: at})
		if err != nil || !strings.Contains(s.Where, "p.tenant_id=?") || !strings.Contains(s.Where, scope.Active) {
			t.Fatal(s, err)
		}
		if bucket == "7_30" && !strings.Contains(s.Where, "p.created_at>? AND p.created_at<=?") {
			t.Fatal("age boundary", s.Where)
		}
	}
	for _, f := range []ReportSourceFilter{{Report: "active_state", Domain: "problem", Bucket: "NEW' OR 1=1"}, {Report: "active_group", Domain: "problem", Bucket: "1 OR 1=1"}, {Report: "sla_risk", Domain: "problem"}, {Report: "planned_changes", Domain: "problem"}, {Report: "arbitrary_sql", Domain: "problem"}, {Report: "backlog_age", Domain: "problem", Bucket: "0_7"}} {
		if _, err := reportScope(scopes, f); err == nil {
			t.Fatal("accepted", f)
		}
	}
}
