package problems

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/chmajster/OpenServiceNOW/internal/catalog"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"strings"
	"time"
)

type Task struct {
	ID           uint64    `json:"id,string"`
	ProblemID    uint64    `json:"problem_id,string"`
	Number       string    `json:"number"`
	Title        string    `json:"title"`
	Instructions string    `json:"instructions"`
	Required     bool      `json:"required"`
	State        string    `json:"state"`
	Result       string    `json:"result"`
	Version      uint64    `json:"version,string"`
	AssignedTo   *uint64   `json:"assigned_to,string"`
	GroupID      *uint64   `json:"assignment_group_id,string"`
	AssignedName string    `json:"assigned_name"`
	GroupName    string    `json:"group_name"`
	UpdatedAt    time.Time `json:"updated_at"`
	CanEdit      bool      `json:"can_edit"`
}
type TaskInput struct {
	Version        uint64  `json:"version,string"`
	ProblemVersion uint64  `json:"problem_version,string"`
	Title          string  `json:"title"`
	Instructions   string  `json:"instructions"`
	Required       bool    `json:"required"`
	State          string  `json:"state"`
	Result         string  `json:"result"`
	AssignedTo     *uint64 `json:"assigned_to,string"`
	GroupID        *uint64 `json:"assignment_group_id,string"`
}
type TaskPage struct {
	Items []Task `json:"items"`
	Next  uint64 `json:"next_before_id,string,omitempty"`
}

const taskSelect = "SELECT t.id,t.problem_id,t.number,t.title,t.instructions,t.required,t.state,t.result,t.version,t.updated_at,wa.assigned_to,wa.assignment_group_id,COALESCE(u.display_name,''),COALESCE(g.name,'') FROM problem_tasks t JOIN work_assignments wa ON wa.tenant_id=t.tenant_id AND wa.object_type='problem_task' AND wa.object_id=CAST(t.id AS CHAR) LEFT JOIN users u ON u.id=wa.assigned_to LEFT JOIN `groups` g ON g.tenant_id=wa.tenant_id AND g.id=wa.assignment_group_id"

