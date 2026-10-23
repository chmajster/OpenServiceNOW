package serviceportfolio

import (
	"context"
	"sort"

	"github.com/chmajster/OpenServiceNOW/internal/cmdb"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
)

type cmdbWorkAdapter struct {
	reader CIReader
	kind   string
}

func (a cmdbWorkAdapter) List(ctx context.Context, actor Actor, ciIDs []uint64, before uint64, limit int) ([]cmdb.Reference, error) {
	items := []cmdb.Reference{}
	seen := map[uint64]bool{}
	for _, ciID := range ciIDs {
		page, err := a.reader.Records(ctx, actor, a.kind, "", ciID, before, limit)
		if err != nil {
			return nil, err
		}
		for _, item := range page.Items {
			if !seen[item.ID] {
				seen[item.ID] = true
				items = append(items, item)
			}
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID > items[j].ID })
	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (s *Service) RelatedRecords(ctx context.Context, a Actor, id uint64, kind string, before uint64, limit int) (RelatedRecordsResult, error) {
	out := RelatedRecordsResult{Kind: kind, Items: []cmdb.Reference{}}
	if !CanRead(a) {
		return out, workrecords.ErrForbidden
	}
	if limit < 1 || limit > 100 {
		return out, ErrInvalid
	}
	if _, err := s.Get(ctx, a, id); err != nil {
		return out, err
	}
	if kind != "incidents" && kind != "requests" && kind != "problems" && kind != "changes" {
		return out, ErrInvalid
	}
	adapter := s.relatedAdapters[kind]
	if adapter == nil {
		out.Available = false
		out.Message = "Domena nie udostępnia jeszcze adaptera portfela usług."
		return out, nil
	}
	if kind == "incidents" && !a.Has("incident.read") && !a.Has("incident.read_own") {
		return out, workrecords.ErrForbidden
	}
	if kind == "requests" && !a.Has("request.read_own") && !a.Has("request.manage") && !a.Has("request.fulfill") {
		return out, workrecords.ErrForbidden
	}
	access, args := portfolioAccess(a, "p")
	ciClause, ciArgs := ciAccess(a, "ci")
	subAccess, subArgs := portfolioAccess(a, "p2")
	query := `SELECT DISTINCT ci.id FROM cmdb_cis ci WHERE ci.tenant_id=? AND ` + ciClause + `
 AND (ci.id IN(SELECT p.ci_id FROM service_portfolio_items p WHERE p.tenant_id=ci.tenant_id AND p.id=? AND ` + access + `)
  OR ci.id IN(SELECT r.target_ci_id FROM service_portfolio_relations r JOIN service_portfolio_items p ON p.tenant_id=r.tenant_id AND p.id=r.source_item_id WHERE r.tenant_id=ci.tenant_id AND p.id=? AND ` + access + `)
	  OR ci.id IN(SELECT p2.ci_id FROM service_portfolio_relations r JOIN service_portfolio_items p ON p.tenant_id=r.tenant_id AND p.id=r.source_item_id JOIN service_portfolio_items p2 ON p2.tenant_id=r.tenant_id AND p2.id=r.target_item_id WHERE r.tenant_id=ci.tenant_id AND p.id=? AND ` + access + ` AND ` + subAccess + `)) ORDER BY ci.id LIMIT 100`
	params := []any{a.TenantID}
	params = append(params, ciArgs...)
	for i := 0; i < 3; i++ {
		params = append(params, id)
		params = append(params, args...)
	}
	params = append(params, subArgs...)
	rows, err := s.DB.QueryContext(ctx, query, params...)
	if err != nil {
		return out, err
	}
	ciIDs := []uint64{}
	for rows.Next() {
		var ciID uint64
		if err := rows.Scan(&ciID); err != nil {
			rows.Close()
			return out, err
		}
		ciIDs = append(ciIDs, ciID)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	out.Items, err = adapter.List(ctx, a, ciIDs, before, limit)
	if err != nil {
		return out, err
	}
	out.Available = true
	return out, nil
}
