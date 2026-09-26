//go:build integration

package integration

import (
	"context"
	"database/sql"
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
	"github.com/chmajster/OpenServiceNOW/internal/jobs"
	"github.com/chmajster/OpenServiceNOW/internal/platform/config"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
)

func TestPhase2WorkPlatform(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	db, err := database.OpenMySQL(ctx, cfg.MySQL)
	if err != nil {
		t.Fatalf("database.OpenMySQL() error = %v", err)
	}
	defer db.Close()

	suffix := fmt.Sprintf("%d", time.Now().UTC().UnixNano())
	tx := database.NewTxManager(db)
	auditRepo := audit.NewMySQLRepository()
	identityRepo := identity.NewMySQLRepository(db)
	bootstrap := identity.NewBootstrapper(identityRepo, tx, auditRepo)

	adminA, err := bootstrap.Run(ctx, identity.BootstrapInput{
		TenantSlug:    "phase2-a-" + suffix,
		TenantName:    "Phase 2 Tenant A",
		AdminEmail:    "phase2-a-" + suffix + "@example.test",
		AdminName:     "Phase 2 Admin A",
		AdminPassword: "phase2-integration-password-a",
	})
	if err != nil {
		t.Fatalf("bootstrap tenant A: %v", err)
	}
	adminB, err := bootstrap.Run(ctx, identity.BootstrapInput{
		TenantSlug:    "phase2-b-" + suffix,
		TenantName:    "Phase 2 Tenant B",
		AdminEmail:    "phase2-b-" + suffix + "@example.test",
		AdminName:     "Phase 2 Admin B",
		AdminPassword: "phase2-integration-password-b",
	})
	if err != nil {
		t.Fatalf("bootstrap tenant B: %v", err)
	}

	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		_, _ = db.ExecContext(
			cleanupCtx,
			"DELETE FROM audit_events WHERE tenant_id IN (?, ?)",
			adminA.TenantID,
			adminB.TenantID,
		)
		_, _ = db.ExecContext(
			cleanupCtx,
			"DELETE FROM tenants WHERE id IN (?, ?)",
			adminA.TenantID,
			adminB.TenantID,
		)
		_, _ = db.ExecContext(
			cleanupCtx,
			"DELETE FROM users WHERE id IN (?, ?)",
			adminA.UserID,
			adminB.UserID,
		)
	})

	t.Run("concurrent record numbering", func(t *testing.T) {
		allocator := workrecords.NewAllocator(tx)
		const count = 20

		type result struct {
			number string
			err    error
		}
		results := make(chan result, count)
		var wg sync.WaitGroup
		wg.Add(count)
		for index := 0; index < count; index++ {
			go func() {
				defer wg.Done()
				number, err := allocator.Next(ctx, adminA.TenantID, "incident", "INC", 7)
				results <- result{number: number, err: err}
			}()
		}
		wg.Wait()
		close(results)

		seen := make(map[string]struct{}, count)
		for item := range results {
			if item.err != nil {
				t.Fatalf("allocator.Next() error = %v", item.err)
			}
			if !strings.HasPrefix(item.number, "INC") {
				t.Fatalf("unexpected number %q", item.number)
			}
			if _, duplicate := seen[item.number]; duplicate {
				t.Fatalf("duplicate record number %q", item.number)
			}
			seen[item.number] = struct{}{}
		}
		if len(seen) != count {
			t.Fatalf("allocated %d unique numbers, want %d", len(seen), count)
		}

		var nextValue uint64
		if err := db.QueryRowContext(ctx, `
			SELECT next_value
			FROM record_sequences
			WHERE tenant_id = ? AND record_type = 'incident'
		`, adminA.TenantID).Scan(&nextValue); err != nil {
			t.Fatalf("read record sequence: %v", err)
		}
		if nextValue != count+1 {
			t.Fatalf("next_value = %d, want %d", nextValue, count+1)
		}

		if _, err := allocator.Next(ctx, adminA.TenantID, "incident", "TKT", 7); !errors.Is(err, workrecords.ErrSequenceConfigurationConflict) {
			t.Fatalf("sequence reconfiguration error = %v", err)
		}
	})

	t.Run("durable job retry and tenant isolation", func(t *testing.T) {
		repo := jobs.NewRepository(db)
		creator := adminA.UserID
		input := jobs.EnqueueInput{
			TenantID:       adminA.TenantID,
			Type:           "integration.echo",
			Payload:        json.RawMessage(`{"value":"phase2"}`),
			MaxAttempts:    2,
			IdempotencyKey: "phase2-job-" + suffix,
			CreatedBy:      &creator,
		}
		first, created, err := repo.Enqueue(ctx, input)
		if err != nil {
			t.Fatalf("enqueue job: %v", err)
		}
		if !created {
			t.Fatal("first idempotent enqueue must create the job")
		}
		wrongCreator := adminB.UserID
		wrongInput := input
		wrongInput.IdempotencyKey = "phase2-job-wrong-creator-" + suffix
		wrongInput.CreatedBy = &wrongCreator
		if _, _, err := repo.Enqueue(ctx, wrongInput); !errors.Is(err, jobs.ErrInvalid) {
			t.Fatalf("cross-tenant job creator error = %v", err)
		}

		second, created, err := repo.Enqueue(ctx, input)
		if err != nil {
			t.Fatalf("enqueue duplicate job: %v", err)
		}
		if created || second.ID != first.ID {
			t.Fatalf("duplicate enqueue created=%v id=%d want id=%d", created, second.ID, first.ID)
		}
		if _, err := repo.Get(ctx, adminB.TenantID, first.ID); !errors.Is(err, jobs.ErrNotFound) {
			t.Fatalf("cross-tenant job get error = %v", err)
		}

		claimed, ok, err := repo.Claim(ctx, "integration-worker-a", 5*time.Second)
		if err != nil || !ok {
			t.Fatalf("claim first attempt ok=%v err=%v", ok, err)
		}
		if claimed.ID != first.ID || claimed.AttemptCount != 1 || claimed.Status != jobs.StatusRunning {
			t.Fatalf("first claim = %+v", claimed)
		}
		if err := repo.AppendLog(ctx, claimed.ID, "integration-worker-a", "WARN", "first attempt will retry", json.RawMessage(`{"attempt":1}`)); err != nil {
			t.Fatalf("append first job log: %v", err)
		}
		if err := repo.Fail(ctx, claimed.ID, "integration-worker-a", errors.New("retry me"), 0); err != nil {
			t.Fatalf("fail first attempt: %v", err)
		}

		retrying, err := repo.Get(ctx, adminA.TenantID, first.ID)
		if err != nil {
			t.Fatalf("get retrying job: %v", err)
		}
		if retrying.Status != jobs.StatusRetrying || retrying.AttemptCount != 1 {
			t.Fatalf("retrying job = %+v", retrying)
		}

		claimedAgain, ok, err := repo.Claim(ctx, "integration-worker-b", 5*time.Second)
		if err != nil || !ok {
			t.Fatalf("claim second attempt ok=%v err=%v", ok, err)
		}
		if claimedAgain.ID != first.ID || claimedAgain.AttemptCount != 2 {
			t.Fatalf("second claim = %+v", claimedAgain)
		}
		if err := repo.AppendLog(ctx, claimedAgain.ID, "integration-worker-b", "INFO", "second attempt succeeds", nil); err != nil {
			t.Fatalf("append second job log: %v", err)
		}
		if err := repo.Complete(
			ctx,
			claimedAgain.ID,
			"integration-worker-b",
			json.RawMessage(`{"ok":true}`),
		); err != nil {
			t.Fatalf("complete job: %v", err)
		}

		completed, err := repo.Get(ctx, adminA.TenantID, first.ID)
		if err != nil {
			t.Fatalf("get completed job: %v", err)
		}
		if completed.Status != jobs.StatusCompleted || completed.Progress != 100 || completed.AttemptCount != 2 {
			t.Fatalf("completed job = %+v", completed)
		}

		var attemptCount int
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM job_attempts WHERE job_id = ?", first.ID).Scan(&attemptCount); err != nil {
			t.Fatalf("count job attempts: %v", err)
		}
		if attemptCount != 2 {
			t.Fatalf("job attempts = %d, want 2", attemptCount)
		}
		logPage, err := repo.ListLogs(ctx, adminA.TenantID, first.ID, 0, 50)
		if err != nil {
			t.Fatalf("list job logs: %v", err)
		}
		if len(logPage.Entries) != 2 {
			t.Fatalf("job logs = %+v", logPage.Entries)
		}
		if _, err := repo.ListLogs(ctx, adminB.TenantID, first.ID, 0, 50); err != nil {
			t.Fatalf("cross-tenant log list should be empty, got error: %v", err)
		}
		wrongLogs, _ := repo.ListLogs(ctx, adminB.TenantID, first.ID, 0, 50)
		if len(wrongLogs.Entries) != 0 {
			t.Fatalf("tenant B saw tenant A job logs: %+v", wrongLogs.Entries)
		}
	})

	t.Run("outbox rollback and activity projection", func(t *testing.T) {
		eventRepo := events.NewRepository(db)
		rollbackKey := "rollback-" + suffix
		expected := errors.New("force rollback")
		err := tx.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(exec database.Executor) error {
			_, _, err := eventRepo.Append(ctx, exec, events.Input{
				TenantID:       adminA.TenantID,
				Type:           "integration.rollback",
				AggregateType:  "integration",
				AggregateID:    suffix,
				Payload:        json.RawMessage(`{"rollback":true}`),
				IdempotencyKey: rollbackKey,
			})
			if err != nil {
				return err
			}
			return expected
		})
		if !errors.Is(err, expected) {
			t.Fatalf("rollback transaction error = %v", err)
		}
		var rollbackCount int
		if err := db.QueryRowContext(ctx, `
			SELECT COUNT(*)
			FROM outbox_events
			WHERE tenant_id = ? AND idempotency_key = ?
		`, adminA.TenantID, rollbackKey).Scan(&rollbackCount); err != nil {
			t.Fatalf("count rolled back outbox event: %v", err)
		}
		if rollbackCount != 0 {
			t.Fatalf("rolled back outbox rows = %d", rollbackCount)
		}

		workRepo := workrecords.NewRepository(db)
		service := workrecords.NewService(workRepo, tx, eventRepo)
		message, err := service.AppendMessage(
			ctx,
			workrecords.Actor{TenantID: adminA.TenantID, UserID: adminA.UserID},
			"incident",
			"INC-PHASE2-"+suffix,
			workrecords.MessageComment,
			"integration comment",
		)
		if err != nil {
			t.Fatalf("append work message: %v", err)
		}

		tenantBPage, err := workRepo.ListMessages(
			ctx,
			adminB.TenantID,
			"incident",
			"INC-PHASE2-"+suffix,
			0,
			50,
		)
		if err != nil {
			t.Fatalf("list tenant B messages: %v", err)
		}
		if len(tenantBPage.Messages) != 0 {
			t.Fatalf("tenant B saw tenant A message: %+v", tenantBPage.Messages)
		}

		activityRepo := activity.NewRepository(db)
		projector := activity.NewProjector(tx, activityRepo)
		dispatcher, err := events.NewDispatcher(
			eventRepo,
			slog.New(slog.NewJSONHandler(io.Discard, nil)),
			"integration-dispatcher",
			1,
			50*time.Millisecond,
			5*time.Second,
			0,
		)
		if err != nil {
			t.Fatalf("create dispatcher: %v", err)
		}
		if err := dispatcher.Register("work.comment.added", projector.Handler("comment", "Comment added")); err != nil {
			t.Fatalf("register projection: %v", err)
		}
		processed, err := dispatcher.RunOnce(ctx)
		if err != nil {
			t.Fatalf("dispatch event: %v", err)
		}
		if !processed {
			t.Fatal("expected one outbox event to be dispatched")
		}

		page, err := activityRepo.List(
			ctx,
			adminA.TenantID,
			"incident",
			"INC-PHASE2-"+suffix,
			0,
			50,
		)
		if err != nil {
			t.Fatalf("list activity: %v", err)
		}
		if len(page.Entries) != 1 {
			t.Fatalf("activity entries = %+v", page.Entries)
		}
		entry := page.Entries[0]
		if entry.Summary != "Comment added" || entry.ActorUserID == nil || *entry.ActorUserID != adminA.UserID {
			t.Fatalf("projected activity = %+v", entry)
		}
		if entry.SourceEventID == nil {
			t.Fatal("projected activity is not linked to source outbox event")
		}
		var eventVersion uint32
		if err := db.QueryRowContext(ctx, "SELECT event_version FROM outbox_events WHERE id = ?", *entry.SourceEventID).Scan(&eventVersion); err != nil {
			t.Fatalf("read source event version: %v", err)
		}
		if eventVersion != 1 {
			t.Fatalf("source event version = %d, want 1", eventVersion)
		}

		messages, err := workRepo.ListMessages(
			ctx,
			adminA.TenantID,
			"incident",
			"INC-PHASE2-"+suffix,
			0,
			50,
		)
		if err != nil {
			t.Fatalf("list work messages: %v", err)
		}
		if len(messages.Messages) != 1 || messages.Messages[0].ID != message.ID {
			t.Fatalf("work messages = %+v", messages.Messages)
		}
	})

	t.Run("assignment watcher and relation primitives", func(t *testing.T) {
		eventRepo := events.NewRepository(db)
		workRepo := workrecords.NewRepository(db)
		service := workrecords.NewService(workRepo, tx, eventRepo)
		actor := workrecords.Actor{TenantID: adminA.TenantID, UserID: adminA.UserID}
		objectID := "INC-WORK-" + suffix

		assignment, err := service.SetAssignment(
			ctx,
			actor,
			"incident",
			objectID,
			0,
			&adminA.UserID,
			nil,
		)
		if err != nil {
			t.Fatalf("create assignment: %v", err)
		}
		if assignment.Version != 1 || assignment.AssignedTo == nil || *assignment.AssignedTo != adminA.UserID {
			t.Fatalf("assignment = %+v", assignment)
		}
		noOp, err := service.SetAssignment(
			ctx,
			actor,
			"incident",
			objectID,
			assignment.Version,
			&adminA.UserID,
			nil,
		)
		if err != nil {
			t.Fatalf("idempotent assignment update: %v", err)
		}
		if noOp.Version != assignment.Version {
			t.Fatalf("idempotent assignment changed version: before=%d after=%d", assignment.Version, noOp.Version)
		}
		if _, err := service.SetAssignment(
			ctx,
			actor,
			"incident",
			objectID,
			assignment.Version+1,
			nil,
			nil,
		); !errors.Is(err, workrecords.ErrConflict) {
			t.Fatalf("stale assignment update error = %v", err)
		}

		added, err := service.AddWatcher(ctx, actor, "incident", objectID, adminA.UserID)
		if err != nil || !added {
			t.Fatalf("add watcher added=%v err=%v", added, err)
		}
		watchers, err := workRepo.ListWatchers(ctx, adminA.TenantID, "incident", objectID)
		if err != nil {
			t.Fatalf("list watchers: %v", err)
		}
		if len(watchers.Watchers) != 1 || watchers.Watchers[0].UserID != adminA.UserID {
			t.Fatalf("watchers = %+v", watchers.Watchers)
		}
		removed, err := service.RemoveWatcher(ctx, actor, "incident", objectID, adminA.UserID)
		if err != nil || !removed {
			t.Fatalf("remove watcher removed=%v err=%v", removed, err)
		}
		readded, err := service.AddWatcher(ctx, actor, "incident", objectID, adminA.UserID)
		if err != nil || !readded {
			t.Fatalf("re-add watcher added=%v err=%v", readded, err)
		}

		relation, created, err := service.AddRelation(
			ctx,
			actor,
			"incident",
			objectID,
			"depends_on",
			"configuration_item",
			"CI-"+suffix,
		)
		if err != nil || !created {
			t.Fatalf("add relation created=%v err=%v", created, err)
		}
		relations, err := workRepo.ListRelations(ctx, adminA.TenantID, "incident", objectID, 0, 50)
		if err != nil {
			t.Fatalf("list relations: %v", err)
		}
		if len(relations.Relations) != 1 || relations.Relations[0].ID != relation.ID {
			t.Fatalf("relations = %+v", relations.Relations)
		}
		relationRemoved, err := service.RemoveRelation(ctx, actor, relation.ID)
		if err != nil || !relationRemoved {
			t.Fatalf("remove relation removed=%v err=%v", relationRemoved, err)
		}
		relationRemoved, err = service.RemoveRelation(ctx, actor, relation.ID)
		if err != nil || relationRemoved {
			t.Fatalf("second relation removal removed=%v err=%v", relationRemoved, err)
		}
	})

	t.Run("attachment tenant isolation and resumable delete", func(t *testing.T) {
		storage, err := attachments.NewLocalStorage(t.TempDir())
		if err != nil {
			t.Fatalf("create attachment storage: %v", err)
		}
		metadata := attachments.NewRepository(db)
		service, err := attachments.NewService(metadata, storage, 1024)
		if err != nil {
			t.Fatalf("create attachment service: %v", err)
		}
		uploader := adminA.UserID
		item, err := service.Upload(ctx, attachments.CreateInput{
			TenantID:    adminA.TenantID,
			ObjectType:  "incident",
			ObjectID:    "INC-ATTACH-"+suffix,
			FileName:    " note.txt ",
			UploadedBy:  &uploader,
		}, strings.NewReader("attachment-body"))
		if err != nil {
			t.Fatalf("upload attachment: %v", err)
		}
		if item.FileName != "note.txt" || item.SizeBytes != uint64(len("attachment-body")) {
			t.Fatalf("attachment metadata = %+v", item)
		}

		if _, _, err := service.Open(ctx, adminB.TenantID, item.ID); !errors.Is(err, attachments.ErrNotFound) {
			t.Fatalf("cross-tenant attachment open error = %v", err)
		}

		_, reader, err := service.Open(ctx, adminA.TenantID, item.ID)
		if err != nil {
			t.Fatalf("open attachment: %v", err)
		}
		data, readErr := io.ReadAll(reader)
		_ = reader.Close()
		if readErr != nil {
			t.Fatalf("read attachment: %v", readErr)
		}
		if string(data) != "attachment-body" {
			t.Fatalf("attachment body = %q", data)
		}

		wrongUploader := adminB.UserID
		if _, err := service.Upload(ctx, attachments.CreateInput{
			TenantID:   adminA.TenantID,
			ObjectType: "incident",
			ObjectID:   "INC-ATTACH-"+suffix,
			FileName:   "foreign.txt",
			UploadedBy: &wrongUploader,
		}, strings.NewReader("foreign")); !errors.Is(err, attachments.ErrNotFound) {
			t.Fatalf("cross-tenant uploader error = %v", err)
		}

		deleting, err := metadata.BeginDelete(ctx, adminA.TenantID, item.ID)
		if err != nil {
			t.Fatalf("begin delete: %v", err)
		}
		if deleting.Status != attachments.StatusDeleting {
			t.Fatalf("deleting attachment status = %s", deleting.Status)
		}
		if err := service.Delete(ctx, adminA.TenantID, item.ID); err != nil {
			t.Fatalf("resume delete: %v", err)
		}
		if _, _, err := service.Open(ctx, adminA.TenantID, item.ID); !errors.Is(err, attachments.ErrNotFound) {
			t.Fatalf("open deleted attachment error = %v", err)
		}
	})
}
