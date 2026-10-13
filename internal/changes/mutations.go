package changes

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/chmajster/OpenServiceNOW/internal/cmdb"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/problems"
	"github.com/chmajster/OpenServiceNOW/internal/workflow"
	"reflect"
	"strings"
	"time"
)

func material(f Fields) Fields { f.ReviewResult = ""; f.ReviewOutcome = ""; return f }
func (s *Service) invalidate(ctx context.Context, e database.Executor, a Actor, r Record) error {
	if err := workflow.InvalidateChangeInTx(ctx, e, a, r.ID, time.Now().UTC()); err != nil {
		return err
	}
	state := r.State
	if contains([]string{"AUTHORIZE", "SCHEDULED"}, state) {
		state = "NEW"
		if r.Type == "NORMAL" {
			state = "ASSESS"
		}
	}
	_, err := e.ExecContext(ctx, "UPDATE changes SET assessment_revision=assessment_revision+1,approved_revision=NULL,evaluated_version=NULL,approval_state=IF(approval_state='NONE','NONE','INVALIDATED'),execution_id=NULL,state=? WHERE tenant_id=? AND id=?", state, a.TenantID, r.ID)
	return err
}
func (s *Service) Save(ctx context.Context, a Actor, id uint64, in Input, key string) (Record, error) {
	var out Record
	f, err := Normalize(in.Fields)
	if err != nil {
		return out, err
	}
	in.Fields = f
	if id > 0 && (in.SourceProblemID > 0 || in.SourceVersion > 0) || id == 0 && in.Version > 0 || in.SourceProblemID == 0 && in.SourceVersion > 0 {
		return out, ErrInvalid
	}
	raw, _ := json.Marshal(struct {
		ID    uint64
		Input Input
	}{id, in})
	err = s.actorTx(ctx, a, func(e database.Executor, a Actor) error {
		if id == 0 {
			if !a.Has("change.create") || !a.Has("change.read") && !a.Has("change.read_own") {
				return ErrForbidden
			}
		} else if !a.Has("change.manage") {
			return ErrForbidden
		}
		old, err := reserve(ctx, e, a, key, "change_save", sha256.Sum256(raw))
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
			if in.Version == 0 || in.Version != before.Version {
				return ErrConflict
			}
			if !before.CanEdit {
				return ErrForbidden
			}
			if in.Type != before.Type {
				return fmt.Errorf("%w: typ zmiany jest niezmienny", ErrInvalid)
			}
			if (!optionalEqual(before.AssignedTo, in.AssignedTo) || !optionalEqual(before.GroupID, in.GroupID)) && !a.Has("change.assign") {
				return ErrForbidden
			}
			if contains([]string{"IMPLEMENT", "REVIEW"}, before.State) && !reflect.DeepEqual(material(before.Fields), material(in.Fields)) {
				return fmt.Errorf("%w: po rozpoczęciu wykonania plan jest niezmienny; anuluj zmianę i zarejestruj kolejną ocenę", ErrTransition)
			}
		} else if (in.AssignedTo != nil && *in.AssignedTo != a.UserID || in.GroupID != nil) && !a.Has("change.assign") {
			return ErrForbidden
		}
		if err = validateAssignment(ctx, e, a.TenantID, in.AssignedTo, in.GroupID); err != nil {
			return err
		}
		if in.WorkflowID != nil {
			c, err := workflow.Published(ctx, e, a.TenantID, *in.WorkflowID, *in.WorkflowVersion)
			if err != nil {
				return err
			}
			if c.Domain != "CHANGE" || c.ChangeType != in.Type || in.GroupID == nil || c.GroupID != *in.GroupID {
				return fmt.Errorf("%w: typ i grupa muszą odpowiadać opublikowanemu procesowi Change", ErrInvalid)
			}
		}
		if in.Type == "STANDARD" {
			if err = s.validateTemplate(ctx, e, a, in.Fields, 0); err != nil {
				return err
			}
		}
		var source problems.ChangeSnapshot
		if id == 0 && in.SourceProblemID > 0 {
			if s.Problems == nil {
				return ErrInvalid
			}
			source, err = s.Problems.ChangeReference(ctx, e, a, in.SourceProblemID, false, 0)
			if err != nil {
				return err
			}
			if !source.CanLink {
				return ErrForbidden
			}
			if in.SourceVersion == 0 || in.SourceVersion != source.Version {
				return ErrConflict
			}
		}
		args := []any{in.Title, in.Description, in.Type, in.Impact, in.Risk, in.Justification, in.Scope, in.ImplementationPlan, in.TestPlan, in.RollbackPlan, in.EmergencyReason, in.ReviewResult, in.ReviewOutcome, in.PlannedStart, in.PlannedEnd, in.TimeZone, in.WorkflowID, in.WorkflowVersion, in.TemplateID, in.TemplateVersion}
		if id == 0 {
			number, err := s.allocator.NextInTx(ctx, e, a.TenantID, "change", "CHG", 7)
			if err != nil {
				return err
			}
			res, err := e.ExecContext(ctx, "INSERT INTO changes(tenant_id,number,reporter_id,title,description,change_type,impact,risk,justification,scope,implementation_plan,test_plan,rollback_plan,emergency_reason,review_result,review_outcome,planned_start,planned_end,time_zone,workflow_id,workflow_version,template_id,template_version,reason) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,'')", append([]any{a.TenantID, number, a.UserID}, args...)...)
			if err != nil {
				return err
			}
			n, err := res.LastInsertId()
			if err != nil {
				return err
			}
			id = uint64(n)
			if _, err = s.work.SetAssignment(ctx, e, a.TenantID, "change", fmt.Sprint(id), 0, in.AssignedTo, in.GroupID); err != nil {
				return err
			}
			if source.ID > 0 {
				if _, err = s.Problems.ChangeReference(ctx, e, a, source.ID, true, source.Version); err != nil {
					return err
				}
				if _, err = e.ExecContext(ctx, "INSERT INTO change_problem_links(tenant_id,change_id,problem_id) VALUES(?,?,?)", a.TenantID, id, source.ID); err != nil {
					return err
				}
			}
			out, err = s.get(ctx, e, a, id, false)
			if err != nil {
				return err
			}
			if err = s.event(ctx, e, a, out, "change.created", "PUBLIC", fmt.Sprintf("change:%d:1", id), map[string]any{"state": "NEW", "source_problem_id": fmt.Sprint(source.ID)}); err != nil {
				return err
			}
		} else {
			if _, err = e.ExecContext(ctx, "UPDATE changes SET title=?,description=?,change_type=?,impact=?,risk=?,justification=?,scope=?,implementation_plan=?,test_plan=?,rollback_plan=?,emergency_reason=?,review_result=?,review_outcome=?,planned_start=?,planned_end=?,time_zone=?,workflow_id=?,workflow_version=?,template_id=?,template_version=? WHERE tenant_id=? AND id=? AND version=?", append(args, a.TenantID, id, in.Version)...); err != nil {
				return err
			}
			var av uint64
			if err = e.QueryRowContext(ctx, "SELECT version FROM work_assignments WHERE tenant_id=? AND object_type='change' AND object_id=? FOR UPDATE", a.TenantID, fmt.Sprint(id)).Scan(&av); err != nil {
				return err
			}
			if _, err = s.work.SetAssignment(ctx, e, a.TenantID, "change", fmt.Sprint(id), av, in.AssignedTo, in.GroupID); err != nil {
				return err
			}
			if !reflect.DeepEqual(material(before.Fields), material(in.Fields)) {
				if err = s.invalidate(ctx, e, a, before); err != nil {
					return err
				}
			}
			out, err = s.touch(ctx, e, a, before, "details")
			if err != nil {
				return err
			}
		}
		return complete(ctx, e, a, key, id)
	})
	return out, err
}

