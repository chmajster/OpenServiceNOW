//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/chmajster/OpenServiceNOW/internal/audit"
	"github.com/chmajster/OpenServiceNOW/internal/changes"
	"github.com/chmajster/OpenServiceNOW/internal/cmdb"
	"github.com/chmajster/OpenServiceNOW/internal/identity"
	"github.com/chmajster/OpenServiceNOW/internal/platform/config"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/problems"
	"github.com/chmajster/OpenServiceNOW/internal/requests"
	"github.com/chmajster/OpenServiceNOW/internal/workflow"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
	"github.com/chmajster/OpenServiceNOW/internal/workspace"
	"strconv"
	"sync"
	"testing"
	"time"
)

func TestChangeVersionedApprovalsSchedulingAndTemplates(t *testing.T) {
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
	repo := identity.NewMySQLRepository(db)
	audits := audit.NewMySQLRepository()
	suffix := fmt.Sprint(time.Now().UnixNano())
	slug := "change-" + suffix
	root, err := identity.NewBootstrapper(repo, tx, audits).Run(ctx, identity.BootstrapInput{TenantSlug: slug, TenantName: "Workflow", AdminEmail: "workflow" + suffix + "@example.test", AdminName: "Root", AdminPassword: "change-test-password"})
	if err != nil {
		t.Fatal(err)
	}
	users := []uint64{root.UserID}
	defer func() {
		_, _ = db.ExecContext(context.Background(), "DELETE FROM audit_events WHERE tenant_id=?", root.TenantID)
		_, _ = db.ExecContext(context.Background(), "DELETE FROM changes WHERE tenant_id=?", root.TenantID)
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
		a := workflow.Actor{Actor: workrecords.Actor{TenantID: root.TenantID, UserID: user, RequestID: "change-proof"}, Permissions: map[string]bool{}}
		for _, key := range keys {
			a.Permissions[key] = true
		}
		return a
	}

	admin := as(root.UserID)
	operator := as(seed("operator", []string{"change.read_own", "change.create", "change.manage", "change.assign", "change.transition", "change.tasks", "change.notes", "change.execute", "workflow.execute", "cmdb.read", "cmdb.link", "problem.read", "problem.manage"}))
	approver := as(seed("approver", []string{"workflow.approve", "change.template_approve", "change.read", "cmdb.read"}))
	outsider := as(seed("outsider", []string{"change.read_own", "workflow.approve"}))
	result, err := db.ExecContext(ctx, "INSERT INTO `groups`(tenant_id,name) VALUES(?,?)", admin.TenantID, "Change operators")
	if err != nil {
		t.Fatal(err)
	}
	n, _ := result.LastInsertId()
	gid := uint64(n)
	exec("INSERT INTO group_memberships(tenant_id,group_id,user_id) VALUES(?,?,?)", admin.TenantID, gid, operator.UserID)
	ps := problems.NewService(db, nil, nil)
	s := changes.NewService(db, nil, ps)
	ws := workflow.NewService(db, requests.NewService(db, nil), s)
	cs := cmdb.NewService(db, nil, map[string]cmdb.RecordDomain{"changes": changes.NewCMDBDomain(s)})
	ci, err := cs.Save(ctx, admin, 0, cmdb.Record{Fields: cmdb.Fields{Identifier: "change-db", Name: "Database", Class: "DATABASE", Status: "ACTIVE", OwnerID: admin.UserID, Environment: "PROD", Attributes: map[string]json.RawMessage{"engine": []byte(`"MYSQL"`)}}}, "change-ci-create")
	if err != nil {
		t.Fatal(err)
	}
	definition := func(kind string) workflow.Definition {
		c := workflow.Config{Domain: "CHANGE", ChangeType: kind, Name: kind + " CAB", ExecutorID: operator.UserID, GroupID: gid, SelfApproval: "DENY", Nodes: []workflow.Node{
			{ID: "start", Label: "Start", Type: "START", Next: "approve"},
			{ID: "approve", Label: "CAB", Type: "APPROVAL", TargetType: "USER", TargetID: approver.UserID, Policy: "ALL", Seconds: 3600, Next: "authorize", Rejected: "reject", Timeout: "reject"},
			{ID: "authorize", Label: "Authorize", Type: "UPDATE_RECORD", State: "AUTHORIZED", Reason: "Approved evaluated version", Next: "end"},
			{ID: "reject", Label: "Reject", Type: "UPDATE_RECORD", State: "REJECTED", Reason: "Rejected evaluated version", Next: "end"},
			{ID: "end", Label: "End", Type: "END"},
		}}
		d, err := ws.Save(ctx, admin, 0, workflow.Definition{Draft: c}, "change-definition-"+kind)
		if err != nil {
			t.Fatal(err)
		}
		d, err = ws.Publish(ctx, admin, d.ID, d.Version)
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	normal := definition("NORMAL")
	v, _ := strconv.ParseUint(normal.PublishedVersion, 10, 64)
	start := time.Now().UTC().Add(30 * time.Minute).Truncate(time.Microsecond)
	end := start.Add(time.Hour)
	fields := changes.Fields{Title: "Repair database", Description: "Public implementation", Type: "NORMAL", Risk: "MEDIUM", Impact: "HIGH", Justification: "Remove recurring outage", Scope: "Database settings", ImplementationPlan: "Deploy validated settings", TestPlan: "Check recovery", RollbackPlan: "Restore snapshot", PlannedStart: &start, PlannedEnd: &end, TimeZone: "Europe/Warsaw", WorkflowID: &normal.ID, WorkflowVersion: &v, GroupID: &gid}
	prb, err := ps.Save(ctx, admin, 0, problems.Input{Fields: problems.Fields{Title: "Recurring outage", Description: "Public problem", Impact: "HIGH"}}, "change-source-problem")
	if err != nil {
		t.Fatal(err)
	}
	_, err = ps.AddMessage(ctx, admin, prb.ID, problems.CommunicationInput{Type: workrecords.MessageWorkNote, Body: "PRIVATE ROOT CAUSE NOTE"}, "change-private-source")
	if err != nil {
		t.Fatal(err)
	}
	source, err := s.SourceProblem(ctx, operator, prb.ID)
	if err != nil || source.Description != "Public problem" {
		t.Fatal(source, err)
	}
	input := changes.Input{Fields: fields, SourceProblemID: prb.ID, SourceVersion: source.Version}
	r, err := s.Save(ctx, operator, 0, input, "change-create-retry")
	if err != nil {
		t.Fatal(err)
	}
	replay, err := s.Save(ctx, operator, 0, input, "change-create-retry")
	if err != nil || replay.ID != r.ID {
		t.Fatal("create replay", replay, err)
	}
	if messages, err := s.Messages(ctx, operator, r.ID, 0, 25); err != nil || len(messages.Messages) != 0 {
		t.Fatal("copied private source note", messages, err)
	}
	if _, err = s.Get(ctx, outsider, r.ID); !errors.Is(err, changes.ErrNotFound) {
		t.Fatal("own-resource access", err)
	}
	link := func(r changes.Record) changes.Record {
		current, err := cs.Get(ctx, admin, ci.ID)
		if err != nil {
			t.Fatal(err)
		}
		_, err = cs.ChangeRecordLink(ctx, operator, "changes", r.ID, ci.ID, cmdb.RecordLinkInput{Version: r.Version, CIVersion: current.Version}, false, "change-ci-link-"+fmt.Sprint(r.ID))
		if err != nil {
			t.Fatal(err)
		}
		r, err = s.Get(ctx, operator, r.ID)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	r = link(r)
	transition := func(r changes.Record, state, key string) changes.Record {
		out, err := s.Transition(ctx, operator, r.ID, changes.TransitionInput{Version: r.Version, State: state, Reason: "Explicit process action"}, key)
		if err != nil {
			t.Fatal(state, err)
		}
		return out
	}
	r = transition(r, "ASSESS", "change-assess")
	r = transition(r, "AUTHORIZE", "change-authorize")
	execution := *r.ExecutionID
	advance := func(id uint64) workflow.Execution {
		t.Helper()
		for i := 0; i < 12; i++ {
			x, err := ws.Get(ctx, admin, id)
			if err != nil {
				t.Fatal(err)
			}
			if x.State != "READY" {
				return x
			}
			if err = ws.Advance(ctx, admin.TenantID, id, x.Version); err != nil {
				t.Fatal(err)
			}
		}
		t.Fatal("workflow did not settle")
		return workflow.Execution{}
	}
	x := advance(execution)
	if x.State != "WAITING" || x.SubjectVersion != r.Version || x.Domain != "CHANGE" {
		t.Fatal("pinned evaluation", x)
	}
	// Rebuilding the adapter/service resumes precisely the stored step and deadline.
	oldDeadline := x.Steps[1].Deadline
	ws = workflow.NewService(db, requests.NewService(db, nil), changes.NewService(db, nil, ps))
	x, err = ws.Get(ctx, approver, execution)
	if err != nil || x.Steps[1].Deadline == nil || !x.Steps[1].Deadline.Equal(*oldDeadline) {
		t.Fatal("durable wait", x, err)
	}
	if _, err = ws.Decide(ctx, operator, execution, "approve", workflow.DecisionInput{Version: x.Steps[1].Version, Decision: "APPROVED", Reason: "Self"}); !errors.Is(err, workrecords.ErrForbidden) {
		t.Fatal("approval RBAC", err)
	}
	stepVersion := x.Steps[1].Version
	decision := workflow.DecisionInput{Version: stepVersion, Decision: "APPROVED", Reason: "Reviewed exact plans"}
	x, err = ws.Decide(ctx, approver, execution, "approve", decision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ws.Decide(ctx, approver, execution, "approve", decision); err != nil {
		t.Fatal("decision replay", err)
	}
	x = advance(execution)
	if x.State != "COMPLETED" {
		t.Fatal(x)
	}
	r, err = s.Get(ctx, operator, r.ID)
	if err != nil || r.ApprovalState != "APPROVED" || r.ApprovedRevision == nil || *r.ApprovedRevision != r.AssessmentRevision {
		t.Fatal("domain approval", r, err)
	}
	// Editing the evaluated plan invalidates both the stamp and stale decisions.
	changed := r.Fields
	changed.Scope = "Database settings and pool limits"
	r, err = s.Save(ctx, operator, r.ID, changes.Input{Fields: changed, Version: r.Version}, "change-scope-edit")
	if err != nil || r.State != "ASSESS" || r.ApprovalState != "INVALIDATED" || r.ApprovedRevision != nil {
		t.Fatal("invalidation", r, err)
	}
	if _, err = ws.Decide(ctx, approver, execution, "approve", decision); !errors.Is(err, workflow.ErrConflict) {
		t.Fatal("stale evaluated decision", err)
	}
	if _, err = s.Transition(ctx, operator, r.ID, changes.TransitionInput{Version: r.Version, State: "SCHEDULED"}, "change-stale-schedule"); !errors.Is(err, changes.ErrTransition) {
		t.Fatal("stale schedule", err)
	}
	approve := func(r changes.Record, key string) changes.Record {
		if r.State == "NEW" && r.Type == "NORMAL" {
			r = transition(r, "ASSESS", key+"-assess")
		}
		r = transition(r, "AUTHORIZE", key+"-authorize")
		x := advance(*r.ExecutionID)
		var st workflow.Step
		for _, v := range x.Steps {
			if v.NodeID == "approve" {
				st = v
			}
		}
		if _, err := ws.Decide(ctx, approver, x.ID, "approve", workflow.DecisionInput{Version: st.Version, Decision: "APPROVED", Reason: "CAB"}); err != nil {
			t.Fatal(err)
		}
		advance(x.ID)
		r, err = s.Get(ctx, operator, r.ID)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	r = approve(r, "change-reassess")
	other, err := s.Save(ctx, operator, 0, changes.Input{Fields: r.Fields}, "change-collision-create")
	if err != nil {
		t.Fatal(err)
	}
	other = approve(link(other), "change-collision")
	// Two concurrent schedule commands serialize against the same affected CI.
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for i, record := range []changes.Record{r, other} {
		wg.Add(1)
		go func(i int, record changes.Record) {
			defer wg.Done()
			_, err := s.Transition(ctx, operator, record.ID, changes.TransitionInput{Version: record.Version, State: "SCHEDULED"}, fmt.Sprintf("change-concurrent-schedule-%d", i))
			results <- err
		}(i, record)
	}
	wg.Wait()
	close(results)
	ok, blocked := 0, 0
	for err := range results {
		if err == nil {
			ok++
		} else if errors.Is(err, changes.ErrCollision) {
			blocked++
		} else {
			t.Fatal(err)
		}
	}
	if ok != 1 || blocked != 1 {
		t.Fatal("concurrent reservations", ok, blocked)
	}
	r, _ = s.Get(ctx, operator, r.ID)
	other, _ = s.Get(ctx, operator, other.ID)
	scheduled, unscheduled := r, other
	if other.State == "SCHEDULED" {
		scheduled, unscheduled = other, r
	}
	// A public-reader with no object scope sees only the safe conflict flag.
	exec("DELETE FROM group_memberships WHERE tenant_id=? AND group_id=? AND user_id=?", admin.TenantID, gid, operator.UserID)
	// Operator still reads their own records; test a separate restricted actor via
	// reporter transfer on just the scheduled test fixture.
	exec("UPDATE changes SET reporter_id=? WHERE tenant_id=? AND id=?", admin.UserID, admin.TenantID, scheduled.ID)
	collision, err := s.Collisions(ctx, operator, unscheduled.ID)
	if err != nil || !collision.RestrictedConflict || len(collision.Items) != 0 {
		t.Fatal("collision leaks protected record", collision, err)
	}
	exec("INSERT INTO group_memberships(tenant_id,group_id,user_id) VALUES(?,?,?)", admin.TenantID, gid, operator.UserID)
	// Half-open boundaries allow a following reservation without an overlap.
	boundary := unscheduled.Fields
	nextStart, nextEnd := *scheduled.PlannedEnd, scheduled.PlannedEnd.Add(time.Hour)
	boundary.PlannedStart = &nextStart
	boundary.PlannedEnd = &nextEnd
	unscheduled, err = s.Save(ctx, operator, unscheduled.ID, changes.Input{Version: unscheduled.Version, Fields: boundary}, "change-boundary-edit")
	if err != nil {
		t.Fatal(err)
	}
	unscheduled = approve(unscheduled, "change-boundary-approve")
	unscheduled = transition(unscheduled, "SCHEDULED", "change-boundary-schedule")
	// Blackouts and service windows are independent of approval.
	rule, err := s.SaveRule(ctx, admin, 0, changes.CalendarRule{CIID: &ci.ID, Kind: "BLACKOUT", Title: "Freeze", StartsAt: nextStart, EndsAt: nextEnd, TimeZone: "Europe/Warsaw", Active: true}, "change-blackout")
	if err != nil {
		t.Fatal(err)
	}
	readRule, ruleErr := s.Rule(ctx, admin, rule.ID)
	if ruleErr != nil || readRule.Version != rule.Version {
		t.Fatal("rule conflict read", readRule, ruleErr)
	}
	if _, ruleErr = s.Rule(ctx, operator, rule.ID); !errors.Is(ruleErr, changes.ErrForbidden) {
		t.Fatal("rule permission", ruleErr)
	}
	check, err := s.Collisions(ctx, operator, unscheduled.ID)
	if err != nil || !check.Blackout {
		t.Fatal("blackout", check, err)
	}
	rule.Active = false
	if _, err = s.SaveRule(ctx, admin, rule.ID, rule, "change-blackout-disable"); err != nil {
		t.Fatal(err)
	}
	calendar, err := s.Calendar(ctx, operator, start.Add(-time.Hour), nextEnd)
	if err != nil || len(calendar.Changes) != 2 {
		t.Fatal("calendar", calendar, err)
	}
	// Standard always pins an approved template and still requires an instance decision.
	standard := definition("STANDARD")
	sv, _ := strconv.ParseUint(standard.PublishedVersion, 10, 64)
	sf := fields
	sf.Type = "STANDARD"
	sf.WorkflowID = &standard.ID
	sf.WorkflowVersion = &sv
	sf.PlannedStart = nil
	sf.PlannedEnd = nil
	template, err := s.SaveTemplate(ctx, admin, 0, changes.TemplateInput{Name: "Database maintenance", Config: changes.TemplateConfig{Fields: sf, CIIDs: []string{fmt.Sprint(ci.ID)}, MaxRisk: "MEDIUM", MaxDurationSeconds: 3600}}, "standard-template")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PublishTemplate(ctx, admin, template.ID, template.Version, false, "standard-self-publish"); !errors.Is(err, changes.ErrForbidden) {
		t.Fatal("self template approval", err)
	}
	template, err = s.PublishTemplate(ctx, approver, template.ID, template.Version, false, "standard-publish")
	if err != nil {
		t.Fatal(err)
	}
	sf.TemplateID = &template.ID
	sf.TemplateVersion = template.PublishedVersion
	sf.PlannedStart = &start
	sf.PlannedEnd = &end
	standardChange, err := s.Save(ctx, operator, 0, changes.Input{Fields: sf}, "standard-change")
	if err != nil {
		t.Fatal(err)
	}
	standardChange = link(standardChange)
	if _, err = s.Transition(ctx, operator, standardChange.ID, changes.TransitionInput{Version: standardChange.Version, State: "SCHEDULED"}, "standard-bypass"); !errors.Is(err, changes.ErrTransition) {
		t.Fatal("standard bypass", err)
	}
	standardChange = approve(standardChange, "standard-instance")
	outside := standardChange.Fields
	outside.Scope = "Outside template"
	if _, err = s.Save(ctx, operator, standardChange.ID, changes.Input{Version: standardChange.Version, Fields: outside}, "standard-outside"); !errors.Is(err, changes.ErrInvalid) {
		t.Fatal("template constraints", err)
	}
	// Emergency has a different definition and cannot skip justification.
	emergency := definition("EMERGENCY")
	ev, _ := strconv.ParseUint(emergency.PublishedVersion, 10, 64)
	ef := fields
	ef.Type = "EMERGENCY"
	ef.WorkflowID = &emergency.ID
	ef.WorkflowVersion = &ev
	urgent, err := s.Save(ctx, operator, 0, changes.Input{Fields: ef}, "emergency-change")
	if err != nil {
		t.Fatal(err)
	}
	urgent = link(urgent)
	if _, err = s.Transition(ctx, operator, urgent.ID, changes.TransitionInput{Version: urgent.Version, State: "AUTHORIZE"}, "emergency-no-reason"); !errors.Is(err, changes.ErrTransition) {
		t.Fatal("emergency justification", err)
	}
	ef.EmergencyReason = "Active outage"
	urgent, err = s.Save(ctx, operator, urgent.ID, changes.Input{Version: urgent.Version, Fields: ef}, "emergency-reason")
	if err != nil {
		t.Fatal(err)
	}
	urgent = approve(urgent, "emergency-instance")

	// Required execution tasks cannot run before the approved implementation window.
	taskFields := fields
	taskStart := time.Now().UTC().Add(3 * time.Second).Truncate(time.Microsecond)
	taskEnd := taskStart.Add(10 * time.Minute)
	taskFields.PlannedStart = &taskStart
	taskFields.PlannedEnd = &taskEnd
	taskChange, err := s.Save(ctx, operator, 0, changes.Input{Fields: taskFields}, "change-task-parent")
	if err != nil {
		t.Fatal(err)
	}
	taskChange = approve(link(taskChange), "change-task-approve")
	taskChange = transition(taskChange, "SCHEDULED", "change-task-schedule")
	taskInput := changes.TaskInput{ChangeVersion: taskChange.Version, Title: "Apply settings", Instructions: "Execute approved plan", Required: true, State: "OPEN", GroupID: &gid, AssignedTo: &operator.UserID}
	task, err := s.SaveTask(ctx, operator, taskChange.ID, 0, taskInput, "change-execution-task")
	if err != nil {
		t.Fatal(err)
	}
	if again, err := s.SaveTask(ctx, operator, taskChange.ID, 0, taskInput, "change-execution-task"); err != nil || again.ID != task.ID {
		t.Fatal("task replay", again, err)
	}
	taskChange, _ = s.Get(ctx, operator, taskChange.ID)
	taskInput.Version = task.Version
	taskInput.ChangeVersion = taskChange.Version
	taskInput.State = "IN_PROGRESS"
	if _, err = s.SaveTask(ctx, operator, taskChange.ID, task.ID, taskInput, "change-task-too-early"); !errors.Is(err, changes.ErrTransition) {
		t.Fatal("premature task", err)
	}
	if wait := time.Until(taskStart); wait > 0 {
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	taskChange = transition(taskChange, "IMPLEMENT", "change-implement")
	taskInput.ChangeVersion = taskChange.Version
	task, err = s.SaveTask(ctx, operator, taskChange.ID, task.ID, taskInput, "change-task-start")
	if err != nil {
		t.Fatal(err)
	}
	taskChange, _ = s.Get(ctx, operator, taskChange.ID)
	if _, err = s.Transition(ctx, operator, taskChange.ID, changes.TransitionInput{Version: taskChange.Version, State: "REVIEW"}, "change-task-incomplete"); !errors.Is(err, changes.ErrTransition) {
		t.Fatal("required task gate", err)
	}
	taskInput.Version = task.Version
	taskInput.ChangeVersion = taskChange.Version
	taskInput.State = "COMPLETED"
	taskInput.Result = "Settings verified"
	task, err = s.SaveTask(ctx, operator, taskChange.ID, task.ID, taskInput, "change-task-complete")
	if err != nil {
		t.Fatal(err)
	}
	taskChange, _ = s.Get(ctx, operator, taskChange.ID)
	taskChange = transition(taskChange, "REVIEW", "change-review")
	reviewed := taskChange.Fields
	reviewed.ReviewResult = "Recovery successful"
	reviewed.ReviewOutcome = "SUCCESS"
	taskChange, err = s.Save(ctx, operator, taskChange.ID, changes.Input{Version: taskChange.Version, Fields: reviewed}, "change-review-result")
	if err != nil || taskChange.ApprovalState != "APPROVED" {
		t.Fatal("review invalidates plan", taskChange, err)
	}
	taskChange = transition(taskChange, "CLOSED", "change-close")
	if taskChange.ActualStart == nil || taskChange.ActualEnd == nil {
		t.Fatal("missing actual instants", taskChange)
	}
	// Communication uses separate visibility rights, even for a full Change reader.
	note, err := s.AddMessage(ctx, operator, taskChange.ID, changes.CommunicationInput{Type: workrecords.MessageWorkNote, Body: "PRIVATE CHANGE EXECUTION"}, "change-private-note")
	if err != nil {
		t.Fatal(err)
	}
	publicReader := as(seed("public-change-reader", []string{"change.read"}))
	if page, err := s.Messages(ctx, publicReader, taskChange.ID, 0, 25); err != nil || len(page.Messages) != 0 {
		t.Fatal("private Change note", note, page, err)
	}
	// A service-window boundary accepts exactly contained intervals and rejects a gap.
	window, err := s.SaveRule(ctx, admin, 0, changes.CalendarRule{CIID: &ci.ID, Kind: "WINDOW", Title: "Maintenance", StartsAt: start, EndsAt: end, TimeZone: "Europe/Warsaw", Active: true}, "change-window")
	if err != nil {
		t.Fatal(err)
	}
	collisions, err := s.Collisions(ctx, operator, unscheduled.ID)
	if err != nil || !collisions.OutsideWindow {
		t.Fatal("window containment", collisions, err)
	}
	window.Active = false
	if _, err = s.SaveRule(ctx, admin, window.ID, window, "change-window-disable"); err != nil {
		t.Fatal(err)
	}
	foreignActor := admin
	foreignActor.TenantID = root.TenantID + 1
	if _, err = s.Get(ctx, foreignActor, r.ID); !errors.Is(err, changes.ErrNotFound) {
		t.Fatal("tenant isolation", err)
	}
	// CAS protects the complete proposal when two editors use the same version.
	var race sync.WaitGroup
	writes := make(chan error, 2)
	for i := 0; i < 2; i++ {
		race.Add(1)
		go func(i int) {
			defer race.Done()
			proposal := urgent.Fields
			proposal.Justification = fmt.Sprintf("Different justification %d", i)
			_, err := s.Save(ctx, operator, urgent.ID, changes.Input{Version: urgent.Version, Fields: proposal}, fmt.Sprintf("change-concurrent-edit-%d", i))
			writes <- err
		}(i)
	}
	race.Wait()
	close(writes)
	saved, conflicts := 0, 0
	for err := range writes {
		if err == nil {
			saved++
		} else if errors.Is(err, changes.ErrConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if saved != 1 || conflicts != 1 {
		t.Fatal("CAS", saved, conflicts)
	}
	urgent, _ = s.Get(ctx, operator, urgent.ID)

	metadataService := workspace.Service{DB: db}
	keys := []string{fmt.Sprintf("/changes/%d", taskChange.ID), fmt.Sprintf("/problems/%d", prb.ID), fmt.Sprintf("/cmdb/%d", ci.ID), fmt.Sprintf("/approvals/%d", *taskChange.ExecutionID)}
	metadata, err := metadataService.Metadata(ctx, operator, keys)
	if err != nil || len(metadata.Items) != 4 {
		t.Fatal("cross-domain metadata", metadata, err)
	}
	for _, kind := range []string{"changes", "cmdb"} {
		id := taskChange.ID
		if kind == "cmdb" {
			id = ci.ID
		}
		contextRecord, err := metadataService.Context(ctx, operator, kind, id)
		if err != nil {
			t.Fatal("cross-domain context", kind, contextRecord, err)
		}
	}
	searchResults, err := metadataService.Search(ctx, operator, task.Number)
	if err != nil || len(searchResults) == 0 || searchResults[0].Type != "change_task" || !searchResults[0].Exact || searchResults[0].URL != fmt.Sprintf("/changes/%d?tab=tasks&task_id=%d", taskChange.ID, task.ID) {
		t.Fatal("task reference", searchResults, err)
	}
	period := workspace.ReportPeriod{From: taskChange.ActualEnd.Add(-time.Hour), To: taskChange.ActualEnd.Add(time.Hour)}
	reports, err := metadataService.Reports(ctx, admin, period)
	if err != nil {
		t.Fatal("Change reports", reports, err)
	}
	foundClosed := false
	for _, report := range reports.Items {
		if report.Key == "closed_changes" {
			foundClosed = true
			if report.Total != 1 {
				t.Fatal("actual closed count", report)
			}
		}
	}
	if !foundClosed {
		t.Fatal("missing closed report")
	}
	sourceFilter := workspace.ReportSourceFilter{Report: "closed_changes", Domain: "change", Period: period, Limit: 25}
	sources, err := metadataService.ReportSources(ctx, admin, sourceFilter)
	if err != nil || sources.Total != 1 || len(sources.Items) != 1 || sources.Items[0].ID != taskChange.ID {
		t.Fatal("actual closed sources", sources, err)
	}
	sourceFilter.Period = workspace.ReportPeriod{From: taskChange.ActualEnd.Add(-time.Hour), To: *taskChange.ActualEnd}
	sources, err = metadataService.ReportSources(ctx, admin, sourceFilter)
	if err != nil || sources.Total != 0 {
		t.Fatal("closed end must be exclusive", sources, err)
	}
	sourceFilter.Period = workspace.ReportPeriod{From: *taskChange.ActualEnd, To: taskChange.ActualEnd.Add(time.Hour)}
	sources, err = metadataService.ReportSources(ctx, admin, sourceFilter)
	if err != nil || sources.Total != 1 {
		t.Fatal("closed start must be inclusive", sources, err)
	}
	if metadata, err = metadataService.Metadata(ctx, outsider, keys); err != nil || len(metadata.Items) != 0 {
		t.Fatal("relationship metadata granted access", metadata, err)
	}
	// Revocation affects stale principals, not merely the frontend's permissions.
	exec("UPDATE tenant_memberships SET active=FALSE WHERE tenant_id=? AND user_id=?", admin.TenantID, operator.UserID)
	if _, err = s.Save(ctx, operator, urgent.ID, changes.Input{Version: urgent.Version, Fields: urgent.Fields}, "change-revoked-write"); !errors.Is(err, workrecords.ErrForbidden) {
		t.Fatal("revoked write", err)
	}
}
