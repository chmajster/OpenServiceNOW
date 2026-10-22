//go:build integration

package integration

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chmajster/OpenServiceNOW/internal/audit"
	"github.com/chmajster/OpenServiceNOW/internal/identity"
	"github.com/chmajster/OpenServiceNOW/internal/knowledge"
	"github.com/chmajster/OpenServiceNOW/internal/platform/config"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
	"github.com/go-chi/chi/v5"
)

func TestKnowledgeSpacesEndToEnd(t *testing.T) {
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
	bootstrap := identity.NewBootstrapper(identities, tx, audits)
	suffix := fmt.Sprint(time.Now().UnixNano())
	adminA, err := bootstrap.Run(ctx, identity.BootstrapInput{
		TenantSlug: "knowledge-a-" + suffix, TenantName: "Knowledge A",
		AdminEmail: "knowledge-a-" + suffix + "@example.test", AdminName: "Admin A", AdminPassword: "knowledge-test-administrator",
	})
	if err != nil {
		t.Fatal(err)
	}
	adminB, err := bootstrap.Run(ctx, identity.BootstrapInput{
		TenantSlug: "knowledge-b-" + suffix, TenantName: "Knowledge B",
		AdminEmail: "knowledge-b-" + suffix + "@example.test", AdminName: "Admin B", AdminPassword: "knowledge-test-administrator",
	})
	if err != nil {
		t.Fatal(err)
	}
	users := []uint64{adminA.UserID, adminB.UserID}
	defer func() {
		for _, tenant := range []uint64{adminA.TenantID, adminB.TenantID} {
			_, _ = db.ExecContext(context.Background(), "DELETE FROM audit_events WHERE tenant_id=?", tenant)
			if _, err := db.ExecContext(context.Background(), "DELETE FROM tenants WHERE id=?", tenant); err != nil {
				t.Error(err)
			}
		}
		for _, user := range users {
			if _, err := db.ExecContext(context.Background(), "DELETE FROM users WHERE id=?", user); err != nil {
				t.Error(err)
			}
		}
	}()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	readerResult, err := db.ExecContext(ctx, "INSERT INTO users(email,display_name,password_hash) SELECT ?,?,password_hash FROM users WHERE id=?", "knowledge-reader-"+suffix+"@example.test", "Reader", adminA.UserID)
	if err != nil {
		t.Fatal(err)
	}
	readerValue, _ := readerResult.LastInsertId()
	readerID := uint64(readerValue)
	users = append(users, readerID)
	exec("INSERT INTO tenant_memberships(tenant_id,user_id) VALUES(?,?)", adminA.TenantID, readerID)
	role, err := db.ExecContext(ctx, "INSERT INTO roles(tenant_id,name) VALUES(?,?)", adminA.TenantID, "Knowledge reader")
	if err != nil {
		t.Fatal(err)
	}
	roleID, _ := role.LastInsertId()
	exec("INSERT INTO role_permissions(role_id,permission_id) SELECT ?,id FROM permissions WHERE permission_key=?", roleID, knowledge.PermissionRead)
	exec("INSERT INTO user_role_assignments(tenant_id,user_id,role_id) VALUES(?,?,?)", adminA.TenantID, readerID, roleID)

	actor := func(tenant, user uint64) knowledge.Actor {
		t.Helper()
		permissions, err := identities.ListPermissions(ctx, tenant, user)
		if err != nil {
			t.Fatal(err)
		}
		result := knowledge.Actor{Actor: workrecords.Actor{TenantID: tenant, UserID: user, RequestID: "knowledge-integration"}, Permissions: map[string]bool{}}
		for _, permission := range permissions {
			result.Permissions[permission] = true
		}
		return result
	}
	admin := actor(adminA.TenantID, adminA.UserID)
	otherAdmin := actor(adminB.TenantID, adminB.UserID)
	reader := actor(adminA.TenantID, readerID)
	service := knowledge.NewService(db)

	space, created, err := service.CreateSpace(ctx, admin, knowledge.SpaceInput{Key: "OPS", Name: "Operacje", Description: "Wiedza operacyjna", Status: knowledge.SpaceActive, ReviewIntervalDays: 90}, "knowledge-space-create")
	if err != nil || !created {
		t.Fatalf("create space: created=%t result=%+v err=%v", created, space, err)
	}
	replayed, created, err := service.CreateSpace(ctx, admin, knowledge.SpaceInput{Key: "OPS", Name: "Operacje", Description: "Wiedza operacyjna", Status: knowledge.SpaceActive, ReviewIntervalDays: 90}, "knowledge-space-create")
	if err != nil || created || replayed.ID != space.ID {
		t.Fatalf("idempotent space retry: created=%t result=%+v err=%v", created, replayed, err)
	}
	secretSearchTerm := "DRAFTONLYTOKEN"
	secret := "SECRET-BODY-" + secretSearchTerm + "-" + suffix
	draft, _, err := service.CreatePage(ctx, admin, space.ID, knowledge.PageInput{Title: "Szkic", Body: secret, Status: knowledge.PageDraft}, "knowledge-draft")
	if err != nil {
		t.Fatal(err)
	}
	past := time.Now().UTC().Add(-time.Hour)
	root, _, err := service.CreatePage(ctx, admin, space.ID, knowledge.PageInput{Title: "Wdrożenie", Body: "# Procedura\n\nBezpieczne wdrożenie", Status: knowledge.PagePublished, ReviewDueAt: &past}, "knowledge-published")
	if err != nil {
		t.Fatal(err)
	}
	readerSpaces, err := service.Spaces(ctx, reader, 0, 50)
	if err != nil || len(readerSpaces.Spaces) != 1 || readerSpaces.Spaces[0].PageCount != 1 {
		t.Fatalf("reader space projection leaked drafts: %+v err=%v", readerSpaces, err)
	}
	if _, err := service.Page(ctx, reader, draft.ID); !errors.Is(err, knowledge.ErrNotFound) {
		t.Fatalf("draft visible to reader: %v", err)
	}
	if _, err := service.Page(ctx, reader, root.ID); err != nil {
		t.Fatalf("published page hidden from reader: %v", err)
	}
	if _, _, err := service.CreatePage(ctx, reader, space.ID, knowledge.PageInput{Title: "Forbidden", Body: "x"}, "reader-cannot-write"); !errors.Is(err, knowledge.ErrForbidden) {
		t.Fatalf("reader write bypass: %v", err)
	}
	if _, err := service.Attention(ctx, reader, space.ID, 0, 50, ""); !errors.Is(err, knowledge.ErrForbidden) {
		t.Fatalf("reader attention bypass: %v", err)
	}
	if _, err := service.Space(ctx, otherAdmin, space.ID); !errors.Is(err, knowledge.ErrNotFound) {
		t.Fatalf("cross-tenant read: %v", err)
	}

	results, err := service.Search(ctx, reader, space.ID, "bezpieczne", 0, 50)
	if err != nil || len(results.Pages) != 1 || results.Pages[0].ID != root.ID {
		t.Fatalf("search results: %+v err=%v", results, err)
	}
	if _, err := service.Search(ctx, reader, space.ID, "wd", 0, 50); !errors.Is(err, knowledge.ErrInvalid) {
		t.Fatalf("short search must be rejected before scanning the corpus: %v", err)
	}
	publishedPage, err := service.Pages(ctx, admin, space.ID, knowledge.PagePublished, 0, 1)
	if err != nil || len(publishedPage.Pages) != 1 || publishedPage.Pages[0].ID != root.ID || publishedPage.NextAfterID != 0 {
		t.Fatalf("status filter must run before pagination: %+v err=%v", publishedPage, err)
	}
	if _, err := service.Pages(ctx, admin, space.ID, "UNKNOWN", 0, 1); !errors.Is(err, knowledge.ErrInvalid) {
		t.Fatalf("unsupported article status accepted: %v", err)
	}
	globalKey := "crossbase-" + suffix
	secondSpace, _, err := service.CreateSpace(ctx, admin, knowledge.SpaceInput{Key: "APPS", Name: "Aplikacje", Status: knowledge.SpaceActive}, "knowledge-space-apps")
	if err != nil {
		t.Fatal(err)
	}
	globalArticle, _, err := service.CreatePage(ctx, admin, secondSpace.ID, knowledge.PageInput{Title: "Integracja", Body: globalKey, Status: knowledge.PagePublished}, "knowledge-global-search")
	if err != nil {
		t.Fatal(err)
	}
	globalResults, err := service.Search(ctx, reader, 0, globalKey, 0, 50)
	if err != nil || len(globalResults.Pages) != 1 || globalResults.Pages[0].ID != globalArticle.ID || globalResults.Pages[0].SpaceID != secondSpace.ID || globalResults.Pages[0].SpaceName != secondSpace.Name {
		t.Fatalf("cross-base search results: %+v err=%v", globalResults, err)
	}
	foreignResults, err := service.Search(ctx, otherAdmin, 0, globalKey, 0, 50)
	if err != nil || len(foreignResults.Pages) != 0 {
		t.Fatalf("cross-tenant global search leaked results: %+v err=%v", foreignResults, err)
	}
	readerDraftResults, err := service.Search(ctx, reader, 0, secretSearchTerm, 0, 50)
	if err != nil || len(readerDraftResults.Pages) != 0 {
		t.Fatalf("global search exposed draft body to reader: %+v err=%v", readerDraftResults, err)
	}
	adminDraftResults, err := service.Search(ctx, admin, 0, secretSearchTerm, 0, 50)
	if err != nil || len(adminDraftResults.Pages) != 1 || adminDraftResults.Pages[0].ID != draft.ID {
		t.Fatalf("global search omitted writer-visible draft: %+v err=%v", adminDraftResults, err)
	}
	attention, err := service.Attention(ctx, admin, space.ID, 0, 50, "")
	if err != nil || len(attention.Pages) != 2 {
		t.Fatalf("attention results: %+v err=%v", attention, err)
	}
	expiring, err := service.Attention(ctx, admin, space.ID, 0, 1, knowledge.AttentionReviewDue)
	if err != nil || len(expiring.Pages) != 1 || expiring.Pages[0].ID != root.ID || expiring.NextAfterID != 0 {
		t.Fatalf("expiring filter must run before pagination: %+v err=%v", expiring, err)
	}
	if _, err := service.Attention(ctx, admin, space.ID, 0, 50, "UNKNOWN"); !errors.Is(err, knowledge.ErrInvalid) {
		t.Fatalf("unsupported attention filter accepted: %v", err)
	}
	if _, err := service.Subscribe(ctx, reader, space.ID, true); err != nil {
		t.Fatal(err)
	}
	subscribed, err := service.Space(ctx, reader, space.ID)
	if err != nil || !subscribed.Subscribed {
		t.Fatalf("subscription state not persisted: %+v err=%v", subscribed, err)
	}

	root.Body = "# Procedura\n\nNowa wersja"
	root, err = service.UpdatePage(ctx, admin, root.ID, knowledge.PageInput{Title: root.Title, Body: root.Body, Status: root.Status, ReviewDueAt: root.ReviewDueAt, Version: root.Version})
	if err != nil || root.Version != 2 {
		t.Fatalf("update page: %+v err=%v", root, err)
	}
	if _, err := service.UpdatePage(ctx, admin, root.ID, knowledge.PageInput{Title: root.Title, Body: root.Body, Status: root.Status, Version: 1}); !errors.Is(err, knowledge.ErrConflict) {
		t.Fatalf("stale update accepted: %v", err)
	}
	versions, err := service.Versions(ctx, admin, root.ID, 0, 20)
	if err != nil || len(versions.Versions) != 2 || versions.Versions[0].Version != 2 {
		t.Fatalf("version history: %+v err=%v", versions, err)
	}
	hiddenTitle := "SECRET-TITLE-" + suffix
	historyPage, _, err := service.CreatePage(ctx, admin, space.ID, knowledge.PageInput{Title: hiddenTitle, Body: "Internal draft", Status: knowledge.PageDraft}, "knowledge-private-history")
	if err != nil {
		t.Fatal(err)
	}
	historyPage, err = service.UpdatePage(ctx, admin, historyPage.ID, knowledge.PageInput{Title: "Public title", Body: "Published content", Status: knowledge.PagePublished, Version: historyPage.Version})
	if err != nil {
		t.Fatal(err)
	}
	readerVersions, err := service.Versions(ctx, reader, historyPage.ID, 0, 20)
	if err != nil || len(readerVersions.Versions) != 1 || readerVersions.Versions[0].Title != "Public title" {
		t.Fatalf("reader history exposed nonpublished revision: %+v err=%v", readerVersions, err)
	}
	adminVersions, err := service.Versions(ctx, admin, historyPage.ID, 0, 20)
	if err != nil || len(adminVersions.Versions) != 2 || adminVersions.Versions[1].Title != hiddenTitle {
		t.Fatalf("writer history omitted draft revision: %+v err=%v", adminVersions, err)
	}

	root, err = service.UpdatePage(ctx, admin, root.ID, knowledge.PageInput{Title: root.Title, Body: root.Body, Status: knowledge.PageRetired, Version: root.Version})
	if err != nil || root.Status != knowledge.PageRetired || root.ReviewDueAt != nil {
		t.Fatalf("retire article: %+v err=%v", root, err)
	}
	if _, err := service.Page(ctx, reader, root.ID); !errors.Is(err, knowledge.ErrNotFound) {
		t.Fatalf("retired article visible to reader: %v", err)
	}

	var auditJSON string
	if err := db.QueryRowContext(ctx, "SELECT CONCAT(COALESCE(GROUP_CONCAT(old_values),''),COALESCE(GROUP_CONCAT(new_values),'')) FROM audit_events WHERE tenant_id=? AND object_type IN ('knowledge_base','knowledge_article')", adminA.TenantID).Scan(&auditJSON); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(auditJSON, secret) {
		t.Fatal("page body leaked into audit events")
	}

	space.Status = knowledge.SpaceArchived
	space, err = service.UpdateSpace(ctx, admin, space.ID, knowledge.SpaceInput{Key: space.Key, Name: space.Name, Description: space.Description, Status: space.Status, ReviewIntervalDays: space.ReviewIntervalDays, Version: space.Version})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Space(ctx, reader, space.ID); !errors.Is(err, knowledge.ErrNotFound) {
		t.Fatalf("archived space visible to reader: %v", err)
	}
	if _, err := service.Space(ctx, admin, space.ID); err != nil {
		t.Fatalf("archived space hidden from manager: %v", err)
	}

	// The HTTP boundary supplies tenant/actor from the session and protects every mutation with CSRF.
	auth, err := identity.NewService(identities, tx, audits, cfg.Session, cfg.Auth)
	if err != nil {
		t.Fatal(err)
	}
	login := func(tenant, email string) identity.LoginResult {
		t.Helper()
		result, err := auth.Login(ctx, identity.LoginInput{Tenant: tenant, Email: email, Password: "knowledge-test-administrator"}, identity.RequestMeta{})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	adminLogin := login("knowledge-a-"+suffix, "knowledge-a-"+suffix+"@example.test")
	readerLogin := login("knowledge-a-"+suffix, "knowledge-reader-"+suffix+"@example.test")
	foreignLogin := login("knowledge-b-"+suffix, "knowledge-b-"+suffix+"@example.test")
	router := chi.NewRouter()
	knowledge.RegisterRoutes(router, knowledge.NewHandler(service, auth), identity.NewMiddleware(auth, cfg.Session))
	request := func(login identity.LoginResult, method, path, body string, csrf bool) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		req.Header.Set("Idempotency-Key", "knowledge-http-key")
		req.AddCookie(&http.Cookie{Name: cfg.Session.CookieName, Value: login.SessionToken})
		req.AddCookie(&http.Cookie{Name: cfg.Session.CSRFCookieName, Value: login.CSRFToken})
		if csrf {
			req.Header.Set("X-CSRF-Token", login.CSRFToken)
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		return response
	}
	if response := request(adminLogin, http.MethodGet, "/api/v1/knowledge/spaces?limit=50", "", false); response.Code != http.StatusOK {
		t.Fatalf("authenticated knowledge list: %d %s", response.Code, response.Body.String())
	}
	if response := request(readerLogin, http.MethodGet, "/api/v1/knowledge/search?limit=50&q="+globalKey, "", false); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), fmt.Sprintf(`"id":"%d"`, globalArticle.ID)) {
		t.Fatalf("authenticated cross-base knowledge search: %d %s", response.Code, response.Body.String())
	}
	if response := request(readerLogin, http.MethodGet, "/api/v1/knowledge/search?limit=50&q=ab", "", false); response.Code != http.StatusBadRequest {
		t.Fatalf("short full-text search was not rejected: %d %s", response.Code, response.Body.String())
	}
	if response := request(adminLogin, http.MethodGet, fmt.Sprintf("/api/v1/knowledge/spaces/%d/pages?status=UNKNOWN", space.ID), "", false); response.Code != http.StatusBadRequest {
		t.Fatalf("unsupported article state was not rejected: %d %s", response.Code, response.Body.String())
	}
	createBody := `{"key":"HTTP","name":"HTTP","description":"","status":"ACTIVE","review_interval_days":30}`
	if response := request(adminLogin, http.MethodPost, "/api/v1/knowledge/spaces", createBody, false); response.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF accepted: %d %s", response.Code, response.Body.String())
	}
	if response := request(readerLogin, http.MethodPost, "/api/v1/knowledge/spaces", createBody, true); response.Code != http.StatusForbidden {
		t.Fatalf("reader manage bypass: %d %s", response.Code, response.Body.String())
	}
	if response := request(foreignLogin, http.MethodGet, fmt.Sprintf("/api/v1/knowledge/spaces/%d", space.ID), "", false); response.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant HTTP read: %d %s", response.Code, response.Body.String())
	}
	spoofed := fmt.Sprintf(`{"tenant_id":"%d","key":"%s","name":"%s","description":"","status":"ACTIVE","review_interval_days":30,"version":"%d"}`, adminB.TenantID, space.Key, space.Name, space.Version)
	if response := request(adminLogin, http.MethodPut, fmt.Sprintf("/api/v1/knowledge/spaces/%d", space.ID), spoofed, true); response.Code != http.StatusBadRequest {
		t.Fatalf("spoofed tenant field accepted: %d %s", response.Code, response.Body.String())
	}
}
