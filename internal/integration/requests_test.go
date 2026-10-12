//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/chmajster/OpenServiceNOW/internal/activity"
	"github.com/chmajster/OpenServiceNOW/internal/attachments"
	"github.com/chmajster/OpenServiceNOW/internal/audit"
	"github.com/chmajster/OpenServiceNOW/internal/catalog"
	"github.com/chmajster/OpenServiceNOW/internal/events"
	"github.com/chmajster/OpenServiceNOW/internal/identity"
	"github.com/chmajster/OpenServiceNOW/internal/platform/config"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
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

func TestCatalogRequestFulfillment(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
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
	root, err := identity.NewBootstrapper(identities, tx, audits).Run(ctx, identity.BootstrapInput{TenantSlug: "request-" + suffix, TenantName: "Requests", AdminEmail: "requests-" + suffix + "@example.test", AdminName: "Root", AdminPassword: "request-test-administrator"})
	if err != nil {
		t.Fatal(err)
	}
	userIDs := []uint64{}
	second, err := identity.NewBootstrapper(identities, tx, audits).Run(ctx, identity.BootstrapInput{TenantSlug: "request-b-" + suffix, TenantName: "Other Requests", AdminEmail: "requests-" + suffix + "@example.test", AdminName: "Root", AdminPassword: "request-test-administrator"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = db.ExecContext(context.Background(), "DELETE FROM audit_events WHERE tenant_id=?", second.TenantID)
		if _, err := db.ExecContext(context.Background(), "DELETE FROM tenants WHERE id=?", second.TenantID); err != nil {
			t.Error(err)
		}
		_, _ = db.ExecContext(context.Background(), "DELETE FROM audit_events WHERE tenant_id=?", root.TenantID)
		if _, err := db.ExecContext(context.Background(), "DELETE FROM tenants WHERE id=?", root.TenantID); err != nil {
			t.Error(err)
		}
		_, _ = db.ExecContext(context.Background(), "DELETE FROM users WHERE id=?", root.UserID)
		for _, id := range userIDs {
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
	seed := func(name string, permissions []string) uint64 {
		t.Helper()
		result, err := db.ExecContext(ctx, "INSERT INTO users(email,display_name,password_hash) SELECT ?,?,password_hash FROM users WHERE id=?", name+suffix+"@example.test", name, root.UserID)
		if err != nil {
			t.Fatal(err)
		}
		n, _ := result.LastInsertId()
		id := uint64(n)
		exec("INSERT INTO tenant_memberships(tenant_id,user_id) VALUES(?,?)", root.TenantID, id)
		role, err := db.ExecContext(ctx, "INSERT INTO roles(tenant_id,name) VALUES(?,?)", root.TenantID, name)
		if err != nil {
			t.Fatal(err)
		}
		rid, _ := role.LastInsertId()
		for _, p := range permissions {
			exec("INSERT INTO role_permissions(role_id,permission_id) SELECT ?,id FROM permissions WHERE permission_key=?", rid, p)
		}
		exec("INSERT INTO user_role_assignments(tenant_id,user_id,role_id) VALUES(?,?,?)", root.TenantID, id, rid)
		userIDs = append(userIDs, id)
		return id
	}
	own := []string{"catalog.read", "request.create", "request.read_own", "request.cancel_own"}
	reporterID := seed("reporter", own)
	otherID := seed("other", own)
	agentID := seed("fulfiller", []string{"request.fulfill"})
	as := func(id uint64) requests.Actor {
		t.Helper()
		keys, err := identities.ListPermissions(ctx, root.TenantID, id)
		if err != nil {
			t.Fatal(err)
		}
		a := requests.Actor{Actor: workrecords.Actor{TenantID: root.TenantID, UserID: id, RequestID: "request-integration"}, Permissions: map[string]bool{}}
		for _, k := range keys {
			a.Permissions[k] = true
		}
		return a
	}
	admin, reporter, other, agent := as(root.UserID), as(reporterID), as(otherID), as(agentID)
	group, err := db.ExecContext(ctx, "INSERT INTO `groups`(tenant_id,name) VALUES(?,?)", root.TenantID, "Request team")
	if err != nil {
		t.Fatal(err)
	}
	gid, _ := group.LastInsertId()
	exec("INSERT INTO group_memberships(tenant_id,group_id,user_id) VALUES(?,?,?)", root.TenantID, gid, agentID)
	c := catalog.NewService(db)
	cat, err := c.SaveNode(ctx, admin, "catalogs", 0, catalog.Node{Name: "IT"})
	if err != nil {
		t.Fatal(err)
	}
	category, err := c.SaveNode(ctx, admin, "categories", 0, catalog.Node{Name: "Access", CatalogID: cat.ID})
	if err != nil {
		t.Fatal(err)
	}
	d := catalog.Definition{Title: "System access", Description: "Public service", Method: "MANUAL_TASKS", GroupID: uint64(gid), Fields: []catalog.Field{{Key: "system", Label: "System", Type: "text", Required: true}, {Key: "extra", Label: "Extra", Type: "checkbox"}, {Key: "details", Label: "Details", Type: "longtext", Required: true, VisibleWhen: &catalog.Condition{Field: "extra", Equals: json.RawMessage("true")}}}, Steps: []catalog.Step{{Title: "Provision", Instructions: "PRIVATE INSTRUCTIONS", Required: true}, {Title: "Deliver", Required: true}, {Title: "Optional follow-up", Required: false}}}
	item, err := c.SaveItem(ctx, admin, 0, catalog.Item{CategoryID: category.ID, Draft: d})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := catalog.Published(ctx, db, reporter, item.ID, 0); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatal("draft leaked", err)
	}
	item, err = c.Publish(ctx, admin, item.ID, item.Version, false)
	if err != nil {
		t.Fatal(err)
	}
	storage, err := attachments.NewLocalStorage(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	files, err := attachments.NewEventingService(attachments.NewRepository(db), storage, attachments.MaxFileBytes, tx, events.NewRepository(db))
	if err != nil {
		t.Fatal(err)
	}
	service := requests.NewService(db, files)
	input := requests.Submission{Items: []requests.SubmissionItem{{ItemID: item.ID, Version: *item.PublishedVersion, Values: map[string]json.RawMessage{"system": json.RawMessage(`"ERP 🛠️"`)}}}}
	bad := input
	bad.Items = append([]requests.SubmissionItem{}, input.Items...)
	bad.Items[0].Values = map[string]json.RawMessage{"system": json.RawMessage(`"ERP"`), "details": json.RawMessage(`"hidden"`)}
	if _, _, err := service.Create(ctx, reporter, bad, "request-hidden"); !errors.Is(err, catalog.ErrInvalid) {
		t.Fatal("hidden field accepted", err)
	}
	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM service_requests WHERE tenant_id=?", root.TenantID).Scan(&count); err != nil || count != 0 {
		t.Fatal("partial validation write", count, err)
	}
	_, _, err = requests.NewServiceWithOutbox(db, files, requestFailingOutbox{}).Create(ctx, reporter, input, "request-rollback")
	if err == nil {
		t.Fatal("outbox failure swallowed")
	}
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM fulfillment_tasks WHERE tenant_id=?", root.TenantID).Scan(&count); err != nil || count != 0 {
		t.Fatal("partial tasks survived", count, err)
	}
	var records [4]requests.Request
	var failures [4]error
	var wg sync.WaitGroup
	for n := range records {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			records[n], _, failures[n] = service.Create(ctx, reporter, input, "request-concurrent")
		}(n)
	}
	wg.Wait()
	r := records[0]
	for n := range records {
		if failures[n] != nil || records[n].ID != r.ID {
			t.Fatal("concurrent duplicate", n, records[n], failures[n])
		}
	}
	if _, _, err := service.Create(ctx, reporter, bad, "request-concurrent"); !errors.Is(err, requests.ErrIdempotency) {
		t.Fatal("different content accepted", err)
	}
	if _, err := service.Get(ctx, other, r.ID); !errors.Is(err, requests.ErrNotFound) {
		t.Fatal("other requester read", err)
	}
	foreign := admin
	foreign.TenantID = second.TenantID
	if _, err := service.Get(ctx, foreign, r.ID); !errors.Is(err, requests.ErrNotFound) {
		t.Fatal("tenant read", err)
	}
	if _, _, err := catalog.Published(ctx, db, foreign, item.ID, 0); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatal("foreign catalog leaked", err)
	}
	item.Draft.Title = "Revised access"
	item.Draft.Fields = []catalog.Field{{Key: "new_field", Label: "New", Type: "text", Required: true}}
	item, err = c.SaveItem(ctx, admin, item.ID, item)
	if err != nil {
		t.Fatal(err)
	}
	item, err = c.Publish(ctx, admin, item.ID, item.Version, false)
	if err != nil {
		t.Fatal(err)
	}
	replayed, created, err := service.Create(ctx, reporter, input, "request-concurrent")
	if err != nil || created || replayed.Items[0].Snapshot.Title != "System access" {
		t.Fatal("historical submission changed", replayed, err)
	}
	page, err := service.List(ctx, agent, true, "", "", "group", 0, 25)
	if err != nil {
		t.Fatal(err)
	}
	tasks := page.(requests.TaskPage).Items
	if len(tasks) != 3 {
		t.Fatal("not actual tasks", tasks)
	}
	if _, err := service.Transition(ctx, admin, r.ID, requests.Change{Version: r.Version, State: "FULFILLED"}); !errors.Is(err, requests.ErrTransition) {
		t.Fatal("closed without required work", err)
	}
	for _, task := range tasks {
		if _, err := service.Task(ctx, reporter, task.ID); !errors.Is(err, requests.ErrNotFound) {
			t.Fatal("operator data leaked", err)
		}
		if !task.Required {
			continue
		}
		assigned, err := service.ChangeTask(ctx, agent, task.ID, requests.Change{Version: task.Version, AssignedTo: &agentID}, true)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := service.ChangeTask(ctx, agent, task.ID, requests.Change{Version: task.Version, State: "IN_PROGRESS"}, false); !errors.Is(err, requests.ErrConflict) {
			t.Fatal("lost update", err)
		}
		active, err := service.ChangeTask(ctx, agent, task.ID, requests.Change{Version: assigned.Version, State: "IN_PROGRESS"}, false)
		if err != nil {
			t.Fatal(err)
		}
		done, err := service.ChangeTask(ctx, agent, task.ID, requests.Change{Version: active.Version, State: "COMPLETED", Reason: "PRIVATE RESULT"}, false)
		if err != nil || done.State != "COMPLETED" {
			t.Fatal(done, err)
		}
	}
	note, err := service.AddMessage(ctx, agent, r.ID, requests.CommunicationInput{Type: workrecords.MessageWorkNote, Body: "PRIVATE NOTE"}, "request-private-note")
	if err != nil {
		t.Fatal(err)
	}
	private, err := service.UploadFile(ctx, agent, r.ID, "secret.txt", "PUBLIC", &note.ID, strings.NewReader("PRIVATE FILE"), "request-private-file")
	if err != nil {
		t.Fatal(err)
	}
	if private.Visibility != "INTERNAL" {
		t.Fatal("message visibility not inherited")
	}
	if _, _, err := service.OpenFile(ctx, reporter, r.ID, private.ID); !errors.Is(err, requests.ErrNotFound) {
		t.Fatal("private file read", err)
	}
	reply, err := service.AddMessage(ctx, agent, r.ID, requests.CommunicationInput{Type: workrecords.MessageComment, Body: "Public response"}, "request-public-reply")
	if err != nil {
		t.Fatal(err)
	}
	repeat, err := service.AddMessage(ctx, agent, r.ID, requests.CommunicationInput{Type: workrecords.MessageComment, Body: "Public response"}, "request-public-reply")
	if err != nil || repeat.ID != reply.ID {
		t.Fatal("duplicate communication")
	}
	messages, err := service.Messages(ctx, reporter, r.ID, 0, 100)
	if err != nil || len(messages.Messages) != 1 {
		t.Fatal("private message leaked", messages, err)
	}
	public, err := service.UploadFile(ctx, reporter, r.ID, "public.txt", "PUBLIC", nil, strings.NewReader("public bytes"), "request-public-file")
	if err != nil {
		t.Fatal(err)
	}
	filepage, err := service.FileList(ctx, reporter, r.ID, 0, 100)
	if err != nil || len(filepage.Files) != 1 || filepage.Files[0].ID != public.ID {
		t.Fatal("private list leaked", filepage, err)
	}
	r, err = service.Get(ctx, admin, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	r, err = service.Transition(ctx, admin, r.ID, requests.Change{Version: r.Version, State: "FULFILLED"})
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range tasks {
		if task.Required {
			continue
		}
		cancelled, err := service.Task(ctx, agent, task.ID)
		if err != nil || cancelled.State != "CANCELLED" || len(cancelled.Actions) != 0 {
			t.Fatal("unfinished optional work survived finalization", cancelled, err)
		}
		if _, err := service.ChangeTask(ctx, agent, task.ID, requests.Change{Version: cancelled.Version, AssignedTo: &agentID}, true); !errors.Is(err, requests.ErrTransition) {
			t.Fatal("finalized task still writable", err)
		}
	}
	r, err = service.Transition(ctx, admin, r.ID, requests.Change{Version: r.Version, State: "CLOSED"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Transition(ctx, reporter, r.ID, requests.Change{Version: r.Version, State: "CANCELLED", Reason: "no"}); !errors.Is(err, requests.ErrTransition) {
		t.Fatal("late cancel allowed", err)
	}
	// New requests can be cancelled only before work, including all child tasks.
	newInput := requests.Submission{Items: []requests.SubmissionItem{{ItemID: item.ID, Version: *item.PublishedVersion, Values: map[string]json.RawMessage{"new_field": json.RawMessage(`"yes"`)}}}}
	pending, _, err := service.Create(ctx, reporter, newInput, "request-cancellable")
	if err != nil {
		t.Fatal(err)
	}
	cancelled, err := service.Transition(ctx, reporter, pending.ID, requests.Change{Version: pending.Version, State: "CANCELLED", Reason: "No longer needed"})
	if err != nil || cancelled.State != "CANCELLED" {
		t.Fatal("own cancellation", cancelled, err)
	}
	var remaining int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM fulfillment_tasks WHERE tenant_id=? AND request_id=? AND state<>'CANCELLED'", root.TenantID, pending.ID).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatal("uncancelled required child", remaining, err)
	}
	// Audience revocation hides both directory branches and direct publication.
	item.Draft.AudienceGroupID = uint64(gid)
	item, err = c.SaveItem(ctx, admin, item.ID, item)
	if err != nil {
		t.Fatal(err)
	}
	item, err = c.Publish(ctx, admin, item.ID, item.Version, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := catalog.Published(ctx, db, reporter, item.ID, 0); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatal("restricted audience read", err)
	}
	if _, _, err := service.Create(ctx, reporter, newInput, "request-restricted"); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatal("restricted audience submitted", err)
	}
	if _, err := c.SaveItem(ctx, reporter, item.ID, item); !errors.Is(err, workrecords.ErrForbidden) {
		t.Fatal("forged admin rejected", err)
	}
	// Actual dispatcher + shared handlers, then replay projections independently.
	dispatcher, err := events.NewDispatcher(events.NewRepository(db), slog.New(slog.NewTextHandler(io.Discard, nil)), "request-test", 1, 50*time.Millisecond, 5*time.Second, 0)
	if err != nil {
		t.Fatal(err)
	}
	projector := activity.NewProjector(tx, activity.NewRepository(db))
	if err := requests.RegisterEventHandlers(dispatcher, projector); err != nil {
		t.Fatal(err)
	}
	if err := workrecords.RegisterActivityHandlers(dispatcher, projector); err != nil {
		t.Fatal(err)
	}
	for n := 0; n < 100; n++ {
		processed, err := dispatcher.RunOnce(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !processed {
			break
		}
	}
	history, err := service.History(ctx, reporter, r.ID, 0, 100)
	if err != nil || len(history.Entries) < 7 {
		t.Fatal("no actual worker history", history, err)
	}
	var source events.Event
	source.TenantID, source.Type, source.AggregateType, source.AggregateID = root.TenantID, "request.created", "request", fmt.Sprint(r.ID)
	if err := db.QueryRowContext(ctx, "SELECT id,payload,created_at FROM outbox_events WHERE tenant_id=? AND event_type=? AND aggregate_id=?", root.TenantID, source.Type, source.AggregateID).Scan(&source.ID, &source.Payload, &source.CreatedAt); err != nil {
		t.Fatal(err)
	}
	for n := 0; n < 2; n++ {
		if err := projector.Handler("request.created", "Złożono wniosek")(ctx, source); err != nil {
			t.Fatal(err)
		}
	}
	repeatedHistory, err := service.History(ctx, reporter, r.ID, 0, 100)
	if err != nil || len(repeatedHistory.Entries) != len(history.Entries) {
		t.Fatal("projection replay duplicated durable history", err)
	}
	payload, _ := json.Marshal(history)
	if bytes.Contains(payload, []byte("PRIVATE")) {
		t.Fatal("private activity leaked", string(payload))
	}
	// A new service reads identical durable records/files; metadata exposes no key.
	restarted := requests.NewService(db, files)
	again, err := restarted.Get(ctx, reporter, r.ID)
	raw, _ := json.Marshal(again)
	if err != nil || again.Number != r.Number || again.State != "CLOSED" || bytes.Contains(raw, []byte("PRIVATE")) {
		t.Fatal("restart/privacy", again, err)
	}
	_, reader, err := restarted.OpenFile(ctx, reporter, r.ID, public.ID)
	if err != nil {
		t.Fatal(err)
	}
	contents, _ := io.ReadAll(reader)
	reader.Close()
	if string(contents) != "public bytes" {
		t.Fatal("lost attachment")
	}
	// Authenticated API rejects spoofed identity and public draft/foreign access.
	auth, err := identity.NewService(identities, tx, audits, cfg.Session, cfg.Auth)
	if err != nil {
		t.Fatal(err)
	}
	login, err := auth.Login(ctx, identity.LoginInput{Tenant: "request-" + suffix, Email: "reporter" + suffix + "@example.test", Password: "request-test-administrator"}, identity.RequestMeta{})
	if err != nil {
		t.Fatal(err)
	}
	router := chi.NewRouter()
	requests.RegisterRoutes(router, requests.NewHandler(service, c, auth), identity.NewMiddleware(auth, cfg.Session))
	req := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest(method, path, strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-CSRF-Token", login.CSRFToken)
		request.AddCookie(&http.Cookie{Name: cfg.Session.CookieName, Value: login.SessionToken})
		request.AddCookie(&http.Cookie{Name: cfg.Session.CSRFCookieName, Value: login.CSRFToken})
		w := httptest.NewRecorder()
		router.ServeHTTP(w, request)
		return w
	}
	if w := req("GET", fmt.Sprintf("/api/v1/catalog/admin/items/%d", item.ID), ""); w.Code != 403 {
		t.Fatal("admin API bypass", w.Code, w.Body.String())
	}
	if w := req("POST", "/api/v1/requests/", `{"tenant_id":"1","items":[]}`); w.Code != 400 {
		t.Fatal("actor spoof accepted", w.Code)
	}
	if w := req("GET", fmt.Sprintf("/api/v1/fulfillment/%d", tasks[0].ID), ""); w.Code != 404 {
		t.Fatal("task API leak", w.Code, w.Body.String())
	}
	exec("DELETE FROM group_memberships WHERE tenant_id=? AND group_id=? AND user_id=?", root.TenantID, gid, agentID)
	if _, err := service.Get(ctx, agent, r.ID); !errors.Is(err, requests.ErrNotFound) {
		t.Fatal("revoked object access", err)
	}
}

type requestFailingOutbox struct{}

func (requestFailingOutbox) Append(context.Context, database.Executor, events.Input) (uint64, bool, error) {
	return 0, false, errors.New("controlled outbox write failure")
}
