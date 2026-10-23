package serviceportfolio

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/chmajster/OpenServiceNOW/internal/assets"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
)

func itemInputFrom(item Item) ItemInput {
	return ItemInput{Version: item.Version, ServiceKey: item.ServiceKey, Name: item.Name, Type: item.Type, Description: item.Description, Criticality: item.Criticality, Environment: item.Environment, Scope: item.Scope,
		BusinessOwnerID: item.BusinessOwnerID, TechnicalOwnerID: item.TechnicalOwnerID, SupportGroupID: item.SupportGroupID, OrgUnitRefID: item.OrgUnitRefID, DepartmentRefID: item.DepartmentRefID, CostCenterRefID: item.CostCenterRefID,
		LocationID: item.LocationID, CIID: item.CIID, CIVersion: item.CIVersion, DependencyMode: item.DependencyMode, DependencyQuorum: item.DependencyQuorum}
}

func (s *Service) TransitionItem(ctx context.Context, a Actor, id, version uint64, action, reason string) (Item, error) {
	var out Item
	if !CanManage(a) {
		return out, workrecords.ErrForbidden
	}
	if version == 0 {
		return out, ErrInvalid
	}
	reason = strings.TrimSpace(reason)
	if action == "retire" && len(reason) == 0 || len(reason) > 1000 {
		return out, ErrInvalid
	}
	err := s.actorTx(ctx, a, func(exec database.Executor, current Actor) error {
		before, err := s.getInTx(ctx, exec, current, id, true)
		if err != nil {
			return err
		}
		if before.Version != version {
			return ErrConflict
		}
		input := itemInputFrom(before)
		var next string
		switch action {
		case "publish":
			if before.Lifecycle != "DRAFT" {
				return ErrConflict
			}
			if err = s.validateReferences(ctx, exec, current, input); err != nil {
				return err
			}
			if err = s.validatePublication(ctx, exec, current, input, id); err != nil {
				return err
			}
			next = "PUBLISHED"
		case "activate":
			if before.Lifecycle != "PUBLISHED" {
				return ErrConflict
			}
			if err = s.validateReferences(ctx, exec, current, input); err != nil {
				return err
			}
			if err = s.validatePublication(ctx, exec, current, input, id); err != nil {
				return err
			}
			next = "ACTIVE"
		case "retire":
			if before.Lifecycle != "ACTIVE" {
				return ErrConflict
			}
			if _, err = s.retirementImpactInTx(ctx, exec, current, id); err != nil {
				return err
			}
			next = "RETIRED"
		default:
			return ErrInvalid
		}
		res, err := exec.ExecContext(ctx, "UPDATE service_portfolio_items SET lifecycle=?,version=version+1,updated_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND id=? AND version=?", next, current.TenantID, id, version)
		if err != nil {
			return err
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if affected != 1 {
			return ErrConflict
		}
		if next == "RETIRED" {
			if err = s.withdrawOffersInTx(ctx, exec, current, id); err != nil {
				return err
			}
		}
		out, err = s.getInTx(ctx, exec, current, id, false)
		if err != nil {
			return err
		}
		if err = s.changed(ctx, exec, current, "service_portfolio.item.changed", "service_portfolio_item", id, out.Version, before, out, []string{"lifecycle", "reason:" + reason}); err != nil {
			return err
		}
		return nil
	})
	return out, err
}

func (s *Service) RetirementImpact(ctx context.Context, a Actor, id uint64) (RetirementImpact, error) {
	if !CanRead(a) {
		return RetirementImpact{}, workrecords.ErrForbidden
	}
	return s.retirementImpactInTx(ctx, s.DB, a, id)
}

func (s *Service) retirementImpactInTx(ctx context.Context, exec database.Executor, a Actor, id uint64) (RetirementImpact, error) {
	item, err := s.getInTx(ctx, exec, a, id, false)
	if err != nil {
		return RetirementImpact{}, err
	}
	out := RetirementImpact{ServiceID: id, RetirementAllowed: item.Lifecycle == "ACTIVE"}
	if err = exec.QueryRowContext(ctx, "SELECT COUNT(*) FROM service_portfolio_offers WHERE tenant_id=? AND parent_service_id=? AND lifecycle='ACTIVE'", a.TenantID, id).Scan(&out.ActiveOffers); err != nil {
		return out, err
	}
	if err = exec.QueryRowContext(ctx, "SELECT COUNT(*) FROM service_portfolio_offers o JOIN catalog_items ci ON ci.tenant_id=o.tenant_id AND ci.id=o.catalog_item_id WHERE o.tenant_id=? AND o.parent_service_id=? AND o.lifecycle='ACTIVE' AND ci.published_version IS NOT NULL", a.TenantID, id).Scan(&out.ActiveCatalogItems); err != nil {
		return out, err
	}
	if a.Has("incident.read") || a.Has("incident.read_own") {
		out.IncidentsVisible = true
		incidentScope := ""
		incidentArgs := []any{}
		if !a.Has("incident.read") {
			incidentScope = " AND (i.reporter_id=? OR i.affected_user_id=?)"
			incidentArgs = append(incidentArgs, a.UserID, a.UserID)
		}
		if err = exec.QueryRowContext(ctx, `SELECT COUNT(DISTINCT i.id) FROM incidents i JOIN cmdb_incident_links l ON l.tenant_id=i.tenant_id AND l.incident_id=i.id
 WHERE i.tenant_id=? AND i.state NOT IN('CLOSED','CANCELLED')`+incidentScope+` AND l.ci_id IN(
  SELECT ci_id FROM service_portfolio_items WHERE tenant_id=? AND id=? AND ci_id IS NOT NULL
  UNION SELECT target_ci_id FROM service_portfolio_relations WHERE tenant_id=? AND source_item_id=? AND target_ci_id IS NOT NULL
 )`, append([]any{a.TenantID}, append(incidentArgs, a.TenantID, id, a.TenantID, id)...)...).Scan(&out.OpenIncidents); err != nil {
			return out, err
		}
	}
	if a.Has("request.read_own") || a.Has("request.manage") || a.Has("request.fulfill") {
		out.RequestsVisible = true
		requestScope := ""
		requestArgs := []any{}
		if !a.Has("request.manage") && !a.Has("request.fulfill") {
			requestScope = " AND r.requester_id=?"
			requestArgs = append(requestArgs, a.UserID)
		}
		if err = exec.QueryRowContext(ctx, `SELECT COUNT(DISTINCT r.id) FROM service_requests r JOIN cmdb_request_links l ON l.tenant_id=r.tenant_id AND l.request_id=r.id
 WHERE r.tenant_id=? AND r.state NOT IN('CLOSED','CANCELLED','FULFILLED')`+requestScope+` AND l.ci_id IN(
  SELECT ci_id FROM service_portfolio_items WHERE tenant_id=? AND id=? AND ci_id IS NOT NULL
  UNION SELECT target_ci_id FROM service_portfolio_relations WHERE tenant_id=? AND source_item_id=? AND target_ci_id IS NOT NULL
 )`, append([]any{a.TenantID}, append(requestArgs, a.TenantID, id, a.TenantID, id)...)...).Scan(&out.OpenRequests); err != nil {
			return out, err
		}
	}
	if err = exec.QueryRowContext(ctx, "SELECT COUNT(*) FROM service_portfolio_relations WHERE tenant_id=? AND target_item_id=? AND relation_type='DEPENDS_ON'", a.TenantID, id).Scan(&out.DependentItems); err != nil {
		return out, err
	}
	return out, nil
}

func (s *Service) Quality(ctx context.Context, a Actor, before uint64, limit int) (ViolationPage, error) {
	page := ViolationPage{Items: []Violation{}}
	if !CanRead(a) {
		return page, workrecords.ErrForbidden
	}
	if limit < 5 || limit > 100 {
		return page, ErrInvalid
	}
	access, args := portfolioAccess(a, "p")
	type rule struct{ code, severity, message, condition string }
	rules := []rule{
		{"owner_missing", "ERROR", "Brakuje właściciela biznesowego lub technicznego", "(p.business_owner_id IS NULL OR p.technical_owner_id IS NULL)"},
		{"support_group_inactive", "ERROR", "Grupa wsparcia jest nieaktywna", "p.support_group_id IS NOT NULL AND g.active=FALSE"},
		{"support_group_missing", "ERROR", "Nie przypisano grupy wsparcia", "p.support_group_id IS NULL"},
		{"service_without_dependency", "WARNING", "Opublikowana lub aktywna usługa nie ma zależności", "p.lifecycle IN('PUBLISHED','ACTIVE') AND NOT EXISTS(SELECT 1 FROM service_portfolio_relations d WHERE d.tenant_id=p.tenant_id AND d.source_item_id=p.id AND d.relation_type='DEPENDS_ON')"},
		{"retired_component_in_use", "ERROR", "Aktywna usługa zależy od wycofanego lub zarchiwizowanego komponentu", "p.lifecycle='ACTIVE' AND EXISTS(SELECT 1 FROM service_portfolio_relations d LEFT JOIN service_portfolio_items t ON t.tenant_id=d.tenant_id AND t.id=d.target_item_id LEFT JOIN cmdb_cis c ON c.tenant_id=d.tenant_id AND c.id=d.target_ci_id WHERE d.tenant_id=p.tenant_id AND d.source_item_id=p.id AND d.relation_type='DEPENDS_ON' AND (t.lifecycle='RETIRED' OR c.status='RETIRED' OR c.archived=TRUE))"},
		{"environment_mismatch", "WARNING", "Środowisko zależności nie zgadza się ze środowiskiem usługi", "p.environment<>'ALL' AND EXISTS(SELECT 1 FROM service_portfolio_relations d LEFT JOIN service_portfolio_items t ON t.tenant_id=d.tenant_id AND t.id=d.target_item_id LEFT JOIN cmdb_cis c ON c.tenant_id=d.tenant_id AND c.id=d.target_ci_id WHERE d.tenant_id=p.tenant_id AND d.source_item_id=p.id AND d.relation_type='DEPENDS_ON' AND (t.environment IS NOT NULL AND t.environment<>'ALL' AND t.environment<>p.environment OR c.environment IS NOT NULL AND c.environment<>'ALL' AND c.environment<>p.environment))"},
	}
	items := []Violation{}
	for _, r := range rules {
		query := `SELECT p.id,p.service_key,p.name FROM service_portfolio_items p LEFT JOIN ` + quotedGroups + ` g ON g.tenant_id=p.tenant_id AND g.id=p.support_group_id WHERE p.tenant_id=? AND ` + r.condition + " AND " + access
		params := []any{a.TenantID}
		params = append(params, args...)
		if before > 0 {
			query += " AND p.id<?"
			params = append(params, before)
		}
		query += " ORDER BY p.id DESC LIMIT 101"
		rows, err := s.DB.QueryContext(ctx, query, params...)
		if err != nil {
			return page, err
		}
		for rows.Next() {
			var item Violation
			item.Rule = r.code
			item.Severity = r.severity
			item.Message = r.message
			if err := rows.Scan(&item.ItemID, &item.ServiceKey, &item.Name); err != nil {
				rows.Close()
				return page, err
			}
			items = append(items, item)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return page, err
		}
		rows.Close()
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].ItemID == items[j].ItemID {
			return items[i].Rule < items[j].Rule
		}
		return items[i].ItemID > items[j].ItemID
	})
	if len(items) > limit {
		cut := limit
		for cut > 0 && cut < len(items) && items[cut-1].ItemID == items[cut].ItemID {
			cut--
		}
		if cut == 0 {
			for cut < len(items) && items[cut].ItemID == items[0].ItemID {
				cut++
			}
		}
		items = items[:cut]
		if len(items) > 0 {
			page.Next = items[len(items)-1].ItemID
		}
	}
	page.Items = items
	return page, nil
}

