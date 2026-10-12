//go:build integration

package integration

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
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
	"github.com/chmajster/OpenServiceNOW/internal/rbac"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
	"github.com/go-chi/chi/v5"
)

func TestIndependentNotifications(t *testing.T) {
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
	password := "notification-test-password"
	bootstrap := identity.NewBootstrapper(identities, tx, audits)
	root, err := bootstrap.Run(ctx, identity.BootstrapInput{TenantSlug: "notify-" + suffix, TenantName: "Notifications", AdminEmail: "root-" + suffix + "@example.test", AdminName: "Root", AdminPassword: password})
	if err != nil {
		t.Fatal(err)
	}
	users := []uint64{root.UserID}
	defer func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM audit_events WHERE tenant_id=?`, root.TenantID)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM tenants WHERE id=?`, root.TenantID)
		for _, user := range users {
			_, _ = db.ExecContext(context.Background(), `DELETE FROM users WHERE id=?`, user)
		}
	}()
	auth, err := identity.NewService(identities, tx, audits, cfg.Session, cfg.Auth)
	if err != nil {
		t.Fatal(err)
	}
	roles := rbac.NewService(rbac.NewMySQLRepository(db), tx, audits, identity.NewAdminSafetyRepository())
	roleActor := rbac.Actor{TenantID: root.TenantID, UserID: root.UserID}
	seed := func(name string, keys []string) (uint64, incidents.Actor, identity.LoginResult, uint64) {
		t.Helper()
		result, err := db.ExecContext(ctx, `INSERT INTO users(email,display_name,password_hash) SELECT ?,?,password_hash FROM users WHERE id=?`, name+suffix+"@example.test", name, root.UserID)
		if err != nil {
			t.Fatal(err)
		}
		value, _ := result.LastInsertId()
		id := uint64(value)
		users = append(users, id)
		if _, err = db.ExecContext(ctx, `INSERT INTO tenant_memberships(tenant_id,user_id) VALUES(?,?)`, root.TenantID, id); err != nil {
			t.Fatal(err)
		}
		role, err := roles.CreateRole(ctx, roleActor, rbac.CreateRoleInput{Name: name, Permissions: keys})
		if err != nil {
			t.Fatal(err)
		}
		if err = roles.AssignRoleToUser(ctx, roleActor, role, id); err != nil {
			t.Fatal(err)
		}
		login, err := auth.Login(ctx, identity.LoginInput{Tenant: "notify-" + suffix, Email: name + suffix + "@example.test", Password: password}, identity.RequestMeta{})
		if err != nil {
			t.Fatal(err)
		}
		a := incidents.Actor{TenantID: root.TenantID, UserID: id, Permissions: map[string]bool{}}
		for _, key := range keys {
			a.Permissions[key] = true
		}
		return id, a, login, role
	}
	reporter, reporterActor, reporterLogin, reporterRole := seed("reporter", []string{"incident.read_own", "incident.create", "incident.communicate"})
	operator, operatorActor, _, _ := seed("operator", []string{"incident.read", "incident.create", "incident.assign", "incident.update", "incident.resolve", "incident.communicate", "incident.notes.read", "incident.notes.write"})
	_, outsiderActor, outsiderLogin, _ := seed("outsider", []string{"incident.read_own"})
	keys, _ := identities.ListPermissions(ctx, root.TenantID, root.UserID)
	rootActor := incidents.Actor{TenantID: root.TenantID, UserID: root.UserID, Permissions: map[string]bool{}}
	for _, key := range keys {
		rootActor.Permissions[key] = true
	}
	outbox := events.NewRepository(db)
	activityRepo := activity.NewRepository(db)
	incidentService := incidents.NewService(incidents.NewRepository(db), tx, workrecords.NewAllocator(tx), workrecords.NewService(workrecords.NewRepository(db), tx, outbox), audits, outbox, activityRepo)
	service := notifications.NewService(db, []byte("01234567890123456789012345678901"), true)
	dispatcher := func() *events.Dispatcher {
		d, err := events.NewDispatcher(outbox, slog.New(slog.NewTextHandler(io.Discard, nil)), "notification-test", 1, 50*time.Millisecond, 5*time.Second, 0)
		if err != nil {
			t.Fatal(err)
		}
		p := activity.NewProjector(tx, activityRepo)
		if err = incidents.RegisterEventHandlers(d, incidents.NewRepository(db), p); err != nil {
			t.Fatal(err)
		}
		if err = workrecords.RegisterActivityHandlers(d, p); err != nil {
			t.Fatal(err)
		}
		if err = service.Register(d); err != nil {
			t.Fatal(err)
		}
		return d
	}
	drain := func(d *events.Dispatcher) {
		t.Helper()
		for i := 0; i < 1000; i++ {
			processed, err := d.RunOnce(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if !processed {
				return
			}
		}
		t.Fatal("outbox did not drain")
	}
	worker := func() *jobs.Worker {
		w, err := jobs.NewWorker(jobs.NewRepository(db), slog.New(slog.NewTextHandler(io.Discard, nil)), "notification-mail-test", 1, 50*time.Millisecond, 5*time.Second, 0)
		if err != nil {
			t.Fatal(err)
		}
		if err = w.Register(notifications.SMTPJobType, service.MailHandler); err != nil {
			t.Fatal(err)
		}
		return w
	}
	fields := incidents.Fields{Title: "Notification incident", Description: "Real test", Impact: 2, Urgency: 2, Category: "software", Subcategory: "application"}
	item, _, err := incidentService.Create(ctx, reporterActor, fields, "notify-create-key")
	if err != nil {
		t.Fatal(err)
	}
	preference, err := service.Preference(ctx, root.TenantID, reporter)
	if err != nil {
		t.Fatal(err)
	}
	preference.SMTP = true
	if err = service.SavePreference(ctx, root.TenantID, reporter, preference); err != nil {
		t.Fatal(err)
	}
	if _, err = incidentService.AddMessage(ctx, operatorActor, item.ID, incidents.CommunicationInput{Type: workrecords.MessageComment, Body: "public response"}, "notify-public-key"); err != nil {
		t.Fatal(err)
	}
	if _, err = incidentService.AddMessage(ctx, operatorActor, item.ID, incidents.CommunicationInput{Type: workrecords.MessageWorkNote, Body: "PRIVATE NEVER NOTIFY REPORTER"}, "notify-private-key"); err != nil {
		t.Fatal(err)
	}
	d := dispatcher()
	drain(d)
	page, err := service.List(ctx, root.TenantID, reporter, 0, 1)
	if err != nil || len(page.Notifications) != 1 || page.Unread != 1 {
		t.Fatal("in-app", page, err)
	}
	notification := page.Notifications[0]
	history, err := incidentService.History(ctx, reporterActor, item.ID, 0, 100)
	if err != nil || len(history.Entries) != 2 || history.Pending {
		t.Fatal("independent timeline", history, err)
	}
	mailPage, err := service.MailList(ctx, root.TenantID, reporter, 0, 25)
	if err != nil || len(mailPage.Deliveries) != 1 || mailPage.Deliveries[0].Status != "NOT_CONFIGURED" {
		t.Fatal("missing configuration is not execution failure", mailPage, err)
	}
	// Each consumer has a durable result, and a failed extra consumer cannot
	// repeat or mask the successful activity and notification effects.
	d2 := dispatcher()
	if err = d2.RegisterConsumer("failing-test.v1", "work.comment.added", func(context.Context, events.Event) error { return errors.New("failed isolated consumer") }); err != nil {
		t.Fatal(err)
	}
	if _, err = incidentService.AddMessage(ctx, operatorActor, item.ID, incidents.CommunicationInput{Type: workrecords.MessageComment, Body: "second public response"}, "notify-public-two"); err != nil {
		t.Fatal(err)
	}
	drain(d2)
	page, err = service.List(ctx, root.TenantID, reporter, 0, 1)
	if err != nil || page.Unread != 2 || page.NextBeforeID == 0 {
		t.Fatal("pagination", page, err)
	}
	var failures, success uint64
	if err = db.QueryRowContext(ctx, `SELECT SUM(d.status='FAILED'),SUM(d.status='DELIVERED') FROM event_deliveries d JOIN outbox_events e ON e.id=d.event_id WHERE e.tenant_id=? AND e.event_type='work.comment.added' AND JSON_UNQUOTE(JSON_EXTRACT(e.payload,'$.body'))='second public response'`, root.TenantID).Scan(&failures, &success); err != nil || failures != 1 || success != 2 {
		t.Fatal("independent statuses", failures, success, err)
	}
	if _, err = db.ExecContext(ctx, `UPDATE event_deliveries d JOIN outbox_events e ON e.id=d.event_id SET d.status='PENDING',d.attempt_count=0,d.available_at=UTC_TIMESTAMP(6) WHERE e.tenant_id=? AND d.consumer_id IN ('activity.v1','notifications.v1')`, root.TenantID); err != nil {
		t.Fatal(err)
	}
	drain(dispatcher())
	page, err = service.List(ctx, root.TenantID, reporter, 0, 25)
	if err != nil || len(page.Notifications) != 2 {
		t.Fatal("consumer replay duplicated effect", page, err)
	}
	if err = service.MarkRead(ctx, root.TenantID, reporter, notification.ID); err != nil {
		t.Fatal(err)
	}
	if err = service.MarkRead(ctx, root.TenantID, reporter, notification.ID); err != nil {
		t.Fatal("repeat read", err)
	}
	hidden, err := service.List(ctx, root.TenantID, outsiderActor.UserID, 0, 25)
	if err != nil || len(hidden.Notifications) != 0 || hidden.Unread != 0 {
		t.Fatal("unrelated user", hidden, err)
	}
	// Real SMTP protocol server captures DATA and accepts it with a final 250.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	var sent atomic.Uint64
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				reader := bufio.NewReader(conn)
				writer := bufio.NewWriter(conn)
				reply := func(v string) { _, _ = writer.WriteString(v + "\r\n"); _ = writer.Flush() }
				reply("220 test SMTP")
				for {
					line, err := reader.ReadString('\n')
					if err != nil {
						return
					}
					switch {
					case strings.HasPrefix(line, "DATA"):
						reply("354 send")
						var data strings.Builder
						for {
							line, err = reader.ReadString('\n')
							if err != nil {
								return
							}
							if line == ".\r\n" {
								break
							}
							data.WriteString(line)
						}
						if strings.Contains(data.String(), "PRIVATE") {
							t.Error("private body in SMTP")
						}
						sent.Add(1)
						reply("250 accepted")
					case strings.HasPrefix(line, "QUIT"):
						reply("221 bye")
						return
					default:
						reply("250 ok")
					}
				}
			}()
		}
	}()
	_, portText, _ := net.SplitHostPort(listener.Addr().String())
	port, _ := strconv.Atoi(portText)
	smtpConfig := notifications.ConfigInput{ProviderID: "local-test-provider", Enabled: true, Host: "127.0.0.1", Port: port, TLSMode: "NONE", From: "sender@example.test", Version: 0}
	if err = service.SaveConfig(ctx, root.TenantID, root.UserID, smtpConfig); err != nil {
		t.Fatal(err)
	}
	existing := mailPage.Deliveries[0]
	if err = service.RetryMail(ctx, root.TenantID, root.UserID, existing.ID, existing.Version); err != nil {
		t.Fatal(err)
	}
	if err = service.RetryMail(ctx, root.TenantID, root.UserID, existing.ID, existing.Version); !errors.Is(err, notifications.ErrConflict) {
		t.Fatal("stale retry accepted", err)
	}
	w := worker()
	if processed, err := w.RunOnce(ctx); err != nil || !processed {
		t.Fatal("real SMTP worker", processed, err)
	}
	mailPage, err = service.MailList(ctx, root.TenantID, reporter, 0, 25)
	if err != nil {
		t.Fatal(err)
	}
	var delivered notifications.MailDelivery
	for _, m := range mailPage.Deliveries {
		if m.ID == existing.ID {
			delivered = m
		}
	}
	if delivered.Status != "DELIVERED" || delivered.Attempts != 1 || sent.Load() != 1 {
		t.Fatal("SMTP not accepted", delivered, sent.Load())
	}
	// A crash after the MySQL mail effect but before job finalization is safe.
	if _, err = db.ExecContext(ctx, `UPDATE jobs j JOIN notification_mail_deliveries d ON d.job_id=j.id SET j.status='QUEUED',j.available_at=UTC_TIMESTAMP(6) WHERE d.id=?`, delivered.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = worker().RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if sent.Load() != 1 {
		t.Fatal("restart duplicated completed SMTP effect")
	}
	if err = service.RetryMail(ctx, root.TenantID, root.UserID, delivered.ID, delivered.Version); !errors.Is(err, notifications.ErrConflict) {
		t.Fatal("successful delivery retried", err)
	}
	// Failed SMTP is visible and does not block new in-app/history effects.
	smtpConfig.Version = 1
	smtpConfig.Port = 1
	if err = service.SaveConfig(ctx, root.TenantID, root.UserID, smtpConfig); err != nil {
		t.Fatal(err)
	}
	if _, err = incidentService.AddMessage(ctx, operatorActor, item.ID, incidents.CommunicationInput{Type: workrecords.MessageComment, Body: "mail failure public"}, "notify-fail-public"); err != nil {
		t.Fatal(err)
	}
	drain(dispatcher())
	for i := 0; i < 3; i++ {
		if _, err = w.RunOnce(ctx); err != nil {
			t.Fatal(err)
		}
	}
	mailPage, err = service.MailList(ctx, root.TenantID, reporter, 0, 25)
	if err != nil || mailPage.Deliveries[0].Status != "FAILED" || mailPage.Deliveries[0].Attempts != 3 || mailPage.Deliveries[0].ErrorCode != "SMTP_CONNECT_FAILED" {
		t.Fatal("SMTP error status", mailPage, err)
	}
	page, err = service.List(ctx, root.TenantID, reporter, 0, 25)
	if err != nil || len(page.Notifications) != 3 {
		t.Fatal("SMTP blocked in-app", page, err)
	}
	history, err = incidentService.History(ctx, reporterActor, item.ID, 0, 100)
	if err != nil || history.Pending || len(history.Entries) != 4 {
		t.Fatal("SMTP blocked timeline", history, err)
	}
	// Persisted deliveries recheck current access before sending; late reads and
	// counters also hide records after role revocation.
	if _, err = incidentService.AddMessage(ctx, operatorActor, item.ID, incidents.CommunicationInput{Type: workrecords.MessageComment, Body: "revoked later"}, "notify-revoked-key"); err != nil {
		t.Fatal(err)
	}
	drain(dispatcher())
	if _, err = db.ExecContext(ctx, `DELETE FROM user_role_assignments WHERE tenant_id=? AND user_id=? AND role_id=?`, root.TenantID, reporter, reporterRole); err != nil {
		t.Fatal(err)
	}
	if _, err = w.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	page, err = service.List(ctx, root.TenantID, reporter, 0, 25)
	if err != nil || len(page.Notifications) != 0 || page.Unread != 0 {
		t.Fatal("revoked access leaked notification", page, err)
	}
	if err = service.MarkRead(ctx, root.TenantID, reporter, notification.ID); !errors.Is(err, notifications.ErrNotFound) {
		t.Fatal("revoked read allowed", err)
	}
	mailPage, err = service.MailList(ctx, root.TenantID, 0, 0, 25)
	if err != nil || mailPage.Deliveries[0].Status != "SKIPPED" {
		t.Fatal("late send ignored revocation", mailPage, err)
	}
	// API session/CSRF/RBAC and DTOs, including no payload/secrets from admin queues.
	middleware := identity.NewMiddleware(auth, cfg.Session)
	router := chi.NewRouter()
	notifications.RegisterRoutes(router, notifications.NewHandler(service, outbox), middleware)
	request := func(login identity.LoginResult, method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.AddCookie(&http.Cookie{Name: cfg.Session.CookieName, Value: login.SessionToken})
		if method != "GET" {
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("X-CSRF-Token", login.CSRFToken)
			r.AddCookie(&http.Cookie{Name: cfg.Session.CSRFCookieName, Value: login.CSRFToken})
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, r)
		return response
	}
	if response := request(reporterLogin, "GET", "/api/v1/notifications/", ""); response.Code != 200 || strings.Contains(response.Body.String(), "PRIVATE") {
		t.Fatal("reporter API", response.Code, response.Body.String())
	}
	if response := request(outsiderLogin, "GET", "/api/v1/admin/notifications/mail", ""); response.Code != 403 {
		t.Fatal("admin RBAC", response.Code)
	}
	rootLogin, err := auth.Login(ctx, identity.LoginInput{Tenant: "notify-" + suffix, Email: "root-" + suffix + "@example.test", Password: password}, identity.RequestMeta{})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/v1/admin/notifications/mail", "/api/v1/admin/notifications/deliveries", "/api/v1/admin/notifications/smtp"} {
		response := request(rootLogin, "GET", path, "")
		if response.Code != 200 || strings.Contains(response.Body.String(), "PRIVATE") || strings.Contains(response.Body.String(), "password_cipher") {
			t.Fatal("unsafe admin DTO", path, response.Code, response.Body.String())
		}
	}
	// New registration never sends history: explicit eligibility survives replay.
	result, err := db.ExecContext(ctx, `INSERT INTO outbox_events(tenant_id,scope_key,event_type,event_version,aggregate_type,aggregate_id,payload,notification_eligible) SELECT tenant_id,scope_key,event_type,event_version,aggregate_type,aggregate_id,payload,FALSE FROM outbox_events WHERE tenant_id=? AND event_type='work.comment.added' LIMIT 1`, root.TenantID)
	if err != nil {
		t.Fatal(err)
	}
	legacyID, _ := result.LastInsertId()
	drain(dispatcher())
	var skipped string
	if err = db.QueryRowContext(ctx, `SELECT status FROM event_deliveries WHERE event_id=? AND consumer_id='notifications.v1'`, legacyID).Scan(&skipped); err != nil || skipped != "SKIPPED" {
		t.Fatal("history replay sent notifications", skipped, err)
	}
	_ = operator
	_ = rootActor
}

