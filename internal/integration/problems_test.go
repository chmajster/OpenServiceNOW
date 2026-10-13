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
	"github.com/chmajster/OpenServiceNOW/internal/cmdb"
	"github.com/chmajster/OpenServiceNOW/internal/events"
	"github.com/chmajster/OpenServiceNOW/internal/identity"
	"github.com/chmajster/OpenServiceNOW/internal/incidents"
	"github.com/chmajster/OpenServiceNOW/internal/platform/config"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/problems"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
	"github.com/chmajster/OpenServiceNOW/internal/workspace"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestProblemProcessPrivacyRelationsAndRetries(t *testing.T) {
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
	as := func(tenant, user uint64) problems.Actor {
		keys, err := identities.ListPermissions(ctx, tenant, user)
		if err != nil {
			t.Fatal(err)
		}
		a := problems.Actor{Actor: workrecords.Actor{TenantID: tenant, UserID: user, RequestID: "cmdb-proof"}, Permissions: map[string]bool{}}
		for _, p := range keys {
			a.Permissions[p] = true
		}
		return a
	}
	seed := func(name string, keys []string) problems.Actor {
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
	reader := seed("own-reader", []string{"problem.read_own", "incident.read_own"})
	operator := seed("update-agent", []string{"problem.read_own", "problem.manage", "problem.assign", "problem.create", "problem.transition", "problem.known_error", "problem.notes", "problem.tasks", "cmdb.read", "cmdb.link", "incident.read", "incident.update"})
	storage, err := attachments.NewLocalStorage(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	outbox := events.NewRepository(db)
	files, err := attachments.NewEventingService(attachments.NewRepository(db), storage, attachments.MaxFileBytes, tx, outbox)
	if err != nil {
		t.Fatal(err)
	}
	is := incidents.NewService(incidents.NewRepository(db), tx, workrecords.NewAllocator(tx), workrecords.NewService(workrecords.NewRepository(db), tx, outbox), audits, outbox, activity.NewRepository(db))
	s := problems.NewService(db, files, incidents.NewProblemDomain(is))
	ciService := cmdb.NewService(db, files, map[string]cmdb.RecordDomain{"problems": problems.NewCMDBDomain(s)})
	group, err := db.ExecContext(ctx, "INSERT INTO `groups`(tenant_id,name) VALUES(?,?)", admin.TenantID, "Problem analysis")
	if err != nil {
		t.Fatal(err)
	}
	n, _ := group.LastInsertId()
	gid := uint64(n)
	exec("INSERT INTO group_memberships(tenant_id,group_id,user_id) VALUES(?,?,?)", admin.TenantID, gid, operator.UserID)
	incActor := incidents.Actor{TenantID: admin.TenantID, UserID: admin.UserID, Permissions: admin.Permissions}
	incident, _, err := is.Create(ctx, incActor, incidents.Fields{Title: "Repeated database outage", Description: "Public symptoms", AffectedUserID: reader.UserID, Category: "software", Subcategory: "application", Impact: 1, Urgency: 2}, "problem-source-incident")
	if err != nil {
		t.Fatal(err)
	}
	preview, err := s.SourceIncident(ctx, operator, incident.ID)
	if err != nil || preview.Description != "Public symptoms" || !preview.CanLink {
		t.Fatal("preview", preview, err)
	}
	in := problems.Input{Fields: problems.Fields{Title: preview.Title, Description: preview.Description, Impact: preview.Impact, Symptoms: "Timeout", GroupID: &gid, AssignedTo: &operator.UserID}, SourceIncidentID: incident.ID, SourceVersion: incident.Version}
	r, err := s.Save(ctx, admin, 0, in, "problem-from-incident")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(r.Number, "PRB") || r.Version != 1 {
		t.Fatal(r)
	}
	again, err := s.Save(ctx, admin, 0, in, "problem-from-incident")
	if err != nil || again.ID != r.ID {
		t.Fatal("retry created duplicate", again, err)
	}
	in.Title = "Different"
	if _, err = s.Save(ctx, admin, 0, in, "problem-from-incident"); !errors.Is(err, workrecords.ErrOperationConflict) {
		t.Fatal("changed retry accepted", err)
	}
	if _, err = s.Get(ctx, other, r.ID); !errors.Is(err, problems.ErrNotFound) {
		t.Fatal("foreign record", err)
	}
	if _, err = s.Get(ctx, reader, r.ID); !errors.Is(err, problems.ErrNotFound) {
		t.Fatal("incident link granted problem access", err)
	}
	r, err = s.Get(ctx, operator, r.ID)
	if err != nil {
		t.Fatal("group/assignment read", err)
	}
	note, err := s.AddMessage(ctx, operator, r.ID, problems.CommunicationInput{Type: workrecords.MessageWorkNote, Body: "Private problem analysis"}, "problem-private-note")
	if err != nil {
		t.Fatal(err)
	}
	if replay, err := s.AddMessage(ctx, operator, r.ID, problems.CommunicationInput{Type: workrecords.MessageWorkNote, Body: note.Body}, "problem-private-note"); err != nil || replay.ID != note.ID {
		t.Fatal("message retry", replay, err)
	}
	f, err := s.UploadFile(ctx, operator, r.ID, "analysis.txt", "INTERNAL", &note.ID, bytes.NewBufferString("private attachment"), "problem-note-file")
	if err != nil {
		t.Fatal(err)
	}
	publicReader := seed("public-reader", []string{"problem.read"})
	if page, err := s.Messages(ctx, publicReader, r.ID, 0, 25); err != nil || len(page.Messages) != 0 {
		t.Fatal("private message leaked", page, err)
	}
	if page, err := s.FileList(ctx, publicReader, r.ID, 0, 25); err != nil || len(page.Files) != 0 {
		t.Fatal("private file leaked", page, err)
	}
	if _, reader, err := s.OpenFile(ctx, publicReader, r.ID, f.ID); !errors.Is(err, problems.ErrNotFound) {
		if reader != nil {
			reader.Close()
		}
		t.Fatal("direct private file", err)
	}
	_, read, err := s.OpenFile(ctx, operator, r.ID, f.ID)
	if err != nil {
		t.Fatal(err)
	}
	content, err := io.ReadAll(read)
	read.Close()
	if err != nil || string(content) != "private attachment" {
		t.Fatal(string(content), err)
	}
	var count int
	if err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM problem_incident_links WHERE tenant_id=? AND problem_id=?", admin.TenantID, r.ID).Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	ci, err := ciService.Save(ctx, admin, 0, cmdb.Record{Fields: cmdb.Fields{Identifier: "problem-database", Name: "Database", Class: "DATABASE", Status: "ACTIVE", OwnerID: admin.UserID, Environment: "PROD", Attributes: map[string]json.RawMessage{"engine": json.RawMessage(`"MYSQL"`)}}}, "problem-ci-create")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ciService.ChangeRecordLink(ctx, operator, "problems", r.ID, ci.ID, cmdb.RecordLinkInput{Version: r.Version, CIVersion: ci.Version}, false, "problem-ci-link"); err != nil {
		t.Fatal(err)
	}
	r, err = s.Get(ctx, operator, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if refs, err := ciService.Records(ctx, publicReader, "problems", "", ci.ID, 0, 25); err == nil {
		t.Fatal("CI access must be independent", refs)
	}
	fresh := func() problems.Record {
		t.Helper()
		v, err := s.Get(ctx, operator, r.ID)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	transition := func(state, reason string) {
		t.Helper()
		v := fresh()
		next, err := s.Transition(ctx, operator, r.ID, problems.Change{Version: v.Version, State: state, Reason: reason}, fmt.Sprintf("problem-transition-%s-%d", state, v.Version))
		if err != nil {
			t.Fatal(state, err)
		}
		r = next
	}
	transition("ASSESS", "")
	transition("ROOT_CAUSE", "")
	if _, err = s.Transition(ctx, operator, r.ID, problems.Change{Version: r.Version, State: "FIX_IN_PROGRESS"}, "problem-missing-analysis"); !errors.Is(err, problems.ErrTransition) {
		t.Fatal("missing analysis accepted", err)
	}
	fields := r.Fields
	fields.Analysis = "Connection exhaustion reproduced"
	fields.RootCause = "Unbounded pool"
	fields.Workaround = "Limit workload"
	fields.SolutionPlan = "Bound pool and test"
	fields.KnownError = true
	r, err = s.Save(ctx, operator, r.ID, problems.Input{Fields: fields, Version: r.Version}, "problem-analysis-save")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Save(ctx, operator, r.ID, problems.Input{Fields: fields, Version: r.Version - 1}, "problem-stale-save"); !errors.Is(err, problems.ErrConflict) {
		t.Fatal("stale save", err)
	}
	transition("FIX_IN_PROGRESS", "")
	taskIn := problems.TaskInput{ProblemVersion: r.Version, Title: "Verify pool limits", Instructions: "Load test", Required: true, State: "OPEN", GroupID: &gid, AssignedTo: &operator.UserID}
	task, err := s.SaveTask(ctx, operator, r.ID, 0, taskIn, "problem-analytical-task")
	if err != nil {
		t.Fatal(err)
	}
	if repeated, err := s.SaveTask(ctx, operator, r.ID, 0, taskIn, "problem-analytical-task"); err != nil || repeated.ID != task.ID {
		t.Fatal("task retry", repeated, err)
	}
	r = fresh()
	fields = r.Fields
	fields.Verification = "Pool remains bounded under load"
	r, err = s.Save(ctx, operator, r.ID, problems.Input{Fields: fields, Version: r.Version}, "problem-verification")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Transition(ctx, operator, r.ID, problems.Change{Version: r.Version, State: "RESOLVED"}, "problem-unfinished-task"); !errors.Is(err, problems.ErrTransition) {
		t.Fatal("unfinished required task accepted", err)
	}
	taskIn.Version = task.Version
	taskIn.ProblemVersion = r.Version
	taskIn.State = "IN_PROGRESS"
	task, err = s.SaveTask(ctx, operator, r.ID, task.ID, taskIn, "problem-task-start")
	if err != nil {
		t.Fatal(err)
	}
	r = fresh()
	taskIn.Version = task.Version
	taskIn.ProblemVersion = r.Version
	taskIn.State = "COMPLETED"
	taskIn.Result = "100 concurrent sessions passed"
	if _, err = s.SaveTask(ctx, operator, r.ID, task.ID, taskIn, "problem-task-complete"); err != nil {
		t.Fatal(err)
	}
	transition("RESOLVED", "")
	transition("CLOSED", "")
	if state, err := is.Get(ctx, incActor, incident.ID); err != nil || state.State != "NEW" {
		t.Fatal("problem automatically closed incident", state, err)
	}
	transition("ASSESS", "New occurrence")
	// Identical record version: exactly one concurrent update may commit.
	r = fresh()
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			fields := r.Fields
			fields.Title = fmt.Sprintf("Concurrent %d", i)
			_, err := s.Save(ctx, operator, r.ID, problems.Input{Fields: fields, Version: r.Version}, fmt.Sprintf("problem-concurrent-%d", i))
			results <- err
		}(i)
	}
	wg.Wait()
	close(results)
	success, conflict := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, problems.ErrConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatal(success, conflict)
	}

	metadataService := workspace.Service{DB: db}
	searchResults, err := metadataService.Search(ctx, operator, task.Number)
	if err != nil || len(searchResults) == 0 || searchResults[0].Type != "problem_task" || !searchResults[0].Exact || searchResults[0].URL != fmt.Sprintf("/problems/%d?tab=tasks&task_id=%d", r.ID, task.ID) {
		t.Fatal("analytical task reference", searchResults, err)
	}
	if _, err = metadataService.Context(ctx, operator, "problems", r.ID); err != nil {
		t.Fatal("Problem context", err)
	}
	period := workspace.ReportPeriod{From: time.Now().UTC().Add(-time.Hour), To: time.Now().UTC().Add(time.Hour)}
	reports, err := metadataService.Reports(ctx, operator, period)
	if err != nil {
		t.Fatal("Problem reports", err)
	}
	ranked := false
	for _, report := range reports.Items {
		if report.Key == "problem_incidents" {
			ranked = true
			if report.Total != 1 || len(report.Items) != 1 || report.Items[0].Count != 1 || report.Items[0].Record.ID != r.ID {
				t.Fatal("visible incidents ranking", report)
			}
		}
	}
	if !ranked {
		t.Fatal("ranking missing")
	}
	sources, err := metadataService.ReportSources(ctx, operator, workspace.ReportSourceFilter{Report: "problem_incidents", Domain: "incident", ProblemID: r.ID, Period: period, Limit: 25})
	if err != nil || sources.Total != 1 || sources.Items[0].ID != incident.ID {
		t.Fatal("ranking sources", sources, err)
	}
	hidden := publicReader
	hidden.Permissions["incident.read_own"] = true
	reports, err = metadataService.Reports(ctx, hidden, period)
	if err != nil {
		t.Fatal(err)
	}
	for _, report := range reports.Items {
		if report.Key == "problem_incidents" && report.Total != 0 {
			t.Fatal("inaccessible incident counted", report)
		}
	}
	if _, err = metadataService.Context(ctx, reader, "problems", r.ID); !errors.Is(err, workrecords.ErrNotFound) {
		t.Fatal("relation grants context", err)
	}
	// Stored permissions on the caller cannot override revoked server membership.
	exec("DELETE FROM group_memberships WHERE tenant_id=? AND group_id=? AND user_id=?", admin.TenantID, gid, operator.UserID)
	exec("UPDATE work_assignments SET assigned_to=NULL WHERE tenant_id=? AND object_type='problem' AND object_id=?", admin.TenantID, fmt.Sprint(r.ID))
	if _, err = s.Get(ctx, operator, r.ID); !errors.Is(err, problems.ErrNotFound) {
		t.Fatal("removed group retained access", err)
	}
	exec("UPDATE tenant_memberships SET active=FALSE WHERE tenant_id=? AND user_id=?", admin.TenantID, operator.UserID)
	if _, err = s.Save(ctx, operator, 0, problems.Input{Fields: problems.Fields{Title: "Revoked", Description: "No access", Impact: "LOW"}}, "problem-revoked-create"); !errors.Is(err, workrecords.ErrForbidden) {
		t.Fatal("revoked user wrote", err)
	}
}