func (s *Service) SaveOrgRef(ctx context.Context, a Actor, id uint64, ref OrgRef) (OrgRef, error) {
	if !CanManage(a) {
		return OrgRef{}, workrecords.ErrForbidden
	}
	ref.Kind = strings.ToUpper(strings.TrimSpace(ref.Kind))
	ref.Key, _ = normalizeKey(ref.Key)
	ref.Name = strings.TrimSpace(ref.Name)
	if !contains([]string{"ORG_UNIT", "DEPARTMENT", "COST_CENTER"}, ref.Kind) || ref.Key == "" || !assets.Text(ref.Name, 1, 160) {
		return OrgRef{}, ErrInvalid
	}
	err := s.actorTx(ctx, a, func(exec database.Executor, current Actor) error {
		if id == 0 {
			if ref.Version != 0 {
				return ErrInvalid
			}
			res, err := exec.ExecContext(ctx, "INSERT INTO service_portfolio_org_refs(tenant_id,kind,ref_key,name,active) VALUES(?,?,?,?,?)", current.TenantID, ref.Kind, ref.Key, ref.Name, ref.Active)
			if err != nil {
				return missing(err)
			}
			n, err := res.LastInsertId()
			if err != nil {
				return err
			}
			id = uint64(n)
			ref.ID = id
			ref.Version = 1
		} else {
			var old OrgRef
			if err := exec.QueryRowContext(ctx, "SELECT id,kind,ref_key,name,active,version FROM service_portfolio_org_refs WHERE tenant_id=? AND id=? FOR UPDATE", current.TenantID, id).Scan(&old.ID, &old.Kind, &old.Key, &old.Name, &old.Active, &old.Version); err != nil {
				return missing(err)
			}
			if ref.Version == 0 || ref.Version != old.Version || ref.Kind != old.Kind || ref.Key != old.Key {
				return ErrConflict
			}
			res, err := exec.ExecContext(ctx, "UPDATE service_portfolio_org_refs SET name=?,active=?,version=version+1,updated_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND id=? AND version=?", ref.Name, ref.Active, current.TenantID, id, ref.Version)
			if err != nil {
				return missing(err)
			}
			n, err := res.RowsAffected()
			if err != nil {
				return err
			}
			if n != 1 {
				return ErrConflict
			}
			ref.Version++
		}
		ref.ID = id
		return s.changed(ctx, exec, current, "service_portfolio.org_ref.changed", "service_portfolio_org_ref", id, ref.Version, nil, ref, []string{"data"})
	})
	return ref, err
}

