package cmdb

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/chmajster/OpenServiceNOW/internal/audit"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
)

func (s *Service) MergePlan(ctx context.Context, a Actor, sourceID, canonicalID uint64) (MergePlan, error) {
	if !canManageIRE(a) {
		return MergePlan{}, workrecords.ErrForbidden
	}
	var plan MergePlan
	err := s.ActorTx(ctx, a, func(e database.Executor, current Actor) error {
		var err error
		plan, err = s.mergePlanInTx(ctx, e, current, sourceID, canonicalID, false)
		return err
	})
	return plan, err
}

func (s *Service) mergePlanInTx(ctx context.Context, e database.Executor, a Actor, sourceID, canonicalID uint64, lock bool) (MergePlan, error) {
	plan := MergePlan{SourceCIID: sourceID, CanonicalCIID: canonicalID, References: map[string]int{}, Blockers: []string{}, UnsupportedReferences: []string{}}
	if sourceID == 0 || canonicalID == 0 || sourceID == canonicalID {
		return plan, ErrInvalid
	}
	query := `SELECT class,archived,version,redirect_ci_id FROM cmdb_cis WHERE tenant_id=? AND id=?`
	if lock {
		query += " FOR UPDATE"
	}
	var sourceClass, canonicalClass string
	var sourceArchived, canonicalArchived bool
	var sourceRedirect, canonicalRedirect sql.NullInt64
	if err := e.QueryRowContext(ctx, query, a.TenantID, sourceID).Scan(&sourceClass, &sourceArchived, &plan.SourceVersion, &sourceRedirect); err != nil {
		return plan, remapCMDBNotFound(err)
	}
	if err := e.QueryRowContext(ctx, query, a.TenantID, canonicalID).Scan(&canonicalClass, &canonicalArchived, &plan.CanonicalVersion, &canonicalRedirect); err != nil {
		return plan, remapCMDBNotFound(err)
	}
	if sourceRedirect.Valid || canonicalRedirect.Valid || sourceArchived || canonicalArchived {
		plan.Blockers = append(plan.Blockers, "CI_ARCHIVED_OR_ALREADY_REDIRECTED")
	}
	if sourceClass != canonicalClass {
		plan.Blockers = append(plan.Blockers, "CI_CLASS_MISMATCH")
	}
	if err := s.mergeReferenceCounts(ctx, e, a.TenantID, &plan); err != nil {
		return plan, err
	}
	if err := s.mergeBlockers(ctx, e, a, sourceID, canonicalID, &plan); err != nil {
		return plan, err
	}
	if err := s.unsupportedMergeReferences(ctx, e, a.TenantID, &plan); err != nil {
		return plan, err
	}
	sort.Strings(plan.Blockers)
	plan.Blockers = uniqueStrings(plan.Blockers)
	tokenValue := plan
	tokenValue.Token = ""
	data, _ := json.Marshal(tokenValue)
	hash := sha256.Sum256(data)
	plan.Token = hex.EncodeToString(hash[:])
	return plan, nil
}

func remapCMDBNotFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func uniqueStrings(input []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, item := range input {
		if !seen[item] {
			seen[item] = true
			out = append(out, item)
		}
	}
	return out
}

func (s *Service) mergeReferenceCounts(ctx context.Context, e database.Executor, tenant uint64, plan *MergePlan) error {
	queries := map[string]string{
		"relations":           `SELECT COUNT(*) FROM cmdb_relations WHERE tenant_id=? AND (source_id=? OR target_id=?)`,
		"asset_links":         `SELECT COUNT(*) FROM cmdb_ci_assets WHERE tenant_id=? AND ci_id=?`,
		"incident_links":      `SELECT COUNT(*) FROM cmdb_incident_links WHERE tenant_id=? AND ci_id=?`,
		"request_links":       `SELECT COUNT(*) FROM cmdb_request_links WHERE tenant_id=? AND ci_id=?`,
		"portfolio_items":     `SELECT COUNT(*) FROM service_portfolio_items WHERE tenant_id=? AND ci_id=?`,
		"portfolio_relations": `SELECT COUNT(*) FROM service_portfolio_relations WHERE tenant_id=? AND target_ci_id=?`,
		"external_identities": `SELECT COUNT(*) FROM cmdb_ire_external_identities WHERE tenant_id=? AND ci_id=?`,
		"field_provenance":    `SELECT COUNT(*) FROM cmdb_ire_field_provenance WHERE tenant_id=? AND ci_id=?`,
		"manual_overrides":    `SELECT COUNT(*) FROM cmdb_ire_manual_overrides WHERE tenant_id=? AND ci_id=?`,
		"quality_findings":    `SELECT COUNT(*) FROM cmdb_ire_quality_findings WHERE tenant_id=? AND ci_id=?`,
		"ire_observations":    `SELECT COUNT(*) FROM cmdb_ire_observations WHERE tenant_id=? AND ci_id=?`,
		"provenance_history":  `SELECT COUNT(*) FROM cmdb_ire_field_provenance_history WHERE tenant_id=? AND ci_id=?`,
	}
	for name, query := range queries {
		var count int
		var err error
		if name == "relations" {
			err = e.QueryRowContext(ctx, query, tenant, plan.SourceCIID, plan.SourceCIID).Scan(&count)
		} else {
			err = e.QueryRowContext(ctx, query, tenant, plan.SourceCIID).Scan(&count)
		}
		if err != nil {
			return err
		}
		plan.References[name] = count
	}
	return nil
}