// SQL rollback and lease fencing are tested against actual MySQL rather than a
// fake store. No other delivery may be acknowledged by a failed split.
func TestEventSplitRollbackAndLease(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	db, err := database.OpenMySQL(ctx, cfg.MySQL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := events.NewRepository(db)
	id, _, err := repo.Append(ctx, db, events.Input{Type: "test.split", AggregateType: "system", AggregateID: "test", MaxAttempts: 3})
	if err != nil {
		t.Fatal(err)
	}
	defer db.ExecContext(ctx, `DELETE FROM outbox_events WHERE id=?`, id)
	event, ok, err := repo.Claim(ctx, "split-owner", 5*time.Second)
	if err != nil || !ok || event.ID != id {
		t.Fatal("claim", event, ok, err)
	}
	if err = repo.Distribute(ctx, id, "split-owner", []string{"activity.v1", "INVALID"}); !errors.Is(err, events.ErrInvalid) {
		t.Fatal(err)
	}
	var count int
	if err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM event_deliveries WHERE event_id=?`, id).Scan(&count); err != nil || count != 0 {
		t.Fatal("partial split persisted", count, err)
	}
	if err = repo.Distribute(ctx, id, "split-owner", []string{"activity.v1", "notifications.v1"}); err != nil {
		t.Fatal(err)
	}
	delivery, ok, err := repo.ClaimDelivery(ctx, "delivery-owner", 5*time.Second)
	if err != nil || !ok {
		t.Fatal(delivery, ok, err)
	}
	if _, err = db.ExecContext(ctx, `UPDATE event_deliveries SET lease_expires_at=UTC_TIMESTAMP(6)-INTERVAL 1 SECOND WHERE id=?`, delivery.ID); err != nil {
		t.Fatal(err)
	}
	if err = repo.FinishDelivery(ctx, delivery.ID, "delivery-owner", nil, 0); !errors.Is(err, events.ErrLeaseLost) {
		t.Fatal("expired lease completed", err)
	}
	claimed, ok, err := repo.ClaimDelivery(ctx, "new-owner", 5*time.Second)
	if err != nil || !ok || claimed.ID != delivery.ID || claimed.AttemptCount != 2 {
		t.Fatal("lease recovery", claimed, ok, err)
	}
	if err = repo.FinishDelivery(ctx, claimed.ID, "new-owner", nil, 0); err != nil {
		t.Fatal(err)
	}
}
