package changes

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/chmajster/OpenServiceNOW/internal/activity"
	"github.com/chmajster/OpenServiceNOW/internal/attachments"
	"github.com/chmajster/OpenServiceNOW/internal/catalog"
	"github.com/chmajster/OpenServiceNOW/internal/events"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/problems"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
	"reflect"
	"time"
)

type Service struct {
	db        *sql.DB
	tx        *database.TxManager
	work      *workrecords.Repository
	allocator *workrecords.Allocator
	files     *attachments.Service
	Problems  *problems.Service
}

func NewService(db *sql.DB, files *attachments.Service, problemsService *problems.Service) *Service {
	tx := database.NewTxManager(db)
	return &Service{db: db, tx: tx, work: workrecords.NewRepository(db), allocator: workrecords.NewAllocator(tx), files: files, Problems: problemsService}
}
func Access(a Actor, alias string) (string, []any) {
	if a.Has("change.read") {
		return "1=1", nil
	}
	if !a.Has("change.read_own") {
		return "1=0", nil
	}
	return "(" + alias + ".reporter_id=? OR wa.assigned_to=? OR EXISTS(SELECT 1 FROM group_memberships gm JOIN `groups` ag ON ag.tenant_id=gm.tenant_id AND ag.id=gm.group_id AND ag.active=TRUE WHERE gm.tenant_id=" + alias + ".tenant_id AND gm.group_id=wa.assignment_group_id AND gm.user_id=?))", []any{a.UserID, a.UserID, a.UserID}
}

const joins = " FROM changes p JOIN work_assignments wa ON wa.tenant_id=p.tenant_id AND wa.object_type='change' AND wa.object_id=CAST(p.id AS CHAR) JOIN users u ON u.id=p.reporter_id LEFT JOIN users au ON au.id=wa.assigned_to LEFT JOIN `groups` g ON g.tenant_id=wa.tenant_id AND g.id=wa.assignment_group_id"
const columns = "p.id,p.number,p.reporter_id,u.display_name,p.title,p.description,p.change_type,p.impact,p.risk,p.justification,p.scope,p.implementation_plan,p.test_plan,p.rollback_plan,p.emergency_reason,p.review_result,p.review_outcome,p.planned_start,p.planned_end,p.time_zone,p.workflow_id,p.workflow_version,p.template_id,p.template_version,p.assessment_revision,p.approved_revision,p.evaluated_version,p.execution_id,p.approval_state,p.actual_start,p.actual_end,p.state,p.reason,p.version,p.created_at,p.updated_at,wa.assigned_to,wa.assignment_group_id,COALESCE(au.display_name,''),COALESCE(g.name,'')"

func scan(row interface{ Scan(...any) error }, a Actor) (Record, error) {
	var r Record
	err := row.Scan(&r.ID, &r.Number, &r.ReporterID, &r.ReporterName, &r.Title, &r.Description, &r.Type, &r.Impact, &r.Risk, &r.Justification, &r.Scope, &r.ImplementationPlan, &r.TestPlan, &r.RollbackPlan, &r.EmergencyReason, &r.ReviewResult, &r.ReviewOutcome, &r.PlannedStart, &r.PlannedEnd, &r.TimeZone, &r.WorkflowID, &r.WorkflowVersion, &r.TemplateID, &r.TemplateVersion, &r.AssessmentRevision, &r.ApprovedRevision, &r.EvaluatedVersion, &r.ExecutionID, &r.ApprovalState, &r.ActualStart, &r.ActualEnd, &r.State, &r.Reason, &r.Version, &r.CreatedAt, &r.UpdatedAt, &r.AssignedTo, &r.GroupID, &r.AssignedName, &r.GroupName)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	r.CanEdit = a.Has("change.manage") && editable(r.State)
	r.CanAssign = r.CanEdit && a.Has("change.assign")
	r.CanTasks = a.Has("change.tasks") && contains([]string{"NEW", "ASSESS", "AUTHORIZE", "SCHEDULED", "IMPLEMENT"}, r.State)
	r.Actions = actions(a, r)
	return r, err
}