func (s *Service) ListLookup(ctx context.Context, a Actor, kind, q string, before uint64, limit int) (Lookup, error) {
	out := Lookup{Items: []map[string]any{}}
	if limit < 1 || limit > 100 || len(q) > 160 {
		return out, ErrInvalid
	}
	q = strings.TrimSpace(q)
	if !CanRead(a) && !a.Has("service_portfolio.offer.read") && !a.Has("catalog.read") {
		return out, workrecords.ErrForbidden
	}
	if kind == "users" && !CanManage(a) {
		return out, workrecords.ErrForbidden
	}
	if kind == "cis" && (!a.Has("cmdb.read") && !a.Has("cmdb.read_own")) {
		return out, workrecords.ErrForbidden
	}
	var query string
	var args []any
	switch kind {
	case "users":
		query = `SELECT u.id,u.display_name,u.email,TRUE FROM users u JOIN tenant_memberships tm ON tm.user_id=u.id WHERE tm.tenant_id=? AND tm.active=TRUE AND u.active=TRUE`
		args = []any{a.TenantID}
	case "groups":
		query = "SELECT id,name,active,version FROM `groups` WHERE tenant_id=?"
		args = []any{a.TenantID}
	case "locations":
		query = "SELECT id,value,active,1 FROM asset_catalog_values WHERE tenant_id=? AND kind='LOCATION'"
		args = []any{a.TenantID}
	case "org_units", "departments", "cost_centers":
		kindValue := map[string]string{"org_units": "ORG_UNIT", "departments": "DEPARTMENT", "cost_centers": "COST_CENTER"}[kind]
		query = "SELECT id,ref_key,name,active,version FROM service_portfolio_org_refs WHERE tenant_id=? AND kind=?"
		args = []any{a.TenantID, kindValue}
	case "cis":
		access, accessArgs := ciAccess(a, "ci")
		query = "SELECT ci.id,ci.identifier,ci.name,ci.class,ci.environment,ci.version FROM cmdb_cis ci WHERE ci.tenant_id=? AND ci.archived=FALSE AND " + access
		args = []any{a.TenantID}
		args = append(args, accessArgs...)
	case "items":
		access, accessArgs := portfolioAccess(a, "p")
		query = "SELECT p.id,p.service_key,p.name,p.item_type,p.lifecycle,p.version FROM service_portfolio_items p WHERE p.tenant_id=? AND " + access
		args = []any{a.TenantID}
		args = append(args, accessArgs...)
	case "catalog_items":
		if !CanManage(a) {
			return out, workrecords.ErrForbidden
		}
		query = `SELECT i.id,JSON_UNQUOTE(JSON_EXTRACT(v.definition,'$.title')),i.published_version FROM catalog_items i JOIN catalog_item_versions v ON v.tenant_id=i.tenant_id AND v.item_id=i.id AND v.version=i.published_version WHERE i.tenant_id=?`
		args = []any{a.TenantID}
	case "sla_policies":
		if !CanManage(a) {
			return out, workrecords.ErrForbidden
		}
		query = `SELECT d.id,COALESCE(JSON_UNQUOTE(JSON_EXTRACT(v.config,'$.name')),CONCAT('SLA #',d.id)),d.version FROM sla_definitions d JOIN sla_definition_versions v ON v.tenant_id=d.tenant_id AND v.definition_id=d.id AND v.version=(SELECT MAX(v2.version) FROM sla_definition_versions v2 WHERE v2.tenant_id=d.tenant_id AND v2.definition_id=d.id) WHERE d.tenant_id=? AND d.kind='policy'`
		args = []any{a.TenantID}
	default:
		return out, ErrInvalid
	}
	if q != "" {
		if kind == "users" {
			query += " AND (u.display_name LIKE ? OR u.email LIKE ?)"
			args = append(args, "%"+q+"%", "%"+q+"%")
		} else if kind == "cis" {
			query += " AND (ci.name LIKE ? OR ci.identifier LIKE ?)"
			args = append(args, "%"+q+"%", "%"+q+"%")
		} else if kind == "locations" {
			query += " AND value LIKE ?"
			args = append(args, "%"+q+"%")
		} else if kind == "org_units" || kind == "departments" || kind == "cost_centers" {
			query += " AND (ref_key LIKE ? OR name LIKE ?)"
			args = append(args, "%"+q+"%", "%"+q+"%")
		} else if kind == "catalog_items" || kind == "sla_policies" {
			query += " AND LOCATE(?,JSON_UNQUOTE(JSON_EXTRACT(v.definition,'$.title')))>0"
			if kind == "sla_policies" {
				query = strings.TrimSuffix(query, " AND LOCATE(?,JSON_UNQUOTE(JSON_EXTRACT(v.definition,'$.title')))>0") + " AND JSON_UNQUOTE(JSON_EXTRACT(v.config,'$.name')) LIKE ?"
			}
			args = append(args, "%"+q+"%")
		} else {
			query += " AND name LIKE ?"
			args = append(args, "%"+q+"%")
		}
	}
	if before > 0 {
		query += " AND id<?"
		if kind == "cis" {
			query = strings.TrimSuffix(query, " AND id<?") + " AND ci.id<?"
		}
		if kind == "items" {
			query = strings.TrimSuffix(query, " AND id<?") + " AND p.id<?"
		}
		if kind == "catalog_items" {
			query = strings.TrimSuffix(query, " AND id<?") + " AND i.id<?"
		}
		if kind == "sla_policies" {
			query = strings.TrimSuffix(query, " AND id<?") + " AND d.id<?"
		}
		args = append(args, before)
	}
	query += " ORDER BY 1 DESC LIMIT ?"
	args = append(args, limit+1)
	rows, err := s.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		item, scanErr := scanLookup(rows, kind)
		if scanErr != nil {
			return out, scanErr
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return out, err
	}
	if len(items) > limit {
		items = items[:limit]
		if id, ok := lookupNextID(items[len(items)-1]); ok {
			out.Next = id
		}
	}
	out.Items = items
	return out, nil
}

