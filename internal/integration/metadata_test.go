//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/chmajster/OpenServiceNOW/internal/audit"
	"github.com/chmajster/OpenServiceNOW/internal/identity"
	"github.com/chmajster/OpenServiceNOW/internal/metadata"
	"github.com/chmajster/OpenServiceNOW/internal/platform/config"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
)

func TestMetadataTenantIsolationReferenceACLAndConcurrentDrafts(t *testing.T) {
	cfg, err := config.Load()
	if err != nil { t.Fatal(err) }
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	db, err := database.OpenMySQL(ctx, cfg.MySQL)
	if err != nil { t.Fatal(err) }
	defer db.Close()
	tx := database.NewTxManager(db)
	identityRepo := identity.NewMySQLRepository(db)
	bootstrap := identity.NewBootstrapper(identityRepo, tx, audit.NewMySQLRepository())
	suffix := fmt.Sprint(time.Now().UnixNano())
	adminA, err := bootstrap.Run(ctx, identity.BootstrapInput{TenantSlug: "metadata-a-" + suffix, TenantName: "Metadata A", AdminEmail: "metadata-a-" + suffix + "@example.test", AdminName: "Metadata admin A", AdminPassword: "metadata-integration-password"})
	if err != nil { t.Fatal(err) }
	adminB, err := bootstrap.Run(ctx, identity.BootstrapInput{TenantSlug: "metadata-b-" + suffix, TenantName: "Metadata B", AdminEmail: "metadata-b-" + suffix + "@example.test", AdminName: "Metadata admin B", AdminPassword: "metadata-integration-password"})
	if err != nil { t.Fatal(err) }
	userIDs := []uint64{adminA.UserID, adminB.UserID}
	t.Cleanup(func() {
		for _, tenantID := range []uint64{adminA.TenantID, adminB.TenantID} {
			_, _ = db.ExecContext(context.Background(), "DELETE FROM audit_events WHERE tenant_id=?", tenantID)
			if _, cleanupErr := db.ExecContext(context.Background(), "DELETE FROM tenants WHERE id=?", tenantID); cleanupErr != nil { t.Error(cleanupErr) }
		}
		for _, userID := range userIDs {
			if _, cleanupErr := db.ExecContext(context.Background(), "DELETE FROM users WHERE id=?", userID); cleanupErr != nil { t.Error(cleanupErr) }
		}
	})
	makeActor := func(tenantID, userID uint64) metadata.Actor {
		permissions, permissionErr := identityRepo.ListPermissions(ctx, tenantID, userID)
		if permissionErr != nil { t.Fatal(permissionErr) }
		keys := map[string]bool{}
		for _, key := range permissions { keys[key] = true }
		return metadata.Actor{TenantID: tenantID, UserID: userID, RequestID: "metadata-integration", Permissions: keys}
	}
	actorA, actorB := makeActor(adminA.TenantID, adminA.UserID), makeActor(adminB.TenantID, adminB.UserID)
	service := metadata.NewService(db)
	initial, err := service.Head(ctx, actorA)
	if err != nil { t.Fatal(err) }
	foreignVersion, err := service.Head(ctx, actorB)
	if err != nil { t.Fatal(err) }

	configuration := initial.Draft
	configuration.Organization.DefaultLocale = "en"
	configuration.RecordTypes["ci"].Fields = append(configuration.RecordTypes["ci"].Fields, metadata.Field{
		Key: "linked_user", Labels: map[string]string{"pl": "Powiązana osoba", "en": "Linked user"},
		Type: metadata.TypeReference, ReferenceType: "user", Active: true,
	})
	configuration.RecordTypes["ci"].Form.Sections = []metadata.FormSection{{Key: "custom", Label: "Dodatkowe", Labels: map[string]string{"pl": "Dodatkowe", "en": "Additional"}, Fields: []string{"linked_user"}, Columns: 2}}
	configuration.RecordTypes["ci"].List.Columns = append(configuration.RecordTypes["ci"].List.Columns, "linked_user")
	configuration.RecordTypes["ci"].List.Filters = append(configuration.RecordTypes["ci"].List.Filters, "linked_user")

	type draftResult struct { head metadata.Head; err error }
	start := make(chan struct{})
	results := make(chan draftResult, 2)
	var wait sync.WaitGroup
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			head, saveErr := service.SaveDraft(ctx, actorA, initial.Version, configuration)
			results <- draftResult{head: head, err: saveErr}
		}()
	}
	close(start)
	wait.Wait()
	close(results)
	wins, conflicts := 0, 0
	var saved metadata.Head
	for result := range results {
		switch {
		case result.err == nil:
			wins++
			saved = result.head
		case errors.Is(result.err, metadata.ErrConflict):
			conflicts++
		default:
			t.Fatalf("concurrent metadata draft save: %v", result.err)
		}
	}
	if wins != 1 || conflicts != 1 { t.Fatalf("draft wins=%d conflicts=%d", wins, conflicts) }
	if _, err := service.SaveDraft(ctx, actorA, initial.Version, configuration); !errors.Is(err, metadata.ErrConflict) {
		t.Fatalf("stale draft update error=%v", err)
	}
	result, err := db.ExecContext(ctx, "INSERT INTO users(email,display_name,password_hash) SELECT ?,?,password_hash FROM users WHERE id=?", "metadata-reader-"+suffix+"@example.test", "Metadata reader", adminA.UserID)
	if err != nil { t.Fatal(err) }
	limitedID, err := result.LastInsertId()
	if err != nil { t.Fatal(err) }
	userIDs = append(userIDs, uint64(limitedID))
	if _, err := db.ExecContext(ctx, "INSERT INTO tenant_memberships(tenant_id,user_id) VALUES(?,?)", adminA.TenantID, limitedID); err != nil { t.Fatal(err) }
	if _, err := service.SaveDraft(ctx, metadata.Actor{TenantID: adminA.TenantID, UserID: uint64(limitedID)}, saved.Version, configuration); !errors.Is(err, workrecords.ErrForbidden) {
		t.Fatalf("unauthorized draft update error=%v", err)
	}
	_, impact, err := service.Publish(ctx, actorA, saved.Version)
	if err != nil { t.Fatal(err) }
	if !impact.CanPublish { t.Fatalf("optional field unexpectedly blocks publication: %+v", impact) }
	schemaA, err := service.Schema(ctx, adminA.TenantID, "ci")
	if err != nil { t.Fatal(err) }
	if _, err := service.ConfigurationAtVersion(ctx, adminB.TenantID, schemaA.Version); err == nil {
		t.Fatal("another tenant's configuration version must not be visible")
	}
	schemaB, err := service.Schema(ctx, adminB.TenantID, "ci")
	if err != nil { t.Fatal(err) }
	for _, field := range schemaB.Fields { if field.Key == "linked_user" { t.Fatal("tenant B received tenant A's field definition") } }
	foreignReference, _ := json.Marshal(fmt.Sprint(adminB.UserID))
	_, _, err = service.PrepareValues(ctx, db, actorA, "ci", 0, map[string]json.RawMessage{"linked_user": foreignReference}, nil, schemaA.Version)
	var fieldError metadata.FieldError
	if !errors.As(err, &fieldError) || fieldError.Code != "REFERENCE_UNAVAILABLE" {
		t.Fatalf("foreign-tenant reference error=%v", err)
	}
	if _, err := service.SetPreferences(ctx, actorA, 0, "en", "Europe/London"); err != nil { t.Fatal(err) }
	if _, err := service.SetPreferences(ctx, actorA, 0, "pl", ""); !errors.Is(err, metadata.ErrConflict) {
		t.Fatalf("repeated stale preference update error=%v", err)
	}
	prefsA, err := service.GetPreferences(ctx, actorA)
	if err != nil { t.Fatal(err) }
	prefsB, err := service.GetPreferences(ctx, actorB)
	if err != nil { t.Fatal(err) }
	if prefsA.Locale != "en" || prefsA.TimeZone != "Europe/London" || prefsA.OrganizationName != "Metadata A" || prefsA.BrandColor == "" || prefsB.Locale != "" || prefsB.DefaultLocale != "pl" || prefsB.OrganizationName != "Metadata B" || foreignVersion.Version != 1 {
		t.Fatalf("tenant preferences/config leaked: A=%+v B=%+v initial B version=%d", prefsA, prefsB, foreignVersion.Version)
	}
}
