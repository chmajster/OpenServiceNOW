//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/chmajster/OpenServiceNOW/internal/activity"
	"github.com/chmajster/OpenServiceNOW/internal/assets"
	"github.com/chmajster/OpenServiceNOW/internal/attachments"
	"github.com/chmajster/OpenServiceNOW/internal/audit"
	"github.com/chmajster/OpenServiceNOW/internal/catalog"
	"github.com/chmajster/OpenServiceNOW/internal/cmdb"
	"github.com/chmajster/OpenServiceNOW/internal/events"
	"github.com/chmajster/OpenServiceNOW/internal/identity"
	"github.com/chmajster/OpenServiceNOW/internal/incidents"
	"github.com/chmajster/OpenServiceNOW/internal/jobs"
	"github.com/chmajster/OpenServiceNOW/internal/platform/config"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/recordlists"
	"github.com/chmajster/OpenServiceNOW/internal/requests"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
	"github.com/go-chi/chi/v5"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCMDBAssetImportAndRecordIsolation(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	db, err := database.OpenMySQL(ctx, cfg.MySQL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tx := database.NewTxManager(db)
	identities := identity.NewMySQLRepository(db)
	audits := audit.NewMySQLRepository()
	suffix := fmt.Sprint(time.Now().UnixNano())
	password := "cmdb-test-administrator"
	bootstrap := identity.NewBootstrapper(identities, tx, audits)
	root, err := bootstrap.Run(ctx, identity.BootstrapInput{TenantSlug: "cmdb-" + suffix, TenantName: "CMDB", AdminEmail: "cmdb" + suffix + "@example.test", AdminName: "Root", AdminPassword: password})
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := bootstrap.Run(ctx, identity.BootstrapInput{TenantSlug: "cmdb-foreign-" + suffix, TenantName: "Foreign", AdminEmail: "cmdb-foreign" + suffix + "@example.test", AdminName: "Foreign", AdminPassword: password})
	if err != nil {
		t.Fatal(err)
	}
	users := []uint64{root.UserID, foreign.UserID}
	defer func() {
		for _, tenant := range []uint64{root.TenantID, foreign.TenantID} {
			_, _ = db.ExecContext(context.Background(), "DELETE FROM audit_events WHERE tenant_id=?", tenant)
			if _, err := db.ExecContext(context.Background(), "DELETE FROM tenants WHERE id=?", tenant); err != nil {
				t.Error(err)
			}
		}
		for _, id := range users {
			if _, err := db.ExecContext(context.Background(), "DELETE FROM users WHERE id=?", id); err != nil {
				t.Error(err)
			}
		}
	}()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	as := func(tenant, user uint64) cmdb.Actor {
		keys, err := identities.ListPermissions(ctx, tenant, user)
		if err != nil {
			t.Fatal(err)
		}
		a := cmdb.Actor{Actor: workrecords.Actor{TenantID: tenant, UserID: user, RequestID: "cmdb-proof"}, Permissions: map[string]bool{}}
		for _, p := range keys {
			a.Permissions[p] = true
		}
		return a
	}
	seed := func(name string, keys []string) cmdb.Actor {
		result, err := db.ExecContext(ctx, "INSERT INTO users(email,display_name,password_hash) SELECT ?,?,password_hash FROM users WHERE id=?", name+suffix+"@example.test", name, root.UserID)
		if err != nil {
			t.Fatal(err)
		}
		n, _ := result.LastInsertId()
		id := uint64(n)
		users = append(users, id)
		exec("INSERT INTO tenant_memberships(tenant_id,user_id) VALUES(?,?)", root.TenantID, id)
		r, err := db.ExecContext(ctx, "INSERT INTO roles(tenant_id,name) VALUES(?,?)", root.TenantID, name)
		if err != nil {
			t.Fatal(err)
		}
		rid, _ := r.LastInsertId()
		for _, key := range keys {
			exec("INSERT INTO role_permissions(role_id,permission_id) SELECT ?,id FROM permissions WHERE permission_key=?", rid, key)
		}
		exec("INSERT INTO user_role_assignments(tenant_id,user_id,role_id) VALUES(?,?,?)", root.TenantID, id, rid)
		return as(root.TenantID, id)
	}
	admin := as(root.TenantID, root.UserID)
	other := as(foreign.TenantID, foreign.UserID)
	reader := seed("own-reader", []string{"cmdb.read_own", "incident.read_own", "cmdb.link"})
	operator := seed("update-agent", []string{"cmdb.read", "cmdb.link", "incident.read", "incident.update"})
	storage, err := attachments.NewLocalStorage(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	outbox := events.NewRepository(db)
	files, err := attachments.NewEventingService(attachments.NewRepository(db), storage, attachments.MaxFileBytes, tx, outbox)
	if err != nil {
		t.Fatal(err)
	}
	incidentService := incidents.NewService(incidents.NewRepository(db), tx, workrecords.NewAllocator(tx), workrecords.NewService(workrecords.NewRepository(db), tx, outbox), audits, outbox, activity.NewRepository(db))
	requestService := requests.NewService(db, files)
	s := cmdb.NewService(db, files, map[string]cmdb.RecordDomain{"incidents": incidents.NewCMDBDomain(incidentService), "requests": requests.NewCMDBDomain(requestService)})
	create := func(key, class string, owner uint64) cmdb.Record {
		t.Helper()
		r, err := s.Save(ctx, admin, 0, cmdb.Record{Fields: cmdb.Fields{Identifier: key, Name: key, Class: class, Status: "ACTIVE", OwnerID: owner, Environment: "PROD"}}, "create-"+key)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	server := create("server", "PHYSICAL_SERVER", admin.UserID)
	vm := create("vm", "VM", reader.UserID)
	app := create("app", "APPLICATION", reader.UserID)
	service := create("service", "SERVICE", reader.UserID)
	fresh := func(id uint64) cmdb.Record {
		t.Helper()
		r, err := s.Get(ctx, admin, id)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	relate := func(source, target cmdb.Record, kind string) {
		t.Helper()
		source = fresh(source.ID)
		target = fresh(target.ID)
		if _, err := s.AddRelation(ctx, admin, source.ID, cmdb.RelationInput{Version: source.Version, TargetID: target.ID, TargetVersion: target.Version, Type: kind}); err != nil {
			t.Fatal(err)
		}
	}
	relate(vm, server, "HOSTED_ON")
	relate(app, vm, "RUNS_ON")
	relate(service, app, "DEPENDS_ON")
	cycle := create("service-cycle", "SERVICE", reader.UserID)
	relate(service, cycle, "DEPENDS_ON")
	relate(cycle, service, "DEPENDS_ON")
	graph, err := s.Graph(ctx, admin, service.ID, cmdb.GraphFilter{Depth: 5, MaxNodes: 100})
	if err != nil || len(graph.Nodes) != 5 || len(graph.Edges) != 5 || graph.Truncated {
		t.Fatal("cycle graph", graph, err)
	}
	graph, err = s.Graph(ctx, reader, service.ID, cmdb.GraphFilter{Depth: 5, MaxNodes: 100})
	if err != nil || len(graph.Nodes) != 4 {
		t.Fatal("hidden graph endpoint", graph, err)
	}
	for _, n := range graph.Nodes {
		if n.ID == server.ID {
			t.Fatal("hidden CI leaked")
		}
	}
	graph, err = s.Graph(ctx, admin, service.ID, cmdb.GraphFilter{Depth: 1, MaxNodes: 2})
	if err != nil || len(graph.Nodes) > 2 || !graph.Truncated {
		t.Fatal("bounded graph", graph, err)
	}
	if _, err := s.Get(ctx, other, server.ID); !errors.Is(err, cmdb.ErrNotFound) {
		t.Fatal("foreign read", err)
	}
	if _, err := s.AddRelation(ctx, reader, vm.ID, cmdb.RelationInput{Version: fresh(vm.ID).Version, TargetID: server.ID, TargetVersion: fresh(server.ID).Version, Type: "HOSTED_ON"}); !errors.Is(err, workrecords.ErrForbidden) {
		t.Fatal("read-only mutation", err)
	}
	invalid := fresh(vm.ID)
	invalid.Class = "APPLICATION"
	if _, err := s.Save(ctx, admin, vm.ID, invalid, ""); !errors.Is(err, cmdb.ErrConflict) {
		t.Fatal("class mutation", err)
	}
	asset, err := s.Assets.Save(ctx, admin, 0, assets.Record{Fields: assets.Fields{AssetTag: "hardware", Name: "Server Asset", OwnerID: admin.UserID, SerialNumber: "serial1", State: "IN_USE", Currency: "PLN", PurchaseCost: "9999999999.99"}}, "create-hardware")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SetAsset(ctx, admin, vm.ID, cmdb.AssetLinkInput{Version: fresh(vm.ID).Version, AssetID: asset.ID, AssetVersion: asset.Version}); err != nil {
		t.Fatal(err)
	}
	if link, err := s.AssetLink(ctx, reader, vm.ID); err != nil || link.Asset != nil {
		t.Fatal("hidden asset exposed", link, err)
	}
	if _, err := s.Assets.Save(ctx, admin, 0, assets.Record{Fields: asset.Fields}, "duplicate-asset"); !errors.Is(err, assets.ErrDuplicate) {
		t.Fatal("duplicate serial/tag", err)
	}
	v := fresh(app.ID)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, name := range []string{"first", "second"} {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			r := v
			r.Name = name
			_, err := s.Save(ctx, admin, r.ID, r, "")
			errs <- err
		}(name)
	}
	wg.Wait()
	close(errs)
	conflicts := 0
	for err := range errs {
		if errors.Is(err, cmdb.ErrConflict) {
			conflicts++
		} else if err != nil {
			t.Fatal(err)
		}
	}
	if conflicts != 1 {
		t.Fatal("lost update", conflicts)
	}
	incActor := incidents.Actor{TenantID: admin.TenantID, UserID: admin.UserID, Permissions: admin.Permissions}
	incident, _, err := incidentService.Create(ctx, incActor, incidents.Fields{Title: "CMDB incident", Description: "Problem", AffectedUserID: admin.UserID, Category: "hardware", Subcategory: "computer", Impact: 2, Urgency: 2}, "cmdb-incident-create")
	if err != nil {
		t.Fatal(err)
	}
	ref, err := s.ChangeRecordLink(ctx, operator, "incidents", incident.ID, service.ID, cmdb.RecordLinkInput{Version: incident.Version, CIVersion: fresh(service.ID).Version}, false)
	if err != nil || ref.Version != incident.Version+1 {
		t.Fatal("agent link", ref, err)
	}
	if _, err := s.RecordCIs(ctx, reader, "incidents", incident.ID, 0, 25); !errors.Is(err, cmdb.ErrNotFound) {
		t.Fatal("protected incident", err)
	}
	if page, err := s.Records(ctx, reader, "incidents", "", service.ID, 0, 25); err != nil || len(page.Items) != 0 {
		t.Fatal("hidden incident list", page, err)
	}
	group, err := db.ExecContext(ctx, "INSERT INTO `groups`(tenant_id,name) VALUES(?,?)", admin.TenantID, "Fulfillment")
	if err != nil {
		t.Fatal(err)
	}
	gid, _ := group.LastInsertId()
	catalogs := catalog.NewService(db)
	cat, err := catalogs.SaveNode(ctx, admin, "catalogs", 0, catalog.Node{Name: "IT"})
	if err != nil {
		t.Fatal(err)
	}
	category, err := catalogs.SaveNode(ctx, admin, "categories", 0, catalog.Node{CatalogID: cat.ID, Name: "Hardware"})
	if err != nil {
		t.Fatal(err)
	}
	item, err := catalogs.SaveItem(ctx, admin, 0, catalog.Item{CategoryID: category.ID, Draft: catalog.Definition{Title: "Equipment", Method: "MANUAL_TASKS", GroupID: uint64(gid), Fields: []catalog.Field{}, Steps: []catalog.Step{{Title: "Deliver", Required: true}}}})
	if err != nil {
		t.Fatal(err)
	}
	item, err = catalogs.Publish(ctx, admin, item.ID, item.Version, false)
	if err != nil {
		t.Fatal(err)
	}
	request, _, err := requestService.Create(ctx, admin, requests.Submission{Items: []requests.SubmissionItem{{ItemID: item.ID, Version: *item.PublishedVersion, Values: map[string]json.RawMessage{}}}}, "cmdb-request-create")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ChangeRecordLink(ctx, admin, "requests", request.ID, service.ID, cmdb.RecordLinkInput{Version: request.Version, CIVersion: fresh(service.ID).Version}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Archive(ctx, admin, service.ID, fresh(service.ID).Version, true, "Wycofano usługę"); err != nil {
		t.Fatal(err)
	}
	links, err := s.RecordCIs(ctx, admin, "incidents", incident.ID, 0, 25)
	if err != nil || len(links.Items) != 1 || !links.Items[0].Archived {
		t.Fatal("archive lost ticket", links, err)
	}
	if _, err := s.Archive(ctx, admin, service.ID, fresh(service.ID).Version, false, "Przywrócono usługę"); err != nil {
		t.Fatal(err)
	}
	// Durable jobs, restart-friendly continuations, replay and compare-and-swap.
	worker, err := jobs.NewWorker(jobs.NewRepository(db), slog.New(slog.NewTextHandler(io.Discard, nil)), "cmdb-test", 1, 50*time.Millisecond, 5*time.Second, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err = worker.Register("cmdb.import", s.ImportHandler); err != nil {
		t.Fatal(err)
	}
	run := func(id uint64, want string) cmdb.Import {
		t.Helper()
		for n := 0; n < 100; n++ {
			i, err := s.GetImport(ctx, admin, id)
			if err != nil {
				t.Fatal(err)
			}
			if i.State == want {
				return i
			}
			if i.State == "FAILED" {
				t.Fatal("import failed", i.ErrorCode)
			}
			if _, err = worker.RunOnce(ctx); err != nil {
				t.Fatal(err)
			}
		}
		t.Fatal("job did not finish")
		return cmdb.Import{}
	}
	mapping := cmdb.ImportConfig{Target: "CI", IdentityKey: "identifier", Mapping: map[string]string{"identifier": "ID", "name": "Name", "class": "Class", "status": "Status", "owner_id": "Owner", "environment": "Env"}}
	csv := "ID,Name,Class,Status,Owner,Env\n"
	for n := 0; n < 30; n++ {
		csv += fmt.Sprintf("import-%d,Imported %d,VM,ACTIVE,%d,TEST\n", n, n, admin.UserID)
	}
	imp, err := s.CreateImport(ctx, admin, mapping, "inventory.csv", "cmdb-import-0001", strings.NewReader(csv))
	if err != nil {
		t.Fatal(err)
	}
	same, err := s.CreateImport(ctx, admin, mapping, "inventory.csv", "cmdb-import-0001", strings.NewReader(csv))
	if err != nil || same.ID != imp.ID {
		t.Fatal("idempotency", same, err)
	}
	if _, err = s.CreateImport(ctx, admin, mapping, "inventory.csv", "cmdb-import-0001", strings.NewReader(strings.Replace(csv, "Imported 1", "Other 1", 1))); !errors.Is(err, workrecords.ErrOperationConflict) {
		t.Fatal("changed replay", err)
	}
	imp = run(imp.ID, "PREVIEW_READY")
	rows, err := s.ImportRows(ctx, admin, imp.ID, 0, 100)
	if err != nil || len(rows.Items) != 30 || rows.Items[0].Action != "CREATE" {
		t.Fatal("preview", rows, err)
	}
	var count int
	if err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM cmdb_cis WHERE tenant_id=? AND identifier LIKE 'import-%'", admin.TenantID).Scan(&count); err != nil || count != 0 {
		t.Fatal("preview wrote", count, err)
	}
	if _, err = s.ImportAction(ctx, admin, imp.ID, imp.Version, "apply"); err != nil {
		t.Fatal(err)
	}
	imp = run(imp.ID, "COMPLETED")
	if imp.Counts["APPLIED"] != 30 {
		t.Fatal(imp)
	}
	var job jobs.Job
	var creator uint64
	err = db.QueryRowContext(ctx, "SELECT id,tenant_id,payload,created_by,attempt_count,max_attempts FROM jobs WHERE id=?", imp.JobID).Scan(&job.ID, &job.TenantID, &job.Payload, &creator, &job.AttemptCount, &job.MaxAttempts)
	if err != nil {
		t.Fatal(err)
	}
	job.CreatedBy = &creator
	if _, err = s.ImportHandler(ctx, job); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM cmdb_cis WHERE tenant_id=? AND identifier LIKE 'import-%'", admin.TenantID).Scan(&count); err != nil || count != 30 {
		t.Fatal("replay duplicate", count, err)
	}
	again, err := s.CreateImport(ctx, admin, mapping, "inventory.csv", "cmdb-import-0002", strings.NewReader(strings.ReplaceAll(csv, "Imported", "Updated")))
	if err != nil {
		t.Fatal(err)
	}
	again = run(again.ID, "PREVIEW_READY")
	rows, err = s.ImportRows(ctx, admin, again.ID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	changed := fresh(rows.Items[0].RecordID)
	changed.Name = "Concurrent"
	if _, err = s.Save(ctx, admin, changed.ID, changed, ""); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ImportAction(ctx, admin, again.ID, again.Version, "apply"); err != nil {
		t.Fatal(err)
	}
	again = run(again.ID, "PARTIAL")
	if again.Counts["CONFLICT"] != 1 || again.Counts["APPLIED"] != 29 || fresh(changed.ID).Name != "Concurrent" {
		t.Fatal("overwrite", again)
	}
	if _, err = s.ImportAction(ctx, admin, again.ID, again.Version, "review"); err != nil {
		t.Fatal(err)
	}
	again = run(again.ID, "PREVIEW_READY")
	if _, err = s.ImportAction(ctx, admin, again.ID, again.Version, "apply"); err != nil {
		t.Fatal(err)
	}
	again = run(again.ID, "COMPLETED")
	if fresh(changed.ID).Name == "Concurrent" {
		t.Fatal("review did not apply")
	}
	if _, err := s.GetImport(ctx, other, imp.ID); !errors.Is(err, cmdb.ErrNotFound) {
		t.Fatal("foreign import", err)
	}
	// Real authenticated HTTP routes, CSRF, string IDs and safe history projection.
	auth, err := identity.NewService(identities, tx, audits, cfg.Session, cfg.Auth)
	if err != nil {
		t.Fatal(err)
	}
	login, err := auth.Login(ctx, identity.LoginInput{Tenant: "cmdb-" + suffix, Email: "cmdb" + suffix + "@example.test", Password: password}, identity.RequestMeta{})
	if err != nil {
		t.Fatal(err)
	}
	router := chi.NewRouter()
	cmdb.RegisterRoutes(router, cmdb.NewHandler(s, auth), identity.NewMiddleware(auth, cfg.Session))
	apiCall := func(method, path string, body any, csrf bool) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(body)
		r := httptest.NewRequest(method, path, bytes.NewReader(raw))
		r.Header.Set("Content-Type", "application/json")
		r.AddCookie(&http.Cookie{Name: cfg.Session.CookieName, Value: login.SessionToken})
		if csrf {
			r.Header.Set("X-CSRF-Token", login.CSRFToken)
			r.AddCookie(&http.Cookie{Name: cfg.Session.CSRFCookieName, Value: login.CSRFToken})
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	path := fmt.Sprintf("/api/v1/cmdb/cis/%d", server.ID)
	if w := apiCall("GET", path, nil, false); w.Code != 200 || !strings.Contains(w.Body.String(), fmt.Sprintf(`"id":"%d"`, server.ID)) {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := apiCall("PUT", path, server.Fields, false); w.Code != 403 {
		t.Fatal("CSRF", w.Code)
	}
	if w := apiCall("GET", path+"/graph?max_nodes=101", nil, false); w.Code != 400 {
		t.Fatal("HTTP graph limit", w.Code)
	}
	projector := activity.NewProjector(tx, activity.NewRepository(db))
	var event events.Event
	err = db.QueryRowContext(ctx, "SELECT id,tenant_id,event_type,aggregate_type,aggregate_id,payload FROM outbox_events WHERE tenant_id=? AND aggregate_type='ci' ORDER BY id LIMIT 1", admin.TenantID).Scan(&event.ID, &event.TenantID, &event.Type, &event.AggregateType, &event.AggregateID, &event.Payload)
	if err != nil {
		t.Fatal(err)
	}
	handler := projector.Handler("ci.changed", "Zmieniono element konfiguracji")
	if err = handler(ctx, event); err != nil {
		t.Fatal(err)
	}
	if err = handler(ctx, event); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM activity_entries WHERE source_event_id=?", event.ID).Scan(&count); err != nil || count != 1 {
		t.Fatal("history replay", count, err)
	}
	t.Run("typed operational fields, containment and retry", func(t *testing.T) {
		databaseCI := create("database", "DATABASE", admin.UserID)
		databaseCI.Attributes = map[string]json.RawMessage{"port": json.RawMessage(`5432`), "hostname": json.RawMessage(`"db.example.test"`)}
		databaseCI.Criticality = "CRITICAL"
		databaseCI.Lifecycle = "OPERATE"
		groupResult, err := db.ExecContext(ctx, "INSERT INTO `groups`(tenant_id,name) VALUES(?,?)", admin.TenantID, "Database support")
		if err != nil {
			t.Fatal(err)
		}
		groupID, _ := groupResult.LastInsertId()
		gid := uint64(groupID)
		databaseCI.SupportGroupID = &gid
		databaseCI, err = s.Save(ctx, admin, databaseCI.ID, databaseCI, "")
		if err != nil || databaseCI.SupportGroupName != "Database support" {
			t.Fatal(databaseCI, err)
		}
		a, b := create("contain-a", "SERVICE", admin.UserID), create("contain-b", "SERVICE", admin.UserID)
		input := cmdb.RelationInput{Version: a.Version, TargetID: b.ID, TargetVersion: b.Version, Type: "PART_OF"}
		saved, err := s.AddRelation(ctx, admin, a.ID, input, "containment-retry")
		if err != nil {
			t.Fatal(err)
		}
		replay, err := s.AddRelation(ctx, admin, a.ID, input, "containment-retry")
		if err != nil || replay.Version != saved.Version {
			t.Fatal("lost response", replay, err)
		}
		b = fresh(b.ID)
		a = fresh(a.ID)
		if _, err = s.AddRelation(ctx, admin, b.ID, cmdb.RelationInput{Version: b.Version, TargetID: a.ID, TargetVersion: a.Version, Type: "PART_OF"}); !errors.Is(err, cmdb.ErrInvalid) {
			t.Fatal("containment cycle", err)
		}
		filters := &recordlists.Node{Logic: "AND", Children: []recordlists.Node{{Field: "class", Operator: "eq", Value: "DATABASE"}, {Field: "criticality", Operator: "eq", Value: "CRITICAL"}}}
		page, err := s.List(ctx, admin, cmdb.Filter{Limit: 1, Expression: filters, GroupBy: "class", Sort: "title"})
		if err != nil || page.Total != 1 || len(page.Groups) != 1 || len(page.Items) != 1 || page.Items[0].ID != databaseCI.ID {
			t.Fatal(page, err)
		}
		own, err := s.List(ctx, reader, cmdb.Filter{Limit: 1, Expression: filters, GroupBy: "class"})
		if err != nil || own.Total != 0 || len(own.Groups) != 0 {
			t.Fatal("grouping leaked inaccessible CI", own, err)
		}
	})

}