func (s *Service) mergeBlockers(ctx context.Context, e database.Executor, a Actor, sourceID, canonicalID uint64, plan *MergePlan) error {
	tenant := a.TenantID
	var sourceAsset, canonicalAsset uint64
	assetErr := e.QueryRowContext(ctx, `SELECT asset_id FROM cmdb_ci_assets WHERE tenant_id=? AND ci_id=?`, tenant, sourceID).Scan(&sourceAsset)
	if assetErr != nil && !errors.Is(assetErr, sql.ErrNoRows) {
		return assetErr
	}
	assetErr = e.QueryRowContext(ctx, `SELECT asset_id FROM cmdb_ci_assets WHERE tenant_id=? AND ci_id=?`, tenant, canonicalID).Scan(&canonicalAsset)
	if assetErr != nil && !errors.Is(assetErr, sql.ErrNoRows) {
		return assetErr
	}
	if sourceAsset > 0 && canonicalAsset > 0 && sourceAsset != canonicalAsset {
		plan.Blockers = append(plan.Blockers, "TWO_ASSETS_REQUIRE_EXPLICIT_RESOLUTION")
	}
	if (sourceAsset > 0 || canonicalAsset > 0) && !(a.Has("assets.manage") && a.Has("assets.read")) {
		plan.Blockers = append(plan.Blockers, "ASSET_WRITE_PERMISSION_REQUIRED")
	}
	var sourcePortfolio, canonicalPortfolio int
	if err := e.QueryRowContext(ctx, `SELECT COUNT(*) FROM service_portfolio_items WHERE tenant_id=? AND ci_id=?`, tenant, sourceID).Scan(&sourcePortfolio); err != nil {
		return err
	}
	if err := e.QueryRowContext(ctx, `SELECT COUNT(*) FROM service_portfolio_items WHERE tenant_id=? AND ci_id=?`, tenant, canonicalID).Scan(&canonicalPortfolio); err != nil {
		return err
	}
	if sourcePortfolio > 0 && canonicalPortfolio > 0 {
		plan.Blockers = append(plan.Blockers, "TWO_PORTFOLIO_ITEMS_REQUIRE_EXPLICIT_RESOLUTION")
	}
	if err := s.checkMergeCollisions(ctx, e, tenant, sourceID, canonicalID, plan); err != nil {
		return err
	}
	return nil
}

