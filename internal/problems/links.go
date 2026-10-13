package problems

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/chmajster/OpenServiceNOW/internal/cmdb"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"strings"
)

func missing(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

type LinkInput struct {
	Version         uint64 `json:"version,string"`
	IncidentVersion uint64 `json:"incident_version,string"`
}
type IncidentPage struct {
	Items []IncidentSnapshot `json:"items"`
	Next  uint64             `json:"next_before_id,string,omitempty"`
}

func (s *Service) SourceIncident(ctx context.Context, a Actor, id uint64) (IncidentSnapshot, error) {
	if !a.Has("problem.read") && !a.Has("problem.read_own") && !a.Has("problem.create") && !a.Has("problem.manage") || s.Incidents == nil {
		return IncidentSnapshot{}, ErrForbidden
	}
	return s.Incidents.ProblemReference(ctx, s.db, a, id, false, 0)
}
func (s *Service) IncidentChoices(ctx context.Context, a Actor, q string, problem, before uint64, limit int) (IncidentPage, error) {
	if s.Incidents == nil {
		return IncidentPage{}, ErrInvalid
	}
	if problem > 0 {
		if _, err := s.Get(ctx, a, problem); err != nil {
			return IncidentPage{}, err
		}
	} else if !a.Has("problem.create") && !a.Has("problem.manage") {
		return IncidentPage{}, ErrForbidden
	}
	items, next, err := s.Incidents.ProblemReferences(ctx, a, q, problem, before, limit)
	return IncidentPage{items, next}, err
}
func (s *Service) LinkIncident(ctx context.Context, a Actor, id, incident uint64, in LinkInput, remove bool, key string) (Record, error) {
	var out Record
	if in.Version == 0 || in.IncidentVersion == 0 || s.Incidents == nil {
		return out, ErrInvalid
	}
	raw, _ := json.Marshal(struct {
		ID, Incident uint64
		Input        LinkInput
		Remove       bool
	}{id, incident, in, remove})
	err := s.withProblem(ctx, a, id, func(e database.Executor, a Actor, r Record) error {
		if !r.CanEdit {
			return ErrForbidden
		}
		old, err := reserve(ctx, e, a, key, "problem_incident_link", sha256.Sum256(raw))
		if err != nil {
			return err
		}
		if old > 0 {
			if _, err = s.Incidents.ProblemReference(ctx, e, a, incident, false, 0); err != nil {
				return err
			}
			out = r
			return nil
		}
		if r.Version != in.Version {
			return ErrConflict
		}
		var exists bool
		if err = e.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM problem_incident_links WHERE tenant_id=? AND problem_id=? AND incident_id=?)", a.TenantID, id, incident).Scan(&exists); err != nil {
			return err
		}
		ref, err := s.Incidents.ProblemReference(ctx, e, a, incident, false, 0)
		if err != nil {
			return err
		}
		if !ref.CanLink {
			return ErrForbidden
		}
		if ref.Version != in.IncidentVersion {
			return ErrConflict
		}
		if exists == !remove {
			out = r
			return complete(ctx, e, a, key, id)
		}
		if !remove {
			var count uint64
			if err = e.QueryRowContext(ctx, "SELECT COUNT(*) FROM problem_incident_links WHERE tenant_id=? AND problem_id=?", a.TenantID, id).Scan(&count); err != nil {
				return err
			}
			if count >= 1000 {
				return fmt.Errorf("%w: limit 1000 powiązanych incydentów", ErrInvalid)
			}
		}
		if _, err = s.Incidents.ProblemReference(ctx, e, a, incident, true, in.IncidentVersion); err != nil {
			return err
		}
		if remove {
			_, err = e.ExecContext(ctx, "DELETE FROM problem_incident_links WHERE tenant_id=? AND problem_id=? AND incident_id=?", a.TenantID, id, incident)
		} else {
			_, err = e.ExecContext(ctx, "INSERT INTO problem_incident_links(tenant_id,problem_id,incident_id) VALUES(?,?,?)", a.TenantID, id, incident)
		}
		if err != nil {
			return err
		}
		out, err = s.touch(ctx, e, a, r, "incident_links")
		if err != nil {
			return err
		}
		return complete(ctx, e, a, key, id)
	})
	return out, err
}

type CMDBDomain struct{ service *Service }

func NewCMDBDomain(s *Service) *CMDBDomain { return &CMDBDomain{s} }
func (d *CMDBDomain) ReferenceInTx(ctx context.Context, e database.Executor, a cmdb.Actor, id uint64, write bool, version uint64) (cmdb.Reference, error) {
	r, err := d.service.get(ctx, e, a, id, write)
	if errors.Is(err, ErrNotFound) {
		return cmdb.Reference{}, cmdb.ErrNotFound
	}
	if err != nil {
		return cmdb.Reference{}, err
	}
	if write {
		if !r.CanEdit {
			return cmdb.Reference{}, cmdb.ErrNotFound
		}
		if version == 0 || r.Version != version {
			return cmdb.Reference{}, cmdb.ErrConflict
		}
		r, err = d.service.touch(ctx, e, a, r, "ci_links")
		if err != nil {
			return cmdb.Reference{}, err
		}
	}
	return cmdb.Reference{ID: r.ID, Number: r.Number, Title: r.Title, State: r.State, Version: r.Version, CanLink: r.CanEdit}, nil
}
func (d *CMDBDomain) References(ctx context.Context, a cmdb.Actor, q string, ci, before uint64, limit int) (cmdb.ReferencePage, error) {
	out := cmdb.ReferencePage{Items: []cmdb.Reference{}}
	if limit < 1 || limit > 100 || len([]rune(q)) > 160 {
		return out, cmdb.ErrInvalid
	}
	p, args := Access(a, "p")
	query := "SELECT p.id,p.number,p.title,p.state,p.version" + joins + " WHERE p.tenant_id=? AND " + p
	args = append([]any{a.TenantID}, args...)
	if ci > 0 {
		query += " AND EXISTS(SELECT 1 FROM cmdb_problem_links l WHERE l.tenant_id=p.tenant_id AND l.problem_id=p.id AND l.ci_id=?)"
		args = append(args, ci)
	}
	if q != "" {
		query += " AND (LOCATE(?,p.number)>0 OR LOCATE(?,p.title)>0)"
		args = append(args, strings.TrimSpace(q), strings.TrimSpace(q))
	}
	if before > 0 {
		query += " AND p.id<?"
		args = append(args, before)
	}
	query += " ORDER BY p.id DESC LIMIT ?"
	args = append(args, limit+1)
	rows, err := d.service.db.QueryContext(ctx, query, args...)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var r cmdb.Reference
		if err = rows.Scan(&r.ID, &r.Number, &r.Title, &r.State, &r.Version); err != nil {
			return out, err
		}
		out.Items = append(out.Items, r)
	}
	if len(out.Items) > limit {
		out.Items = out.Items[:limit]
		out.Next = out.Items[limit-1].ID
	}
	return out, rows.Err()
}
