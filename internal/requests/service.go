package requests

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
	"github.com/chmajster/OpenServiceNOW/internal/workflow"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
	"regexp"
	"strings"
	"time"
)

type Service struct {
	db        *sql.DB
	tx        *database.TxManager
	work      *workrecords.Service
	allocator *workrecords.Allocator
	outbox    OutboxAppender
	files     *attachments.Service
}
type OutboxAppender interface {
	Append(context.Context, database.Executor, events.Input) (uint64, bool, error)
}

func NewService(db *sql.DB, files *attachments.Service) *Service {
	return NewServiceWithOutbox(db, files, events.NewRepository(db))
}
func NewServiceWithOutbox(db *sql.DB, files *attachments.Service, outbox OutboxAppender) *Service {
	tx := database.NewTxManager(db)
	out := events.NewRepository(db)
	return &Service{db, tx, workrecords.NewService(workrecords.NewRepository(db), tx, out), workrecords.NewAllocator(tx), outbox, files}
}

var requestKeyPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{8,128}$`)

func (s *Service) event(ctx context.Context, e database.Executor, a Actor, r Request, kind, visibility, key string, data map[string]any) error {
	data["actor_user_id"] = fmt.Sprint(a.UserID)
	data["occurred_at"] = time.Now().UTC()
	data["request_version"] = fmt.Sprint(r.Version)
	data["visibility"] = visibility
	// Public events carry only safe metadata, never submitted values/instructions.
	metadata := map[string]any{"state": r.State, "version": fmt.Sprint(r.Version), "visibility": visibility, "resource_id": data["resource_id"]}
	for _, key := range []string{"task_number", "assigned_to", "assigned_before", "task_state_before", "task_state_after"} {
		if value, ok := data[key]; ok {
			metadata[key] = value
		}
	}
	if err := catalog.Audit(ctx, e, a, kind, "request", r.ID, metadata); err != nil {
		return err
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, _, err = s.outbox.Append(ctx, e, events.Input{TenantID: a.TenantID, Type: kind, AggregateType: "request", AggregateID: fmt.Sprint(r.ID), Payload: raw, IdempotencyKey: key})
	return err
}
func (s *Service) Create(ctx context.Context, a Actor, in Submission, key string) (Request, bool, error) {
	if !requestKeyPattern.MatchString(key) || len(in.Items) < 1 || len(in.Items) > 10 {
		return Request{}, false, ErrInvalid
	}
	raw, _ := json.Marshal(in)
	hash := sha256.Sum256(raw)
	var r Request
	created := false
	err := s.tx.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(e database.Executor) error {
		current, err := workrecords.LockPrincipal(ctx, e, a)
		if err != nil {
			return err
		}
		a = current
		if !a.Has("request.create") || !a.Has("request.read_own") || !a.Has("catalog.read") {
			return ErrForbidden
		}
		existing, err := reserve(ctx, e, a, key, "submission", hash)
		if err != nil {
			return err
		}
		if existing > 0 {
			r, err = s.get(ctx, e, a, existing, false)
			return err
		}
		number, err := s.allocator.NextInTx(ctx, e, a.TenantID, "request", "REQ", 7)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		result, err := e.ExecContext(ctx, "INSERT INTO service_requests(tenant_id,number,requester_id,reason,created_at,updated_at) VALUES(?,?,?,'',?,?)", a.TenantID, number, a.UserID, now, now)
		if err != nil {
			return err
		}
		id, err := result.LastInsertId()
		if err != nil {
			return err
		}
		r = Request{ID: uint64(id), Version: 1, State: "SUBMITTED"}
		for _, item := range in.Items {
			if item.ItemID == 0 || item.Version == 0 {
				return ErrInvalid
			}
			publication, d, err := catalog.Published(ctx, e, a, item.ItemID, item.Version)
			if err != nil {
				return err
			}
			// Combining process instances needs a join policy for the parent request.
			// Manual multi-item submissions remain supported; workflow is one item.
			if d.Method == "WORKFLOW" && len(in.Items) != 1 {
				return fmt.Errorf("%w: pozycję workflow należy złożyć jako osobny wniosek", ErrInvalid)
			}
			if err := catalog.ActiveGroup(ctx, e, a.TenantID, d.GroupID); err != nil {
				return err
			}
			values, err := catalog.ValidateValues(d.Fields, item.Values)
			if err != nil {
				return err
			}
			snapshot, _ := json.Marshal(publication)
			encoded, _ := json.Marshal(values)
			num, err := s.allocator.NextInTx(ctx, e, a.TenantID, "requested_item", "RITM", 7)
			if err != nil {
				return err
			}
			result, err := e.ExecContext(ctx, "INSERT INTO requested_items(tenant_id,request_id,number,catalog_item_id,catalog_version,snapshot,form_values) VALUES(?,?,?,?,?,?,?)", a.TenantID, r.ID, num, item.ItemID, item.Version, snapshot, encoded)
			if err != nil {
				return err
			}
			requested, err := result.LastInsertId()
			if err != nil {
				return err
			}
			if d.Method == "WORKFLOW" {
				if _, err = workflow.StartInTx(ctx, e, a, d.WorkflowID, d.WorkflowVersion, r.ID, uint64(requested), d.GroupID, now); err != nil {
					return err
				}
			} else {
				for _, step := range d.Steps {
					if _, err = s.createTask(ctx, e, a, r.ID, uint64(requested), d.GroupID, step, now); err != nil {
						return err
					}
				}
			}
		}
		if err := complete(ctx, e, a, key, r.ID); err != nil {
			return err
		}
		if err := s.event(ctx, e, a, r, "request.created", "PUBLIC", fmt.Sprintf("request:%d:1", r.ID), map[string]any{"state": "SUBMITTED"}); err != nil {
			return err
		}
		r, err = s.get(ctx, e, a, r.ID, false)
		created = true
		return err
	})
	return r, created, err
}
func (s *Service) Get(ctx context.Context, a Actor, id uint64) (Request, error) {
	return s.get(ctx, s.db, a, id, false)
}
func (s *Service) Task(ctx context.Context, a Actor, id uint64) (Task, error) {
	return s.getTask(ctx, s.db, a, id)
}
func (s *Service) withRequest(ctx context.Context, a Actor, id uint64, fn func(database.Executor, Actor, Request) error) error {
	return s.tx.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(e database.Executor) error {
		current, err := workrecords.LockPrincipal(ctx, e, a)
		if err != nil {
			return err
		}
		r, err := s.get(ctx, e, current, id, true)
		if err != nil {
			return err
		}
		return fn(e, current, r)
	})
}
func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}
func (s *Service) Transition(ctx context.Context, a Actor, id uint64, in Change) (Request, error) {
	var out Request
	err := s.withRequest(ctx, a, id, func(e database.Executor, a Actor, r Request) error {
		if in.Version == 0 || r.Version != in.Version {
			return ErrConflict
		}
		if !contains(r.Actions, in.State) {
			return ErrTransition
		}
		if err := s.applyStateInTx(ctx, e, a, &r, in, 0); err != nil {
			return err
		}
		var err error
		out, err = s.get(ctx, e, a, id, false)
		return err
	})
	return out, err
}
func (s *Service) ChangeTask(ctx context.Context, a Actor, id uint64, in Change, assign bool) (Task, error) {
	initial, err := s.getTask(ctx, s.db, a, id)
	if err != nil {
		return initial, err
	}
	var out Task
	err = s.withRequest(ctx, a, initial.RequestID, func(e database.Executor, a Actor, r Request) error {
		t, err := s.getTask(ctx, e, a, id)
		if err != nil {
			return err
		}
		if in.Version == 0 || t.Version != in.Version {
			return ErrConflict
		}
		if r.State == "CLOSED" || r.State == "CANCELLED" || r.State == "FULFILLED" || t.State == "COMPLETED" || t.State == "CANCELLED" {
			return ErrTransition
		}
		visibility, kind := "PUBLIC", "request.task.changed"
		data := map[string]any{"task_number": t.Number}
		if assign {
			if in.AssignedTo != nil {
				if err := workrecords.ValidateActiveUser(ctx, e, a.TenantID, *in.AssignedTo); err != nil {
					if errors.Is(err, workrecords.ErrNotFound) {
						return ErrInvalid
					}
					return err
				}
				var member bool
				if err := e.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM group_memberships WHERE tenant_id=? AND group_id=? AND user_id=?)", a.TenantID, t.Assignment.AssignmentGroupID, *in.AssignedTo).Scan(&member); err != nil {
					return err
				}
				if !member {
					return ErrInvalid
				}
			}
			if _, err := s.work.SetAssignmentInTx(ctx, e, a.Actor, "fulfillment_task", fmt.Sprint(id), t.Assignment.Version, in.AssignedTo, t.Assignment.AssignmentGroupID); err != nil {
				return err
			}
			visibility = "INTERNAL"
			data["assigned_to"] = messageIDString(in.AssignedTo)
			data["assigned_before"] = messageIDString(t.Assignment.AssignedTo)
			kind = "request.task.assigned"
		} else {
			if !contains(t.Actions, in.State) {
				return ErrTransition
			}
			if t.Assignment.AssignedTo == nil {
				return fmt.Errorf("%w: przypisz zadanie przed rozpoczęciem", ErrInvalid)
			}
			if !a.Has("request.manage") && *t.Assignment.AssignedTo != a.UserID {
				return ErrForbidden
			}
			in.Reason = strings.TrimSpace(in.Reason)
			if !catalog.Text(in.Reason, 0, 10000) || (in.State == "ON_HOLD" || in.State == "COMPLETED") && !catalog.Text(in.Reason, 1, 10000) {
				return ErrInvalid
			}
			data["task_state_before"] = t.State
			t.State = in.State
			data["task_state_after"] = t.State
			data["state"] = t.State
			if _, err := e.ExecContext(ctx, "UPDATE fulfillment_tasks SET state=?,result=? WHERE tenant_id=? AND id=?", t.State, in.Reason, a.TenantID, id); err != nil {
				return err
			}
			if r.State == "SUBMITTED" {
				r.State = "IN_PROGRESS"
			}
		}
		if _, err := e.ExecContext(ctx, "UPDATE fulfillment_tasks SET version=version+1,updated_at=? WHERE tenant_id=? AND id=?", time.Now().UTC(), a.TenantID, id); err != nil {
			return err
		}
		if _, err := e.ExecContext(ctx, "UPDATE service_requests SET version=version+1,state=?,updated_at=? WHERE tenant_id=? AND id=?", r.State, time.Now().UTC(), a.TenantID, r.ID); err != nil {
			return err
		}
		r.Version++
		if err := s.event(ctx, e, a, r, kind, visibility, fmt.Sprintf("request-task:%d:%d", id, t.Version+1), data); err != nil {
			return err
		}
		out, err = s.getTask(ctx, e, a, id)
		return err
	})
	return out, err
}
func (s *Service) List(ctx context.Context, a Actor, tasks bool, q, state, view string, before uint64, limit int) (any, error) {
	if limit < 1 || limit > 100 || !catalog.Text(q, 0, 200) {
		return nil, ErrInvalid
	}
	if tasks && !a.Has("request.manage") && !a.Has("request.fulfill") || !tasks && !a.Has("request.manage") && !a.Has("request.fulfill") && !a.Has("request.read_own") {
		return nil, ErrForbidden
	}
	if state != "" && !contains([]string{"SUBMITTED", "IN_PROGRESS", "FULFILLED", "CLOSED", "CANCELLED", "OPEN", "ON_HOLD", "COMPLETED"}, state) {
		return nil, ErrInvalid
	}
	alias := "r"
	p, pa := requestPredicate(a, "r")
	if tasks {
		alias = "t"
		p, pa = taskPredicate(a, "t")
	}
	query := "SELECT r.id,r.number,r.requester_id,u.display_name,r.state,r.reason,r.version,r.created_at,r.updated_at FROM service_requests r JOIN users u ON u.id=r.requester_id"
	if tasks {
		query = "SELECT " + taskColumns + taskJoins
	}
	query += " WHERE " + alias + ".tenant_id=? AND " + p
	args := []any{a.TenantID}
	args = append(args, pa...)
	if before > 0 {
		query += " AND " + alias + ".id<?"
		args = append(args, before)
	}
	if state != "" {
		query += " AND " + alias + ".state=?"
		args = append(args, state)
	}
	if !tasks {
		if view != "mine" && view != "all" {
			return nil, ErrInvalid
		}
		if view == "mine" {
			query += " AND r.requester_id=?"
			args = append(args, a.UserID)
		}
	} else {
		if view != "group" && view != "mine" && view != "unassigned" {
			return nil, ErrInvalid
		}
		if view != "group" {
			query += " AND EXISTS(SELECT 1 FROM work_assignments wa WHERE wa.tenant_id=t.tenant_id AND wa.object_type='fulfillment_task' AND wa.object_id=CAST(t.id AS CHAR) AND "
			if view == "mine" {
				query += "wa.assigned_to=?)"
				args = append(args, a.UserID)
			} else {
				query += "wa.assigned_to IS NULL)"
			}
		}
	}
	query += " AND (?='' OR LOCATE(?," + alias + ".number)>0"
	args = append(args, q, q)
	if tasks {
		query += " OR LOCATE(?,t.title)>0"
		args = append(args, q)
	}
	query += ") ORDER BY " + alias + ".id DESC LIMIT ?"
	args = append(args, limit+1)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if tasks {
		out := TaskPage{Items: []Task{}}
		for rows.Next() {
			t, err := scanTask(rows, a.TenantID)
			if err != nil {
				return nil, err
			}
			out.Items = append(out.Items, t)
		}
		if len(out.Items) > limit {
			out.Items = out.Items[:limit]
			out.Next = out.Items[limit-1].ID
		}
		return out, rows.Err()
	}
	out := RequestPage{Items: []Request{}}
	for rows.Next() {
		var r Request
		r.Actions = []string{}
		if err := rows.Scan(&r.ID, &r.Number, &r.RequesterID, &r.RequesterName, &r.State, &r.Reason, &r.Version, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		out.Items = append(out.Items, r)
	}
	if len(out.Items) > limit {
		out.Items = out.Items[:limit]
		out.Next = out.Items[limit-1].ID
	}
	return out, rows.Err()
}
func (s *Service) History(ctx context.Context, a Actor, id, before uint64, limit int) (activity.Page, error) {
	if _, err := s.Get(ctx, a, id); err != nil {
		return activity.Page{}, err
	}
	internal, err := s.canOperate(ctx, s.db, a, id)
	if err != nil {
		return activity.Page{}, err
	}
	return activity.NewRepository(s.db).ListVisibleBySourceEvent(ctx, a.TenantID, "request", fmt.Sprint(id), before, limit, internal)
}
func RegisterEventHandlers(d *events.Dispatcher, p *activity.Projector) error {
	for kind, summary := range map[string]string{"catalog.changed": "Zmieniono katalog usług", "request.created": "Złożono wniosek", "request.state.changed": "Zmieniono stan wniosku", "request.task.changed": "Zmieniono stan zadania", "request.task.assigned": "Zmieniono przypisanie zadania", "request.comment.added": "Odpowiedź dla zgłaszającego", "request.note.added": "Notatka wewnętrzna", "request.ci.changed": "Zmieniono powiązania CMDB"} {
		if err := d.Register(kind, p.Handler(kind, summary)); err != nil {
			return err
		}
	}
	return nil
}
