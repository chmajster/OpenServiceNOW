//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/chmajster/OpenServiceNOW/internal/activity"
	"github.com/chmajster/OpenServiceNOW/internal/audit"
	"github.com/chmajster/OpenServiceNOW/internal/events"
	"github.com/chmajster/OpenServiceNOW/internal/identity"
	"github.com/chmajster/OpenServiceNOW/internal/incidents"
	"github.com/chmajster/OpenServiceNOW/internal/platform/config"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/recordlists"
	"github.com/chmajster/OpenServiceNOW/internal/recordviews"
	"github.com/chmajster/OpenServiceNOW/internal/requests"
	"github.com/chmajster/OpenServiceNOW/internal/workflow"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
	"github.com/chmajster/OpenServiceNOW/internal/workspace"
	"testing"
	"time"
)

func TestWorkspaceCountsAndSearchScope(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	db, err := database.OpenMySQL(ctx, cfg.MySQL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tx := database.NewTxManager(db)
	ar := audit.NewMySQLRepository()
	ir := identity.NewMySQLRepository(db)
	suffix := fmt.Sprint(time.Now().UnixNano())
	b, err := identity.NewBootstrapper(ir, tx, ar).Run(ctx, identity.BootstrapInput{TenantSlug: "workspace-" + suffix, TenantName: "Workspace test", AdminEmail: "workspace-" + suffix + "@example.test", AdminName: "Admin", AdminPassword: "workspace-test-administrator"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		db.ExecContext(context.Background(), "DELETE FROM audit_events WHERE tenant_id=?", b.TenantID)
		db.ExecContext(context.Background(), "DELETE FROM tenants WHERE id=?", b.TenantID)
		db.ExecContext(context.Background(), "DELETE FROM users WHERE id=?", b.UserID)
	}()
	keys, err := ir.ListPermissionsInTx(ctx, db, b.TenantID, b.UserID)
	if err != nil {
		t.Fatal(err)
	}
	p := workrecords.Principal{Actor: workrecords.Actor{TenantID: b.TenantID, UserID: b.UserID}, Permissions: map[string]bool{}}
	for _, k := range keys {
		p.Permissions[k] = true
	}
	out := events.NewRepository(db)
	is := incidents.NewService(incidents.NewRepository(db), tx, workrecords.NewAllocator(tx), workrecords.NewService(workrecords.NewRepository(db), tx, out), ar, out, activity.NewRepository(db))
	rs := requests.NewService(db, nil)
	ws := workspace.Service{DB: db, Incidents: is, Requests: rs, Workflow: workflow.NewService(db, rs)}
	a := incidents.Actor{TenantID: b.TenantID, UserID: b.UserID, Permissions: p.Permissions}
	var first incidents.Record
	for n := 0; n < 31; n++ {
		r, _, err := is.Create(ctx, a, incidents.Fields{Title: fmt.Sprintf("Workspace case %d", n), Description: "Public description", AffectedUserID: b.UserID, Category: "other", Subcategory: "other", Impact: 3, Urgency: 3}, fmt.Sprintf("workspace-%s-%d", suffix, n))
		if err != nil {
			t.Fatal(err)
		}
		r, err = is.Assign(ctx, a, r.ID, incidents.AssignmentInput{Version: r.Version, AssignedTo: &b.UserID})
		if err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			first = r
		}
	}
	summary, err := ws.Summary(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range summary.Metrics {
		if m.Key == "my_incidents" {
			found = true
			if m.Count != 31 {
				t.Fatalf("count=%d", m.Count)
			}
		}
	}
	if !found {
		t.Fatal("missing own metric")
	}
	page, err := is.List(ctx, a, incidents.Filter{View: "assigned", Sort: "newest", Limit: 25, Active: true})
	if err != nil || len(page.Incidents) != 25 || page.NextAfterID == 0 || page.Total != 31 {
		t.Fatalf("page=%+v err=%v", page, err)
	}
	expression := &recordlists.Node{Logic: "AND", Children: []recordlists.Node{{Field: "active", Operator: "eq", Value: "true"}, {Logic: "OR", Children: []recordlists.Node{{Field: "priority", Operator: "eq", Value: "5"}, {Field: "title", Operator: "contains", Value: "does-not-exist"}}}}}
	advanced := incidents.Filter{View: "all", Sort: "title", Limit: 10, Expression: expression, GroupBy: "state"}
	seen := map[uint64]bool{}
	for {
		result, err := is.List(ctx, a, advanced)
		if err != nil {
			t.Fatal(err)
		}
		if result.Total != 31 || len(result.Groups) != 1 || result.Groups[0].Count != 31 {
			t.Fatalf("whole dataset count/group: %+v", result)
		}
		for _, record := range result.Incidents {
			if seen[record.ID] {
				t.Fatal("duplicate cursor record", record.ID)
			}
			seen[record.ID] = true
		}
		if result.NextAfterID == 0 {
			break
		}
		advanced.AfterID = result.NextAfterID
		advanced.AfterValue = result.NextValue
	}
	if len(seen) != 31 {
		t.Fatalf("pagination lost records: %d", len(seen))
	}
	foreignActor := a
	foreignActor.UserID += 999999
	foreignActor.Permissions = map[string]bool{"incident.read_own": true}
	result, err := is.List(ctx, foreignActor, incidents.Filter{View: "all", Sort: "newest", Limit: 10, Expression: expression, GroupBy: "state"})
	if err != nil || result.Total != 0 || len(result.Groups) != 0 {
		t.Fatalf("group/query access leak %+v %v", result, err)
	}
	configRaw := json.RawMessage(`{"sort":"title","group_by":"state","columns":[{"key":"number","width":160},{"key":"title","width":250}]}`)
	vs := recordviews.Service{DB: db, Domains: map[string]recordviews.Domain{"incidents": {Read: func(p workrecords.Principal) bool { return p.Has("incident.read") || p.Has("incident.read_own") }, Validate: func(p workrecords.Principal, raw json.RawMessage) error {
		_, err := recordlists.ValidateConfig(raw, incidents.ListSchema(p.UserID), incidents.ListSorts, incidents.ListGroups, incidents.ListColumns)
		return err
	}}}}
	input := recordviews.Input{Domain: "incidents", Name: "Private view", Configuration: configRaw}
	view, err := vs.Save(ctx, p, 0, input, "view-create-"+suffix)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := vs.Save(ctx, p, 0, input, "view-create-"+suffix)
	if err != nil || retry.ID != view.ID {
		t.Fatalf("duplicate retry %+v %v", retry, err)
	}
	input.Name = "Changed request"
	if _, err = vs.Save(ctx, p, 0, input, "view-create-"+suffix); !errors.Is(err, workrecords.ErrOperationConflict) {
		t.Fatal("key mismatch", err)
	}
	input.Name = "New name"
	input.Version = view.Version
	view, err = vs.Save(ctx, p, view.ID, input, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = vs.Save(ctx, p, view.ID, input, ""); !errors.Is(err, recordviews.ErrConflict) {
		t.Fatal("stale view overwritten", err)
	}
	foreign := p
	foreign.TenantID += 99999
	if _, err = vs.Get(ctx, foreign, view.ID); !errors.Is(err, recordviews.ErrNotFound) {
		t.Fatal("foreign view", err)
	}
	resultGroup, err := db.ExecContext(ctx, "INSERT INTO `groups`(tenant_id,name) VALUES(?,?)", b.TenantID, "View group "+suffix)
	if err != nil {
		t.Fatal(err)
	}
	groupNumber, err := resultGroup.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	groupID := uint64(groupNumber)
	input.Name = "Shared view"
	input.GroupID = &groupID
	input.Version = 0
	if _, err = vs.Save(ctx, p, 0, input, "view-not-member-"+suffix); !errors.Is(err, workrecords.ErrForbidden) {
		t.Fatal("shared outside group", err)
	}
	if _, err = db.ExecContext(ctx, "INSERT INTO group_memberships(tenant_id,group_id,user_id) VALUES(?,?,?)", b.TenantID, groupID, b.UserID); err != nil {
		t.Fatal(err)
	}
	shared, err := vs.Save(ctx, p, 0, input, "view-shared-"+suffix)
	if err != nil {
		t.Fatal(err)
	}
	items, _, err := vs.List(ctx, p, "incidents", 0, 50)
	if err != nil || len(items) != 2 {
		t.Fatal("saved views", items, err)
	}
	if err = vs.Delete(ctx, p, shared.ID, shared.Version); err != nil {
		t.Fatal(err)
	}
	results, err := ws.Search(ctx, p, first.Number)
	if err != nil || len(results) == 0 || results[0].ID != first.ID || !results[0].Exact {
		t.Fatalf("exact=%+v err=%v", results, err)
	}
	if _, err = is.AddMessage(ctx, a, first.ID, incidents.CommunicationInput{Type: "WORK_NOTE", Body: "confidential-needle"}, "workspace-private-"+suffix); err != nil {
		t.Fatal(err)
	}
	results, err = ws.Search(ctx, p, "confidential-needle")
	if err != nil || len(results) != 0 {
		t.Fatalf("notes leaked: %+v %v", results, err)
	}
	other := p
	other.UserID = b.UserID + 999999
	other.Permissions = map[string]bool{"incident.read_own": true}
	results, err = ws.Search(ctx, other, "Workspace")
	if err != nil || len(results) != 0 {
		t.Fatalf("own scope: %+v %v", results, err)
	}
	other.TenantID = b.TenantID + 999999
	other.Permissions = map[string]bool{"incident.read": true}
	results, err = ws.Search(ctx, other, first.Number)
	if err != nil || len(results) != 0 {
		t.Fatalf("tenant scope: %+v %v", results, err)
	}

	period := workspace.ReportPeriod{From: time.Now().UTC().Add(-24 * time.Hour), To: time.Now().UTC().Add(24 * time.Hour)}
	reports, err := ws.Reports(ctx, p, period)
	if err != nil {
		t.Fatal("reports", err)
	}
	found = false
	for _, r := range reports.Items {
		if r.Domain == "incident" && r.Key == "active_state" {
			found = true
			if r.Total != 31 || r.Eligible != 31 || len(r.Items) != 1 || r.Items[0].Count != 31 {
				t.Fatal("report count", r)
			}
			source, err := ws.ReportSources(ctx, p, workspace.ReportSourceFilter{Report: r.Key, Domain: r.Domain, Bucket: r.Items[0].Value, Period: period, Limit: 25})
			if err != nil || source.Total != 31 || len(source.Items) != 25 || source.Next == 0 {
				t.Fatal("report source", source, err)
			}
			next, err := ws.ReportSources(ctx, p, workspace.ReportSourceFilter{Report: r.Key, Domain: r.Domain, Bucket: r.Items[0].Value, Period: period, Before: source.Next, Limit: 25})
			if err != nil || len(next.Items) != 6 || next.Total != 31 {
				t.Fatal("report next", next, err)
			}
		}
	}
	if !found {
		t.Fatal("missing incident report")
	}
	key := fmt.Sprintf("/incidents/%d", first.ID)
	metadata, err := ws.Metadata(ctx, p, []string{key})
	if err != nil || len(metadata.Items) != 1 || metadata.Items[0].Key != key || metadata.Items[0].Record.Title != first.Title {
		t.Fatal("metadata", metadata, err)
	}
	if metadata, err = ws.Metadata(ctx, other, []string{key}); err != nil || len(metadata.Items) != 0 {
		t.Fatal("foreign metadata", metadata, err)
	}
	if _, err = ws.Context(ctx, other, "incidents", first.ID); !errors.Is(err, workrecords.ErrNotFound) {
		t.Fatal("foreign context", err)
	}
	contextRecord, err := ws.Context(ctx, p, "incidents", first.ID)
	if err != nil || contextRecord.Record.ID != first.ID {
		t.Fatal("context", contextRecord, err)
	}
	if reports, err = ws.Reports(ctx, other, period); err != nil {
		t.Fatal(err)
	} else {
		for _, r := range reports.Items {
			if r.Total != 0 {
				t.Fatal("foreign count", r)
			}
		}
	}
	p.Permissions = map[string]bool{}
	summary, err = ws.Summary(ctx, p)
	if err != nil || len(summary.Metrics) != 0 {
		t.Fatalf("revoked: %+v %v", summary, err)
	}
}