func (s *Service) get(ctx context.Context, e database.Executor, a Actor, id uint64, lock bool) (Record, error) {
	p, args := Access(a, "p")
	q := "SELECT " + columns + joins + " WHERE p.tenant_id=? AND p.id=? AND " + p
	if lock {
		q += " FOR UPDATE"
	}
	return scan(e.QueryRowContext(ctx, q, append([]any{a.TenantID, id}, args...)...), a)
}
func (s *Service) Get(ctx context.Context, a Actor, id uint64) (Record, error) {
	return s.get(ctx, s.db, a, id, false)
}
func (s *Service) actorTx(ctx context.Context, a Actor, fn func(database.Executor, Actor) error) error {
	return s.tx.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(e database.Executor) error {
		current, err := workrecords.LockPrincipal(ctx, e, a)
		if err != nil {
			return err
		}
		return fn(e, current)
	})
}
func (s *Service) withChange(ctx context.Context, a Actor, id uint64, fn func(database.Executor, Actor, Record) error) error {
	return s.actorTx(ctx, a, func(e database.Executor, a Actor) error {
		r, err := s.get(ctx, e, a, id, true)
		if err != nil {
			return err
		}
		return fn(e, a, r)
	})
}
func (s *Service) event(ctx context.Context, e database.Executor, a Actor, r Record, kind, visibility, key string, data map[string]any) error {
	data["actor_user_id"] = fmt.Sprint(a.UserID)
	data["occurred_at"] = time.Now().UTC()
	data["change_version"] = fmt.Sprint(r.Version)
	data["visibility"] = visibility
	if err := catalog.Audit(ctx, e, a, kind, "change", r.ID, map[string]any{"state": r.State, "version": fmt.Sprint(r.Version), "assessment_revision": fmt.Sprint(r.AssessmentRevision), "approval_state": r.ApprovalState, "visibility": visibility, "resource_id": data["resource_id"], "fields": data["fields"]}); err != nil {
		return err
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, _, err = events.NewRepository(s.db).Append(ctx, e, events.Input{TenantID: a.TenantID, Type: kind, AggregateType: "change", AggregateID: fmt.Sprint(r.ID), Payload: raw, IdempotencyKey: key})
	return err
}
func (s *Service) touch(ctx context.Context, e database.Executor, a Actor, r Record, field string) (Record, error) {
	result, err := e.ExecContext(ctx, "UPDATE changes SET version=version+1,updated_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND id=? AND version=?", a.TenantID, r.ID, r.Version)
	if err != nil {
		return r, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return r, err
	}
	if n != 1 {
		return r, ErrConflict
	}
	out, err := s.get(ctx, e, a, r.ID, false)
	if err != nil {
		return r, err
	}
	return out, s.event(ctx, e, a, out, "change.changed", "PUBLIC", fmt.Sprintf("change:%d:%d", r.ID, out.Version), map[string]any{"fields": []string{field}, "state": out.State, "before": publicChanges(r, out, true), "after": publicChanges(r, out, false)})
}
func reserve(ctx context.Context, e database.Executor, a Actor, key, kind string, hash [32]byte) (uint64, error) {
	return workrecords.ReserveOperation(ctx, e, a.Actor, key, kind, hash)
}
func complete(ctx context.Context, e database.Executor, a Actor, key string, id uint64) error {
	return workrecords.CompleteOperation(ctx, e, a.Actor, key, id)
}
func (s *Service) History(ctx context.Context, a Actor, id, before uint64, limit int) (activity.Page, error) {
	if _, err := s.Get(ctx, a, id); err != nil {
		return activity.Page{}, err
	}
	if limit < 1 || limit > 100 {
		return activity.Page{}, ErrInvalid
	}
	return activity.NewRepository(s.db).ListVisibleBySourceEvent(ctx, a.TenantID, "change", fmt.Sprint(id), before, limit, notesAllowed(a))
}

func optionalEqual(a, b *uint64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func validateAssignment(ctx context.Context, e database.Executor, tenant uint64, user, group *uint64) error {
	if user == nil || group == nil {
		return nil
	}
	var member bool
	if err := e.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM group_memberships WHERE tenant_id=? AND group_id=? AND user_id=?)", tenant, *group, *user).Scan(&member); err != nil {
		return err
	}
	if !member {
		return fmt.Errorf("%w: wykonawca musi należeć do wybranej grupy", ErrInvalid)
	}
	return nil
}
func publicChanges(before, after Record, old bool) map[string]any {
	snapshot := func(r Record) map[string]any {
		raw, _ := json.Marshal(r.Fields)
		values := map[string]any{}
		_ = json.Unmarshal(raw, &values)
		values["state"] = r.State
		values["reason"] = r.Reason
		return values
	}
	a, b := snapshot(before), snapshot(after)
	out := map[string]any{}
	for key, value := range a {
		if !reflect.DeepEqual(value, b[key]) {
			if old {
				out[key] = value
			} else {
				out[key] = b[key]
			}
		}
	}
	return out
}