func (s *Service) checkMergeCollisions(ctx context.Context, e database.Executor, tenant, sourceID, canonicalID uint64, plan *MergePlan) error {
	rows, err := e.QueryContext(ctx, `SELECT source_id,target_id,relation_type FROM cmdb_relations WHERE tenant_id=? AND (source_id IN(?,?) OR target_id IN(?,?))`, tenant, sourceID, canonicalID, sourceID, canonicalID)
	if err != nil {
		return err
	}
	relationKeys := map[string]int{}
	selfRelation := false
	for rows.Next() {
		var source, target uint64
		var kind string
		if err = rows.Scan(&source, &target, &kind); err != nil {
			rows.Close()
			return err
		}
		if source == sourceID {
			source = canonicalID
		}
		if target == sourceID {
			target = canonicalID
		}
		if source == target {
			selfRelation = true
		}
		key := fmt.Sprintf("%d:%d:%s", source, target, kind)
		relationKeys[key]++
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	if selfRelation {
		plan.Blockers = append(plan.Blockers, "MERGE_WOULD_CREATE_SELF_RELATION")
	}
	for _, count := range relationKeys {
		if count > 1 {
			plan.Blockers = append(plan.Blockers, "DUPLICATE_RELATION_REQUIRES_EXPLICIT_RESOLUTION")
			break
		}
	}
	for _, tableCheck := range []struct{ table string }{{"service_portfolio_relations"}} {
		query := `SELECT COUNT(*) FROM (SELECT source_item_id,relation_type,target_item_key,CASE WHEN target_ci_id=? THEN ? ELSE target_ci_id END AS mapped_target_ci FROM ` + tableCheck.table + ` WHERE tenant_id=? AND target_ci_id IN(?,?) GROUP BY source_item_id,relation_type,target_item_key,CASE WHEN target_ci_id=? THEN ? ELSE target_ci_id END HAVING COUNT(*)>1) duplicates`
		var count int
		if err = e.QueryRowContext(ctx, query, sourceID, canonicalID, tenant, sourceID, canonicalID, sourceID, canonicalID).Scan(&count); err != nil {
			return err
		}
		if count > 0 {
			plan.Blockers = append(plan.Blockers, "DUPLICATE_PORTFOLIO_RELATION_REQUIRES_EXPLICIT_RESOLUTION")
		}
	}
	var conflictCount int
	if err = e.QueryRowContext(ctx, `SELECT COUNT(*) FROM cmdb_ire_manual_overrides a JOIN cmdb_ire_manual_overrides b ON b.tenant_id=a.tenant_id AND b.ci_id=? AND b.field_key=a.field_key WHERE a.tenant_id=? AND a.ci_id=?`, canonicalID, tenant, sourceID).Scan(&conflictCount); err != nil {
		return err
	}
	if conflictCount > 0 {
		plan.Blockers = append(plan.Blockers, "OVERLAPPING_MANUAL_OVERRIDES_REQUIRE_EXPLICIT_RESOLUTION")
	}
	var qualityCollision int
	if err = e.QueryRowContext(ctx, `SELECT COUNT(*) FROM cmdb_ire_quality_findings a JOIN cmdb_ire_quality_findings b ON b.tenant_id=a.tenant_id AND b.ci_id=? AND b.rule_key=a.rule_key AND b.field_key=a.field_key WHERE a.tenant_id=? AND a.ci_id=?`, canonicalID, tenant, sourceID).Scan(&qualityCollision); err != nil {
		return err
	}
	if qualityCollision > 0 {
		plan.Blockers = append(plan.Blockers, "QUALITY_FINDINGS_REQUIRE_RECHECK")
	}
	return nil
}

func (s *Service) unsupportedMergeReferences(ctx context.Context, e database.Executor, tenant uint64, plan *MergePlan) error {
	rows, err := e.QueryContext(ctx, `SELECT table_name,column_name FROM information_schema.key_column_usage WHERE referenced_table_schema=DATABASE() AND referenced_table_name='cmdb_cis' AND referenced_column_name='id' ORDER BY table_name,column_name`)
	if err != nil {
		return err
	}
	allowed := map[string]bool{
		"cmdb_cis.redirect_ci_id":  true,
		"cmdb_relations.source_id": true, "cmdb_relations.target_id": true,
		"cmdb_ci_assets.ci_id": true, "cmdb_incident_links.ci_id": true, "cmdb_request_links.ci_id": true,
		"service_portfolio_items.ci_id": true, "service_portfolio_relations.target_ci_id": true,
		"cmdb_ire_external_identities.ci_id": true, "cmdb_ire_field_provenance.ci_id": true,
		"cmdb_ire_manual_overrides.ci_id": true, "cmdb_ire_quality_findings.ci_id": true,
		"cmdb_ire_observations.ci_id": true, "cmdb_ire_observations.parent_ci_id": true,
		"cmdb_ire_field_provenance_history.ci_id": true,
		"cmdb_ire_merge_history.source_ci_id":     true, "cmdb_ire_merge_history.canonical_ci_id": true,
	}
	type reference struct{ table, column string }
	references := []reference{}
	for rows.Next() {
		var table, column string
		if err = rows.Scan(&table, &column); err != nil {
			rows.Close()
			return err
		}
		references = append(references, reference{table: table, column: column})
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err = rows.Close(); err != nil {
		return err
	}
	for _, ref := range references {
		key := ref.table + "." + ref.column
		if allowed[key] {
			continue
		}
		quotedTable := "`" + strings.ReplaceAll(ref.table, "`", "``") + "`"
		quotedColumn := "`" + strings.ReplaceAll(ref.column, "`", "``") + "`"
		query := "SELECT COUNT(*) FROM " + quotedTable + " WHERE " + quotedColumn + "=?"
		var tenantScoped bool
		if err = e.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema=DATABASE() AND table_name=? AND column_name='tenant_id')`, ref.table).Scan(&tenantScoped); err != nil {
			return err
		}
		args := []any{plan.SourceCIID}
		if tenantScoped {
			query += " AND tenant_id=?"
			args = append(args, tenant)
		}
		var count int
		if err = e.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
			return err
		}
		if count > 0 {
			plan.Blockers = append(plan.Blockers, "UNSUPPORTED_REFERENCE:"+key)
			plan.UnsupportedReferences = append(plan.UnsupportedReferences, key)
			plan.References["unsupported:"+key] = count
		}
	}
	return nil
}

func (s *Service) MergeCIs(ctx context.Context, a Actor, input MergeInput) (MergePlan, error) {
	if !canManageIRE(a) || input.SourceCIID == 0 || input.CanonicalCIID == 0 || input.SourceCIID == input.CanonicalCIID || input.SourceVersion == 0 || input.CanonicalVersion == 0 || len(strings.TrimSpace(input.Reason)) < 5 || len(input.Reason) > 1000 || len(input.PlanToken) != 64 {
		return MergePlan{}, ErrInvalid
	}
	var out MergePlan
	err := s.ActorTx(ctx, a, func(e database.Executor, current Actor) error {
		if !canManageIRE(current) {
			return workrecords.ErrForbidden
		}
		plan, err := s.mergePlanInTx(ctx, e, current, input.SourceCIID, input.CanonicalCIID, true)
		if err != nil {
			return err
		}
		if plan.SourceVersion != input.SourceVersion || plan.CanonicalVersion != input.CanonicalVersion || plan.Token != input.PlanToken {
			return ErrConflict
		}
		if len(plan.Blockers) > 0 {
			return fmt.Errorf("%w: scalanie zablokowane: %s", ErrConflict, strings.Join(plan.Blockers, ", "))
		}
		return s.applyMergeInTx(ctx, e, current, plan, strings.TrimSpace(input.Reason), &out)
	})
	return out, err
}

func (s *Service) applyMergeInTx(ctx context.Context, e database.Executor, a Actor, plan MergePlan, reason string, out *MergePlan) error {
	source, err := s.GetInTx(ctx, e, a, plan.SourceCIID, true)
	if err != nil {
		return err
	}
	canonical, err := s.GetInTx(ctx, e, a, plan.CanonicalCIID, true)
	if err != nil {
		return err
	}
	planJSON, _ := json.Marshal(plan)
	if _, err = e.ExecContext(ctx, `INSERT INTO cmdb_ire_merge_history(tenant_id,source_ci_id,canonical_ci_id,merged_by,reason,plan) VALUES(?,?,?,?,?,?)`, a.TenantID, source.ID, canonical.ID, a.UserID, reason, planJSON); err != nil {
		return err
	}
	if err = s.mergeAssetLink(ctx, e, a, source.ID, canonical.ID); err != nil {
		return err
	}
	for _, table := range []struct{ name, column, otherColumn string }{{"cmdb_incident_links", "ci_id", "incident_id"}, {"cmdb_request_links", "ci_id", "request_id"}} {
		query := `INSERT IGNORE INTO ` + table.name + `(tenant_id,ci_id,` + table.otherColumn + `) SELECT tenant_id,?,` + table.otherColumn + ` FROM ` + table.name + ` WHERE tenant_id=? AND ci_id=?`
		if _, err = e.ExecContext(ctx, query, canonical.ID, a.TenantID, source.ID); err != nil {
			return err
		}
		if _, err = e.ExecContext(ctx, `DELETE FROM `+table.name+` WHERE tenant_id=? AND ci_id=?`, a.TenantID, source.ID); err != nil {
			return err
		}
	}
	if _, err = e.ExecContext(ctx, `UPDATE service_portfolio_items SET ci_id=?,version=version+1,configuration_version=configuration_version+1,updated_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND ci_id=?`, canonical.ID, a.TenantID, source.ID); err != nil {
		return err
	}
	if _, err = e.ExecContext(ctx, `UPDATE service_portfolio_relations SET target_ci_id=?,version=version+1 WHERE tenant_id=? AND target_ci_id=?`, canonical.ID, a.TenantID, source.ID); err != nil {
		return err
	}
	if _, err = e.ExecContext(ctx, `UPDATE cmdb_relations SET source_id=IF(source_id=?, ?, source_id),target_id=IF(target_id=?, ?, target_id),version=version+1 WHERE tenant_id=? AND (source_id=? OR target_id=?)`, source.ID, canonical.ID, source.ID, canonical.ID, a.TenantID, source.ID, source.ID); err != nil {
		return err
	}
	if _, err = e.ExecContext(ctx, `UPDATE cmdb_ire_external_identities SET ci_id=?,updated_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND ci_id=?`, canonical.ID, a.TenantID, source.ID); err != nil {
		return err
	}
	if _, err = e.ExecContext(ctx, `INSERT INTO cmdb_ire_field_provenance_history(tenant_id,ci_id,field_key,source_id,run_type,run_id,rule_version,source_priority,observed_at,accepted_at,value_hash,archived_at) SELECT tenant_id,ci_id,field_key,source_id,run_type,run_id,rule_version,source_priority,observed_at,accepted_at,value_hash,UTC_TIMESTAMP(6) FROM cmdb_ire_field_provenance WHERE tenant_id=? AND ci_id=?`, a.TenantID, source.ID); err != nil {
		return err
	}
	if _, err = e.ExecContext(ctx, `INSERT IGNORE INTO cmdb_ire_field_provenance(tenant_id,ci_id,field_key,source_id,run_type,run_id,rule_version,source_priority,observed_at,accepted_at,value_hash) SELECT tenant_id,?,field_key,source_id,run_type,run_id,rule_version,source_priority,observed_at,accepted_at,value_hash FROM cmdb_ire_field_provenance WHERE tenant_id=? AND ci_id=?`, canonical.ID, a.TenantID, source.ID); err != nil {
		return err
	}
	if _, err = e.ExecContext(ctx, `DELETE FROM cmdb_ire_field_provenance WHERE tenant_id=? AND ci_id=?`, a.TenantID, source.ID); err != nil {
		return err
	}
	if _, err = e.ExecContext(ctx, `UPDATE cmdb_ire_manual_overrides SET ci_id=? WHERE tenant_id=? AND ci_id=?`, canonical.ID, a.TenantID, source.ID); err != nil {
		return err
	}
	if _, err = e.ExecContext(ctx, `UPDATE cmdb_ire_quality_findings SET ci_id=? WHERE tenant_id=? AND ci_id=?`, canonical.ID, a.TenantID, source.ID); err != nil {
		return err
	}
	if _, err = e.ExecContext(ctx, `UPDATE cmdb_cis SET redirect_ci_id=?,archived=TRUE,version=version+1,updated_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND id=? AND version=? AND redirect_ci_id IS NULL`, canonical.ID, a.TenantID, source.ID, source.Version); err != nil {
		return err
	}
	if _, err = e.ExecContext(ctx, `UPDATE cmdb_cis SET redirect_ci_id=? WHERE tenant_id=? AND redirect_ci_id=?`, canonical.ID, a.TenantID, source.ID); err != nil {
		return err
	}
	if _, err = s.touch(ctx, e, a, canonical, "merge"); err != nil {
		return err
	}
	if err = audit.NewMySQLRepository().Append(ctx, e, audit.Event{TenantID: &a.TenantID, ActorUserID: &a.UserID, Action: "cmdb.ire.ci.merged", ObjectType: "ci", ObjectID: fmt.Sprint(canonical.ID), OldValues: map[string]any{"source_ci_id": source.ID}, NewValues: map[string]any{"canonical_ci_id": canonical.ID, "reason": reason, "references": plan.References}, RequestID: a.RequestID, IPAddress: a.IPAddress, Source: "cmdb.ire"}); err != nil {
		return err
	}
	*out = plan
	return nil
}

func (s *Service) mergeAssetLink(ctx context.Context, e database.Executor, a Actor, sourceID, canonicalID uint64) error {
	var assetID uint64
	err := e.QueryRowContext(ctx, `SELECT asset_id FROM cmdb_ci_assets WHERE tenant_id=? AND ci_id=?`, a.TenantID, sourceID).Scan(&assetID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	asset, err := s.Assets.GetInTx(ctx, e, a, assetID, true)
	if err != nil {
		return err
	}
	if _, err = e.ExecContext(ctx, `UPDATE cmdb_ci_assets SET ci_id=? WHERE tenant_id=? AND ci_id=? AND asset_id=?`, canonicalID, a.TenantID, sourceID, assetID); err != nil {
		return err
	}
	_, err = s.Assets.TouchInTx(ctx, e, a, asset, "ci_link")
	return err
}
