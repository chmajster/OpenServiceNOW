package serviceportfolio

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
)

func (s *Service) Relations(ctx context.Context, a Actor, itemID, before uint64, limit int) ([]Relation, uint64, error) {
	if !CanRead(a) {
		return nil, 0, workrecords.ErrForbidden
	}
	if limit < 1 || limit > 100 {
		return nil, 0, ErrInvalid
	}
	if _, err := s.Get(ctx, a, itemID); err != nil {
		return nil, 0, err
	}
	sp, sargs := portfolioAccess(a, "src")
	tp, targs := portfolioAccess(a, "tgt")
	cp, cargs := ciAccess(a, "ci")
	query := `SELECT r.id,r.version,r.source_item_id,COALESCE(r.target_item_id,0),COALESCE(r.target_ci_id,0),COALESCE(tgt.version,ci.version,0),r.relation_type,
 src.name,src.item_type,COALESCE(tgt.name,ci.name,''),COALESCE(tgt.item_type,''),COALESCE(ci.class,'')
 FROM service_portfolio_relations r JOIN service_portfolio_items src ON src.tenant_id=r.tenant_id AND src.id=r.source_item_id
 LEFT JOIN service_portfolio_items tgt ON tgt.tenant_id=r.tenant_id AND tgt.id=r.target_item_id
 LEFT JOIN cmdb_cis ci ON ci.tenant_id=r.tenant_id AND ci.id=r.target_ci_id
 WHERE r.tenant_id=? AND r.source_item_id=? AND ` + sp + ` AND (r.target_item_id IS NULL OR ` + tp + `) AND (r.target_ci_id IS NULL OR ` + cp + `)`
	params := []any{a.TenantID, itemID}
	params = append(params, sargs...)
	params = append(params, targs...)
	params = append(params, cargs...)
	if before > 0 {
		query += " AND r.id<?"
		params = append(params, before)
	}
	query += " ORDER BY r.id DESC LIMIT ?"
	params = append(params, limit+1)
	rows, err := s.DB.QueryContext(ctx, query, params...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := []Relation{}
	for rows.Next() {
		var item Relation
		if err := rows.Scan(&item.ID, &item.Version, &item.SourceItemID, &item.TargetItemID, &item.TargetCIID, &item.TargetVersion, &item.Type, &item.SourceName, &item.SourceType, &item.TargetName, &item.TargetType, &item.TargetCIClass); err != nil {
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
		next = items[len(items)-1].ID
	}
	return items, next, nil
}

func (s *Service) AddRelation(ctx context.Context, a Actor, sourceID uint64, in RelationInput) (Item, error) {
	var out Item
	if !CanManage(a) {
		return out, workrecords.ErrForbidden
	}
	in.Type = strings.ToUpper(strings.TrimSpace(in.Type))
	if in.SourceVersion == 0 || (in.TargetItemID == 0) == (in.TargetCIID == 0) || in.TargetVersion == 0 {
		return out, ErrInvalid
	}
	if in.TargetItemID > 0 && in.TargetItemID == sourceID {
		return out, ErrInvalid
	}
	err := s.actorTx(ctx, a, func(exec database.Executor, current Actor) error {
		if !CanManage(current) {
			return workrecords.ErrForbidden
		}
		source, err := s.getInTx(ctx, exec, current, sourceID, true)
		if err != nil {
			return err
		}
		if source.Lifecycle == "RETIRED" {
			return ErrConflict
		}
		targetType, targetCIClass := "", ""
		if in.TargetItemID > 0 {
			target, err := s.getInTx(ctx, exec, current, in.TargetItemID, true)
			if err != nil {
				return err
			}
			if target.Version != in.TargetVersion || target.Lifecycle == "RETIRED" {
				return ErrConflict
			}
			targetType = target.Type
		} else {
			access, args := ciAccess(current, "ci")
			var archived bool
			var ciVersion uint64
			params := []any{current.TenantID, in.TargetCIID}
			params = append(params, args...)
			err = exec.QueryRowContext(ctx, "SELECT ci.class,ci.archived,ci.version FROM cmdb_cis ci WHERE ci.tenant_id=? AND ci.id=? AND "+access+" FOR UPDATE", params...).Scan(&targetCIClass, &archived, &ciVersion)
			if err == sql.ErrNoRows {
				return workrecords.ErrForbidden
			}
			if err != nil {
				return err
			}
			if ciVersion != in.TargetVersion || archived {
				return ErrConflict
			}
		}
		if !relationAllowed(source.Type, in.Type, targetType, targetCIClass) {
			return fmt.Errorf("%w: typ relacji nie jest dozwolony dla wybranych klas", ErrInvalid)
		}
		if in.Type == "DEPENDS_ON" && in.TargetItemID > 0 {
			cycle, err := s.dependencyCycle(ctx, exec, current.TenantID, sourceID, in.TargetItemID)
			if err != nil {
				return err
			}
			if cycle {
				return fmt.Errorf("%w: relacja utworzyłaby cykl zależności", ErrInvalid)
			}
		}
		var exists bool
		if in.TargetItemID > 0 {
			err = exec.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM service_portfolio_relations WHERE tenant_id=? AND source_item_id=? AND target_item_id=? AND relation_type=?)", current.TenantID, sourceID, in.TargetItemID, in.Type).Scan(&exists)
		} else {
			err = exec.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM service_portfolio_relations WHERE tenant_id=? AND source_item_id=? AND target_ci_id=? AND relation_type=?)", current.TenantID, sourceID, in.TargetCIID, in.Type).Scan(&exists)
		}
		if err != nil {
			return err
		}
		if exists {
			out = source
			return nil
		}
		if source.Version != in.SourceVersion {
			return ErrConflict
		}
		var count int
		if err = exec.QueryRowContext(ctx, "SELECT COUNT(*) FROM service_portfolio_relations WHERE tenant_id=? AND source_item_id=?", current.TenantID, sourceID).Scan(&count); err != nil {
			return err
		}
		if count >= 100 {
			return fmt.Errorf("%w: maksymalnie 100 relacji na usługę", ErrInvalid)
		}
		_, err = exec.ExecContext(ctx, "INSERT INTO service_portfolio_relations(tenant_id,source_item_id,target_item_id,target_ci_id,relation_type,created_by) VALUES(?,?,?,?,?,?)", current.TenantID, sourceID, nullableID(in.TargetItemID), nullableID(in.TargetCIID), in.Type, current.UserID)
		if err != nil {
			return missing(err)
		}
		out, err = s.touchItem(ctx, exec, current, source, "relations")
		return err
	})
	return out, err
}

