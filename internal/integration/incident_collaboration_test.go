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
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chmajster/OpenServiceNOW/internal/activity"
	"github.com/chmajster/OpenServiceNOW/internal/attachments"
	"github.com/chmajster/OpenServiceNOW/internal/audit"
	"github.com/chmajster/OpenServiceNOW/internal/events"
	"github.com/chmajster/OpenServiceNOW/internal/identity"
	"github.com/chmajster/OpenServiceNOW/internal/incidents"
	"github.com/chmajster/OpenServiceNOW/internal/jobs"
	"github.com/chmajster/OpenServiceNOW/internal/platform/config"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/rbac"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
	"github.com/go-chi/chi/v5"
)

func TestIncidentCollaboration(t *testing.T) {
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
	bootstrap := identity.NewBootstrapper(identities, tx, audits)
	suffix := fmt.Sprint(time.Now().UnixNano())
	password := "collaboration-test-password"
	root, err := bootstrap.Run(ctx, identity.BootstrapInput{TenantSlug: "collaboration-" + suffix, TenantName: "Collaboration", AdminEmail: "collaboration-root-" + suffix + "@example.test", AdminName: "Root", AdminPassword: password})
	if err != nil {
		t.Fatal(err)
	}
	userIDs := []uint64{root.UserID}
	tenantIDs := []uint64{root.TenantID}
	defer func() {
		for _, tenantID := range tenantIDs {
			if _, err := db.ExecContext(context.Background(), "DELETE FROM audit_events WHERE tenant_id=?", tenantID); err != nil {
				t.Error("audit cleanup", err)
			}
			if _, err := db.ExecContext(context.Background(), "DELETE FROM tenants WHERE id=?", tenantID); err != nil {
				t.Error("tenant cleanup", err)
			}
		}
		for _, id := range userIDs {
			if _, err := db.ExecContext(context.Background(), "DELETE FROM users WHERE id=?", id); err != nil {
				t.Error("user cleanup", err)
			}
		}
	}()
	auth, err := identity.NewService(identities, tx, audits, cfg.Session, cfg.Auth)
	if err != nil {
		t.Fatal(err)
	}
	roles := rbac.NewService(rbac.NewMySQLRepository(db), tx, audits, identity.NewAdminSafetyRepository())
	roleActor := rbac.Actor{TenantID: root.TenantID, UserID: root.UserID}
	seed := func(name string, permissions []string, big bool) (identity.LoginResult, incidents.Actor) {
		t.Helper()
		email := name + suffix + "@example.test"
		var id uint64
		if big {
			id = 9007199254741999
			_, err = db.ExecContext(ctx, `INSERT INTO users(id,email,display_name,password_hash) SELECT ?,?,?,password_hash FROM users WHERE id=?`, id, email, name, root.UserID)
		} else {
			result, e := db.ExecContext(ctx, `INSERT INTO users(email,display_name,password_hash) SELECT ?,?,password_hash FROM users WHERE id=?`, email, name, root.UserID)
			err = e
			if e == nil {
				v, _ := result.LastInsertId()
				id = uint64(v)
			}
		}
		if err != nil {
			t.Fatal(err)
		}
		userIDs = append(userIDs, id)
		if _, err := db.ExecContext(ctx, `INSERT INTO tenant_memberships(tenant_id,user_id) VALUES(?,?)`, root.TenantID, id); err != nil {
			t.Fatal(err)
		}
		role, err := roles.CreateRole(ctx, roleActor, rbac.CreateRoleInput{Name: name, Permissions: permissions})
		if err != nil {
			t.Fatal(err)
		}
		if err := roles.AssignRoleToUser(ctx, roleActor, role, id); err != nil {
			t.Fatal(err)
		}
		login, err := auth.Login(ctx, identity.LoginInput{Tenant: "collaboration-" + suffix, Email: email, Password: password}, identity.RequestMeta{})
		if err != nil {
			t.Fatal(err)
		}
		a := incidents.Actor{TenantID: root.TenantID, UserID: id, Permissions: map[string]bool{}, RequestID: "collaboration", IPAddress: "127.0.0.1"}
		for _, p := range permissions {
			a.Permissions[p] = true
		}
		return login, a
	}
	reporterLogin, reporter := seed("reporter", []string{"incident.create", "incident.read_own", "incident.communicate"}, true)
	operatorLogin, operator := seed("operator", []string{"incident.read", "incident.create", "incident.update", "incident.assign", "incident.communicate", "incident.notes.read", "incident.notes.write", "incident.attachments.delete"}, false)
	outsiderLogin, outsider := seed("outsider", []string{"incident.create", "incident.read_own", "incident.communicate"}, false)
	outbox := events.NewRepository(db)
	activityRepo := activity.NewRepository(db)
	storageRoot := t.TempDir()
	storage, err := attachments.NewLocalStorage(storageRoot)
	if err != nil {
		t.Fatal(err)
	}
	metadata := attachments.NewRepository(db)
	files, err := attachments.NewEventingService(metadata, storage, attachments.MaxFileBytes, tx, outbox)
	if err != nil {
		t.Fatal(err)
	}
	service := incidents.NewService(incidents.NewRepository(db), tx, workrecords.NewAllocator(tx), workrecords.NewService(workrecords.NewRepository(db), tx, outbox), audits, outbox, activityRepo).WithAttachments(files)
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
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}
	input := incidents.Fields{Title: "Komunikacja", Description: "Treść", Category: "other", Subcategory: "other", Impact: 2, Urgency: 2}
	record, _, err := service.Create(ctx, reporter, input, "collaboration-create")
	if err != nil {
		t.Fatal(err)
	}
	base := "/api/v1/incidents/" + fmt.Sprint(record.ID)
	foreign, err := bootstrap.Run(ctx, identity.BootstrapInput{TenantSlug: "collaboration-foreign-" + suffix, TenantName: "Foreign", AdminEmail: "collaboration-foreign-" + suffix + "@example.test", AdminName: "Foreign", AdminPassword: password})
	if err != nil {
		t.Fatal(err)
	}
	tenantIDs = append(tenantIDs, foreign.TenantID)
	userIDs = append(userIDs, foreign.UserID)
	foreignLogin, err := auth.Login(ctx, identity.LoginInput{Tenant: "collaboration-foreign-" + suffix, Email: "collaboration-foreign-" + suffix + "@example.test", Password: password}, identity.RequestMeta{})
	if err != nil {
		t.Fatal(err)
	}
	// All collaboration SQL must use the transaction executor, also with a one-connection pool.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	note, err := service.AddMessage(ctx, operator, record.ID, incidents.CommunicationInput{Type: workrecords.MessageWorkNote, Body: "POUFNA-NOTATKA-TEST"}, "private-message-key")
	if err != nil {
		t.Fatal(err)
	}
	reply, err := service.AddMessage(ctx, operator, record.ID, incidents.CommunicationInput{Type: workrecords.MessageComment, Body: "Odpowiedź publiczna"}, "public-message-key")
	if err != nil {
		t.Fatal(err)
	}
	repeat, err := service.AddMessage(ctx, operator, record.ID, incidents.CommunicationInput{Type: workrecords.MessageComment, Body: "Odpowiedź publiczna"}, "public-message-key")
	if err != nil || repeat.ID != reply.ID {
		t.Fatal("message duplicated", err)
	}
	if _, err := service.AddMessage(ctx, operator, record.ID, incidents.CommunicationInput{Type: workrecords.MessageComment, Body: "Inna treść"}, "public-message-key"); !errors.Is(err, incidents.ErrIdempotency) {
		t.Fatal(err)
	}
	for _, login := range []identity.LoginResult{reporterLogin, outsiderLogin} {
		w := request(login, "POST", base+"/messages", incidents.CommunicationInput{Type: workrecords.MessageWorkNote, Body: "x"}, "forbidden-notes-key")
		if w.Code != 403 {
			t.Fatalf("note permission=%d %s", w.Code, w.Body)
		}
	}
	if w := request(reporterLogin, "POST", base+"/messages", map[string]any{"type": "COMMENT", "body": "x", "object_type": "other", "object_id": "1"}, "object-injection-key"); w.Code != 400 {
		t.Fatal("client object ref accepted", w.Code)
	}
	private, err := service.UploadFile(ctx, operator, record.ID, "sekret.txt", "PUBLIC", &note.ID, strings.NewReader("POUFNE-BAJTY-TEST"), "private-file-key")
	if err != nil || private.Visibility != "INTERNAL" {
		t.Fatalf("inheritance=%+v %v", private, err)
	}
	public, err := service.UploadFile(ctx, operator, record.ID, "załącznik.txt", "PUBLIC", &reply.ID, strings.NewReader("public bytes"), "public-file-key")
	if err != nil {
		t.Fatal(err)
	}
	retry, err := service.UploadFile(ctx, operator, record.ID, "załącznik.txt", "PUBLIC", &reply.ID, strings.NewReader("public bytes"), "public-file-key")
	if err != nil || retry.ID != public.ID {
		t.Fatal("file duplicated", err)
	}
	if _, err := service.UploadFile(ctx, operator, record.ID, "załącznik.txt", "PUBLIC", &reply.ID, strings.NewReader("changed bytes"), "public-file-key"); !errors.Is(err, incidents.ErrIdempotency) {
		t.Fatal(err)
	}
	if _, err := service.UploadFile(ctx, reporter, record.ID, "attempt.txt", "PUBLIC", &note.ID, strings.NewReader("x"), "private-bypass-key"); !errors.Is(err, incidents.ErrForbidden) {
		t.Fatal("note-file bypass", err)
	}
	for _, path := range []string{base, base + "/messages?limit=1", base + "/files?limit=1", base + "/history?limit=1", base + "/relations", base + "/watchers"} {
		w := request(reporterLogin, "GET", path, nil, "")
		if w.Code != 200 || strings.Contains(w.Body.String(), "POUFNA") || strings.Contains(w.Body.String(), "sekret.txt") || strings.Contains(w.Body.String(), "storage_key") {
			t.Fatalf("private response %s: %d %s", path, w.Code, w.Body)
		}
		other := request(outsiderLogin, "GET", path, nil, "")
		if other.Code != 404 {
			t.Fatalf("object read bypass %s %d", path, other.Code)
		}
		if other := request(foreignLogin, "GET", path, nil, ""); other.Code != 404 {
			t.Fatalf("foreign organization %s %d %s", path, other.Code, other.Body)
		}
	}
	for _, fileID := range []uint64{private.ID, public.ID} {
		if w := request(foreignLogin, "GET", base+"/files/"+fmt.Sprint(fileID), nil, ""); w.Code != 404 {
			t.Fatal("foreign file URL", w.Code)
		}
	}
	if w := request(foreignLogin, "POST", base+"/messages", incidents.CommunicationInput{Type: workrecords.MessageComment, Body: "cross tenant"}, "foreign-message-key"); w.Code != 404 {
		t.Fatal("foreign write", w.Code)
	}
	t.Run("collaboration rollback leaves no partial write or retry reservation", func(t *testing.T) {
		failing := incidents.NewService(incidents.NewRepository(db), tx, workrecords.NewAllocator(tx), workrecords.NewService(workrecords.NewRepository(db), tx, outbox), audits, failingIncidentOutbox{outbox}, activityRepo).WithAttachments(files)
		if _, err := failing.AddMessage(ctx, operator, record.ID, incidents.CommunicationInput{Type: workrecords.MessageWorkNote, Body: "ROLLBACK-PRIVATE"}, "rollback-message-key"); err == nil {
			t.Fatal("outbox error accepted")
		}
		if _, err := failing.UploadFile(ctx, operator, record.ID, "rollback.txt", "INTERNAL", nil, strings.NewReader("ROLLBACK-BYTES"), "rollback-file-key"); err == nil {
			t.Fatal("outbox error accepted")
		}
		var count int
		for _, query := range []string{`SELECT COUNT(*) FROM work_messages WHERE tenant_id=? AND body='ROLLBACK-PRIVATE'`, `SELECT COUNT(*) FROM attachments WHERE tenant_id=? AND file_name='rollback.txt'`, `SELECT COUNT(*) FROM incident_collaboration_requests WHERE tenant_id=? AND request_key IN ('rollback-message-key','rollback-file-key')`, `SELECT COUNT(*) FROM outbox_events WHERE tenant_id=? AND payload LIKE '%ROLLBACK-PRIVATE%'`} {
			if err := db.QueryRowContext(ctx, query, root.TenantID).Scan(&count); err != nil || count != 0 {
				t.Fatal("partial write", query, count, err)
			}
		}
		if _, err := service.AddMessage(ctx, operator, record.ID, incidents.CommunicationInput{Type: workrecords.MessageComment, Body: "Retry after rollback"}, "rollback-message-key"); err != nil {
			t.Fatal("retry blocked", err)
		}
	})
	for _, login := range []identity.LoginResult{reporterLogin, outsiderLogin} {
		w := request(login, "GET", base+"/files/"+fmt.Sprint(private.ID), nil, "")
		if w.Code != 404 {
			t.Fatalf("private download=%d", w.Code)
		}
	}
	if w := request(reporterLogin, "DELETE", base+"/files/"+fmt.Sprint(private.ID), nil, ""); w.Code != 404 {
		t.Fatal("private deletion exposed", w.Code)
	}
	if w := request(reporterLogin, "GET", base+"/files/"+fmt.Sprint(public.ID), nil, ""); w.Code != 200 || w.Body.String() != "public bytes" || !strings.HasPrefix(w.Header().Get("Content-Disposition"), "attachment;") {
		t.Fatal("download", w.Code, w.Body)
	}
	if w := request(reporterLogin, "DELETE", base+"/files/"+fmt.Sprint(public.ID), nil, ""); w.Code != 403 {
		t.Fatal("other uploader deletion", w.Code)
	}
	if _, err := service.UploadFile(ctx, operator, record.ID, "bad\nname", "PUBLIC", nil, strings.NewReader("x"), "bad-filename-key"); !errors.Is(err, attachments.ErrInvalid) {
		t.Fatal("unsafe name", err)
	}
	if _, err := service.UploadFile(ctx, operator, record.ID, "too-big.txt", "PUBLIC", nil, strings.NewReader(strings.Repeat("x", int(attachments.MaxFileBytes)+1)), "oversized-file-key"); !errors.Is(err, attachments.ErrTooLarge) {
		t.Fatal("oversize", err)
	}
	t.Run("real multipart boundary and strict object scope", func(t *testing.T) {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		part, _ := writer.CreateFormFile("file", "multipart.txt")
		_, _ = part.Write([]byte("multipart bytes"))
		_ = writer.WriteField("visibility", "PUBLIC")
		_ = writer.Close()
		req := httptest.NewRequest("POST", base+"/files", &body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		req.Header.Set("X-CSRF-Token", reporterLogin.CSRFToken)
		req.Header.Set("Idempotency-Key", "multipart-test-key")
		req.AddCookie(&http.Cookie{Name: cfg.Session.CookieName, Value: reporterLogin.SessionToken})
		req.AddCookie(&http.Cookie{Name: cfg.Session.CSRFCookieName, Value: reporterLogin.CSRFToken})
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != 200 || strings.Contains(w.Body.String(), "storage_key") {
			t.Fatal(w.Code, w.Body)
		}
	})
	t.Run("watchers and authorized relation ends", func(t *testing.T) {
		if err := service.Watch(ctx, reporter, record.ID, reporter.UserID, true); err != nil {
			t.Fatal(err)
		}
		if err := service.Watch(ctx, operator, record.ID, outsider.UserID, true); !errors.Is(err, incidents.ErrInvalid) {
			t.Fatal("watcher lacked object access", err)
		}
		if err := service.Watch(ctx, operator, record.ID, operator.UserID, true); err != nil {
			t.Fatal(err)
		}
		w, _ := service.Watchers(ctx, reporter, record.ID, 0, 1)
		if len(w.Watchers) != 1 || w.Watchers[0].UserID != reporter.UserID || w.NextAfterID != 0 {
			t.Fatal("watcher privacy", w)
		}
		t.Run("operators can remove former organization members from the bounded list", func(t *testing.T) {
			if _, err := db.ExecContext(ctx, `UPDATE tenant_memberships SET active=FALSE WHERE tenant_id=? AND user_id=?`, root.TenantID, reporter.UserID); err != nil {
				t.Fatal(err)
			}
			defer func() {
				if _, err := db.ExecContext(context.Background(), `UPDATE tenant_memberships SET active=TRUE WHERE tenant_id=? AND user_id=?`, root.TenantID, reporter.UserID); err != nil {
					t.Error(err)
				}
			}()
			p, err := service.Watchers(ctx, operator, record.ID, 0, 100)
			if err != nil || len(p.Watchers) != 2 {
				t.Fatal("hidden stale watcher prevents removal", p, err)
			}
			if err := service.Watch(ctx, operator, record.ID, reporter.UserID, false); err != nil {
				t.Fatal(err)
			}
		})
		target, _, err := service.Create(ctx, outsider, input, "target-private-incident")
		if err != nil {
			t.Fatal(err)
		}
		rel, err := service.AddRelation(ctx, operator, record.ID, incidents.RelationInput{Type: "PARENT", TargetID: target.ID})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := service.AddRelation(ctx, operator, target.ID, incidents.RelationInput{Type: "PARENT", TargetID: record.ID}); !errors.Is(err, incidents.ErrInvalid) {
			t.Fatal("cycle allowed", err)
		}
		p, err := service.Relations(ctx, reporter, record.ID, 0, 1)
		if err != nil || len(p.Relations) != 0 || p.NextBeforeID != 0 {
			t.Fatal("related target leaked", p, err)
		}
		if w := request(reporterLogin, "DELETE", base+"/relations/"+fmt.Sprint(rel.ID), nil, ""); w.Code != 403 {
			t.Fatal(w.Code)
		}
		if err := service.RemoveRelation(ctx, operator, record.ID, rel.ID); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("per-record bulk permissions and versions", func(t *testing.T) {
		good, _, err := service.Create(ctx, operator, input, "batch-good-record-key")
		if err != nil {
			t.Fatal(err)
		}
		stale, _, err := service.Create(ctx, operator, input, "batch-stale-record-key")
		if err != nil {
			t.Fatal(err)
		}
		input.Title = "Concurrently changed"
		input.AffectedUserID = stale.AffectedUserID
		if _, err := service.Update(ctx, operator, stale.ID, incidents.UpdateInput{Fields: input, Version: stale.Version}); err != nil {
			t.Fatal(err)
		}
		w := request(operatorLogin, "POST", "/api/v1/incidents/batch", incidents.BatchInput{Action: "assign_self", Items: []incidents.BatchItem{{ID: good.ID, Version: good.Version}, {ID: stale.ID, Version: stale.Version}, {ID: 999999999, Version: 1}}}, "")
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body)
		}
		var p struct {
			Results []incidents.BatchResult `json:"results"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
			t.Fatal(err)
		}
		if len(p.Results) != 3 || p.Results[0].Status != 200 || p.Results[1].Status != 409 || p.Results[2].Status != 404 {
			t.Fatal("individual results", p)
		}
	})
	dispatcher, err := events.NewDispatcher(outbox, slog.New(slog.NewTextHandler(io.Discard, nil)), "collaboration-worker", 1, 50*time.Millisecond, 5*time.Second, 0)
	if err != nil {
		t.Fatal(err)
	}
	projector := activity.NewProjector(tx, activityRepo)
	if err := workrecords.RegisterActivityHandlers(dispatcher, projector); err != nil {
		t.Fatal(err)
	}
	if err := incidents.RegisterEventHandlers(dispatcher, incidents.NewRepository(db), projector); err != nil {
		t.Fatal(err)
	}
	drain := func() {
		t.Helper()
		for n := 0; n < 500; n++ {
			processed, err := dispatcher.RunOnce(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if !processed {
				return
			}
		}
		t.Fatal("unbounded processing")
	}
	drain()
	h, err := service.History(ctx, reporter, record.ID, 0, 100)
	if err != nil || h.Pending {
		t.Fatal(err, h)
	}
	encoded, _ := json.Marshal(h)
	if bytes.Contains(encoded, []byte("POUFNA")) || bytes.Contains(encoded, []byte("sekret.txt")) {
		t.Fatal("private timeline")
	}
	// Pending hidden events must not be visible through a public boolean/counter.
	hidden, err := service.AddMessage(ctx, operator, record.ID, incidents.CommunicationInput{Type: workrecords.MessageWorkNote, Body: "SECOND-SECRET"}, "hidden-pending-message")
	if err != nil {
		t.Fatal(err)
	}
	h, err = service.History(ctx, reporter, record.ID, 0, 100)
	if err != nil || h.Pending {
		t.Fatal("pending private leak", h, err)
	}
	opHistory, err := service.History(ctx, operator, record.ID, 0, 100)
	if err != nil || !opHistory.Pending {
		t.Fatal("operator missing pending", err)
	}
	drain()
	opHistory, _ = service.History(ctx, operator, record.ID, 0, 100)
	var eventID uint64
	if err := db.QueryRowContext(ctx, `SELECT id FROM outbox_events WHERE tenant_id=? AND event_type='work.work_note.added' AND JSON_UNQUOTE(JSON_EXTRACT(payload,'$.message_id'))=?`, root.TenantID, fmt.Sprint(hidden.ID)).Scan(&eventID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE outbox_events SET status='PENDING',available_at=UTC_TIMESTAMP(6) WHERE id=?`, eventID); err != nil {
		t.Fatal(err)
	}
	drain()
	again, _ := service.History(ctx, operator, record.ID, 0, 100)
	if len(again.Entries) != len(opHistory.Entries) {
		t.Fatal("replayed event duplicated timeline")
	}
	if w := request(reporterLogin, "GET", base+"/messages", nil, ""); !strings.Contains(w.Body.String(), `"9007199254741999"`) && strings.Contains(w.Body.String(), "9007199254741999") {
		t.Fatal("BIGINT not string", w.Body)
	}
	rjson := request(reporterLogin, "GET", base, nil, "")
	if !strings.Contains(rjson.Body.String(), `"reporter_id":"9007199254741999"`) {
		t.Fatal("BIGINT rounded", rjson.Body)
	}
	t.Run("persistent storage and orphan reconciliation", func(t *testing.T) {
		reopened, err := attachments.NewLocalStorage(storageRoot)
		if err != nil {
			t.Fatal(err)
		}
		stored, err := metadata.Get(ctx, root.TenantID, public.ID)
		if err != nil {
			t.Fatal(err)
		}
		reader, err := reopened.Open(ctx, stored.StorageKey)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := io.ReadAll(reader)
		reader.Close()
		if string(raw) != "public bytes" {
			t.Fatal("storage restart lost file")
		}
		orphan, err := files.Prepare(ctx, attachments.CreateInput{TenantID: root.TenantID, ObjectType: "incident", ObjectID: fmt.Sprint(record.ID), FileName: "orphan.txt"}, strings.NewReader("uncommitted bytes"))
		if err != nil {
			t.Fatal(err)
		}
		tempKey := fmt.Sprintf("%d/.upload-123456", root.TenantID)
		tempPath := filepath.Join(storageRoot, tempKey)
		if err := os.WriteFile(tempPath, []byte("interrupted write"), 0600); err != nil {
			t.Fatal(err)
		}
		old := time.Now().Add(-48 * time.Hour)
		for _, key := range []string{orphan.Key, tempKey, stored.StorageKey} {
			if err := os.Chtimes(filepath.Join(storageRoot, key), old, old); err != nil {
				t.Fatal(err)
			}
		}
		removed, err := reopened.Cleanup(ctx, metadata, time.Now().Add(-24*time.Hour))
		if err != nil || removed != 2 {
			t.Fatal("orphan cleanup", removed, err)
		}
		if _, err := reopened.Open(ctx, orphan.Key); !errors.Is(err, attachments.ErrNotFound) {
			t.Fatal("orphan survived", err)
		}
		reader, err = reopened.Open(ctx, stored.StorageKey)
		if err != nil {
			t.Fatal("active file removed", err)
		}
		reader.Close()
		if err := service.DeleteFile(ctx, operator, record.ID, public.ID); err != nil {
			t.Fatal(err)
		}
		if w := request(reporterLogin, "GET", base+"/files/"+fmt.Sprint(public.ID), nil, ""); w.Code != 404 {
			t.Fatal("deleted download", w.Code)
		}
		payload, _ := json.Marshal(map[string]time.Time{"before": time.Now().Add(-24 * time.Hour)})
		result, err := attachments.CleanupHandler(reopened, metadata)(ctx, jobs.Job{Type: attachments.CleanupJobType, Payload: payload})
		if err != nil || string(result) != `{"removed_files":1}` {
			t.Fatal("deleted cleanup job", string(result), err)
		}
	})
}
