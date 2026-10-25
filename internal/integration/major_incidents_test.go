//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chmajster/OpenServiceNOW/internal/activity"
	"github.com/chmajster/OpenServiceNOW/internal/attachments"
	"github.com/chmajster/OpenServiceNOW/internal/audit"
	"github.com/chmajster/OpenServiceNOW/internal/events"
	"github.com/chmajster/OpenServiceNOW/internal/identity"
	"github.com/chmajster/OpenServiceNOW/internal/incidents"
	"github.com/chmajster/OpenServiceNOW/internal/jobs"
	"github.com/chmajster/OpenServiceNOW/internal/metadata"
	"github.com/chmajster/OpenServiceNOW/internal/notifications"
	"github.com/chmajster/OpenServiceNOW/internal/platform/config"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/rbac"
	"github.com/chmajster/OpenServiceNOW/internal/sla"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
	"github.com/chmajster/OpenServiceNOW/internal/workspace"
)

func TestModule08ProcessCoordinationIsolationAndRecovery(t *testing.T) {
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
	t.Cleanup(func() { _ = db.Close() })
	tx := database.NewTxManager(db)
	auditRepo := audit.NewMySQLRepository()
	identities := identity.NewMySQLRepository(db)
	bootstrap := identity.NewBootstrapper(identities, tx, auditRepo)
	suffix := fmt.Sprint(time.Now().UnixNano())
	createTenant := func(tag string) identity.BootstrapResult {
		t.Helper()
		value, e := bootstrap.Run(ctx, identity.BootstrapInput{TenantSlug: "module08-" + tag + suffix, TenantName: "Module 08 " + tag, AdminEmail: "module08-" + tag + suffix + "@example.test", AdminName: "Coordinator", AdminPassword: "module08-integration-password"})
		if e != nil {
			t.Fatal(e)
		}
		return value
	}
	first, second := createTenant("a"), createTenant("b")
	users := []uint64{first.UserID, second.UserID}
	t.Cleanup(func() {
		for _, tenant := range []uint64{first.TenantID, second.TenantID} {
			_, _ = db.ExecContext(context.Background(), `DELETE FROM metadata_config_heads WHERE tenant_id=?`, tenant)
			_, _ = db.ExecContext(context.Background(), `DELETE FROM audit_events WHERE tenant_id=?`, tenant)
			_, _ = db.ExecContext(context.Background(), `DELETE FROM tenants WHERE id=?`, tenant)
		}
		for _, id := range users {
			_, _ = db.ExecContext(context.Background(), `DELETE FROM users WHERE id=?`, id)
		}
	})
	actor := func(tenant, user uint64) incidents.Actor {
		t.Helper()
		keys, e := identities.ListPermissions(ctx, tenant, user)
		if e != nil {
			t.Fatal(e)
		}
		a := incidents.Actor{TenantID: tenant, UserID: user, Permissions: map[string]bool{}, RequestID: "module08-test"}
		for _, key := range keys {
			a.Permissions[key] = true
		}
		return a
	}
	admin, foreign := actor(first.TenantID, first.UserID), actor(second.TenantID, second.UserID)
	roles := rbac.NewService(rbac.NewMySQLRepository(db), tx, auditRepo, identity.NewAdminSafetyRepository())
	roleActor := rbac.Actor{TenantID: first.TenantID, UserID: first.UserID}
	seedUser := func(tag string, permissions []string) uint64 {
		t.Helper()
		result, e := db.ExecContext(ctx, `INSERT INTO users(email,display_name,password_hash) SELECT ?,?,password_hash FROM users WHERE id=?`, "module08-"+tag+suffix+"@example.test", tag, first.UserID)
		if e != nil {
			t.Fatal(e)
		}
		id, e := result.LastInsertId()
		if e != nil {
			t.Fatal(e)
		}
		users = append(users, uint64(id))
		if _, e = db.ExecContext(ctx, `INSERT INTO tenant_memberships(tenant_id,user_id) VALUES(?,?)`, first.TenantID, id); e != nil {
			t.Fatal(e)
		}
		role, e := roles.CreateRole(ctx, roleActor, rbac.CreateRoleInput{Name: tag, Permissions: permissions})
		if e != nil {
			t.Fatal(e)
		}
		if e = roles.AssignRoleToUser(ctx, roleActor, role, uint64(id)); e != nil {
			t.Fatal(e)
		}
		return uint64(id)
	}
	requesterID := seedUser("requester", []string{"incident.read_own", "incident.create"})
	nominatorID := seedUser("nominator", []string{"incident.read", "incident.create", "incident.major.nominate"})
	requester, nominator := actor(first.TenantID, requesterID), actor(first.TenantID, nominatorID)
	group, err := roles.CreateGroup(ctx, roleActor, rbac.CreateGroupInput{Name: "Service queue"})
	if err != nil {
		t.Fatal(err)
	}
	if err = roles.AddGroupMember(ctx, roleActor, group, first.UserID); err != nil {
		t.Fatal(err)
	}
	result, err := db.ExecContext(ctx, `INSERT INTO service_portfolio_items(tenant_id,service_key,name,item_type,description,lifecycle,criticality,environment,business_owner_id,technical_owner_id,support_group_id,created_by) VALUES(?,'CONTROLLED.SERVICE','Controlled local service','BUSINESS_SERVICE','Integration fixture','ACTIVE','HIGH','PROD',?,?,?,?)`, first.TenantID, first.UserID, first.UserID, group, first.UserID)
	if err != nil {
		t.Fatal(err)
	}
	serviceIDRaw, _ := result.LastInsertId()
	serviceID := uint64(serviceIDRaw)
	meta := metadata.NewService(db)
	ma := metadata.Actor{TenantID: first.TenantID, UserID: first.UserID, Permissions: admin.Permissions}
	head, err := meta.Head(ctx, ma)
	if err != nil {
		t.Fatal(err)
	}
	process := head.Draft.IncidentProcess
	process.DefaultGroupID = fmt.Sprint(group)
	process.AssignmentRules = []metadata.IncidentAssignmentRule{{Key: "service", Name: "Local service assignment", Active: true, GroupID: fmt.Sprint(group), ServiceID: fmt.Sprint(serviceID), Category: "network", Location: "local", Priority: 1}}
	process.AutoCloseHours = 1
	process.ReopenHours = 4
	process.RequesterCanReopen = true
	head, err = meta.SaveDraft(ctx, ma, head.Version, head.Draft)
	if err != nil {
		t.Fatal(err)
	}
	head, _, err = meta.Publish(ctx, ma, head.Version)
	if err != nil {
		t.Fatal(err)
	}
	outbox := events.NewRepository(db)
	work := workrecords.NewService(workrecords.NewRepository(db), tx, outbox)
	service := incidents.NewService(incidents.NewRepository(db), tx, workrecords.NewAllocator(tx), work, auditRepo, outbox, activity.NewRepository(db)).WithMetadata(meta)
	slaService := sla.NewService(db)
	calendar := sla.Calendar{Name: "Local 24 hour service", Timezone: "UTC"}
	for day := 0; day < 7; day++ {
		calendar.Week = append(calendar.Week, sla.Day{Weekday: day, Windows: []sla.Window{{Start: "00:00", End: "24:00"}}})
	}
	rawCalendar, _ := json.Marshal(calendar)
	calendarDefinition, e := slaService.SaveDefinition(ctx, admin, "calendar", 0, 0, "module08-calendar", rawCalendar)
	if e != nil {
		t.Fatal(e)
	}
	rawPolicy, _ := json.Marshal(sla.Policy{Name: "Local incident SLA", Enabled: true, Order: 10, CalendarID: calendarDefinition.ID, CalendarVersion: 1, ResponseSeconds: 600, ResolutionSeconds: 3600, WarningPercent: 50})
	if _, e = slaService.SaveDefinition(ctx, admin, "policy", 0, 0, "module08-sla-policy", rawPolicy); e != nil {
		t.Fatal(e)
	}
	fields := incidents.Fields{Title: "Network outage", Description: "Local service is unavailable", AffectedUserID: first.UserID, Category: "network", Subcategory: "connectivity", Impact: 1, Urgency: 1, ServiceID: &serviceID, Location: "local"}
	parent, _, err := service.Create(ctx, admin, fields, "module08-parent-create")
	if err != nil {
		t.Fatal(err)
	}
	if parent.Assignment.AssignmentGroupID == nil || *parent.Assignment.AssignmentGroupID != group || parent.AssignmentOrigin != "service" {
		t.Fatalf("assignment not applied: %+v", parent.Assignment)
	}
	parent, err = service.Assign(ctx, admin, parent.ID, incidents.AssignmentInput{Version: parent.Version, AssignedTo: &first.UserID, AssignmentGroupID: &group, Reason: "Coordinator takes ownership"})
	if err != nil {
		t.Fatal(err)
	}
	fields.Description = "Updated description"
	parent, err = service.Update(ctx, admin, parent.ID, incidents.UpdateInput{Fields: fields, Version: parent.Version})
	if err != nil {
		t.Fatal(err)
	}
	if parent.Assignment.AssignedTo == nil || *parent.Assignment.AssignedTo != first.UserID || parent.AssignmentOrigin != "manual" {
		t.Fatal("editing description stole manual assignment")
	}
	if _, err = service.ReapplyAssignment(ctx, admin, parent.ID, parent.Version); !errors.Is(err, incidents.ErrConflict) {
		t.Fatalf("rules stole active work: %v", err)
	}
	if _, err = service.Get(ctx, foreign, parent.ID); !errors.Is(err, incidents.ErrNotFound) {
		t.Fatal("foreign incident read accepted")
	}
	secondHead, err := meta.Head(ctx, metadata.Actor{TenantID: second.TenantID, UserID: second.UserID, Permissions: foreign.Permissions})
	if err != nil || secondHead.Published.IncidentProcess.DefaultGroupID != "" {
		t.Fatal("organization configurations leaked", err)
	}
	candidate, err := service.NominateMajor(ctx, nominator, parent.ID, incidents.MajorDecisionInput{Version: parent.Version, Reason: "Significant local service impact"}, "module08-nominate")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.DecideMajor(ctx, nominator, parent.ID, "promote", incidents.MajorDecisionInput{Version: candidate.Version, Reason: "No approval permission"}, "module08-invalid-approve"); !errors.Is(err, incidents.ErrForbidden) {
		t.Fatalf("nominator approved: %v", err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, e := service.DecideMajor(ctx, admin, parent.ID, "promote", incidents.MajorDecisionInput{Version: candidate.Version, Reason: "Confirmed broad impact"}, fmt.Sprintf("module08-promote-%d", i))
			results <- e
		}(i)
	}
	wg.Wait()
	close(results)
	success, conflict := 0, 0
	for e := range results {
		if e == nil {
			success++
		} else if errors.Is(e, incidents.ErrConflict) {
			conflict++
		} else {
			t.Fatal(e)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("promotion success=%d conflict=%d", success, conflict)
	}
	view, err := service.MajorView(ctx, admin, parent.ID)
	if err != nil || view.Case == nil {
		t.Fatal("major missing", err)
	}
	detected := time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)
	major, err := service.UpdateMajorCoordination(ctx, admin, parent.ID, incidents.MajorCoordinationInput{Version: view.Case.Version, CommanderUserID: &first.UserID, CommunicationsOwnerID: &first.UserID, ImpactSummary: "Two departments affected", ImpactConfidence: "ESTIMATED", CommunicationAudience: "ALL_LINKED", UpdateIntervalMinutes: 5, DetectedAt: &detected, TeamGroupIDs: incidents.IDList{group}, ServiceIDs: incidents.IDList{serviceID}}, "module08-coordinate")
	if err != nil {
		t.Fatal(err)
	}
	childFields := fields
	childFields.ServiceID = nil
	childFields.AffectedUserID = requesterID
	childFields.Title = "Linked requester incident"
	child, _, err := service.Create(ctx, admin, childFields, "module08-child-create")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.AddRelation(ctx, admin, child.ID, incidents.RelationInput{TargetID: parent.ID, Type: "PARENT"}); err != nil {
		t.Fatal(err)
	}
	sibling, _, err := service.Create(ctx, admin, childFields, "module08-sibling-create")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.AddRelation(ctx, admin, sibling.ID, incidents.RelationInput{TargetID: parent.ID, Type: "PARENT"}); err != nil {
		t.Fatalf("multiple children of one parent were rejected: %v", err)
	}
	if _, err = service.AddRelation(ctx, admin, child.ID, incidents.RelationInput{TargetID: sibling.ID, Type: "PARENT"}); err == nil {
		t.Fatal("second parent accepted")
	}
	if _, err = service.AddRelation(ctx, admin, parent.ID, incidents.RelationInput{TargetID: child.ID, Type: "PARENT"}); err == nil {
		t.Fatal("parent cycle accepted")
	}
	internal, err := service.PublishMajorUpdate(ctx, admin, parent.ID, incidents.MajorUpdateInput{Version: major.Version, Audience: "OPERATORS", Body: "PRIVATE technical findings"}, "module08-internal-update")
	if err != nil || internal.Delivery != "PENDING" {
		t.Fatal("false internal delivery state", err)
	}
	view, err = service.MajorView(ctx, admin, parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	public, err := service.PublishMajorUpdate(ctx, admin, parent.ID, incidents.MajorUpdateInput{Version: view.Case.Version, Audience: "REQUESTERS", Body: "Public restoration work is in progress"}, "module08-public-update")
	if err != nil {
		t.Fatal(err)
	}
	replay, err := service.PublishMajorUpdate(ctx, admin, parent.ID, incidents.MajorUpdateInput{Version: view.Case.Version, Audience: "REQUESTERS", Body: "Public restoration work is in progress"}, "module08-public-update")
	if err != nil || replay.ID != public.ID {
		t.Fatal("publication retry duplicated update", err)
	}
	ownView, err := service.MajorView(ctx, requester, child.ID)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(ownView)
	if !strings.Contains(string(encoded), "Public restoration") || strings.Contains(string(encoded), "PRIVATE") || ownView.Case != nil || len(ownView.InternalUpdates) > 0 || len(ownView.Timeline) > 0 {
		t.Fatalf("coordination privacy: %s", encoded)
	}
	if _, err = service.MajorView(ctx, foreign, parent.ID); !errors.Is(err, incidents.ErrNotFound) {
		t.Fatal("foreign major accepted")
	}
	view, _ = service.MajorView(ctx, admin, parent.ID)
	batch, err := service.MajorChildren(ctx, admin, parent.ID, incidents.MajorChildrenInput{Version: view.Case.Version, Action: "solution", Message: "Approved workaround", Items: []incidents.MajorChildTarget{{ID: child.ID, Version: child.Version}, {ID: sibling.ID, Version: 999}}}, "module08-bulk-results")
	if err != nil || len(batch) != 2 || batch[0].Status != 200 || batch[1].Status != 409 {
		t.Fatalf("partial outcomes: %+v %v", batch, err)
	}
	action, err := service.CreateMajorAction(ctx, admin, parent.ID, incidents.MajorActionCreateInput{Version: view.Case.Version, MajorActionInput: incidents.MajorActionInput{Description: "Restore network path", OwnerUserID: &first.UserID, DueAt: time.Now().Add(time.Hour)}}, "module08-action-create")
	if err != nil {
		t.Fatal(err)
	}
	storage, e := attachments.NewLocalStorage(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	files, e := attachments.NewEventingService(attachments.NewRepository(db), storage, attachments.MaxFileBytes, tx, outbox)
	if e != nil {
		t.Fatal(e)
	}
	ws := workspace.NewService(db, service, nil, files, identities).WithMetadata(meta)
	wa := workspace.Actor{TenantID: first.TenantID, UserID: first.UserID, Permissions: admin.Permissions}
	queue, err := ws.Query(ctx, wa, workspace.Query{Types: []string{workspace.ActionType}, View: "mine", Limit: 25})
	if err != nil || len(queue.Items) != 1 || queue.Items[0].ObjectID != fmt.Sprint(action.ID) {
		t.Fatalf("task not registered with workspace: %+v %v", queue, err)
	}
	if _, err = ws.Get(ctx, wa, workspace.IncidentType, fmt.Sprint(parent.ID)); err != nil {
		t.Fatal("workspace incident adapter", err)
	}
	taskItem, e := ws.Get(ctx, wa, workspace.ActionType, fmt.Sprint(action.ID))
	if e != nil || taskItem.ObjectID != fmt.Sprint(action.ID) {
		t.Fatal("workspace action adapter", e)
	}
	if _, err = service.GetMajorAction(ctx, requester, action.ID); !errors.Is(err, incidents.ErrNotFound) {
		t.Fatal("private action exposed", err)
	}
	noteInput := incidents.MajorActionNoteInput{Version: action.Version, Body: "Private workrecords coordination note"}
	note, e := service.AddMajorActionNote(ctx, admin, action.ID, noteInput, "module08-action-note")
	if e != nil {
		t.Fatal(e)
	}
	noteReplay, e := service.AddMajorActionNote(ctx, admin, action.ID, noteInput, "module08-action-note")
	if e != nil || noteReplay.ID != note.ID {
		t.Fatal("note replay", e)
	}
	notePage, e := service.MajorActionNotes(ctx, admin, action.ID, 0, 25)
	if e != nil || len(notePage.Messages) != 1 {
		t.Fatal("shared notes", e)
	}
	if _, err = service.MajorActionNotes(ctx, requester, action.ID, 0, 25); err == nil {
		t.Fatal("requester read coordination notes")
	}
	// A batch keeps its own outcomes when a subsequent operation changes the parent version.
	batchReplay, e := service.MajorChildren(ctx, admin, parent.ID, incidents.MajorChildrenInput{Version: view.Case.Version, Action: "solution", Message: "Approved workaround", Items: []incidents.MajorChildTarget{{ID: child.ID, Version: child.Version}, {ID: sibling.ID, Version: 999}}}, "module08-bulk-results")
	if e != nil || len(batchReplay) != 2 || batchReplay[0].Status != 200 || batchReplay[1].Status != 409 {
		t.Fatal("durable batch replay", batchReplay, e)
	}
	currentView, e := service.MajorView(ctx, admin, parent.ID)
	if e != nil {
		t.Fatal(e)
	}
	approved, e := service.MajorChildren(ctx, admin, parent.ID, incidents.MajorChildrenInput{Version: currentView.Case.Version, Action: "message", UpdateID: public.ID, Message: "must be ignored", Items: []incidents.MajorChildTarget{{ID: sibling.ID, Version: sibling.Version}}}, "module08-approved-message")
	if e != nil || len(approved) != 1 || approved[0].Status != 200 {
		t.Fatal("approved update apply", approved, e)
	}
	// Export a private task through the real durable worker and shared storage.
	ownerID := seedUser("task-owner", []string{"incident.read"})
	action, e = service.AssignMajorAction(ctx, admin, action.ID, action.Version, &ownerID)
	if e != nil {
		t.Fatal(e)
	}
	ownerActor := actor(first.TenantID, ownerID)
	ownerWorkspace := workspace.Actor{TenantID: first.TenantID, UserID: ownerID, Permissions: ownerActor.Permissions}
	export, e := ws.CreateExport(ctx, ownerWorkspace, workspace.Query{Types: []string{workspace.ActionType}, View: "mine", Limit: 25}, "module08-action-export")
	if e != nil {
		t.Fatal(e)
	}
	jobWorker, e := jobs.NewWorker(jobs.NewRepository(db), slog.New(slog.NewTextHandler(io.Discard, nil)), "module08-export-worker", 1, 50*time.Millisecond, 10*time.Second, 0)
	if e != nil {
		t.Fatal(e)
	}
	if e = jobWorker.Register(workspace.ExportJobType, ws.ExportJobHandler); e != nil {
		t.Fatal(e)
	}
	if e = jobWorker.Register(sla.JobType, slaService.JobHandler); e != nil {
		t.Fatal(e)
	}
	for attempt := 0; attempt < 25; attempt++ {
		_, e = jobWorker.RunOnce(ctx)
		if e != nil {
			t.Fatal(e)
		}
		state, e := ws.ExportStatus(ctx, ownerWorkspace, export.JobID)
		if e != nil {
			t.Fatal(e)
		}
		if state.Status == "COMPLETED" {
			break
		}
		if attempt == 24 {
			t.Fatal("export did not finish", state)
		}
	}
	attachmentID, e := ws.CanDownloadExport(ctx, ownerWorkspace, export.JobID)
	if e != nil {
		t.Fatal(e)
	}
	_, reader, e := files.Open(ctx, first.TenantID, attachmentID)
	if e != nil {
		t.Fatal(e)
	}
	contents, e := io.ReadAll(reader)
	reader.Close()
	if e != nil || !strings.Contains(string(contents), action.Number) || !strings.Contains(string(contents), "incident_action") {
		t.Fatal("export bytes missing real task", e)
	}
	action, e = service.AssignMajorAction(ctx, admin, action.ID, action.Version, &first.UserID)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = ws.CanDownloadExport(ctx, ownerWorkspace, export.JobID); !errors.Is(e, workspace.ErrNotFound) {
		t.Fatal("export retained access after private task reassignment", e)
	}
	if _, err = service.ChangeMajorAction(ctx, admin, action.ID, incidents.MajorActionChangeInput{Version: action.Version, Status: "DONE"}); err != nil {
		t.Fatal(err)
	}
	// A new service instance discovers the persisted deadline after worker restart.
	if _, err = db.ExecContext(ctx, `UPDATE incident_major_cases SET next_update_at=UTC_TIMESTAMP(6)-INTERVAL 1 MINUTE WHERE tenant_id=? AND incident_id=?`, first.TenantID, parent.ID); err != nil {
		t.Fatal(err)
	}
	restarted := incidents.NewService(incidents.NewRepository(db), tx, workrecords.NewAllocator(tx), work, auditRepo, outbox, activity.NewRepository(db)).WithMetadata(meta)
	if _, err = restarted.MaintenanceHandler(ctx, jobs.Job{Payload: json.RawMessage(`{"version":1}`)}); err != nil {
		t.Fatal(err)
	}
	if _, err = restarted.MaintenanceHandler(ctx, jobs.Job{Payload: json.RawMessage(`{"version":1}`)}); err != nil {
		t.Fatal(err)
	}
	var reminderCount int
	if err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM outbox_events WHERE tenant_id=? AND aggregate_id=? AND event_type='incident.major.reminder.due'`, first.TenantID, fmt.Sprint(parent.ID)).Scan(&reminderCount); err != nil || reminderCount != 1 {
		t.Fatal("reminder was lost or duplicated", reminderCount, err)
	}
	notificationService := notifications.NewService(db, nil, false)
	var event events.Event
	event.TenantID = first.TenantID
	event.Version = 1
	event.AggregateType = "incident"
	event.AggregateID = fmt.Sprint(parent.ID)
	if err = db.QueryRowContext(ctx, `SELECT id,event_type,payload,notification_eligible FROM outbox_events WHERE tenant_id=? AND event_type='incident.major.update.published' AND aggregate_id=?`, first.TenantID, event.AggregateID).Scan(&event.ID, &event.Type, &event.Payload, &event.NotificationEligible); err != nil {
		t.Fatal(err)
	}
	if err = notificationService.HandleEvent(ctx, event); err != nil {
		t.Fatal(err)
	}
	if err = notificationService.HandleEvent(ctx, event); err != nil {
		t.Fatal(err)
	}
	var delivered int
	if err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM notifications WHERE tenant_id=? AND user_id=? AND source_event_id=? AND in_app_enabled=TRUE`, first.TenantID, requesterID, event.ID).Scan(&delivered); err != nil || delivered != 1 {
		t.Fatal("in-app delivery duplicated or missing", delivered, err)
	}
	var internalEvent events.Event
	internalEvent.TenantID = first.TenantID
	internalEvent.Version = 1
	internalEvent.AggregateType = "incident"
	internalEvent.AggregateID = fmt.Sprint(parent.ID)
	if err = db.QueryRowContext(ctx, `SELECT id,event_type,payload,notification_eligible FROM outbox_events WHERE tenant_id=? AND aggregate_id=? AND event_type='incident.major.update.internal'`, first.TenantID, internalEvent.AggregateID).Scan(&internalEvent.ID, &internalEvent.Type, &internalEvent.Payload, &internalEvent.NotificationEligible); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err = notificationService.HandleEvent(ctx, internalEvent); err != nil {
			t.Fatal(err)
		}
	}
	var deliveryState string
	if err = db.QueryRowContext(ctx, `SELECT delivery_state FROM incident_major_updates WHERE tenant_id=? AND id=?`, first.TenantID, internal.ID).Scan(&deliveryState); err != nil || deliveryState != "IN_APP_DELIVERED" {
		t.Fatal("internal actual delivery", deliveryState, err)
	}
	if err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM notifications WHERE tenant_id=? AND source_event_id=? AND user_id=?`, first.TenantID, internalEvent.ID, requesterID).Scan(&delivered); err != nil || delivered != 0 {
		t.Fatal("private delivery leaked", err)
	}
	parent, err = service.Transition(ctx, admin, parent.ID, incidents.TransitionInput{Version: parent.Version, State: "IN_PROGRESS"})
	if err != nil {
		t.Fatal(err)
	}
	parent, err = service.Transition(ctx, admin, parent.ID, incidents.TransitionInput{Version: parent.Version, State: "RESOLVED", ResolutionCode: "FIXED", ResolutionDescription: "Network path restored"})
	if err != nil {
		t.Fatal(err)
	}
	view, _ = service.MajorView(ctx, admin, parent.ID)
	major, err = service.MarkMajorRestored(ctx, admin, parent.ID, incidents.MajorRestoreInput{Version: view.Case.Version, DetectedAt: detected, RestoredAt: time.Now().Add(-time.Second), Reason: "Service tested"}, "module08-confirm-restored")
	if err != nil {
		t.Fatal(err)
	}
	if major.ReviewStatus != "NOT_STARTED" {
		t.Fatal("restoration incorrectly completed review")
	}
	review, err := service.SaveMajorReview(ctx, admin, parent.ID, incidents.MajorReviewInput{DetectedAt: *major.DetectedAt, RestoredAt: *major.RestoredAt, ImpactSummary: "Two departments", KnownCauses: "Failed path", WhatWorked: "Manual failover", Complete: true, Actions: []incidents.MajorActionInput{{Description: "Replace path", OwnerUserID: &first.UserID, DueAt: time.Now().Add(24 * time.Hour)}}}, "module08-review-complete")
	if err != nil || review.CompletedAt == nil {
		t.Fatal(err)
	}
	parent, err = service.Transition(ctx, admin, parent.ID, incidents.TransitionInput{Version: parent.Version, State: "CLOSED"})
	if err != nil {
		t.Fatal(err)
	}
	actions, _, err := service.ListMyMajorActions(ctx, admin, 0, 100)
	if err != nil || len(actions) != 2 || actions[1].Status != "OPEN" {
		t.Fatal("review action disappeared at incident closure", err)
	}
	parent, err = service.Transition(ctx, admin, parent.ID, incidents.TransitionInput{Version: parent.Version, State: "IN_PROGRESS", Reason: "Service failed again"})
	if err != nil || parent.ResolvedAt != nil {
		t.Fatal("reopen did not reset resolution cycle", err)
	}
	if err = slaService.Reconcile(ctx, first.TenantID, parent.ID); err != nil {
		t.Fatal(err)
	}
	var cycles int
	var newestCycle uint64
	if err = db.QueryRowContext(ctx, `SELECT COUNT(*),MAX(cycle) FROM sla_instances WHERE tenant_id=? AND incident_id=? AND kind='resolution'`, first.TenantID, parent.ID).Scan(&cycles, &newestCycle); err != nil || cycles != 2 || newestCycle != 2 {
		t.Fatal("reopening failed to create the second real SLA cycle", cycles, newestCycle, err)
	}
	// Withdrawn values stay readable and editable only on their original records.
	head, err = meta.Head(ctx, ma)
	if err != nil {
		t.Fatal(err)
	}
	for i := range head.Draft.Dictionaries["incident.category"] {
		if head.Draft.Dictionaries["incident.category"][i].Value == "network" {
			head.Draft.Dictionaries["incident.category"][i].Retired = true
		}
	}
	head.Draft.IncidentProcess.AssignmentRules = nil
	head, err = meta.SaveDraft(ctx, ma, head.Version, head.Draft)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = meta.Publish(ctx, ma, head.Version); err != nil {
		t.Fatal(err)
	}
	if _, _, err = service.Create(ctx, admin, fields, "module08-retired-create"); !errors.Is(err, incidents.ErrInvalid) {
		t.Fatal("retired category used in new incident", err)
	}
	fields.Description = "Historical category is retained"
	if _, err = service.Update(ctx, admin, parent.ID, incidents.UpdateInput{Fields: fields, Version: parent.Version}); err != nil {
		t.Fatal("historical category broke edit", err)
	}
	// The worker acts with current permissions and the policy pinned at creation.
	autoFields := fields
	autoFields.Category = "other"
	autoFields.Subcategory = "other"
	autoFields.ServiceID = nil
	autoFields.Title = "Automatic closure policy"
	auto, _, e := service.Create(ctx, admin, autoFields, "module08-auto-create")
	if e != nil {
		t.Fatal(e)
	}
	auto, e = service.Assign(ctx, admin, auto.ID, incidents.AssignmentInput{Version: auto.Version, AssignedTo: &first.UserID, Reason: "Policy owner"})
	if e != nil {
		t.Fatal(e)
	}
	auto, e = service.Transition(ctx, admin, auto.ID, incidents.TransitionInput{Version: auto.Version, State: "IN_PROGRESS"})
	if e != nil {
		t.Fatal(e)
	}
	workspaceCommand := func(input workspace.ActionInput) {
		t.Helper()
		input.Items = []workspace.ActionTarget{{WorkReference: workspace.WorkReference{TenantID: first.TenantID, ObjectType: workspace.IncidentType, ObjectID: fmt.Sprint(auto.ID)}, Version: auto.Version}}
		page, e := ws.Action(ctx, wa, input)
		if e != nil || len(page.Results) != 1 || page.Results[0].Status != "SUCCEEDED" {
			t.Fatal("workspace incident command", page, e)
		}
		auto, e = service.Get(ctx, admin, auto.ID)
		if e != nil {
			t.Fatal(e)
		}
	}
	workspaceCommand(workspace.ActionInput{Action: "assign", AssignedTo: &first.UserID, Reason: "Explicit manual workspace assignment"})
	workspaceCommand(workspace.ActionInput{Action: "transition", State: "ON_HOLD", HoldReason: "AWAITING_USER", Reason: "Task-only free text must not replace a published incident reason"})
	if auto.HoldReason != "AWAITING_USER" {
		t.Fatal("workspace lost stable hold key")
	}
	workspaceCommand(workspace.ActionInput{Action: "transition", State: "IN_PROGRESS", Reason: "Sticky assignment reason is unrelated to starting"})
	workspaceCommand(workspace.ActionInput{Action: "transition", State: "RESOLVED", ResolutionCode: "FIXED", ResolutionDescription: "Verified restoration", Reason: "Sticky manual reason is unrelated to resolving"})

	if _, err = db.ExecContext(ctx, `UPDATE incidents SET resolved_at=UTC_TIMESTAMP(6)-INTERVAL 2 HOUR WHERE tenant_id=? AND id=?`, first.TenantID, auto.ID); err != nil {
		t.Fatal(err)
	}
	head, e = meta.Head(ctx, ma)
	if e != nil {
		t.Fatal(e)
	}
	head.Draft.IncidentProcess.AutoCloseHours = 0
	head, e = meta.SaveDraft(ctx, ma, head.Version, head.Draft)
	if e != nil {
		t.Fatal(e)
	}
	if _, _, e = meta.Publish(ctx, ma, head.Version); e != nil {
		t.Fatal(e)
	}
	if _, e = service.MaintenanceHandler(ctx, jobs.Job{Payload: json.RawMessage(`{"version":1}`)}); e != nil {
		t.Fatal(e)
	}
	auto, e = service.Get(ctx, admin, auto.ID)
	if e != nil || auto.State != "CLOSED" {
		t.Fatal("pinned automatic closure", auto.State, e)
	}
	var reopenedEvents int
	if err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM outbox_events WHERE tenant_id=? AND aggregate_id=? AND event_type='incident.reopened'`, first.TenantID, fmt.Sprint(parent.ID)).Scan(&reopenedEvents); err != nil || reopenedEvents != 1 {
		t.Fatal("reopen event cycle", reopenedEvents, err)
	}

}