func scanLookup(row interface{ Scan(...any) error }, kind string) (map[string]any, error) {
	switch kind {
	case "users":
		var id uint64
		var name, email string
		var active bool
		err := row.Scan(&id, &name, &email, &active)
		return map[string]any{"id": strconv.FormatUint(id, 10), "name": name, "email": email, "active": active}, err
	case "groups":
		var id, version uint64
		var name string
		var active bool
		err := row.Scan(&id, &name, &active, &version)
		return map[string]any{"id": strconv.FormatUint(id, 10), "name": name, "active": active, "version": strconv.FormatUint(version, 10)}, err
	case "locations":
		var id, version uint64
		var name string
		var active bool
		err := row.Scan(&id, &name, &active, &version)
		return map[string]any{"id": strconv.FormatUint(id, 10), "name": name, "active": active, "version": strconv.FormatUint(version, 10)}, err
	case "org_units", "departments", "cost_centers":
		var id, version uint64
		var key, name string
		var active bool
		err := row.Scan(&id, &key, &name, &active, &version)
		return map[string]any{"id": strconv.FormatUint(id, 10), "key": key, "name": name, "active": active, "version": strconv.FormatUint(version, 10)}, err
	case "cis":
		var id, version uint64
		var identifier, name, class, environment string
		err := row.Scan(&id, &identifier, &name, &class, &environment, &version)
		return map[string]any{"id": strconv.FormatUint(id, 10), "identifier": identifier, "name": name, "class": class, "environment": environment, "version": strconv.FormatUint(version, 10)}, err
	case "items":
		var id, version uint64
		var key, name, typ, lifecycle string
		err := row.Scan(&id, &key, &name, &typ, &lifecycle, &version)
		return map[string]any{"id": strconv.FormatUint(id, 10), "service_key": key, "name": name, "type": typ, "lifecycle": lifecycle, "version": strconv.FormatUint(version, 10)}, err
	case "catalog_items", "sla_policies":
		var id, version uint64
		var name string
		err := row.Scan(&id, &name, &version)
		return map[string]any{"id": strconv.FormatUint(id, 10), "name": name, "version": strconv.FormatUint(version, 10)}, err
	default:
		return nil, ErrInvalid
	}
}

