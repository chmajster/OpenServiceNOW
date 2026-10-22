package incidents

import (
	"context"
	"errors"
	"github.com/chmajster/OpenServiceNOW/internal/assets"
	"github.com/chmajster/OpenServiceNOW/internal/cmdb"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"strings"
)

type CMDBDomain struct{ service *Service }

func NewCMDBDomain(s *Service) *CMDBDomain { return &CMDBDomain{s} }
func cmdbActor(a cmdb.Actor) Actor {
	return Actor{TenantID: a.TenantID, UserID: a.UserID, Permissions: a.Permissions, RequestID: a.RequestID, IPAddress: a.IPAddress}
}
func cmdbError(err error) error {
	if errors.Is(err, ErrNotFound) || errors.Is(err, ErrForbidden) {
		return cmdb.ErrNotFound
	}
	if errors.Is(err, ErrConflict) {
		return cmdb.ErrConflict
	}
	return err
}
func canLinkCI(a Actor, r Record) bool {
	return readable(a, r) && editable(r) && (a.Has("update") || r.State == "NEW" && r.ReporterID == a.UserID && a.Has("create"))
}
func (d *CMDBDomain) ReferenceInTx(ctx context.Context, e database.Executor, p cmdb.Actor, id uint64, write bool, version uint64) (cmdb.Reference, error) {
	a := cmdbActor(p)
	r, err := d.service.repo.Get(ctx, e, a.TenantID, id, write)
	if err != nil {
		return cmdb.Reference{}, cmdbError(err)
	}
	if !readable(a, r) {
		return cmdb.Reference{}, cmdb.ErrNotFound
	}
	allowed := canLinkCI(a, r)
	if write {
		if !allowed {
			return cmdb.Reference{}, cmdb.ErrNotFound
		}
		if version == 0 || r.Version != version {
			return cmdb.Reference{}, cmdb.ErrConflict
		}
		before := r
		r.UpdatedAt = d.service.now().UTC()
		if err = d.service.repo.Save(ctx, e, r, version); err != nil {
			return cmdb.Reference{}, cmdbError(err)
		}
		r.Version++
		if err = d.service.recordChange(ctx, e, a, "incident.updated", &before, r, "Zaktualizowano powiązania CMDB"); err != nil {
			return cmdb.Reference{}, err
		}
	}
	return cmdb.Reference{ID: r.ID, Number: r.Number, Title: r.Title, State: r.State, Version: r.Version, CanLink: allowed}, nil
}
func (d *CMDBDomain) References(ctx context.Context, p cmdb.Actor, q string, ci, before uint64, limit int) (cmdb.ReferencePage, error) {
	out := cmdb.ReferencePage{Items: []cmdb.Reference{}}
	a := cmdbActor(p)
	if !a.Has("read") && !a.Has("read_own") {
		return out, cmdb.ErrNotFound
	}
	if limit < 1 || limit > 100 || !assets.Text(q, 0, 160) {
		return out, cmdb.ErrInvalid
	}
	query := "SELECT i.id,i.number,i.title,i.state,i.version FROM incidents i WHERE i.tenant_id=?"
	args := []any{a.TenantID}
	if !a.Has("read") {
		query += " AND (i.reporter_id=? OR i.affected_user_id=?)"
		args = append(args, a.UserID, a.UserID)
	}
	if ci > 0 {
		query += " AND EXISTS(SELECT 1 FROM cmdb_incident_links l WHERE l.tenant_id=i.tenant_id AND l.incident_id=i.id AND l.ci_id=?)"
		args = append(args, ci)
	}
	if q != "" {
		query += " AND (i.number LIKE ? ESCAPE '!' OR i.title LIKE ? ESCAPE '!')"
		v := "%" + strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(q) + "%"
		args = append(args, v, v)
	}
	if before > 0 {
		query += " AND i.id<?"
		args = append(args, before)
	}
	query += " ORDER BY i.id DESC LIMIT ?"
	args = append(args, limit+1)
	rows, err := d.service.repo.db.QueryContext(ctx, query, args...)
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
