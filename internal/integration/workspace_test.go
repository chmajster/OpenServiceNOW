//go:build integration

package integration

import (
	"context"
	"fmt"
	"github.com/chmajster/OpenServiceNOW/internal/activity"
	"github.com/chmajster/OpenServiceNOW/internal/audit"
	"github.com/chmajster/OpenServiceNOW/internal/events"
	"github.com/chmajster/OpenServiceNOW/internal/identity"
	"github.com/chmajster/OpenServiceNOW/internal/incidents"
	"github.com/chmajster/OpenServiceNOW/internal/platform/config"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
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
	ws := workspace.Service{Incidents: is, Requests: rs, Workflow: workflow.NewService(db, rs)}
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
	if err != nil || len(page.Incidents) != 25 || page.NextAfterID == 0 {
		t.Fatalf("page=%+v err=%v", page, err)
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
	p.Permissions = map[string]bool{}
	summary, err = ws.Summary(ctx, p)
	if err != nil || len(summary.Metrics) != 0 {
		t.Fatalf("revoked: %+v %v", summary, err)
	}
}