func (s *Service) SetAvailability(ctx context.Context, a Actor, in AvailabilityInput) (Availability, error) {
	var out Availability
	if !CanManage(a) {
		return out, workrecords.ErrForbidden
	}
	in.NodeKind = strings.ToUpper(strings.TrimSpace(in.NodeKind))
	in.State = strings.ToUpper(strings.TrimSpace(in.State))
	in.Note = strings.TrimSpace(in.Note)
	if !contains([]string{"ITEM", "CI"}, in.NodeKind) || in.NodeID == 0 || !contains([]string{"AVAILABLE", "DEGRADED", "UNAVAILABLE", "UNKNOWN"}, in.State) || !assets.Text(in.Note, 0, 500) {
		return out, ErrInvalid
	}
	err := s.actorTx(ctx, a, func(exec database.Executor, current Actor) error {
		if in.NodeKind == "ITEM" {
			if _, err := s.getInTx(ctx, exec, current, in.NodeID, true); err != nil {
				return err
			}
		} else {
			if !current.Has("cmdb.read") && !current.Has("cmdb.read_own") {
				return workrecords.ErrForbidden
			}
			access, args := ciAccess(current, "ci")
			params := []any{current.TenantID, in.NodeID}
			params = append(params, args...)
			var found int
			if err := exec.QueryRowContext(ctx, "SELECT 1 FROM cmdb_cis ci WHERE ci.tenant_id=? AND ci.id=? AND "+access+" FOR UPDATE", params...).Scan(&found); errors.Is(err, sql.ErrNoRows) {
				return workrecords.ErrForbidden
			} else if err != nil {
				return err
			}
		}
		var old Availability
		oldErr := exec.QueryRowContext(ctx, "SELECT node_kind,node_id,state,source,note,version,observed_at FROM service_portfolio_component_availability WHERE tenant_id=? AND node_kind=? AND node_id=? FOR UPDATE", current.TenantID, in.NodeKind, in.NodeID).Scan(&old.NodeKind, &old.NodeID, &old.State, &old.Source, &old.Note, &old.Version, &old.ObservedAt)
		if oldErr != nil && !errors.Is(oldErr, sql.ErrNoRows) {
			return oldErr
		}
		if errors.Is(oldErr, sql.ErrNoRows) {
			if in.Version != 0 {
				return ErrConflict
			}
			_, err := exec.ExecContext(ctx, "INSERT INTO service_portfolio_component_availability(tenant_id,node_kind,node_id,state,source,note,version,observed_by,observed_at) VALUES(?,?,?,?,'MANUAL',?,1,?,UTC_TIMESTAMP(6))", current.TenantID, in.NodeKind, in.NodeID, in.State, in.Note, current.UserID)
			if err != nil {
				return err
			}
			out = Availability{NodeKind: in.NodeKind, NodeID: in.NodeID, State: in.State, Source: "MANUAL", Note: in.Note, Version: 1, ObservedAt: time.Now().UTC()}
		} else {
			if old.Version == in.Version+1 && old.State == in.State && old.Note == in.Note {
				out = old
				return nil
			}
			if old.Version != in.Version || in.Version == 0 {
				return ErrConflict
			}
			_, err := exec.ExecContext(ctx, "UPDATE service_portfolio_component_availability SET state=?,note=?,version=version+1,observed_by=?,observed_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND node_kind=? AND node_id=? AND version=?", in.State, in.Note, current.UserID, current.TenantID, in.NodeKind, in.NodeID, in.Version)
			if err != nil {
				return err
			}
			out = Availability{NodeKind: in.NodeKind, NodeID: in.NodeID, State: in.State, Source: "MANUAL", Note: in.Note, Version: in.Version + 1, ObservedAt: time.Now().UTC()}
		}
		return s.changed(ctx, exec, current, "service_portfolio.availability.changed", "service_portfolio_availability_"+strings.ToLower(in.NodeKind), in.NodeID, out.Version, old, out, []string{"availability"})
	})
	return out, err
}

