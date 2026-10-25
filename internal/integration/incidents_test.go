//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chmajster/OpenServiceNOW/internal/activity"
	"github.com/chmajster/OpenServiceNOW/internal/audit"
	"github.com/chmajster/OpenServiceNOW/internal/events"
	"github.com/chmajster/OpenServiceNOW/internal/identity"
	"github.com/chmajster/OpenServiceNOW/internal/incidents"
	"github.com/chmajster/OpenServiceNOW/internal/platform/config"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/rbac"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
	"github.com/go-chi/chi/v5"
)

func TestIncidentProcess(t *testing.T) {
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
	t.Cleanup(func() { _ = db.Close() })
	suffix := fmt.Sprint(time.Now().UnixNano())
	tx := database.NewTxManager(db)
	auditRepo := audit.NewMySQLRepository()
	identityRepo := identity.NewMySQLRepository(db)
	bootstrap := identity.NewBootstrapper(identityRepo, tx, auditRepo)
	tenantA, err := bootstrap.Run(ctx, identity.BootstrapInput{TenantSlug: "incident-a-" + suffix, TenantName: "Incidents A", AdminEmail: "incident-admin-a-" + suffix + "@example.test", AdminName: "Admin A", AdminPassword: "incident-test-administrator"})
	if err != nil {
		t.Fatal(err)
	}
	tenantB, err := bootstrap.Run(ctx, identity.BootstrapInput{TenantSlug: "incident-b-" + suffix, TenantName: "Incidents B", AdminEmail: "incident-admin-b-" + suffix + "@example.test", AdminName: "Admin B", AdminPassword: "incident-test-administrator"})
	if err != nil {
		t.Fatal(err)
	}
	userIDs := []uint64{tenantA.UserID, tenantB.UserID}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		if _, err := db.ExecContext(cleanupCtx, "DELETE FROM audit_events WHERE tenant_id IN (?,?)", tenantA.TenantID, tenantB.TenantID); err != nil {
			t.Error(err)
		}
		if _, err := db.ExecContext(cleanupCtx, "DELETE FROM tenants WHERE id IN (?,?)", tenantA.TenantID, tenantB.TenantID); err != nil {
			t.Error(err)
		}
		for _, id := range userIDs {
			if _, err := db.ExecContext(cleanupCtx, "DELETE FROM users WHERE id=?", id); err != nil {
				t.Error(err)
			}
		}
	})
	auth, err := identity.NewService(identityRepo, tx, auditRepo, cfg.Session, cfg.Auth)
	if err != nil {
		t.Fatal(err)
	}
	seedUser := func(name string) uint64 {
		t.Helper()
		email := "incident-" + name + "-" + suffix + "@example.test"
		result, err := db.ExecContext(ctx, `INSERT INTO users(email,display_name,password_hash) SELECT ?,?,password_hash FROM users WHERE id=?`, email, name, tenantA.UserID)
		if err != nil {
			t.Fatal(err)
		}
		id, err := result.LastInsertId()
		if err != nil {
			t.Fatal(err)
		}
		userIDs = append(userIDs, uint64(id))
		if _, err := db.ExecContext(ctx, `INSERT INTO tenant_memberships(tenant_id,user_id) VALUES(?,?)`, tenantA.TenantID, id); err != nil {
			t.Fatal(err)
		}
		return uint64(id)
	}
	requesterID := seedUser("requester")
	otherID := seedUser("other")
	agentID := seedUser("agent")
	disabledID := seedUser("disabled")
	rbacService := rbac.NewService(rbac.NewMySQLRepository(db), tx, auditRepo, identity.NewAdminSafetyRepository())
	roleActor := rbac.Actor{TenantID: tenantA.TenantID, UserID: tenantA.UserID}
	requesterRole, err := rbacService.CreateRole(ctx, roleActor, rbac.CreateRoleInput{Name: "incident-requester", Permissions: []string{"incident.create", "incident.read_own"}})
	if err != nil {
		t.Fatal(err)
	}
	agentRole, err := rbacService.CreateRole(ctx, roleActor, rbac.CreateRoleInput{Name: "incident-agent", Permissions: []string{"incident.create", "incident.read", "incident.update", "incident.assign", "incident.resolve"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []uint64{requesterID, otherID} {
		if err := rbacService.AssignRoleToUser(ctx, roleActor, requesterRole, id); err != nil {
			t.Fatal(err)
		}
	}
	if err := rbacService.AssignRoleToUser(ctx, roleActor, agentRole, agentID); err != nil {
		t.Fatal(err)
	}
	groupID, err := rbacService.CreateGroup(ctx, roleActor, rbac.CreateGroupInput{Name: "incident-support"})
	if err != nil {
		t.Fatal(err)
	}
	if err := rbacService.AddGroupMember(ctx, roleActor, groupID, agentID); err != nil {
		t.Fatal(err)
	}
	login := func(tenant, email string) identity.LoginResult {
		t.Helper()
		result, err := auth.Login(ctx, identity.LoginInput{Tenant: tenant, Email: email, Password: "incident-test-administrator"}, identity.RequestMeta{})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	adminLogin := login("incident-a-"+suffix, "incident-admin-a-"+suffix+"@example.test")
	requesterLogin := login("incident-a-"+suffix, "incident-requester-"+suffix+"@example.test")
	otherLogin := login("incident-a-"+suffix, "incident-other-"+suffix+"@example.test")
	agentLogin := login("incident-a-"+suffix, "incident-agent-"+suffix+"@example.test")
	foreignLogin := login("incident-b-"+suffix, "incident-admin-b-"+suffix+"@example.test")
	asActor := func(login identity.LoginResult) incidents.Actor {
		t.Helper()
		permissions, err := auth.Permissions(ctx, login.Identity)
		if err != nil {
			t.Fatal(err)
		}
		a := incidents.Actor{TenantID: login.Identity.TenantID, UserID: login.Identity.UserID, Permissions: map[string]bool{}, RequestID: "incident-test", IPAddress: "127.0.0.1"}
		for _, p := range permissions {
			a.Permissions[p] = true
		}
		return a
	}
	requester := asActor(requesterLogin)
	agent := asActor(agentLogin)
	admin := asActor(adminLogin)
	outbox := events.NewRepository(db)
	activityRepo := activity.NewRepository(db)
	service := incidents.NewService(incidents.NewRepository(db), tx, workrecords.NewAllocator(tx), workrecords.NewService(workrecords.NewRepository(db), tx, outbox), auditRepo, outbox, activityRepo)
	router := chi.NewRouter()
	incidents.RegisterRoutes(router, incidents.NewHandler(service, auth), identity.NewMiddleware(auth, cfg.Session))
	request := func(login identity.LoginResult, method, path string, body any, key string) *httptest.ResponseRecorder {
		t.Helper()
		raw, _ := json.Marshal(body)
		req := httptest.NewRequest(method, path, bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", key)
		req.Header.Set("X-CSRF-Token", login.CSRFToken)
		req.AddCookie(&http.Cookie{Name: cfg.Session.CookieName, Value: login.SessionToken})
		req.AddCookie(&http.Cookie{Name: cfg.Session.CSRFCookieName, Value: login.CSRFToken})
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		return response
	}
	input := incidents.Fields{Title: "VPN nie działa", Description: "Nie można połączyć się z siecią", AffectedUserID: requesterID, Category: "network", Subcategory: "vpn", Impact: 2, Urgency: 2}
	response := request(requesterLogin, "POST", "/api/v1/incidents/", input, "incident-first-request")
	if response.Code != 201 {
		t.Fatalf("create=%d %s", response.Code, response.Body)
	}
	var item incidents.Record
	if err := json.Unmarshal(response.Body.Bytes(), &item); err != nil {
		t.Fatal(err)
	}
	if item.Number != "INC0000001" || item.Version != 1 || item.Priority != 3 || item.Assignment.Version != 1 || item.Assignment.AssignedTo != nil {
		t.Fatalf("initial record=%+v", item)
	}
	path := fmt.Sprintf("/api/v1/incidents/%d", item.ID)
	for _, login := range []identity.LoginResult{otherLogin, foreignLogin} {
		if response := request(login, "GET", path, nil, ""); response.Code != 404 {
			t.Fatalf("foreign read=%d", response.Code)
		}
	}
	if response := request(otherLogin, "GET", path+"/history", nil, ""); response.Code != 404 {
		t.Fatalf("foreign history=%d", response.Code)
	}
	if response := request(requesterLogin, "GET", "/api/v1/incidents/?view=all", nil, ""); response.Code != 200 || !strings.Contains(response.Body.String(), item.Number) {
		t.Fatalf("own list=%d %s", response.Code, response.Body)
	}
	if response := request(otherLogin, "GET", "/api/v1/incidents/", nil, ""); strings.Contains(response.Body.String(), item.Number) {
		t.Fatal("other person's report leaked in list")
	}
	if response := request(requesterLogin, "POST", "/api/v1/incidents/", input, "incident-first-request"); response.Code != 200 {
		t.Fatal(response.Body)
	}
	different := input
	different.Title = "Inne dane"
	if response := request(requesterLogin, "POST", "/api/v1/incidents/", different, "incident-first-request"); response.Code != 409 {
		t.Fatalf("idempotency changed input=%d", response.Code)
	}
	if response := request(agentLogin, "POST", path+"/transition", incidents.TransitionInput{Version: item.Version, State: "CLOSED"}, ""); response.Code != 422 {
		t.Fatalf("invalid transition=%d %s", response.Code, response.Body)
	}
	if _, err := service.Assign(ctx, agent, item.ID, incidents.AssignmentInput{Version: item.Version, AssignedTo: &tenantB.UserID}); !errors.Is(err, incidents.ErrInvalid) {
		t.Fatalf("foreign assignee=%v", err)
	}
	if _, err := service.Assign(ctx, agent, item.ID, incidents.AssignmentInput{Version: item.Version, AssignedTo: &requesterID, AssignmentGroupID: &groupID}); !errors.Is(err, incidents.ErrInvalid) {
		t.Fatalf("nonmember assignee=%v", err)
	}
	if _, err := db.ExecContext(ctx, "UPDATE tenant_memberships SET active=FALSE WHERE tenant_id=? AND user_id=?", tenantA.TenantID, disabledID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Assign(ctx, agent, item.ID, incidents.AssignmentInput{Version: item.Version, AssignedTo: &disabledID}); !errors.Is(err, incidents.ErrInvalid) {
		t.Fatalf("inactive assignee=%v", err)
	}
	if _, err := db.ExecContext(ctx, "UPDATE `groups` SET active=FALSE WHERE id=?", groupID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Assign(ctx, agent, item.ID, incidents.AssignmentInput{Version: item.Version, AssignedTo: &agentID, AssignmentGroupID: &groupID}); !errors.Is(err, incidents.ErrInvalid) {
		t.Fatalf("inactive group=%v", err)
	}
	if _, err := db.ExecContext(ctx, "UPDATE `groups` SET active=TRUE WHERE id=?", groupID); err != nil {
		t.Fatal(err)
	}
	item, err = service.Assign(ctx, agent, item.ID, incidents.AssignmentInput{Version: item.Version, AssignedTo: &agentID, AssignmentGroupID: &groupID})
	if err != nil {
		t.Fatal(err)
	}
	groupPage, err := service.List(ctx, agent, incidents.Filter{View: "my_group", Sort: "newest", Limit: 10})
	if err != nil || len(groupPage.Incidents) != 1 {
		t.Fatalf("group view=%+v err=%v", groupPage, err)
	}
	transition := func(state, reason, code, description string) {
		t.Helper()
		holdReason := ""
		if state == "ON_HOLD" {
			holdReason = "AWAITING_USER"
			reason = ""
		}
		next, err := service.Transition(ctx, agent, item.ID, incidents.TransitionInput{Version: item.Version, State: state, Reason: reason, HoldReason: holdReason, ResolutionCode: code, ResolutionDescription: description})
		if err != nil {
			t.Fatal(err)
		}
		item = next
	}
	transition("IN_PROGRESS", "", "", "")
	if _, err := service.Transition(ctx, agent, item.ID, incidents.TransitionInput{Version: item.Version, State: "ON_HOLD"}); !errors.Is(err, incidents.ErrInvalid) {
		t.Fatalf("missing hold reason=%v", err)
	}
	transition("ON_HOLD", "Oczekiwanie na użytkownika", "", "")
	transition("RESOLVED", "", "FIXED", "Naprawiono konfigurację VPN")
	if response := request(requesterLogin, "POST", path+"/transition", incidents.TransitionInput{Version: item.Version, State: "CLOSED"}, ""); response.Code != 403 {
		t.Fatalf("requester resolve permission=%d", response.Code)
	}
	transition("CLOSED", "", "", "")
	if item.ResolvedAt == nil || item.ClosedAt == nil || item.StartedAt == nil {
		t.Fatal("lifecycle dates missing")
	}
	if _, err := service.Update(ctx, agent, item.ID, incidents.UpdateInput{Fields: input, Version: item.Version}); !errors.Is(err, incidents.ErrTransition) {
		t.Fatalf("closed edit=%v", err)
	}
	transition("IN_PROGRESS", "Problem wystąpił ponownie", "", "")
	if item.ResolvedAt != nil || item.ClosedAt != nil || item.ResolutionCode != "" {
		t.Fatal("reopen retained current resolution")
	}
	p1 := 1
	if response := request(agentLogin, "PUT", path+"/priority", incidents.PriorityInput{Version: item.Version, Priority: &p1, Reason: "Skutek biznesowy"}, ""); response.Code != 403 {
		t.Fatalf("priority override permission=%d", response.Code)
	}
	if _, err := service.OverridePriority(ctx, admin, item.ID, incidents.PriorityInput{Version: item.Version, Priority: &p1}); !errors.Is(err, incidents.ErrInvalid) {
		t.Fatalf("missing priority reason=%v", err)
	}
	item, err = service.OverridePriority(ctx, admin, item.ID, incidents.PriorityInput{Version: item.Version, Priority: &p1, Reason: "Usługa krytyczna"})
	if err != nil || item.Priority != 1 || item.PriorityOverride == nil {
		t.Fatalf("override=%+v %v", item, err)
	}
	input.Impact = 3
	input.Urgency = 3
	item, err = service.Update(ctx, agent, item.ID, incidents.UpdateInput{Fields: input, Version: item.Version})
	if err != nil || item.Priority != 5 || item.PriorityOverride != nil {
		t.Fatalf("priority recalculation=%+v %v", item, err)
	}
	t.Run("concurrent edits do not lose changes", func(t *testing.T) {
		version := item.Version
		results := make(chan error, 2)
		var wg sync.WaitGroup
		for _, title := range []string{"Zmiana A", "Zmiana B"} {
			wg.Add(1)
			go func(title string) {
				defer wg.Done()
				fields := input
				fields.Title = title
				_, err := service.Update(ctx, agent, item.ID, incidents.UpdateInput{Fields: fields, Version: version})
				results <- err
			}(title)
		}
		wg.Wait()
		close(results)
		successes, conflicts := 0, 0
		for err := range results {
			if err == nil {
				successes++
			} else if errors.Is(err, incidents.ErrConflict) {
				conflicts++
			} else {
				t.Fatal(err)
			}
		}
		if successes != 1 || conflicts != 1 {
			t.Fatalf("success=%d conflict=%d", successes, conflicts)
		}
		item, err = service.Get(ctx, agent, item.ID)
		if err != nil || item.Version != version+1 {
			t.Fatalf("version=%d %v", item.Version, err)
		}
	})
	t.Run("parallel creation and idempotent retry", func(t *testing.T) {
		type result struct {
			record  incidents.Record
			created bool
			err     error
		}
		results := make(chan result, 16)
		var wg sync.WaitGroup
		for index := 0; index < 16; index++ {
			wg.Add(1)
			go func(index int) {
				defer wg.Done()
				fields := input
				fields.Title = "Concurrent incident"
				fields.Impact = index%3 + 1
				fields.Urgency = 1
				key := fmt.Sprintf("concurrent-unique-%d", index)
				if index < 4 {
					fields.Impact = 1
					key = "concurrent-same-key"
				}
				record, created, err := service.Create(ctx, requester, fields, key)
				results <- result{record, created, err}
			}(index)
		}
		wg.Wait()
		close(results)
		seen := map[string]bool{}
		createdCount := 0
		for result := range results {
			if result.err != nil {
				t.Fatal(result.err)
			}
			if result.created {
				if seen[result.record.Number] {
					t.Fatal("duplicate number")
				}
				seen[result.record.Number] = true
				createdCount++
			}
		}
		if createdCount != 13 {
			t.Fatalf("created=%d want13", createdCount)
		}
		seenIDs := map[uint64]bool{}
		f := incidents.Filter{View: "all", Sort: "priority", Limit: 3, Query: "Concurrent incident"}
		previousPriority := 0
		var previousID uint64
		for {
			page, err := service.List(ctx, agent, f)
			if err != nil {
				t.Fatal(err)
			}
			for _, record := range page.Incidents {
				if seenIDs[record.ID] {
					t.Fatal("pagination duplicate")
				}
				seenIDs[record.ID] = true
				if record.Priority < previousPriority || record.Priority == previousPriority && previousID != 0 && record.ID >= previousID {
					t.Fatal("unstable priority ordering")
				}
				previousPriority = record.Priority
				previousID = record.ID
			}
			if page.NextAfterID == 0 {
				break
			}
			f.AfterID = page.NextAfterID
			f.AfterPriority = page.NextPriority
		}
		if len(seenIDs) != 13 {
			t.Fatalf("paginated %d want13", len(seenIDs))
		}
	})
	t.Run("atomic rollback preserves retry", func(t *testing.T) {
		fields := input
		fields.Title = "Rollback proof"
		bad := requester
		bad.IPAddress = strings.Repeat("x", 100)
		if _, _, err := service.Create(ctx, bad, fields, "rollback-audit-proof"); err == nil {
			t.Fatal("audit failure accepted")
		}
		var count int
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM incidents WHERE tenant_id=? AND title='Rollback proof'", tenantA.TenantID).Scan(&count); err != nil || count != 0 {
			t.Fatalf("partial audit rollback count=%d err=%v", count, err)
		}
		failing := incidents.NewService(incidents.NewRepository(db), tx, workrecords.NewAllocator(tx),
			workrecords.NewService(workrecords.NewRepository(db), tx, outbox), auditRepo, failingIncidentOutbox{outbox}, activityRepo)
		if _, _, err := failing.Create(ctx, requester, fields, "rollback-outbox-proof"); err == nil {
			t.Fatal("outbox failure accepted")
		}
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM incidents WHERE tenant_id=? AND title='Rollback proof'", tenantA.TenantID).Scan(&count); err != nil || count != 0 {
			t.Fatalf("partial outbox rollback count=%d err=%v", count, err)
		}
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM incident_requests WHERE tenant_id=? AND request_key IN ('rollback-audit-proof','rollback-outbox-proof')", tenantA.TenantID).Scan(&count); err != nil || count != 0 {
			t.Fatal("failed request reservation persisted")
		}
		if _, created, err := service.Create(ctx, requester, fields, "rollback-outbox-proof"); err != nil || !created {
			t.Fatalf("retry created=%v err=%v", created, err)
		}
	})
	t.Run("worker projection and persistence", func(t *testing.T) {
		cancelled, _, err := service.Create(ctx, agent, input, "cancelled-incident-proof")
		if err != nil {
			t.Fatal(err)
		}
		cancelled, err = service.Transition(ctx, agent, cancelled.ID, incidents.TransitionInput{Version: cancelled.Version, State: "CANCELLED", Reason: "Zgłoszenie wycofane"})
		if err != nil || cancelled.CancelledAt == nil {
			t.Fatalf("cancel=%+v %v", cancelled, err)
		}
		if _, err := service.Transition(ctx, agent, cancelled.ID, incidents.TransitionInput{Version: cancelled.Version, State: "IN_PROGRESS"}); !errors.Is(err, incidents.ErrTransition) {
			t.Fatalf("cancelled reopened: %v", err)
		}
		history, err := service.History(ctx, requester, item.ID, 0, 100)
		if err != nil || !history.Pending || len(history.Entries) != 0 {
			t.Fatalf("pre-worker history=%+v %v", history, err)
		}
		dispatcher, err := events.NewDispatcher(outbox, slog.New(slog.NewTextHandler(io.Discard, nil)), "incident-integration-worker", 1, 50*time.Millisecond, 5*time.Second, 0)
		if err != nil {
			t.Fatal(err)
		}
		if err := incidents.RegisterEventHandlers(dispatcher, incidents.NewRepository(db), activity.NewProjector(tx, activityRepo)); err != nil {
			t.Fatal(err)
		}
		// Process the newest event first using the real dispatcher, then older
		// events. History must follow source order rather than projection row IDs.
		if _, err := db.ExecContext(ctx, "UPDATE outbox_events SET available_at=UTC_TIMESTAMP(6)+INTERVAL 1 MINUTE WHERE tenant_id=? AND status='PENDING'", tenantA.TenantID); err != nil {
			t.Fatal(err)
		}
		var latestEventID uint64
		if err := db.QueryRowContext(ctx, "SELECT MAX(id) FROM outbox_events WHERE tenant_id=? AND aggregate_type='incident' AND aggregate_id=?", tenantA.TenantID, fmt.Sprint(item.ID)).Scan(&latestEventID); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, "UPDATE outbox_events SET available_at=UTC_TIMESTAMP(6) WHERE id=?", latestEventID); err != nil {
			t.Fatal(err)
		}
		if processed, err := dispatcher.RunOnce(ctx); err != nil || !processed {
			t.Fatalf("newest projection: %t %v", processed, err)
		}
		firstHistory, err := service.History(ctx, requester, item.ID, 0, 100)
		if err != nil || len(firstHistory.Entries) != 1 {
			t.Fatalf("first history: %+v %v", firstHistory, err)
		}
		if _, err := db.ExecContext(ctx, "UPDATE outbox_events SET available_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND status='PENDING'", tenantA.TenantID); err != nil {
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
		history, err = service.History(ctx, requester, item.ID, 0, 100)
		if err != nil || history.Pending || len(history.Entries) < 10 {
			t.Fatalf("worker history=%+v err=%v", history, err)
		}
		sawReopen, sawOverride := false, false
		for _, entry := range history.Entries {
			var event incidents.ChangeEvent
			if err := json.Unmarshal(entry.Data, &event); err != nil {
				t.Fatal(err)
			}
			if !entry.EventTime.Equal(event.OccurredAt) || event.IncidentVersion != event.After.Version {
				t.Fatal("lost business time/version")
			}
			if entry.Type == "incident.reopened" {
				sawReopen = true
				if event.Before.State != "CLOSED" || event.After.State != "IN_PROGRESS" {
					t.Fatal("reopen before/after invalid")
				}
			}
			if entry.Type == "incident.priority.changed" {
				sawOverride = true
				if event.Reason != "Usługa krytyczna" {
					t.Fatal("override reason missing")
				}
			}
		}
		if !sawReopen || !sawOverride {
			t.Fatal("expected domain history missing")
		}
		ordinaryTypes := 0
		for key := range incidents.EventSummaries {
			if !strings.HasPrefix(key, "incident.major.") {
				ordinaryTypes++
			}
		}
		var registeredTypes int
		if err := db.QueryRowContext(ctx, "SELECT COUNT(DISTINCT entry_type) FROM activity_entries WHERE tenant_id=? AND object_type='incident'", tenantA.TenantID).Scan(&registeredTypes); err != nil || registeredTypes != ordinaryTypes {
			t.Fatalf("projected %d event types, want %d: %v", registeredTypes, ordinaryTypes, err)
		}
		previousVersion := item.Version + 1
		cursor := uint64(0)
		count := 0
		for {
			page, err := service.History(ctx, requester, item.ID, cursor, 3)
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range page.Entries {
				var event incidents.ChangeEvent
				if err := json.Unmarshal(entry.Data, &event); err != nil {
					t.Fatal(err)
				}
				if event.IncidentVersion >= previousVersion {
					t.Fatal("history cursor lost source event ordering")
				}
				previousVersion = event.IncidentVersion
				count++
			}
			if page.NextBeforeEventID == 0 {
				break
			}
			cursor = page.NextBeforeEventID
		}
		if count != len(history.Entries) {
			t.Fatalf("paginated history %d != %d", count, len(history.Entries))
		}
		var eventID uint64
		if err := db.QueryRowContext(ctx, "SELECT id FROM outbox_events WHERE tenant_id=? AND aggregate_type='incident' AND aggregate_id=? ORDER BY id LIMIT 1", tenantA.TenantID, fmt.Sprint(item.ID)).Scan(&eventID); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, "UPDATE outbox_events SET status='PENDING',available_at=UTC_TIMESTAMP(6) WHERE id=?", eventID); err != nil {
			t.Fatal(err)
		}
		if processed, err := dispatcher.RunOnce(ctx); err != nil || !processed {
			t.Fatal(err)
		}
		replay, err := service.History(ctx, requester, item.ID, 0, 100)
		if err != nil || len(replay.Entries) != len(history.Entries) {
			t.Fatal("worker retry duplicated history")
		}
		freshDB, err := database.OpenMySQL(ctx, cfg.MySQL)
		if err != nil {
			t.Fatal(err)
		}
		defer freshDB.Close()
		freshTx := database.NewTxManager(freshDB)
		freshOutbox := events.NewRepository(freshDB)
		restored := incidents.NewService(incidents.NewRepository(freshDB), freshTx, workrecords.NewAllocator(freshTx), workrecords.NewService(workrecords.NewRepository(freshDB), freshTx, freshOutbox), auditRepo, freshOutbox, activity.NewRepository(freshDB))
		record, err := restored.Get(ctx, requester, item.ID)
		if err != nil || record.Version != item.Version || record.Number != item.Number || record.Assignment.AssignedTo == nil || *record.Assignment.AssignedTo != agentID {
			t.Fatal("record not restored")
		}
		persistedHistory, err := restored.History(ctx, requester, item.ID, 0, 100)
		if err != nil || len(persistedHistory.Entries) != len(history.Entries) {
			t.Fatal("history not restored")
		}
	})
}

// Append the real outbox row, then inject a real SQL failure in the same
// transaction. No trigger/SUPER privileges or replacement persistence is needed.
type failingIncidentOutbox struct{ repo *events.Repository }

func (f failingIncidentOutbox) Append(ctx context.Context, exec database.Executor, input events.Input) (uint64, bool, error) {
	if _, _, err := f.repo.Append(ctx, exec, input); err != nil {
		return 0, false, err
	}
	_, err := exec.ExecContext(ctx, "INSERT INTO incident_test_missing_table(id) VALUES(1)")
	return 0, false, err
}
