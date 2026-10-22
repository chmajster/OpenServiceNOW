//go:build integration

package integration

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/chmajster/OpenServiceNOW/internal/audit"
	"github.com/chmajster/OpenServiceNOW/internal/identity"
	"github.com/chmajster/OpenServiceNOW/internal/knowledge"
	"github.com/chmajster/OpenServiceNOW/internal/platform/config"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
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
	secret := "SECRET-BODY-" + suffix
	draft, _, err := service.CreatePage(ctx, admin, space.ID, knowledge.PageInput{Title: "Szkic", Body: secret, Status: knowledge.PageDraft}, "knowledge-draft")
	if err != nil {
		t.Fatal(err)
	}
	past := time.Now().UTC().Add(-time.Hour)
	root, _, err := service.CreatePage(ctx, admin, space.ID, knowledge.PageInput{Title: "Wdrożenie", Body: "# Procedura\n\nBezpieczne wdrożenie", Status: knowledge.PagePublished, ReviewDueAt: &past}, "knowledge-published")
	if err != nil {
		t.Fatal(err)
	}
	child, _, err := service.CreatePage(ctx, admin, space.ID, knowledge.PageInput{ParentID: &root.ID, Title: "Rollback", Body: "Wycofaj zmianę", Status: knowledge.PagePublished}, "knowledge-child")
	if err != nil {
		t.Fatal(err)
	}

	readerSpaces, err := service.Spaces(ctx, reader, 0, 50)
	if err != nil || len(readerSpaces.Spaces) != 1 || readerSpaces.Spaces[0].PageCount != 2 {
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
	if _, err := service.Attention(ctx, reader, space.ID, 0, 50); !errors.Is(err, knowledge.ErrForbidden) {
		t.Fatalf("reader attention bypass: %v", err)
	}
	if _, err := service.Space(ctx, otherAdmin, space.ID); !errors.Is(err, knowledge.ErrNotFound) {
		t.Fatalf("cross-tenant read: %v", err)
	}

	results, err := service.Search(ctx, reader, space.ID, "bezpieczne", 0, 50)
	if err != nil || len(results.Pages) != 1 || results.Pages[0].ID != root.ID {
		t.Fatalf("search results: %+v err=%v", results, err)
	}
	attention, err := service.Attention(ctx, admin, space.ID, 0, 50)
	if err != nil || len(attention.Pages) != 2 {
		t.Fatalf("attention results: %+v err=%v", attention, err)
	}
	if _, err := service.Watch(ctx, reader, space.ID, true); err != nil {
		t.Fatal(err)
	}
	watched, err := service.Space(ctx, reader, space.ID)
	if err != nil || !watched.Watched {
		t.Fatalf("watch state not persisted: %+v err=%v", watched, err)
	}

	root.Body = "# Procedura\n\nNowa wersja"
	root, err = service.UpdatePage(ctx, admin, root.ID, knowledge.PageInput{Title: root.Title, Body: root.Body, Status: root.Status, ReviewDueAt: root.ReviewDueAt, Version: root.Version})
	if err != nil || root.Version != 2 {
		t.Fatalf("update page: %+v err=%v", root, err)
	}
	if _, err := service.UpdatePage(ctx, admin, root.ID, knowledge.PageInput{Title: root.Title, Body: root.Body, Status: root.Status, Version: 1}); !errors.Is(err, knowledge.ErrConflict) {
		t.Fatalf("stale update accepted: %v", err)
	}
	if _, err := service.UpdatePage(ctx, admin, root.ID, knowledge.PageInput{ParentID: &child.ID, Title: root.Title, Body: root.Body, Status: root.Status, Version: root.Version}); !errors.Is(err, knowledge.ErrInvalid) {
		t.Fatalf("hierarchy cycle accepted: %v", err)
	}
	versions, err := service.Versions(ctx, admin, root.ID, 0, 20)
	if err != nil || len(versions.Versions) != 2 || versions.Versions[0].Version != 2 {
		t.Fatalf("version history: %+v err=%v", versions, err)
	}

	archive, _, err := service.Export(ctx, admin, space.ID)
	if err != nil || len(archive) == 0 {
		t.Fatalf("export: bytes=%d err=%v", len(archive), err)
	}
	target, _, err := service.CreateSpace(ctx, admin, knowledge.SpaceInput{Key: "COPY", Name: "Kopia", Status: knowledge.SpaceActive}, "knowledge-target")
	if err != nil {
		t.Fatal(err)
	}
	imported, err := service.Import(ctx, admin, target.ID, archive, "knowledge-import")
	if err != nil || imported.Created != 3 || imported.Replayed {
		t.Fatalf("import: %+v err=%v", imported, err)
	}
	retry, err := service.Import(ctx, admin, target.ID, archive, "knowledge-import")
	if err != nil || !retry.Replayed || retry.Created != 0 {
		t.Fatalf("import retry: %+v err=%v", retry, err)
	}
	copyPages, err := service.Pages(ctx, admin, target.ID, 0, 50)
	if err != nil || len(copyPages.Pages) != 3 {
		t.Fatalf("imported pages: %+v err=%v", copyPages, err)
	}

	var auditJSON string
	if err := db.QueryRowContext(ctx, "SELECT CONCAT(COALESCE(GROUP_CONCAT(old_values),''),COALESCE(GROUP_CONCAT(new_values),'')) FROM audit_events WHERE tenant_id=? AND object_type IN ('knowledge_space','knowledge_page')", adminA.TenantID).Scan(&auditJSON); err != nil {
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
}
