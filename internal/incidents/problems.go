package incidents

import (
	"context"
	"errors"
	"fmt"
	"github.com/chmajster/OpenServiceNOW/internal/assets"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/problems"
)

type ProblemDomain struct{ service *Service }

func NewProblemDomain(s *Service) *ProblemDomain { return &ProblemDomain{s} }
func problemActor(p problems.Actor) Actor {
	return Actor{TenantID: p.TenantID, UserID: p.UserID, Permissions: p.Permissions, RequestID: p.RequestID, IPAddress: p.IPAddress}
}
func (d *ProblemDomain) ProblemReference(ctx context.Context, e database.Executor, p problems.Actor, id uint64, write bool, version uint64) (problems.IncidentSnapshot, error) {
	a := problemActor(p)
	r, err := d.service.repo.Get(ctx, e, a.TenantID, id, write)
	if err != nil {
		return problems.IncidentSnapshot{}, problemError(err)
	}
	if !readable(a, r) {
		return problems.IncidentSnapshot{}, problems.ErrNotFound
	}
	canLink := canLinkCI(a, r)
	if write {
		if !canLink {
			return problems.IncidentSnapshot{}, problems.ErrForbidden
		}
		if version == 0 || r.Version != version {
			return problems.IncidentSnapshot{}, problems.ErrConflict
		}
		before := r
		r.UpdatedAt = d.service.now().UTC()
		if err = d.service.repo.Save(ctx, e, r, version); err != nil {
			return problems.IncidentSnapshot{}, err
		}
		r.Version++
		if err = d.service.recordChange(ctx, e, a, "incident.updated", &before, r, "Zmieniono powiązania z problemami"); err != nil {
			return problems.IncidentSnapshot{}, err
		}
	}
	impact := "LOW"
	if r.Impact == 1 {
		impact = "HIGH"
	} else if r.Impact == 2 {
		impact = "MEDIUM"
	}
	return problems.IncidentSnapshot{ID: r.ID, Number: r.Number, Title: r.Title, Description: r.Description, Impact: impact, State: r.State, Version: r.Version, CanLink: canLink}, nil
}
func (d *ProblemDomain) ProblemReferences(ctx context.Context, p problems.Actor, q string, problem, before uint64, limit int) ([]problems.IncidentSnapshot, uint64, error) {
	out := []problems.IncidentSnapshot{}
	a := problemActor(p)
	if limit < 1 || limit > 100 || !assets.Text(q, 0, 160) {
		return out, 0, problems.ErrInvalid
	}
	if !a.Has("read") && !a.Has("read_own") {
		return out, 0, nil
	}
	query := "SELECT i.id,i.number,i.title,i.state,i.version FROM incidents i WHERE i.tenant_id=?"
	args := []any{a.TenantID}
	if !a.Has("read") {
		query += " AND (i.reporter_id=? OR i.affected_user_id=?)"
		args = append(args, a.UserID, a.UserID)
	}
	if problem > 0 {
		query += " AND EXISTS(SELECT 1 FROM problem_incident_links l WHERE l.tenant_id=i.tenant_id AND l.incident_id=i.id AND l.problem_id=?)"
		args = append(args, problem)
	}
	if q != "" {
		query += " AND (LOCATE(?,i.number)>0 OR LOCATE(?,i.title)>0)"
		args = append(args, q, q)
	}
	if before > 0 {
		query += " AND i.id<?"
		args = append(args, before)
	}
	query += " ORDER BY i.id DESC LIMIT ?"
	args = append(args, limit+1)
	rows, err := d.service.repo.db.QueryContext(ctx, query, args...)
	if err != nil {
		return out, 0, err
	}
	defer rows.Close()
	for rows.Next() {
		var r problems.IncidentSnapshot
		if err = rows.Scan(&r.ID, &r.Number, &r.Title, &r.State, &r.Version); err != nil {
			return out, 0, fmt.Errorf("read incident reference: %w", err)
		}
		out = append(out, r)
	}
	next := uint64(0)
	if len(out) > limit {
		out = out[:limit]
		next = out[limit-1].ID
	}
	return out, next, rows.Err()
}

func problemError(err error) error {
	if errors.Is(err, ErrNotFound) || errors.Is(err, ErrForbidden) {
		return problems.ErrNotFound
	}
	if errors.Is(err, ErrConflict) {
		return problems.ErrConflict
	}
	return err
}
