//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/chmajster/OpenServiceNOW/internal/activity"
	"github.com/chmajster/OpenServiceNOW/internal/audit"
	"github.com/chmajster/OpenServiceNOW/internal/catalog"
	"github.com/chmajster/OpenServiceNOW/internal/events"
	"github.com/chmajster/OpenServiceNOW/internal/identity"
	"github.com/chmajster/OpenServiceNOW/internal/notifications"
	"github.com/chmajster/OpenServiceNOW/internal/platform/config"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/requests"
	"github.com/chmajster/OpenServiceNOW/internal/workflow"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
	"github.com/go-chi/chi/v5"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDurableRequestWorkflow(t *testing.T) {
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
	repo := identity.NewMySQLRepository(db)
	audits := audit.NewMySQLRepository()
	suffix := fmt.Sprint(time.Now().UnixNano())
	slug := "workflow-" + suffix
	root, err := identity.NewBootstrapper(repo, tx, audits).Run(ctx, identity.BootstrapInput{TenantSlug: slug, TenantName: "Workflow", AdminEmail: "workflow" + suffix + "@example.test", AdminName: "Root", AdminPassword: "workflow-test-password"})
	if err != nil {
		t.Fatal(err)
	}
	users := []uint64{root.UserID}
	defer func() {
		_, _ = db.ExecContext(context.Background(), "DELETE FROM audit_events WHERE tenant_id=?", root.TenantID)
		if _, err := db.ExecContext(context.Background(), "DELETE FROM tenants WHERE id=?", root.TenantID); err != nil {
			t.Error(err)
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
	seed := func(name string, permissions []string) uint64 {
		t.Helper()
		result, err := db.ExecContext(ctx, "INSERT INTO users(email,display_name,password_hash) SELECT ?,?,password_hash FROM users WHERE id=?", name+suffix+"@example.test", name, root.UserID)
		if err != nil {
			t.Fatal(err)
		}
		n, _ := result.LastInsertId()
		id := uint64(n)
		users = append(users, id)
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
		return id
	}
	as := func(user uint64) workflow.Actor {
		keys, err := repo.ListPermissions(ctx, root.TenantID, user)
		if err != nil {
			t.Fatal(err)
		}
		a := workflow.Actor{Actor: workrecords.Actor{TenantID: root.TenantID, UserID: user, RequestID: "workflow-proof"}, Permissions: map[string]bool{}}
		for _, key := range keys {
			a.Permissions[key] = true
		}
		return a
	}
	reporter := as(seed("requester", []string{"catalog.read", "request.create", "request.read_own", "request.cancel_own", "workflow.approve"}))
	executor := as(seed("executor", []string{"workflow.execute", "request.fulfill"}))
	approver := as(seed("approver", []string{"workflow.approve"}))
	other := as(seed("other-approver", []string{"workflow.approve"}))
	outsider := as(seed("outsider", []string{"workflow.approve", "request.read_own"}))
	admin := as(root.UserID)
	group, err := db.ExecContext(ctx, "INSERT INTO `groups`(tenant_id,name) VALUES(?,?)", root.TenantID, "Fulfillment")
	if err != nil {
		t.Fatal(err)
	}
	g, _ := group.LastInsertId()
	gid := uint64(g)
	group, err = db.ExecContext(ctx, "INSERT INTO `groups`(tenant_id,name) VALUES(?,?)", root.TenantID, "Approvers")
	if err != nil {
		t.Fatal(err)
	}
	g, _ = group.LastInsertId()
	ag := uint64(g)
	exec("INSERT INTO group_memberships(tenant_id,group_id,user_id) VALUES(?,?,?),(?,?,?),(?,?,?),(?,?,?)", root.TenantID, gid, executor.UserID, root.TenantID, ag, approver.UserID, root.TenantID, ag, other.UserID, root.TenantID, ag, reporter.UserID)
	now := time.Now().UTC().Truncate(time.Microsecond)
	clock := func() time.Time { return now }
	requestsService := requests.NewService(db, nil)
	s := workflow.NewServiceWithClock(db, requestsService, clock)
	c := workflow.Config{Name: "Access approval", ExecutorID: executor.UserID, GroupID: gid, SelfApproval: "DENY", Nodes: []workflow.Node{
		{ID: "start", Label: "Submission", Type: "START", Next: "approval"},
		{ID: "approval", Label: "Approval", Type: "APPROVAL", Next: "task", Rejected: "cancel", Timeout: "cancel", TargetType: "GROUP", TargetID: ag, Policy: "ANY", Seconds: 60},
		{ID: "task", Label: "Fulfillment", Type: "CREATE_TASK", Next: "wait", Title: "Provision", Instructions: "PRIVATE WORKFLOW INSTRUCTIONS"},
		{ID: "wait", Label: "Wait for task", Type: "WAIT_TASK", Next: "complete", Timeout: "cancel", TaskRef: "task", Seconds: 120},
		{ID: "complete", Label: "Complete", Type: "UPDATE_RECORD", State: "FULFILLED", Reason: "Approved and delivered", Next: "notify"},
		{ID: "cancel", Label: "Cancel", Type: "UPDATE_RECORD", State: "CANCELLED", Reason: "Rejected or timeout", Next: "notify"},
		{ID: "notify", Label: "Notification", Type: "NOTIFY", Audience: "REQUESTER", Next: "end"},
		{ID: "end", Label: "End", Type: "END"},
	}}
	if _, err := s.Save(ctx, approver, 0, workflow.Definition{Draft: c}, "forbidden-create"); !errors.Is(err, workrecords.ErrForbidden) {
		t.Fatal("author privilege bypass", err)
	}
	def, err := s.Save(ctx, admin, 0, workflow.Definition{Draft: c}, "workflow-definition")
	if err != nil {
		t.Fatal(err)
	}
	def, err = s.Publish(ctx, admin, def.ID, def.Version)
	if err != nil {
		t.Fatal(err)
	}
	version, _ := strconv.ParseUint(def.PublishedVersion, 10, 64)
	catalogs := catalog.NewService(db)
	cat, err := catalogs.SaveNode(ctx, admin, "catalogs", 0, catalog.Node{Name: "IT"})
	if err != nil {
		t.Fatal(err)
	}
	category, err := catalogs.SaveNode(ctx, admin, "categories", 0, catalog.Node{CatalogID: cat.ID, Name: "Access"})
	if err != nil {
		t.Fatal(err)
	}
	draft := catalog.Definition{Title: "Access", Method: "WORKFLOW", GroupID: gid, WorkflowID: def.ID, WorkflowVersion: version, Fields: []catalog.Field{{Key: "system", Label: "System", Type: "text", Required: true}}, Steps: []catalog.Step{}}
	item, err := catalogs.SaveItem(ctx, admin, 0, catalog.Item{CategoryID: category.ID, Draft: draft})
	if err != nil {
		t.Fatal(err)
	}
	item, err = catalogs.Publish(ctx, admin, item.ID, item.Version, false)
	if err != nil {
		t.Fatal(err)
	}
	input := requests.Submission{Items: []requests.SubmissionItem{{ItemID: item.ID, Version: *item.PublishedVersion, Values: map[string]json.RawMessage{"system": json.RawMessage(`"ERP"`)}}}}
	combined := requests.Submission{Items: []requests.SubmissionItem{input.Items[0], input.Items[0]}}
	if _, _, err := requestsService.Create(ctx, reporter, combined, "workflow-no-join"); !errors.Is(err, requests.ErrInvalid) {
		t.Fatal("combined workflow request accepted without a join policy", err)
	}
	if _, _, err := requests.NewServiceWithOutbox(db, nil, requestFailingOutbox{}).Create(ctx, reporter, input, "workflow-rollback"); err == nil {
		t.Fatal("failed outbox committed workflow")
	}
	var count int
	if err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM workflow_executions WHERE tenant_id=?", root.TenantID).Scan(&count); err != nil || count != 0 {
		t.Fatal("partial execution survived", count, err)
	}
	create := func(key string) (requests.Request, workflow.Execution) {
		t.Helper()
		r, _, err := requestsService.Create(ctx, reporter, input, key)
		if err != nil {
			t.Fatal(err)
		}
		p, err := s.List(ctx, admin, false, r.ID, 0, "", 10)
		if err != nil || len(p.Items) != 1 {
			t.Fatal("missing execution", p, err)
		}
		return r, p.Items[0]
	}
	get := func(id uint64) workflow.Execution {
		t.Helper()
		x, err := s.Get(ctx, admin, id)
		if err != nil {
			t.Fatal(err)
		}
		return x
	}
	advance := func(id uint64) workflow.Execution {
		t.Helper()
		x := get(id)
		if err := s.Advance(ctx, root.TenantID, id, x.Version); err != nil {
			t.Fatal(err)
		}
		return get(id)
	}
	step := func(x workflow.Execution, node string) workflow.Step {
		for _, st := range x.Steps {
			if st.NodeID == node {
				return st
			}
		}
		t.Fatalf("missing step %s", node)
		return workflow.Step{}
	}
	r, x := create("workflow-submission")
	if r.Items[0].Total != 0 {
		t.Fatal("tasks created before approval")
	}
	replay, created, err := requestsService.Create(ctx, reporter, input, "workflow-submission")
	if err != nil || created || replay.ID != r.ID {
		t.Fatal("submission duplicated", err)
	}
	if _, err := requestsService.Transition(ctx, admin, r.ID, requests.Change{Version: r.Version, State: "FULFILLED"}); !errors.Is(err, requests.ErrTransition) {
		t.Fatal("direct bypass of approval", err)
	}
	if _, err := s.Get(ctx, outsider, x.ID); !errors.Is(err, workflow.ErrNotFound) {
		t.Fatal("foreign requester read execution", err)
	}
	foreign := admin
	foreign.TenantID++
	if _, err := s.Get(ctx, foreign, x.ID); !errors.Is(err, workflow.ErrNotFound) {
		t.Fatal("cross tenant workflow", err)
	}
	x = advance(x.ID)
	x = advance(x.ID)
	if x.State != "WAITING" || x.CurrentNode != "approval" {
		t.Fatal(x)
	}
	if n,err:=s.PendingCount(ctx,approver);err!=nil || n!=1 {t.Fatalf("pending count=%d err=%v",n,err)}
	if n,err:=s.PendingCount(ctx,reporter);err!=nil || n!=0 {t.Fatalf("self approval count=%d err=%v",n,err)}
	// New definitions and a service reconstruction cannot mutate an active version.
	c.Nodes[2].Title = "CHANGED NEW VERSION"
	def.Draft = c
	def, err = s.Save(ctx, admin, def.ID, def, "")
	if err != nil {
		t.Fatal(err)
	}
	def, err = s.Publish(ctx, admin, def.ID, def.Version)
	if err != nil {
		t.Fatal(err)
	}
	s = workflow.NewServiceWithClock(db, requestsService, clock)
	x = get(x.ID)
	if x.DefinitionVersion != version || x.State != "WAITING" {
		t.Fatal("version or wait changed after restart", x)
	}
	if _, err = s.Decide(ctx, reporter, x.ID, "approval", workflow.DecisionInput{Version: step(x, "approval").Version, Decision: "APPROVED", Reason: "Self"}); !errors.Is(err, workrecords.ErrForbidden) {
		t.Fatal("self approval bypass", err)
	}
	decisionVersion := step(x, "approval").Version
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, a := range []workflow.Actor{approver, other} {
		wg.Add(1)
		go func(i int, a workflow.Actor) {
			defer wg.Done()
			_, errs[i] = s.Decide(ctx, a, x.ID, "approval", workflow.DecisionInput{Version: decisionVersion, Decision: "APPROVED", Reason: "Approved"})
		}(i, a)
	}
	wg.Wait()
	wins := 0
	for _, err := range errs {
		if err == nil {
			wins++
		} else if !errors.Is(err, workflow.ErrConflict) {
			t.Fatal(err)
		}
	}
	if wins != 1 {
		t.Fatal("concurrent decisions", errs)
	}
	// Two concurrent duplicate job executions still create exactly one task.
	x = get(x.ID)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.Advance(ctx, root.TenantID, x.ID, x.Version); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM fulfillment_tasks WHERE tenant_id=? AND request_id=?", root.TenantID, r.ID).Scan(&count); err != nil || count != 1 {
		t.Fatal("duplicate tasks", count, err)
	}
	x = advance(x.ID)
	if x.State != "WAITING" || x.CurrentNode != "wait" {
		t.Fatal(x)
	}
	rawTasks, err := requestsService.List(ctx, executor, true, "", "", "group", 0, 25)
	if err != nil {
		t.Fatal(err)
	}
	tasks := rawTasks.(requests.TaskPage)
	if len(tasks.Items) != 1 {
		t.Fatal(tasks)
	}
	task := tasks.Items[0]
	if task.Title != "Provision" {
		t.Fatal("new publication altered task", task.Title)
	}
	target := executor.UserID
	task, err = requestsService.ChangeTask(ctx, executor, task.ID, requests.Change{Version: task.Version, AssignedTo: &target}, true)
	if err != nil {
		t.Fatal(err)
	}
	task, err = requestsService.ChangeTask(ctx, executor, task.ID, requests.Change{Version: task.Version, State: "IN_PROGRESS"}, false)
	if err != nil {
		t.Fatal(err)
	}
	task, err = requestsService.ChangeTask(ctx, executor, task.ID, requests.Change{Version: task.Version, State: "COMPLETED", Reason: "Delivered"}, false)
	if err != nil {
		t.Fatal(err)
	}
	// A late worker must use the persisted business completion, not receipt time.
	beforeDelay := now
	now = step(x, "wait").Deadline.Add(30 * time.Second)
	for i := 0; i < 5 && x.State != "COMPLETED"; i++ {
		x = advance(x.ID)
	}
	now = beforeDelay
	if x.State != "COMPLETED" {
		t.Fatal(x)
	}
	r, err = requestsService.Get(ctx, reporter, r.ID)
	if err != nil || r.State != "FULFILLED" {
		t.Fatal(r, err)
	}
	// Real independent event consumers produce timeline and notifications, and
	// repeating the source event cannot duplicate either MySQL effect.
	notificationService := notifications.NewService(db, nil, false)
	dispatcher, err := events.NewDispatcher(events.NewRepository(db), slog.New(slog.NewTextHandler(io.Discard, nil)), "workflow-proof", 1, 50*time.Millisecond, 5*time.Second, 0)
	if err != nil {
		t.Fatal(err)
	}
	projector := activity.NewProjector(tx, activity.NewRepository(db))
	for kind, label := range map[string]string{"workflow.changed": "Process", "workflow.approval.requested": "Approval", "workflow.notification": "Notification"} {
		if err = dispatcher.Register(kind, projector.Handler(kind, label)); err != nil {
			t.Fatal(err)
		}
	}
	if err = notificationService.Register(dispatcher); err != nil {
		t.Fatal(err)
	}
	if err = requests.RegisterEventHandlers(dispatcher, projector); err != nil {
		t.Fatal(err)
	}
	if err = workrecords.RegisterActivityHandlers(dispatcher, projector); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		done, err := dispatcher.RunOnce(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !done {
			break
		}
	}
	inbox, err := notificationService.List(ctx, root.TenantID, reporter.UserID, 0, 100)
	if err != nil || len(inbox.Notifications) == 0 {
		t.Fatal("missing process notification", inbox, err)
	}
	for _, n := range inbox.Notifications {
		if n.ExecutionID != x.ID {
			t.Fatal("wrong notification", n)
		}
	}
	// Group revocation removes notification metadata and counts before pagination.
	before, err := notificationService.List(ctx, root.TenantID, approver.UserID, 0, 100)
	if err != nil || len(before.Notifications) != 1 {
		t.Fatal(before, err)
	}
	exec("DELETE FROM group_memberships WHERE tenant_id=? AND group_id=? AND user_id=?", root.TenantID, ag, approver.UserID)
	after, err := notificationService.List(ctx, root.TenantID, approver.UserID, 0, 100)
	if err != nil || len(after.Notifications) != 0 || after.Unread != 0 {
		t.Fatal("revoked notification leaked", after, err)
	}
	exec("INSERT INTO group_memberships(tenant_id,group_id,user_id) VALUES(?,?,?)", root.TenantID, ag, approver.UserID)
	// Explicit API authentication, session identity, CSRF and object authorization.
	auth, err := identity.NewService(repo, tx, audits, cfg.Session, cfg.Auth)
	if err != nil {
		t.Fatal(err)
	}
	login, err := auth.Login(ctx, identity.LoginInput{Tenant: slug, Email: "approver" + suffix + "@example.test", Password: "workflow-test-password"}, identity.RequestMeta{})
	if err != nil {
		t.Fatal(err)
	}
	router := chi.NewRouter()
	workflow.RegisterRoutes(router, workflow.NewHandler(s, auth), identity.NewMiddleware(auth, cfg.Session))
	call := func(method, path, body string, csrf bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: cfg.Session.CookieName, Value: login.SessionToken})
		req.AddCookie(&http.Cookie{Name: cfg.Session.CSRFCookieName, Value: login.CSRFToken})
		if csrf {
			req.Header.Set("X-CSRF-Token", login.CSRFToken)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}
	if w := call("POST", fmt.Sprintf("/api/v1/workflow/executions/%d/decisions/approval", x.ID), `{"version":"1","decision":"APPROVED","reason":"retry"}`, false); w.Code != 403 {
		t.Fatal("missing CSRF accepted", w.Code, w.Body.String())
	}
	if w := call("POST", "/api/v1/workflow/definitions", `{"tenant_id":"1","draft":{}}`, true); w.Code != 400 {
		t.Fatal("spoofed identity accepted", w.Code)
	}
	if w := call("GET", fmt.Sprintf("/api/v1/workflow/executions/%d", x.ID), "", true); w.Code != 200 || strings.Contains(w.Body.String(), "PRIVATE WORKFLOW INSTRUCTIONS") {
		t.Fatal("unsafe process DTO", w.Code, w.Body.String())
	}
	// Timeout races are serialized on the same execution and cannot create tasks.
	_, timed := create("workflow-timeout")
	timed = advance(timed.ID)
	timed = advance(timed.ID)
	now = *step(timed, "approval").Deadline
	errs = make([]error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, errs[0] = s.Decide(ctx, approver, timed.ID, "approval", workflow.DecisionInput{Version: step(timed, "approval").Version, Decision: "APPROVED", Reason: "Too late"})
	}()
	go func() { defer wg.Done(); errs[1] = s.Advance(ctx, root.TenantID, timed.ID, timed.Version) }()
	wg.Wait()
	if errs[0] == nil || errs[1] != nil {
		t.Fatal("late approval accepted", errs)
	}
	timed = get(timed.ID)
	if timed.Approved || timed.CurrentNode != "cancel" {
		t.Fatal(timed)
	}
	// Executor revocation is a visible stop; a reviewed CAS resume reuses the node.
	_, failed := create("workflow-repair")
	failed = advance(failed.ID)
	exec("DELETE FROM group_memberships WHERE tenant_id=? AND group_id=? AND user_id=?", root.TenantID, gid, executor.UserID)
	failed = advance(failed.ID)
	if failed.State != "ERROR" || failed.ErrorCode != "EXECUTOR_ACCESS_REVOKED" || len(failed.Attempts) == 0 {
		t.Fatal("missing diagnostic", failed)
	}
	exec("INSERT INTO group_memberships(tenant_id,group_id,user_id) VALUES(?,?,?)", root.TenantID, gid, executor.UserID)
	failed, err = s.Repair(ctx, admin, failed.ID, failed.Version, "RESUME", "Restored group membership")
	if err != nil {
		t.Fatal(err)
	}
	failed = advance(failed.ID)
	if failed.State != "WAITING" {
		t.Fatal("resume failed", failed)
	}
	if _, err = s.Repair(ctx, admin, failed.ID, failed.Version-1, "CANCEL", "Old version"); !errors.Is(err, workflow.ErrConflict) {
		t.Fatal("repair CAS bypass", err)
	}
	if _, err = s.Repair(ctx, admin, failed.ID, failed.Version, "CANCEL", "Stop execution"); err != nil {
		t.Fatal(err)
	}
	// ALL freezes its participant set, does not silently shrink after revocation,
	// and checks the prior signatures again before permitting continuation.
	publishVariant := func(config workflow.Config, label string) {
		t.Helper()
		config.Name = label
		d, err := s.Save(ctx, admin, 0, workflow.Definition{Draft: config}, "variant-"+label)
		if err != nil {
			t.Fatal(err)
		}
		d, err = s.Publish(ctx, admin, d.ID, d.Version)
		if err != nil {
			t.Fatal(err)
		}
		v, _ := strconv.ParseUint(d.PublishedVersion, 10, 64)
		data := draft
		data.Title = label
		data.WorkflowID = d.ID
		data.WorkflowVersion = v
		i, err := catalogs.SaveItem(ctx, admin, 0, catalog.Item{CategoryID: category.ID, Draft: data})
		if err != nil {
			t.Fatal(err)
		}
		i, err = catalogs.Publish(ctx, admin, i.ID, i.Version, false)
		if err != nil {
			t.Fatal(err)
		}
		input.Items[0].ItemID = i.ID
		input.Items[0].Version = *i.PublishedVersion
	}
	all := c
	all.Nodes = append([]workflow.Node{}, c.Nodes...)
	all.Nodes[1].Policy = "ALL"
	publishVariant(all, "all-policy")
	_, allWait := create("workflow-all-revoked")
	allWait = advance(allWait.ID)
	allWait = advance(allWait.ID)
	allWait, err = s.Decide(ctx, approver, allWait.ID, "approval", workflow.DecisionInput{Version: step(allWait, "approval").Version, Decision: "APPROVED", Reason: "First signature"})
	if err != nil || allWait.State != "WAITING" {
		t.Fatal(allWait, err)
	}
	exec("INSERT INTO group_memberships(tenant_id,group_id,user_id) VALUES(?,?,?)", root.TenantID, ag, outsider.UserID)
	if _, err = s.Decide(ctx, outsider, allWait.ID, "approval", workflow.DecisionInput{Version: step(allWait, "approval").Version, Decision: "APPROVED", Reason: "Joined late"}); !errors.Is(err, workflow.ErrNotFound) {
		t.Fatal("late member joined existing approval", err)
	}
	exec("DELETE FROM group_memberships WHERE tenant_id=? AND group_id=? AND user_id=?", root.TenantID, ag, approver.UserID)
	allWait, err = s.Decide(ctx, other, allWait.ID, "approval", workflow.DecisionInput{Version: step(allWait, "approval").Version, Decision: "APPROVED", Reason: "Second signature"})
	if err != nil || allWait.State != "WAITING" || allWait.Approved {
		t.Fatal("revoked prior signature authorized continuation", allWait, err)
	}
	now = *step(allWait, "approval").Deadline
	allWait = advance(allWait.ID)
	if allWait.CurrentNode != "cancel" || allWait.Approved {
		t.Fatal("ALL revoked signature did not timeout", allWait)
	}
	exec("DELETE FROM group_memberships WHERE tenant_id=? AND group_id=? AND user_id=?", root.TenantID, ag, outsider.UserID)
	exec("INSERT INTO group_memberships(tenant_id,group_id,user_id) VALUES(?,?,?)", root.TenantID, ag, approver.UserID)
	_, allSuccess := create("workflow-all-success")
	allSuccess = advance(allSuccess.ID)
	allSuccess = advance(allSuccess.ID)
	for _, a := range []workflow.Actor{approver, other} {
		allSuccess, err = s.Decide(ctx, a, allSuccess.ID, "approval", workflow.DecisionInput{Version: step(allSuccess, "approval").Version, Decision: "APPROVED", Reason: "All authorized"})
		if err != nil {
			t.Fatal(err)
		}
	}
	if allSuccess.State != "READY" || !allSuccess.Approved {
		t.Fatal("ALL did not progress", allSuccess)
	}
	// Explicit ALLOW permits only an authorized target, never arbitrary requesters.
	self := c
	self.Nodes = append([]workflow.Node{}, c.Nodes...)
	self.SelfApproval = "ALLOW"
	self.Nodes[1].TargetType = "USER"
	self.Nodes[1].TargetID = reporter.UserID
	publishVariant(self, "self-allow")
	_, selfWait := create("workflow-self-allow")
	selfWait = advance(selfWait.ID)
	selfWait = advance(selfWait.ID)
	selfWait, err = s.Decide(ctx, reporter, selfWait.ID, "approval", workflow.DecisionInput{Version: step(selfWait, "approval").Version, Decision: "APPROVED", Reason: "Explicit policy"})
	if err != nil || !selfWait.Approved {
		t.Fatal("explicit self policy did not work", selfWait, err)
	}
	// Declarative conditions read the immutable form values; timers and repeated
	// early scheduler visits do not hold a process or churn business versions.
	timer := c
	timer.Nodes = append([]workflow.Node{}, c.Nodes...)
	timer.Nodes[0].Next = "condition"
	timer.Nodes = append(timer.Nodes, workflow.Node{ID: "condition", Label: "Form condition", Type: "CONDITION", Field: "system", Equals: json.RawMessage(`"ERP"`), Next: "timer", Otherwise: "cancel"}, workflow.Node{ID: "timer", Label: "Timer", Type: "WAIT_TIMER", Seconds: 10, Next: "approval"})
	publishVariant(timer, "timer-condition")
	_, timerWait := create("workflow-timer")
	timerWait = advance(timerWait.ID)
	timerWait = advance(timerWait.ID)
	timerWait = advance(timerWait.ID)
	if timerWait.CurrentNode != "timer" || timerWait.State != "WAITING" {
		t.Fatal(timerWait)
	}
	v := timerWait.Version
	timerWait = advance(timerWait.ID)
	if timerWait.Version != v {
		t.Fatal("early wait visit created a business version")
	}
	now = *step(timerWait, "timer").Deadline
	timerWait = advance(timerWait.ID)
	if timerWait.CurrentNode != "approval" || timerWait.State != "READY" {
		t.Fatal("timer did not advance", timerWait)
	}
}