func (s *Service) dependencyCycle(ctx context.Context, exec database.Executor, tenant, source, target uint64) (bool, error) {
	frontier := []uint64{target}
	visited := map[uint64]bool{target: true}
	for depth := 0; len(frontier) > 0 && depth < 100; depth++ {
		if len(frontier) > 100 {
			return false, ErrInvalid
		}
		marks := strings.TrimSuffix(strings.Repeat("?,", len(frontier)), ",")
		params := []any{tenant}
		for _, id := range frontier {
			params = append(params, id)
		}
		params = append(params, 101)
		rows, err := exec.QueryContext(ctx, "SELECT target_item_id FROM service_portfolio_relations WHERE tenant_id=? AND source_item_id IN ("+marks+") AND relation_type='DEPENDS_ON' AND target_item_id IS NOT NULL ORDER BY id LIMIT ?", params...)
		if err != nil {
			return false, err
		}
		next := []uint64{}
		for rows.Next() {
			var id uint64
			if err = rows.Scan(&id); err != nil {
				rows.Close()
				return false, err
			}
			if id == source {
				rows.Close()
				return true, nil
			}
			if !visited[id] {
				visited[id] = true
				next = append(next, id)
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return false, err
		}
		if len(next) > 100 {
			return false, ErrInvalid
		}
		frontier = next
	}
	if len(frontier) > 0 {
		return false, fmt.Errorf("%w: graf zależności przekracza limit walidacji cykli", ErrInvalid)
	}
	return false, nil
}

func (s *Service) RemoveRelation(ctx context.Context, a Actor, sourceID, relationID, sourceVersion, relationVersion, targetVersion uint64) (Item, error) {
	var out Item
	if !CanManage(a) {
		return out, workrecords.ErrForbidden
	}
	if sourceVersion == 0 || relationVersion == 0 || targetVersion == 0 {
		return out, ErrInvalid
	}
	err := s.actorTx(ctx, a, func(exec database.Executor, current Actor) error {
		source, err := s.getInTx(ctx, exec, current, sourceID, true)
		if err != nil {
			return err
		}
		if source.Version != sourceVersion || source.Lifecycle == "RETIRED" {
			return ErrConflict
		}
		var relation Relation
		var targetItem sql.Null[uint64]
		var targetCI sql.Null[uint64]
		if err = exec.QueryRowContext(ctx, "SELECT id,version,source_item_id,target_item_id,target_ci_id,relation_type FROM service_portfolio_relations WHERE tenant_id=? AND id=? AND source_item_id=? FOR UPDATE", current.TenantID, relationID, sourceID).Scan(&relation.ID, &relation.Version, &relation.SourceItemID, &targetItem, &targetCI, &relation.Type); err != nil {
			return missing(err)
		}
		if relation.Version != relationVersion {
			return ErrConflict
		}
		if targetItem.Valid {
			relation.TargetItemID = targetItem.V
			target, err := s.getInTx(ctx, exec, current, targetItem.V, true)
			if err != nil {
				return err
			}
			if target.Version != targetVersion || target.Lifecycle == "RETIRED" {
				return ErrConflict
			}
		}
		if targetCI.Valid {
			relation.TargetCIID = targetCI.V
			access, args := ciAccess(current, "ci")
			params := []any{current.TenantID, targetCI.V}
			params = append(params, args...)
			var ciVersion uint64
			if err = exec.QueryRowContext(ctx, "SELECT version FROM cmdb_cis ci WHERE ci.tenant_id=? AND ci.id=? AND "+access+" FOR UPDATE", params...).Scan(&ciVersion); err == sql.ErrNoRows {
				return workrecords.ErrForbidden
			}
			if err != nil {
				return err
			}
			if ciVersion != targetVersion {
				return ErrConflict
			}
		}
		if relation.Type == "DEPENDS_ON" && source.DependencyMode != "NOT_MODELED" {
			var count uint64
			if err = exec.QueryRowContext(ctx, "SELECT COUNT(*) FROM service_portfolio_relations WHERE tenant_id=? AND source_item_id=? AND relation_type='DEPENDS_ON'", current.TenantID, sourceID).Scan(&count); err != nil {
				return err
			}
			remaining := count - 1
			if remaining == 0 || source.DependencyMode == "QUORUM" && remaining < uint64(source.DependencyQuorum) {
				return fmt.Errorf("%w: najpierw zmień model redundancji lub dodaj inną zależność", ErrInvalid)
			}
		}
		if _, err = exec.ExecContext(ctx, "DELETE FROM service_portfolio_relations WHERE tenant_id=? AND id=?", current.TenantID, relationID); err != nil {
			return err
		}
		out, err = s.touchItem(ctx, exec, current, source, "relations")
		return err
	})
	return out, err
}

func (s *Service) touchItem(ctx context.Context, exec database.Executor, a Actor, item Item, field string) (Item, error) {
	result, err := exec.ExecContext(ctx, "UPDATE service_portfolio_items SET version=version+1,configuration_version=configuration_version+1,updated_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND id=? AND version=?", a.TenantID, item.ID, item.Version)
	if err != nil {
		return item, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return item, err
	}
	if changed != 1 {
		return item, ErrConflict
	}
	after, err := s.getInTx(ctx, exec, a, item.ID, false)
	if err != nil {
		return Item{}, err
	}
	if err = s.changed(ctx, exec, a, "service_portfolio.item.changed", "service_portfolio_item", after.ID, after.Version, item, after, []string{field}); err != nil {
		return Item{}, err
	}
	return after, nil
}

func (r Relation) TargetRef() string {
	if r.TargetItemID > 0 {
		return "service:" + strconv.FormatUint(r.TargetItemID, 10)
	}
	return "ci:" + strconv.FormatUint(r.TargetCIID, 10)
}
