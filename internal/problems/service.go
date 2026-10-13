package problems

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/chmajster/OpenServiceNOW/internal/activity"
	"github.com/chmajster/OpenServiceNOW/internal/attachments"
	"github.com/chmajster/OpenServiceNOW/internal/catalog"
	"github.com/chmajster/OpenServiceNOW/internal/events"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
	"reflect"
	"strings"
	"time"
)

type Service struct {
	db        *sql.DB
	tx        *database.TxManager
	work      *workrecords.Repository
	allocator *workrecords.Allocator
	files     *attachments.Service
	Incidents IncidentDomain
}

func NewService(db *sql.DB, files *attachments.Service, incidents IncidentDomain) *Service {
	tx := database.NewTxManager(db)
	return &Service{db: db, tx: tx, work: workrecords.NewRepository(db), allocator: workrecords.NewAllocator(tx), files: files, Incidents: incidents}
}
func Access(a Actor, alias string) (string, []any) {
	if a.Has("problem.read") {
		return "1=1", nil
	}
	if !a.Has("problem.read_own") {
		return "1=0", nil
	}
	return "(" + alias + ".reporter_id=? OR wa.assigned_to=? OR EXISTS(SELECT 1 FROM group_memberships gm JOIN `groups` ag ON ag.tenant_id=gm.tenant_id AND ag.id=gm.group_id AND ag.active=TRUE WHERE gm.tenant_id=" + alias + ".tenant_id AND gm.group_id=wa.assignment_group_id AND gm.user_id=?))", []any{a.UserID, a.UserID, a.UserID}
}

const joins = " FROM problems p JOIN work_assignments wa ON wa.tenant_id=p.tenant_id AND wa.object_type='problem' AND wa.object_id=CAST(p.id AS CHAR) JOIN users u ON u.id=p.reporter_id LEFT JOIN users au ON au.id=wa.assigned_to LEFT JOIN `groups` g ON g.tenant_id=wa.tenant_id AND g.id=wa.assignment_group_id"
const columns = "p.id,p.number,p.reporter_id,u.display_name,p.title,p.description,p.impact,p.symptoms,p.analysis,p.root_cause,p.workaround,p.solution_plan,p.verification,p.known_error,p.state,p.reason,p.version,p.created_at,p.updated_at,wa.assigned_to,wa.assignment_group_id,COALESCE(au.display_name,''),COALESCE(g.name,'')"

