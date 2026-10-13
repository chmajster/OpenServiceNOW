package problems

import (
	"context"
	"fmt"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
)

// ChangeSnapshot includes only the public fields explicitly proposed for a
// remedial Change. Messages, internal notes and attachments are never copied.
type ChangeSnapshot struct {
	ID           uint64 `json:"id,string"`
	Number       string `json:"number"`
	Title        string `json:"title"`
	Description  string `json:"description"`
	Impact       string `json:"impact"`
	SolutionPlan string `json:"solution_plan"`
	State        string `json:"state"`
	Version      uint64 `json:"version,string"`
	CanLink      bool   `json:"can_link"`
}

func (s *Service) ChangeReferences(ctx context.Context, a Actor, q string, change, before uint64, limit int) ([]ChangeSnapshot, uint64, error) {
	out := []ChangeSnapshot{}
	if limit < 1 || limit > 100 || len([]rune(q)) > 160 {
		return out, 0, ErrInvalid
	}
	p, args := Access(a, "p")
	params := append([]any{a.TenantID}, args...)
	query := "SELECT p.id,p.number,p.title,p.impact,p.state,p.version" + joins + " WHERE p.tenant_id=? AND " + p
	if change > 0 {
		query += " AND EXISTS(SELECT 1 FROM change_problem_links l WHERE l.tenant_id=p.tenant_id AND l.problem_id=p.id AND l.change_id=?)"
		params = append(params, change)
	}
	if q != "" {
		query += " AND (LOCATE(?,p.title)>0 OR LOCATE(?,p.number)>0)"
		params = append(params, q, q)
	}
	if before > 0 {
		query += " AND p.id<?"
		params = append(params, before)
	}
	query += " ORDER BY p.id DESC LIMIT ?"
	params = append(params, limit+1)
	rows, err := s.db.QueryContext(ctx, query, params...)
	if err != nil {
		return out, 0, err
	}
	defer rows.Close()
	for rows.Next() {
		var r ChangeSnapshot
		if err = rows.Scan(&r.ID, &r.Number, &r.Title, &r.Impact, &r.State, &r.Version); err != nil {
			return out, 0, err
		}
		r.CanLink = a.Has("problem.manage") && editable(r.State)
		out = append(out, r)
	}
	var next uint64
	if len(out) > limit {
		out = out[:limit]
		next = out[limit-1].ID
	}
	if err = rows.Err(); err != nil {
		return out, 0, fmt.Errorf("list problem references: %w", err)
	}
	return out, next, nil
}
func (s *Service) ChangeReference(ctx context.Context, e database.Executor, a Actor, id uint64, write bool, version uint64) (ChangeSnapshot, error) {
	r, err := s.get(ctx, e, a, id, write)
	if err != nil {
		return ChangeSnapshot{}, err
	}
	if write {
		if !r.CanEdit {
			return ChangeSnapshot{}, ErrForbidden
		}
		if version == 0 || version != r.Version {
			return ChangeSnapshot{}, ErrConflict
		}
		r, err = s.touch(ctx, e, a, r, "change_links")
		if err != nil {
			return ChangeSnapshot{}, err
		}
	}
	return ChangeSnapshot{r.ID, r.Number, r.Title, r.Description, r.Impact, r.SolutionPlan, r.State, r.Version, r.CanEdit}, nil
}