func scanTask(row interface{ Scan(...any) error }, edit bool) (Task, error) {
	var t Task
	err := row.Scan(&t.ID, &t.ProblemID, &t.Number, &t.Title, &t.Instructions, &t.Required, &t.State, &t.Result, &t.Version, &t.UpdatedAt, &t.AssignedTo, &t.GroupID, &t.AssignedName, &t.GroupName)
	t.CanEdit = edit
	return t, err
}
func (s *Service) Tasks(ctx context.Context, a Actor, id, before uint64, limit int) (TaskPage, error) {
	out := TaskPage{Items: []Task{}}
	r, err := s.Get(ctx, a, id)
	if err != nil {
		return out, err
	}
	if limit < 1 || limit > 100 {
		return out, ErrInvalid
	}
	q := taskSelect + " WHERE t.tenant_id=? AND t.problem_id=?"
	args := []any{a.TenantID, id}
	if before > 0 {
		q += " AND t.id<?"
		args = append(args, before)
	}
	q += " ORDER BY t.id DESC LIMIT ?"
	args = append(args, limit+1)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		t, err := scanTask(rows, r.CanTasks)
		if err != nil {
			return out, err
		}
		out.Items = append(out.Items, t)
	}
	if len(out.Items) > limit {
		out.Items = out.Items[:limit]
		out.Next = out.Items[limit-1].ID
	}
	return out, rows.Err()
}
func (s *Service) SaveTask(ctx context.Context, a Actor, problem, id uint64, in TaskInput, key string) (Task, error) {
	var out Task
	in.Title = strings.TrimSpace(in.Title)
	in.Instructions = strings.TrimSpace(in.Instructions)
	in.Result = strings.TrimSpace(in.Result)
	if !catalog.Text(in.Title, 1, 160) || !catalog.Text(in.Instructions, 1, 20000) || !catalog.Text(in.Result, 0, 20000) || !contains([]string{"OPEN", "IN_PROGRESS", "COMPLETED", "CANCELLED"}, in.State) || in.GroupID == nil || *in.GroupID == 0 {
		return out, ErrInvalid
	}
	if id == 0 && (in.Version != 0 || in.State != "OPEN" || in.Result != "") {
		return out, ErrInvalid
	}
	raw, _ := json.Marshal(struct {
		Problem, ID uint64
		Input       TaskInput
	}{problem, id, in})
	err := s.withProblem(ctx, a, problem, func(e database.Executor, a Actor, r Record) error {
		if !a.Has("problem.tasks") {
			return ErrForbidden
		}
		old, err := reserve(ctx, e, a, key, "problem_task_save", sha256.Sum256(raw))
		if err != nil {
			return err
		}
		if old > 0 {
			out, err = scanTask(e.QueryRowContext(ctx, taskSelect+" WHERE t.tenant_id=? AND t.id=? AND t.problem_id=?", a.TenantID, old, problem), r.CanTasks)
			return err
		}
		if !r.CanTasks {
			return ErrTransition
		}
		if in.ProblemVersion == 0 || r.Version != in.ProblemVersion {
			return ErrConflict
		}
		if in.AssignedTo != nil {
			var member bool
			if err = e.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM group_memberships WHERE tenant_id=? AND group_id=? AND user_id=?)", a.TenantID, *in.GroupID, *in.AssignedTo).Scan(&member); err != nil {
				return err
			}
			if !member {
				return ErrInvalid
			}
		}
		if id == 0 {
			number, err := s.allocator.NextInTx(ctx, e, a.TenantID, "problem_task", "PRBTASK", 7)
			if err != nil {
				return err
			}
			result, err := e.ExecContext(ctx, "INSERT INTO problem_tasks(tenant_id,problem_id,number,title,instructions,required,result) VALUES(?,?,?,?,?,?,'')", a.TenantID, problem, number, in.Title, in.Instructions, in.Required)
			if err != nil {
				return err
			}
			n, err := result.LastInsertId()
			if err != nil {
				return err
			}
			id = uint64(n)
			if _, err = s.work.SetAssignment(ctx, e, a.TenantID, "problem_task", fmt.Sprint(id), 0, in.AssignedTo, in.GroupID); err != nil {
				return err
			}
		} else {
			old, err := scanTask(e.QueryRowContext(ctx, taskSelect+" WHERE t.tenant_id=? AND t.problem_id=? AND t.id=? FOR UPDATE", a.TenantID, problem, id), true)
			if err != nil {
				return missing(err)
			}
			if in.Version == 0 || old.Version != in.Version {
				return ErrConflict
			}
			if old.State != in.State {
				allowed := old.State == "OPEN" && (in.State == "IN_PROGRESS" || in.State == "CANCELLED") || old.State == "IN_PROGRESS" && (in.State == "COMPLETED" || in.State == "CANCELLED") || old.State == "CANCELLED" && in.State == "OPEN"
				if !allowed {
					return ErrTransition
				}
			}
			if old.State == "COMPLETED" {
				return ErrTransition
			}
			if (in.State == "COMPLETED" || in.State == "CANCELLED") && in.Result == "" {
				return ErrInvalid
			}
			var av uint64
			if err = e.QueryRowContext(ctx, "SELECT version FROM work_assignments WHERE tenant_id=? AND object_type='problem_task' AND object_id=? FOR UPDATE", a.TenantID, fmt.Sprint(id)).Scan(&av); err != nil {
				return err
			}
			if _, err = s.work.SetAssignment(ctx, e, a.TenantID, "problem_task", fmt.Sprint(id), av, in.AssignedTo, in.GroupID); err != nil {
				return err
			}
			if _, err = e.ExecContext(ctx, "UPDATE problem_tasks SET title=?,instructions=?,required=?,state=?,result=?,version=version+1,updated_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND id=? AND version=?", in.Title, in.Instructions, in.Required, in.State, in.Result, a.TenantID, id, in.Version); err != nil {
				return err
			}
		}
		out, err = scanTask(e.QueryRowContext(ctx, taskSelect+" WHERE t.tenant_id=? AND t.id=?", a.TenantID, id), true)
		if err != nil {
			return err
		}
		r, err = s.touch(ctx, e, a, r, "analytical_tasks")
		if err != nil {
			return err
		}
		if err = s.event(ctx, e, a, r, "problem.task.changed", "PUBLIC", fmt.Sprintf("problem-task:%d:%d", id, out.Version), map[string]any{"resource_id": fmt.Sprint(id), "task_number": out.Number, "state": out.State}); err != nil {
			return err
		}
		return complete(ctx, e, a, key, id)
	})
	return out, err
}

func (s *Service) Task(ctx context.Context, a Actor, problem, id uint64) (Task, error) {
	r, err := s.Get(ctx, a, problem)
	if err != nil {
		return Task{}, err
	}
	t, err := scanTask(s.db.QueryRowContext(ctx, taskSelect+" WHERE t.tenant_id=? AND t.problem_id=? AND t.id=?", a.TenantID, problem, id), r.CanTasks)
	return t, missing(err)
}
