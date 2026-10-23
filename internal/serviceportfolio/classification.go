package serviceportfolio

import (
	"context"
	"database/sql"
	"strings"

	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
)

func (s *Service) ClassificationPreview(ctx context.Context, a Actor, before uint64, limit int) ([]ClassificationCandidate, uint64, error) {
	if !CanManage(a) {
		return nil, 0, workrecords.ErrForbidden
	}
	if !a.Has("cmdb.read") && !a.Has("cmdb.read_own") {
		return nil, 0, workrecords.ErrForbidden
	}
	if limit < 1 || limit > 100 {
		return nil, 0, ErrInvalid
	}
	access, args := ciAccess(a, "ci")
	query := `SELECT ci.id,ci.name,ci.identifier,ci.class,ci.version FROM cmdb_cis ci
 WHERE ci.tenant_id=? AND ci.class IN('SERVICE','APPLICATION') AND NOT EXISTS(SELECT 1 FROM service_portfolio_items p WHERE p.tenant_id=ci.tenant_id AND p.ci_id=ci.id) AND ` + access
	params := []any{a.TenantID}
	params = append(params, args...)
	if before > 0 {
		query += " AND ci.id<?"
		params = append(params, before)
	}
	query += " ORDER BY ci.id DESC LIMIT ?"
	params = append(params, limit+1)
	rows, err := s.DB.QueryContext(ctx, query, params...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := []ClassificationCandidate{}
	for rows.Next() {
		var item ClassificationCandidate
		if err := rows.Scan(&item.CIID, &item.Name, &item.Identifier, &item.Class, &item.Version); err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	var next uint64
	if len(items) > limit {
		items = items[:limit]
		next = items[len(items)-1].CIID
	}
	return items, next, nil
}

func (s *Service) Classify(ctx context.Context, a Actor, mappings []ClassificationInput) (ClassificationResult, error) {
	out := ClassificationResult{Created: []Item{}}
	if !CanManage(a) || !a.Has("cmdb.read") {
		return out, workrecords.ErrForbidden
	}
	if len(mappings) < 1 || len(mappings) > 50 {
		return out, ErrInvalid
	}
	err := s.actorTx(ctx, a, func(exec database.Executor, current Actor) error {
		seen := map[uint64]bool{}
		for _, mapping := range mappings {
			if mapping.CIID == 0 || mapping.CIVersion == 0 || !itemTypes[mapping.Type] || seen[mapping.CIID] {
				return ErrInvalid
			}
			seen[mapping.CIID] = true
			key := strings.TrimSpace(mapping.ServiceKey)
			var name, identifier, class string
			var owner uint64
			var archived bool
			var version uint64
			err := exec.QueryRowContext(ctx, "SELECT name,identifier,class,owner_id,archived,version FROM cmdb_cis WHERE tenant_id=? AND id=? FOR UPDATE", current.TenantID, mapping.CIID).Scan(&name, &identifier, &class, &owner, &archived, &version)
			if err == sql.ErrNoRows {
				return ErrNotFound
			}
			if err != nil {
				return err
			}
			if version != mapping.CIVersion || archived || class != "SERVICE" && class != "APPLICATION" || !ciClassAllowed(mapping.Type, class) {
				return ErrConflict
			}
			if key == "" {
				key = "LEGACY-CI-" + strings.ToUpper(identifier)
			}
			if len(key) > 64 {
				key = key[:64]
			}
			key, err = normalizeKey(key)
			if err != nil {
				return err
			}
			result, err := exec.ExecContext(ctx, `INSERT INTO service_portfolio_items
 (tenant_id,service_key,name,item_type,description,lifecycle,criticality,environment,scope,ci_id,dependency_mode,dependency_quorum,configuration_version,created_by)
 VALUES(?,?,?,?,?,'DRAFT','MEDIUM','ALL','',?,'NOT_MODELED',0,1,?)`, current.TenantID, key, name, mapping.Type, "", mapping.CIID, current.UserID)
			if err != nil {
				return missing(err)
			}
			id, err := result.LastInsertId()
			if err != nil {
				return err
			}
			created, err := s.getInTx(ctx, exec, current, uint64(id), false)
			if err != nil {
				return err
			}
			if err = s.changed(ctx, exec, current, "service_portfolio.item.classified", "service_portfolio_item", created.ID, created.Version, nil, created, []string{"classification", "ci_link"}); err != nil {
				return err
			}
			out.Created = append(out.Created, created)
		}
		return nil
	})
	return out, err
}