func (s *Service) GetAvailability(ctx context.Context, a Actor, kind string, id uint64) (Availability, error) {
	kind = strings.ToUpper(strings.TrimSpace(kind))
	if id == 0 || kind != "ITEM" && kind != "CI" {
		return Availability{}, ErrInvalid
	}
	if !CanRead(a) {
		return Availability{}, workrecords.ErrForbidden
	}
	if kind == "ITEM" {
		if _, err := s.Get(ctx, a, id); err != nil {
			return Availability{}, err
		}
	} else {
		if !a.Has("cmdb.read") && !a.Has("cmdb.read_own") {
			return Availability{}, workrecords.ErrForbidden
		}
		access, args := ciAccess(a, "ci")
		params := []any{a.TenantID, id}
		params = append(params, args...)
		var exists bool
		if err := s.DB.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM cmdb_cis ci WHERE ci.tenant_id=? AND ci.id=? AND "+access+")", params...).Scan(&exists); err != nil {
			return Availability{}, err
		}
		if !exists {
			return Availability{}, workrecords.ErrForbidden
		}
	}
	out := Availability{NodeKind: kind, NodeID: id, State: "UNKNOWN", Source: "NONE"}
	err := s.DB.QueryRowContext(ctx, "SELECT state,source,note,version,observed_at FROM service_portfolio_component_availability WHERE tenant_id=? AND node_kind=? AND node_id=?", a.TenantID, kind, id).Scan(&out.State, &out.Source, &out.Note, &out.Version, &out.ObservedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return out, nil
	}
	return out, err
}

func boolLifecycle(status string) bool { return status == "ACTIVE" || status == "PUBLISHED" }
