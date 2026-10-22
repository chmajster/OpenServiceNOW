package requests

import (
	"context"
	"errors"
	"fmt"
	"github.com/chmajster/OpenServiceNOW/internal/assets"
	"github.com/chmajster/OpenServiceNOW/internal/cmdb"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"strings"
)

type CMDBDomain struct{ service *Service }

func NewCMDBDomain(s *Service) *CMDBDomain { return &CMDBDomain{s} }
func cmdbError(err error) error {
	if errors.Is(err, ErrNotFound) || errors.Is(err, ErrForbidden) {
		return cmdb.ErrNotFound
	}
	if errors.Is(err, ErrConflict) {
		return cmdb.ErrConflict
	}
	return err
}
func (d *CMDBDomain) ReferenceInTx(ctx context.Context, e database.Executor, a cmdb.Actor, id uint64, write bool, version uint64) (cmdb.Reference, error) {
	r, err := d.service.get(ctx, e, a, id, write)
	if err != nil {
		return cmdb.Reference{}, cmdbError(err)
	}
	operator, err := d.service.canOperate(ctx, e, a, id)
	if err != nil {
		return cmdb.Reference{}, err
	}
	allowed := (r.State == "SUBMITTED" || r.State == "IN_PROGRESS") && (operator || r.State == "SUBMITTED" && r.RequesterID == a.UserID && a.Has("request.create") && a.Has("request.read_own"))
	if write {
		if !allowed {
			return cmdb.Reference{}, cmdb.ErrNotFound
		}
		if version == 0 || version != r.Version {
			return cmdb.Reference{}, cmdb.ErrConflict
		}
		if _, err = e.ExecContext(ctx, "UPDATE service_requests SET version=version+1,updated_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND id=? AND version=?", a.TenantID, id, version); err != nil {
			return cmdb.Reference{}, err
		}
		r.Version++
		if err = d.service.event(ctx, e, a, r, "request.ci.changed", "PUBLIC", fmt.Sprintf("request:%d:%d", r.ID, r.Version), map[string]any{"state": r.State}); err != nil {
			return cmdb.Reference{}, err
		}
	}
	title := ""
	if len(r.Items) > 0 {
		title = r.Items[0].Snapshot.Title
	}
	return cmdb.Reference{ID: r.ID, Number: r.Number, Title: title, State: r.State, Version: r.Version, CanLink: allowed}, nil
}
func (d *CMDBDomain) References(ctx context.Context, a cmdb.Actor, q string, ci, before uint64, limit int) (cmdb.ReferencePage, error) {
	out := cmdb.ReferencePage{Items: []cmdb.Reference{}}
	if limit < 1 || limit > 100 || !assets.Text(q, 0, 160) {
		return out, cmdb.ErrInvalid
	}
	if !a.Has("request.manage") && !a.Has("request.read_own") && !a.Has("request.fulfill") {
		return out, cmdb.ErrNotFound
	}
	p, args := requestPredicate(a, "r")
	params := []any{a.TenantID}
	params = append(params, args...)
	title := "COALESCE((SELECT JSON_UNQUOTE(JSON_EXTRACT(ri.snapshot,'$.title')) FROM requested_items ri WHERE ri.tenant_id=r.tenant_id AND ri.request_id=r.id ORDER BY ri.id LIMIT 1),'')"
	query := "SELECT r.id,r.number," + title + ",r.state,r.version FROM service_requests r WHERE r.tenant_id=? AND " + p
	if ci > 0 {
		query += " AND EXISTS(SELECT 1 FROM cmdb_request_links l WHERE l.tenant_id=r.tenant_id AND l.request_id=r.id AND l.ci_id=?)"
		params = append(params, ci)
	}
	if q != "" {
		query += " AND (r.number LIKE ? ESCAPE '!' OR " + title + " LIKE ? ESCAPE '!')"
		v := "%" + strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(q) + "%"
		params = append(params, v, v)
	}
	if before > 0 {
		query += " AND r.id<?"
		params = append(params, before)
	}
	query += " ORDER BY r.id DESC LIMIT ?"
	params = append(params, limit+1)
	rows, err := d.service.db.QueryContext(ctx, query, params...)
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