func scan(row interface{ Scan(...any) error }, a Actor) (Record, error) {
	var r Record
	err := row.Scan(&r.ID, &r.Number, &r.ReporterID, &r.ReporterName, &r.Title, &r.Description, &r.Impact, &r.Symptoms, &r.Analysis, &r.RootCause, &r.Workaround, &r.SolutionPlan, &r.Verification, &r.KnownError, &r.State, &r.Reason, &r.Version, &r.CreatedAt, &r.UpdatedAt, &r.AssignedTo, &r.GroupID, &r.AssignedName, &r.GroupName)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	r.CanEdit = a.Has("problem.manage") && editable(r.State)
	r.CanAssign = r.CanEdit && a.Has("problem.assign")
	r.CanKnownError = r.CanEdit && a.Has("problem.known_error")
	r.CanTasks = a.Has("problem.tasks") && active(r.State)
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
func (s *Service) withProblem(ctx context.Context, a Actor, id uint64, fn func(database.Executor, Actor, Record) error) error {
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
	data["problem_version"] = fmt.Sprint(r.Version)
	data["visibility"] = visibility
	if err := catalog.Audit(ctx, e, a, kind, "problem", r.ID, map[string]any{"state": r.State, "version": fmt.Sprint(r.Version), "visibility": visibility, "resource_id": data["resource_id"], "fields": data["fields"]}); err != nil {
		return err
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, _, err = events.NewRepository(s.db).Append(ctx, e, events.Input{TenantID: a.TenantID, Type: kind, AggregateType: "problem", AggregateID: fmt.Sprint(r.ID), Payload: raw, IdempotencyKey: key})
	return err
}
func (s *Service) touch(ctx context.Context, e database.Executor, a Actor, r Record, field string) (Record, error) {
	result, err := e.ExecContext(ctx, "UPDATE problems SET version=version+1,updated_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND id=? AND version=?", a.TenantID, r.ID, r.Version)
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
	return out, s.event(ctx, e, a, out, "problem.changed", "PUBLIC", fmt.Sprintf("problem:%d:%d", r.ID, out.Version), map[string]any{"fields": []string{field}, "state": out.State, "before": publicChanges(r, out, true), "after": publicChanges(r, out, false)})
}
func reserve(ctx context.Context, e database.Executor, a Actor, key, kind string, hash [32]byte) (uint64, error) {
	return workrecords.ReserveOperation(ctx, e, a.Actor, key, kind, hash)
}
func complete(ctx context.Context, e database.Executor, a Actor, key string, id uint64) error {
	return workrecords.CompleteOperation(ctx, e, a.Actor, key, id)
}
func (s *Service) Save(ctx context.Context, a Actor, id uint64, in Input, key string) (Record, error) {
	var out Record
	f, err := Normalize(in.Fields)
	if err != nil {
		return out, err
	}
	in.Fields = f
	if id > 0 && (in.SourceIncidentID > 0 || in.SourceVersion > 0) || id == 0 && in.Version > 0 || in.SourceIncidentID == 0 && in.SourceVersion > 0 {
		return out, ErrInvalid
	}
	raw, _ := json.Marshal(struct {
		ID    uint64
		Input Input
	}{id, in})
	err = s.actorTx(ctx, a, func(e database.Executor, a Actor) error {
		if id == 0 {
			if !a.Has("problem.create") || !a.Has("problem.read") && !a.Has("problem.read_own") {
				return ErrForbidden
			}
		} else if !a.Has("problem.manage") {
			return ErrForbidden
		}
		old, err := reserve(ctx, e, a, key, "problem_save", sha256.Sum256(raw))
		if err != nil {
			return err
		}
		if old > 0 {
			out, err = s.get(ctx, e, a, old, false)
			return err
		}
		var before Record
		if id > 0 {
			before, err = s.get(ctx, e, a, id, true)
			if err != nil {
				return err
			}
			if before.Version != in.Version || in.Version == 0 {
				return ErrConflict
			}
			if !before.CanEdit {
				return ErrForbidden
			}
			if (!optionalEqual(before.AssignedTo, in.AssignedTo) || !optionalEqual(before.GroupID, in.GroupID)) && !a.Has("problem.assign") {
				return ErrForbidden
			}
			if before.KnownError != in.KnownError && !a.Has("problem.known_error") {
				return ErrForbidden
			}
		}
		if id == 0 && in.KnownError && !a.Has("problem.known_error") {
			return ErrForbidden
		}
		if id == 0 && (in.AssignedTo != nil && *in.AssignedTo != a.UserID || in.GroupID != nil) && !a.Has("problem.assign") {
			return ErrForbidden
		}
		var source IncidentSnapshot
		if id == 0 && in.SourceIncidentID > 0 {
			if s.Incidents == nil {
				return ErrInvalid
			}
			source, err = s.Incidents.ProblemReference(ctx, e, a, in.SourceIncidentID, false, 0)
			if err != nil {
				return err
			}
			if !source.CanLink {
				return ErrForbidden
			}
			if source.Version != in.SourceVersion || in.SourceVersion == 0 {
				return ErrConflict
			}
		}
		if err := validateAssignment(ctx, e, a.TenantID, in.AssignedTo, in.GroupID); err != nil {
			return err
		}
		args := []any{in.Title, in.Description, in.Impact, in.Symptoms, in.Analysis, in.RootCause, in.Workaround, in.SolutionPlan, in.Verification, in.KnownError}
		if id == 0 {
			number, err := s.allocator.NextInTx(ctx, e, a.TenantID, "problem", "PRB", 7)
			if err != nil {
				return err
			}
			result, err := e.ExecContext(ctx, "INSERT INTO problems(tenant_id,number,reporter_id,title,description,impact,symptoms,analysis,root_cause,workaround,solution_plan,verification,known_error,reason) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,'')", append([]any{a.TenantID, number, a.UserID}, args...)...)
			if err != nil {
				return err
			}
			n, err := result.LastInsertId()
			if err != nil {
				return err
			}
			out.ID = uint64(n)
			if _, err = s.work.SetAssignment(ctx, e, a.TenantID, "problem", fmt.Sprint(out.ID), 0, in.AssignedTo, in.GroupID); err != nil {
				return err
			}
			if source.ID > 0 {
				if _, err = s.Incidents.ProblemReference(ctx, e, a, source.ID, true, source.Version); err != nil {
					return err
				}
				if _, err = e.ExecContext(ctx, "INSERT INTO problem_incident_links(tenant_id,problem_id,incident_id) VALUES(?,?,?)", a.TenantID, out.ID, source.ID); err != nil {
					return err
				}
			}
			out, err = s.get(ctx, e, a, out.ID, false)
			if err != nil {
				return err
			}
			if err = s.event(ctx, e, a, out, "problem.created", "PUBLIC", fmt.Sprintf("problem:%d:1", out.ID), map[string]any{"state": "NEW", "source_incident_id": fmt.Sprint(source.ID)}); err != nil {
				return err
			}
		} else {
			result, err := e.ExecContext(ctx, "UPDATE problems SET title=?,description=?,impact=?,symptoms=?,analysis=?,root_cause=?,workaround=?,solution_plan=?,verification=?,known_error=? WHERE tenant_id=? AND id=? AND version=?", append(args, a.TenantID, id, in.Version)...)
			if err != nil {
				return err
			}
			if n, err := result.RowsAffected(); err != nil {
				return err
			} else if n > 1 {
				return ErrConflict
			}
			var assignmentVersion uint64
			if err = e.QueryRowContext(ctx, "SELECT version FROM work_assignments WHERE tenant_id=? AND object_type='problem' AND object_id=? FOR UPDATE", a.TenantID, fmt.Sprint(id)).Scan(&assignmentVersion); err != nil {
				return err
			}
			if _, err = s.work.SetAssignment(ctx, e, a.TenantID, "problem", fmt.Sprint(id), assignmentVersion, in.AssignedTo, in.GroupID); err != nil {
				return err
			}
			out, err = s.touch(ctx, e, a, before, "details")
			if err != nil {
				return err
			}
		}
		return complete(ctx, e, a, key, out.ID)
	})
	return out, err
}
func (s *Service) Transition(ctx context.Context, a Actor, id uint64, in Change, key string) (Record, error) {
	var out Record
	in.Reason = strings.TrimSpace(in.Reason)
	raw, _ := json.Marshal(struct {
		ID     uint64
		Change Change
	}{id, in})
	err := s.withProblem(ctx, a, id, func(e database.Executor, a Actor, r Record) error {
		if !a.Has("problem.transition") {
			return ErrForbidden
		}
		old, err := reserve(ctx, e, a, key, "problem_transition", sha256.Sum256(raw))
		if err != nil {
			return err
		}
		if old > 0 {
			out = r
			return nil
		}
		if in.Version == 0 || r.Version != in.Version {
			return ErrConflict
		}
		if !contains(r.Actions, in.State) {
			return ErrTransition
		}
		if err = validateTransition(r, in); err != nil {
			return err
		}
		if in.State == "RESOLVED" || in.State == "CLOSED" {
			var incomplete bool
			if err = e.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM problem_tasks WHERE tenant_id=? AND problem_id=? AND required=TRUE AND state<>'COMPLETED')", a.TenantID, id).Scan(&incomplete); err != nil {
				return err
			}
			if incomplete {
				return errors.Join(ErrTransition, errors.New("zakończ wymagane zadania analityczne"))
			}
		}
		if in.State == "CANCELLED" {
			if _, err = e.ExecContext(ctx, "UPDATE problem_tasks SET state='CANCELLED',version=version+1,updated_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND problem_id=? AND state NOT IN('COMPLETED','CANCELLED')", a.TenantID, id); err != nil {
				return err
			}
		}
		if _, err = e.ExecContext(ctx, "UPDATE problems SET state=?,reason=? WHERE tenant_id=? AND id=?", in.State, in.Reason, a.TenantID, id); err != nil {
			return err
		}
		out, err = s.touch(ctx, e, a, r, "state")
		if err != nil {
			return err
		}
		return complete(ctx, e, a, key, id)
	})
	return out, err
}
func (s *Service) History(ctx context.Context, a Actor, id, before uint64, limit int) (activity.Page, error) {
	if _, err := s.Get(ctx, a, id); err != nil {
		return activity.Page{}, err
	}
	if limit < 1 || limit > 100 {
		return activity.Page{}, ErrInvalid
	}
	return activity.NewRepository(s.db).ListVisibleBySourceEvent(ctx, a.TenantID, "problem", fmt.Sprint(id), before, limit, notesAllowed(a))
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
		return fmt.Errorf("%w: analityk musi należeć do wybranej grupy", ErrInvalid)
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
