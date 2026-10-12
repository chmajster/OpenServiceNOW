//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chmajster/OpenServiceNOW/internal/activity"
	"github.com/chmajster/OpenServiceNOW/internal/audit"
	"github.com/chmajster/OpenServiceNOW/internal/events"
	"github.com/chmajster/OpenServiceNOW/internal/identity"
	"github.com/chmajster/OpenServiceNOW/internal/incidents"
	"github.com/chmajster/OpenServiceNOW/internal/jobs"
	"github.com/chmajster/OpenServiceNOW/internal/notifications"
	"github.com/chmajster/OpenServiceNOW/internal/platform/config"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/sla"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
	"github.com/go-chi/chi/v5"
)

func TestDurableSLA(t *testing.T) {
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
	audits := audit.NewMySQLRepository()
	identities := identity.NewMySQLRepository(db)
	suffix := fmt.Sprint(time.Now().UnixNano())
	root, err := identity.NewBootstrapper(identities, tx, audits).Run(ctx, identity.BootstrapInput{TenantSlug: "sla-" + suffix, TenantName: "SLA", AdminEmail: "sla-" + suffix + "@example.test", AdminName: "Root", AdminPassword: "sla-test-administrator"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM audit_events WHERE tenant_id=?`, root.TenantID)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM tenants WHERE id=?`, root.TenantID)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM users WHERE id=?`, root.UserID)
	}()
	actor := incidents.Actor{TenantID: root.TenantID, UserID: root.UserID, Permissions: map[string]bool{"admin.sla.manage": true, "incident.read": true, "incident.create": true, "incident.update": true, "incident.assign": true, "incident.resolve": true, "incident.communicate": true}}
	// No sleep: both policy activation and worker time come from this test clock.
	base := time.Now().UTC().Truncate(time.Second).Add(-time.Hour)
	clock := base
	service := sla.NewServiceWithClock(db, func() time.Time { return clock })
	calendar := sla.Calendar{Name: "Całą dobę", Timezone: "Europe/Warsaw", Week: []sla.Day{}}
	for d := 0; d < 7; d++ {
		calendar.Week = append(calendar.Week, sla.Day{Weekday: d, Windows: []sla.Window{{Start: "00:00", End: "24:00"}}})
	}
	raw, _ := json.Marshal(calendar)
	cal, err := service.SaveDefinition(ctx, actor, "calendar", 0, 0, "sla-calendar-creation", raw)
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := service.SaveDefinition(ctx, actor, "calendar", 0, 0, "sla-calendar-creation", raw)
	if err != nil || repeated.ID != cal.ID {
		t.Fatal("creation retry duplicated calendar", err)
	}
	calendar.Name = "Inna"
	different, _ := json.Marshal(calendar)
	if _, err = service.SaveDefinition(ctx, actor, "calendar", 0, 0, "sla-calendar-creation", different); !errors.Is(err, sla.ErrConflict) {
		t.Fatal("changed idempotency body accepted", err)
	}
	policy := sla.Policy{Name: "Standard", Enabled: true, Order: 10, CalendarID: cal.ID, CalendarVersion: 1, ResponseSeconds: 600, ResolutionSeconds: 1200, WarningPercent: 50, PauseReasons: []string{"Klient"}}
	raw, _ = json.Marshal(policy)
	def, err := service.SaveDefinition(ctx, actor, "policy", 0, 0, "sla-policy-creation", raw)
	if err != nil {
		t.Fatal(err)
	}
	forbidden := actor
	forbidden.Permissions = map[string]bool{}
	if _, err = service.SaveDefinition(ctx, forbidden, "policy", def.ID, 1, "", raw); !errors.Is(err, incidents.ErrForbidden) {
		t.Fatal("RBAC bypass", err)
	}
	other := actor
	other.TenantID++
	if _, err = service.Definition(ctx, db, other.TenantID, def.ID, 0); !errors.Is(err, sla.ErrNotFound) {
		t.Fatal("definition tenant leak", err)
	}
	outbox := events.NewRepository(db)
	work := workrecords.NewService(workrecords.NewRepository(db), tx, outbox)
	incidentService := incidents.NewService(incidents.NewRepository(db), tx, workrecords.NewAllocator(tx), work, audits, outbox, activity.NewRepository(db))
	record, _, err := incidentService.Create(ctx, actor, incidents.Fields{Title: "SLA próba", Description: "Trwała próba", AffectedUserID: root.UserID, Category: "network", Subcategory: "vpn", Impact: 1, Urgency: 1}, "sla-incident-creation")
	if err != nil {
		t.Fatal(err)
	}
	var create events.Event
	create.TenantID = root.TenantID
	create.AggregateType = "incident"
	create.AggregateID = fmt.Sprint(record.ID)
	create.Type = "incident.created"
	create.Version = 1
	if err = db.QueryRowContext(ctx, `SELECT id,payload FROM outbox_events WHERE tenant_id=? AND aggregate_id=? AND event_type='incident.created'`, root.TenantID, create.AggregateID).Scan(&create.ID, &create.Payload); err != nil {
		t.Fatal(err)
	}
	change := incidents.ChangeEvent{}
	_ = json.Unmarshal(create.Payload, &change)
	// Fixture business times are explicit, independent from wall-clock test speed.
	change.OccurredAt = base.Add(time.Minute)
	change.After.CreatedAt = change.OccurredAt
	change.After.UpdatedAt = change.OccurredAt
	create.Payload, _ = json.Marshal(change)
	if _, err = db.ExecContext(ctx, `UPDATE outbox_events SET payload=? WHERE id=?`, create.Payload, create.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `UPDATE incidents SET created_at=?,updated_at=? WHERE tenant_id=? AND id=?`, change.OccurredAt, change.OccurredAt, root.TenantID, record.ID); err != nil {
		t.Fatal(err)
	}
	// Insert v3 before v2 to prove replay uses version/business time, not IDs or
	// handler arrival order. Both have committed before the delayed worker runs.
	appendState := func(version uint64, at time.Time, state, reason string) events.Event {
		t.Helper()
		next := change
		next.IncidentVersion = version
		next.After.Version = version
		next.OccurredAt = at
		next.After.UpdatedAt = at
		next.After.State = state
		next.After.HoldReason = reason
		raw, _ := json.Marshal(next)
		event := events.Event{TenantID: root.TenantID, Type: "incident.state.changed", Version: 1, AggregateType: "incident", AggregateID: create.AggregateID, Payload: raw}
		event.ID, _, err = outbox.Append(ctx, db, events.Input{TenantID: event.TenantID, Type: event.Type, AggregateType: event.AggregateType, AggregateID: event.AggregateID, Payload: raw, IdempotencyKey: fmt.Sprintf("sla-fixture:%d:%d", record.ID, version)})
		if err != nil {
			t.Fatal(err)
		}
		return event
	}
	later := appendState(3, base.Add(21*time.Minute), "IN_PROGRESS", "")
	earlier := appendState(2, base.Add(6*time.Minute), "ON_HOLD", "Klient")
	if _, err = db.ExecContext(ctx, `UPDATE incidents SET version=3,state='IN_PROGRESS',updated_at=? WHERE tenant_id=? AND id=?`, base.Add(21*time.Minute), root.TenantID, record.ID); err != nil {
		t.Fatal(err)
	}
	_ = earlier
	clock = base.Add(40 * time.Minute)
	if err = service.HandleEvent(ctx, later); err != nil {
		t.Fatal(err)
	}
	if err = service.HandleEvent(ctx, create); err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	worker, err := jobs.NewWorker(jobs.NewRepository(db), logger, "sla-proof", 1, 50*time.Millisecond, 10*time.Second, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err = worker.Register(sla.JobType, service.JobHandler); err != nil {
		t.Fatal(err)
	}
	// Directly reconcile then execute the queued retry through the real worker.
	if err = service.Reconcile(ctx, root.TenantID, record.ID); err != nil {
		t.Fatal(err)
	}
	for n := 0; n < 8; n++ {
		processed, e := worker.RunOnce(ctx)
		if e != nil {
			t.Fatal(e)
		}
		if !processed {
			break
		}
	}
	page, err := service.Instances(ctx, actor, []uint64{record.ID})
	if err != nil || len(page.Items) != 2 || page.Pending {
		t.Fatal("missing durable SLA", page, err)
	}
	for _, i := range page.Items {
		if i.AccruedSeconds != 24*60 || i.BreachedAt == nil || !i.BreachedAt.Equal(base.Add(time.Minute+time.Duration(i.TargetSeconds+15*60)*time.Second)) {
			t.Fatalf("incorrect delayed pause accounting %+v", i)
		}
	}
	// New service instance proves MySQL is the source of checkpoint/policy state.
	restarted := sla.NewServiceWithClock(db, func() time.Time { return clock })
	if err = restarted.Reconcile(ctx, root.TenantID, record.ID); err != nil {
		t.Fatal(err)
	}
	// Scheduler selects the durable due row and the real worker completes its
	// known-ID job, independently from event-handler delivery or browser reads.
	clock = clock.Add(time.Minute)
	scheduled, err := service.Schedule(ctx, clock)
	if err != nil || scheduled < 1 {
		t.Fatal("scheduler did not enqueue due SLA", scheduled, err)
	}
	var scheduledID uint64
	if err = db.QueryRowContext(ctx, `SELECT id FROM jobs WHERE tenant_id=? AND job_type='sla.reconcile' AND idempotency_key LIKE 'sla:tick:%' ORDER BY id DESC LIMIT 1`, root.TenantID).Scan(&scheduledID); err != nil {
		t.Fatal(err)
	}
	// Controlled business time can be ahead of the database clock; making this
	// fixture job available does not replace the real claim/attempt/handler path.
	if _, err = db.ExecContext(ctx, `UPDATE jobs SET available_at=UTC_TIMESTAMP(6) WHERE id=?`, scheduledID); err != nil {
		t.Fatal(err)
	}
	for n := 0; n < 8; n++ {
		processed, e := worker.RunOnce(ctx)
		if e != nil {
			t.Fatal(e)
		}
		if !processed {
			break
		}
	}
	var completed int
	if err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM job_attempts a JOIN jobs j ON j.id=a.job_id WHERE j.id=? AND j.status='COMPLETED' AND a.status='COMPLETED'`, scheduledID).Scan(&completed); err != nil || completed != 1 {
		t.Fatal("scheduler PID is not processing proof", completed, err)
	}
	var thresholdCount int
	if err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM outbox_events WHERE tenant_id=? AND aggregate_id=? AND event_type IN ('sla.warning','sla.breached')`, root.TenantID, create.AggregateID).Scan(&thresholdCount); err != nil || thresholdCount != 4 {
		t.Fatal("duplicated escalation", thresholdCount, err)
	}
	policy.ResolutionSeconds = 3600
	raw, _ = json.Marshal(policy)
	clock = clock.Add(time.Second)
	updated, err := service.SaveDefinition(ctx, actor, "policy", def.ID, 1, "", raw)
	if err != nil || updated.Version != 2 {
		t.Fatal(err)
	}
	if _, err = service.SaveDefinition(ctx, actor, "policy", def.ID, 1, "", raw); !errors.Is(err, sla.ErrConflict) {
		t.Fatal("stale version accepted", err)
	}
	page, err = restarted.Instances(ctx, actor, []uint64{record.ID})
	if err != nil || page.Items[0].PolicyVersion != 1 {
		t.Fatal("history silently changed", err)
	}
	noRead := actor
	noRead.UserID++
	noRead.Permissions = map[string]bool{"incident.read_own": true}
	if _, err = service.Instances(ctx, noRead, []uint64{record.ID}); !errors.Is(err, sla.ErrNotFound) {
		t.Fatal("object access leak", err)
	}
	other.Permissions = actor.Permissions
	if _, err = service.Instances(ctx, other, []uint64{record.ID}); !errors.Is(err, sla.ErrNotFound) {
		t.Fatal("cross tenant instance leak", err)
	}
	projection := activity.NewProjector(tx, activity.NewRepository(db))
	d, err := events.NewDispatcher(outbox, logger, "sla-deliveries", 1, 50*time.Millisecond, 10*time.Second, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err = incidents.RegisterEventHandlers(d, incidents.NewRepository(db), projection); err != nil {
		t.Fatal(err)
	}
	if err = service.Register(d, projection); err != nil {
		t.Fatal(err)
	}
	notify := notifications.NewService(db, nil, false)
	if err = notify.Register(d); err != nil {
		t.Fatal(err)
	}
	for n := 0; n < 70; n++ {
		processed, e := d.RunOnce(ctx)
		if e != nil {
			t.Fatal(e)
		}
		if !processed {
			break
		}
	}
	var alerts int
	if err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM notifications n JOIN outbox_events e ON e.id=n.source_event_id WHERE n.tenant_id=? AND e.event_type IN ('sla.warning','sla.breached')`, root.TenantID).Scan(&alerts); err != nil || alerts != 4 {
		t.Fatal("SLA notifications failed", alerts, err)
	}
	auth, err := identity.NewService(identities, tx, audits, cfg.Session, cfg.Auth)
	if err != nil {
		t.Fatal(err)
	}
	login, err := auth.Login(ctx, identity.LoginInput{Tenant: "sla-" + suffix, Email: "sla-" + suffix + "@example.test", Password: "sla-test-administrator"}, identity.RequestMeta{})
	if err != nil {
		t.Fatal(err)
	}
	router := chi.NewRouter()
	sla.RegisterRoutes(router, sla.NewHandler(service, auth), identity.NewMiddleware(auth, cfg.Session))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sla/incidents?ids="+create.AggregateID, nil)
	req.AddCookie(&http.Cookie{Name: cfg.Session.CookieName, Value: login.SessionToken})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"policy_version":"1"`) {
		t.Fatalf("SLA API %d %s", w.Code, w.Body.String())
	}
	var jobsBefore, jobsAfter int
	if err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM jobs WHERE tenant_id=?`, root.TenantID).Scan(&jobsBefore); err != nil {
		t.Fatal(err)
	}
	router.ServeHTTP(httptest.NewRecorder(), req.Clone(ctx))
	if err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM jobs WHERE tenant_id=?`, root.TenantID).Scan(&jobsAfter); err != nil || jobsAfter != jobsBefore {
		t.Fatal("SLA GET scheduled work", jobsBefore, jobsAfter, err)
	}
	// A backlog larger than one batch needs durable continuation, and timer
	// advancement must wait until every committed transition has been applied.
	for v := uint64(4); v <= 208; v++ {
		appendState(v, base.Add(42*time.Minute+time.Duration(v)*time.Second), "IN_PROGRESS", "")
	}
	if _, err = db.ExecContext(ctx, `UPDATE incidents SET version=208 WHERE tenant_id=? AND id=?`, root.TenantID, record.ID); err != nil {
		t.Fatal(err)
	}
	clock = base.Add(100 * time.Minute)
	if err = service.Reconcile(ctx, root.TenantID, record.ID); err != nil {
		t.Fatal(err)
	}
	partial, err := service.Instances(ctx, actor, []uint64{record.ID})
	if err != nil || !partial.Pending || partial.Items[0].IncidentVersion >= 208 {
		t.Fatal("unbounded or prematurely advanced source batch", partial, err)
	}
	if err = restarted.Reconcile(ctx, root.TenantID, record.ID); err != nil {
		t.Fatal(err)
	}
	final, err := service.Instances(ctx, actor, []uint64{record.ID})
	if err != nil || final.Pending || final.Items[0].IncidentVersion != 208 {
		t.Fatal("continuation did not recover checkpoint", final, err)
	}
	if err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM outbox_events WHERE tenant_id=? AND aggregate_id=? AND event_type IN ('sla.warning','sla.breached')`, root.TenantID, create.AggregateID).Scan(&thresholdCount); err != nil || thresholdCount != 4 {
		t.Fatal("batch restart duplicated escalation", thresholdCount, err)
	}
	// Disabled organizations must not repeatedly occupy the bounded due queue.
	if _, err = db.ExecContext(ctx, `UPDATE tenants SET status='DISABLED' WHERE id=?`, root.TenantID); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM jobs WHERE tenant_id=?`, root.TenantID).Scan(&jobsBefore); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Schedule(ctx, clock.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM jobs WHERE tenant_id=?`, root.TenantID).Scan(&jobsAfter); err != nil || jobsAfter != jobsBefore {
		t.Fatal("disabled organization occupied scheduler queue", jobsBefore, jobsAfter, err)
	}
}