func (s *Service) Transition(ctx context.Context, a Actor, id uint64, in TransitionInput, key string) (Record, error) {
	var out Record
	in.Reason = strings.TrimSpace(in.Reason)
	if len([]rune(in.Reason)) > 10000 || strings.ContainsRune(in.Reason, 0) {
		return out, ErrInvalid
	}
	raw, _ := json.Marshal(struct {
		ID    uint64
		Input TransitionInput
	}{id, in})
	err := s.withChange(ctx, a, id, func(e database.Executor, a Actor, r Record) error {
		if !a.Has("change.transition") {
			return ErrForbidden
		}
		old, err := reserve(ctx, e, a, key, "change_transition", sha256.Sum256(raw))
		if err != nil {
			return err
		}
		if old > 0 {
			out = r
			return nil
		}
		if in.Version == 0 || in.Version != r.Version {
			return ErrConflict
		}
		if !contains(r.Actions, in.State) {
			return ErrTransition
		}
		if in.Override && in.State != "SCHEDULED" {
			return ErrInvalid
		}
		if contains([]string{"NEW", "ASSESS", "CANCELLED"}, in.State) && in.Reason == "" {
			return fmt.Errorf("%w: podaj uzasadnienie powrotu lub anulowania", ErrTransition)
		}
		switch in.State {
		case "AUTHORIZE":
			if r.ApprovalState == "REJECTED" {
				if err = s.invalidate(ctx, e, a, r); err != nil {
					return err
				}
				r.AssessmentRevision++
			}
			if err = s.ready(ctx, e, a, r); err != nil {
				return err
			}
			if r.PlannedStart == nil || !r.PlannedStart.After(time.Now().UTC()) {
				return fmt.Errorf("%w: wskaż przyszły planowany termin", ErrTransition)
			}
			snapshot, err := s.evaluationSnapshot(ctx, e, a, r)
			if err != nil {
				return err
			}
			execution, err := workflow.StartChangeInTx(ctx, e, a, *r.WorkflowID, *r.WorkflowVersion, r.ID, r.Version+1, r.AssessmentRevision, *r.GroupID, snapshot, time.Now().UTC())
			if err != nil {
				return err
			}
			if _, err = e.ExecContext(ctx, "UPDATE changes SET approval_state='PENDING',execution_id=?,evaluated_version=? WHERE tenant_id=? AND id=?", execution, r.Version+1, a.TenantID, id); err != nil {
				return err
			}
		case "SCHEDULED":
			if r.ApprovedRevision == nil || *r.ApprovedRevision != r.AssessmentRevision || r.ApprovalState != "APPROVED" {
				return ErrTransition
			}
			if err = s.ready(ctx, e, a, r); err != nil {
				return err
			}
			if err = s.reserveSchedule(ctx, e, a, r, in.Override, in.Reason); err != nil {
				return err
			}
		case "IMPLEMENT":
			if r.ApprovalState != "APPROVED" || r.ApprovedRevision == nil || *r.ApprovedRevision != r.AssessmentRevision || r.PlannedStart == nil || r.PlannedEnd == nil {
				return ErrTransition
			}
			now := time.Now().UTC()
			if now.Before(*r.PlannedStart) || !now.Before(*r.PlannedEnd) {
				return fmt.Errorf("%w: wykonanie można rozpocząć w zaplanowanym przedziale", ErrTransition)
			}
			if _, err = e.ExecContext(ctx, "UPDATE changes SET actual_start=UTC_TIMESTAMP(6) WHERE tenant_id=? AND id=?", a.TenantID, id); err != nil {
				return err
			}
		case "REVIEW", "CLOSED":
			var incomplete bool
			if err = e.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM change_tasks WHERE tenant_id=? AND change_id=? AND required=TRUE AND state<>'COMPLETED')", a.TenantID, id).Scan(&incomplete); err != nil {
				return err
			}
			if incomplete {
				return fmt.Errorf("%w: zakończ wszystkie wymagane zadania wykonawcze", ErrTransition)
			}
			if in.State == "REVIEW" {
				if _, err = e.ExecContext(ctx, "UPDATE changes SET actual_end=UTC_TIMESTAMP(6) WHERE tenant_id=? AND id=?", a.TenantID, id); err != nil {
					return err
				}
			} else if r.ReviewResult == "" || r.ReviewOutcome == "" || r.ActualStart == nil || r.ActualEnd == nil {
				return fmt.Errorf("%w: zapisz wynik przeglądu i rzeczywiste wykonanie", ErrTransition)
			}
		case "NEW", "ASSESS", "CANCELLED":
			if err = s.invalidate(ctx, e, a, r); err != nil {
				return err
			}
			if in.State == "CANCELLED" {
				if _, err = e.ExecContext(ctx, "UPDATE change_tasks SET state='CANCELLED',version=version+1,updated_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND change_id=? AND state NOT IN('COMPLETED','CANCELLED')", a.TenantID, id); err != nil {
					return err
				}
			}
		}
		if _, err = e.ExecContext(ctx, "UPDATE changes SET state=?,reason=? WHERE tenant_id=? AND id=?", in.State, in.Reason, a.TenantID, id); err != nil {
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
func (s *Service) ready(ctx context.Context, e database.Executor, a Actor, r Record) error {
	for _, v := range []string{r.Justification, r.Scope, r.ImplementationPlan, r.TestPlan, r.RollbackPlan} {
		if strings.TrimSpace(v) == "" {
			return fmt.Errorf("%w: uzupełnij uzasadnienie, zakres i trzy plany", ErrTransition)
		}
	}
	if r.GroupID == nil || r.WorkflowID == nil || r.PlannedStart == nil {
		return fmt.Errorf("%w: wskaż grupę, proces i terminy", ErrTransition)
	}
	if r.Type == "EMERGENCY" && r.EmergencyReason == "" {
		return fmt.Errorf("%w: Emergency wymaga uzasadnienia pilności", ErrTransition)
	}
	c, err := workflow.Published(ctx, e, a.TenantID, *r.WorkflowID, *r.WorkflowVersion)
	if err != nil {
		return err
	}
	if c.Domain != "CHANGE" || c.ChangeType != r.Type || c.GroupID != *r.GroupID {
		return ErrInvalid
	}
	if _, err = workflow.ActiveExecutor(ctx, e, a.TenantID, c); err != nil {
		return err
	}
	cis, err := s.ciSnapshots(ctx, e, a, r.ID)
	if err != nil {
		return err
	}
	if len(cis) == 0 {
		return fmt.Errorf("%w: powiąż co najmniej jeden CI lub usługę biznesową", ErrTransition)
	}
	if r.Type == "STANDARD" {
		return s.validateTemplate(ctx, e, a, r.Fields, r.ID)
	}
	return nil
}
func (s *Service) ciSnapshots(ctx context.Context, e database.Executor, a Actor, id uint64) ([]cmdb.Record, error) {
	rows, err := e.QueryContext(ctx, "SELECT ci_id FROM cmdb_change_links WHERE tenant_id=? AND change_id=? ORDER BY ci_id LIMIT 21", a.TenantID, id)
	if err != nil {
		return nil, err
	}
	ids := []uint64{}
	for rows.Next() {
		var id uint64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(ids) > 20 {
		return nil, ErrInvalid
	}
	service := cmdb.NewService(s.db, nil, nil)
	out := []cmdb.Record{}
	for _, ci := range ids {
		r, err := service.GetInTx(ctx, e, a, ci, false)
		if err != nil {
			return nil, err
		}
		if r.Archived {
			return nil, errors.Join(ErrTransition, errors.New("wycofany CI wymaga usunięcia z planowanego zakresu"))
		}
		out = append(out, r)
	}
	return out, nil
}
func (s *Service) evaluationSnapshot(ctx context.Context, e database.Executor, a Actor, r Record) (json.RawMessage, error) {
	cis, err := s.ciSnapshots(ctx, e, a, r.ID)
	if err != nil {
		return nil, err
	}
	refs := []map[string]any{}
	for _, ci := range cis {
		refs = append(refs, map[string]any{"id": fmt.Sprint(ci.ID), "identifier": ci.Identifier, "name": ci.Name, "class": ci.Class, "version": fmt.Sprint(ci.Version)})
	}
	return json.Marshal(map[string]any{"number": r.Number, "version": fmt.Sprint(r.Version + 1), "assessment_revision": fmt.Sprint(r.AssessmentRevision), "fields": r.Fields, "cis": refs})
}
