package serviceportfolio

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/chmajster/OpenServiceNOW/internal/activity"
	"github.com/chmajster/OpenServiceNOW/internal/audit"
	"github.com/chmajster/OpenServiceNOW/internal/events"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
	mysql "github.com/go-sql-driver/mysql"
)

const impactRuleVersion uint64 = 1

type Service struct {
	DB              *sql.DB
	tx              *database.TxManager
	ci              CIReader
	outbox          *events.Repository
	relatedAdapters map[string]RelatedWorkAdapter
}

func NewService(db *sql.DB, ci CIReader) *Service {
	service := &Service{DB: db, tx: database.NewTxManager(db), ci: ci, outbox: events.NewRepository(db), relatedAdapters: map[string]RelatedWorkAdapter{}}
	if ci != nil {
		service.relatedAdapters["incidents"] = cmdbWorkAdapter{reader: ci, kind: "incidents"}
		service.relatedAdapters["requests"] = cmdbWorkAdapter{reader: ci, kind: "requests"}
	}
	return service
}

func (s *Service) WithRelatedAdapter(kind string, adapter RelatedWorkAdapter) *Service {
	if s.relatedAdapters == nil {
		s.relatedAdapters = map[string]RelatedWorkAdapter{}
	}
	if kind != "" && adapter != nil {
		s.relatedAdapters[kind] = adapter
	}
	return s
}

func CanRead(a Actor) bool {
	return a.Has("service_portfolio.read") || a.Has("service_portfolio.read_own") || a.Has("service_portfolio.manage")
}

func CanManage(a Actor) bool { return a.Has("service_portfolio.manage") }

func (s *Service) actorTx(ctx context.Context, a Actor, fn func(database.Executor, Actor) error) error {
	return s.tx.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(exec database.Executor) error {
		current, err := workrecords.LockPrincipal(ctx, exec, a)
		if err != nil {
			return err
		}
		return fn(exec, current)
	})
}

func portfolioAccess(a Actor, alias string) (string, []any) {
	if a.Has("service_portfolio.read") || a.Has("service_portfolio.manage") {
		return "1=1", nil
	}
	if a.Has("service_portfolio.read_own") {
		return "(" + alias + ".business_owner_id=? OR " + alias + ".technical_owner_id=?)", []any{a.UserID, a.UserID}
	}
	return "1=0", nil
}

func ciAccess(a Actor, alias string) (string, []any) {
	if a.Has("cmdb.read") {
		return "1=1", nil
	}
	if a.Has("cmdb.read_own") {
		return alias + ".owner_id=?", []any{a.UserID}
	}
	return "1=0", nil
}

const itemColumns = `p.id,p.service_key,p.name,p.item_type,p.description,p.lifecycle,p.criticality,p.environment,p.scope,
 COALESCE(p.business_owner_id,0),COALESCE(bo.display_name,''),COALESCE(p.technical_owner_id,0),COALESCE(to_u.display_name,''),
 COALESCE(p.support_group_id,0),COALESCE(g.name,''),COALESCE(g.active,FALSE),
 COALESCE(p.org_unit_ref_id,0),COALESCE(ou.name,''),COALESCE(p.department_ref_id,0),COALESCE(dep.name,''),
 COALESCE(p.cost_center_ref_id,0),COALESCE(cc.name,''),COALESCE(p.location_id,0),COALESCE(loc.value,''),
 COALESCE(ci.id,0),COALESCE(ci.name,''),COALESCE(ci.class,''),COALESCE(ci.version,0),
 p.dependency_mode,p.dependency_quorum,p.version,p.configuration_version,p.created_at,p.updated_at`

const quotedGroups = "\x60groups\x60"

func itemJoins(a Actor) (string, []any) {
	ciAccessSQL, ciArgs := ciAccess(a, "ci")
	return ` FROM service_portfolio_items p
 LEFT JOIN users bo ON bo.id=p.business_owner_id
 LEFT JOIN users to_u ON to_u.id=p.technical_owner_id
 LEFT JOIN ` + quotedGroups + ` g ON g.tenant_id=p.tenant_id AND g.id=p.support_group_id
 LEFT JOIN service_portfolio_org_refs ou ON ou.tenant_id=p.tenant_id AND ou.id=p.org_unit_ref_id AND ou.kind='ORG_UNIT'
 LEFT JOIN service_portfolio_org_refs dep ON dep.tenant_id=p.tenant_id AND dep.id=p.department_ref_id AND dep.kind='DEPARTMENT'
 LEFT JOIN service_portfolio_org_refs cc ON cc.tenant_id=p.tenant_id AND cc.id=p.cost_center_ref_id AND cc.kind='COST_CENTER'
 LEFT JOIN asset_catalog_values loc ON loc.id=p.location_id AND loc.tenant_id=p.tenant_id AND loc.kind='LOCATION'
 LEFT JOIN cmdb_cis ci ON ci.tenant_id=p.tenant_id AND ci.id=p.ci_id AND ` + ciAccessSQL, ciArgs
}

func scanItem(row interface{ Scan(...any) error }) (Item, error) {
	var out Item
	err := row.Scan(&out.ID, &out.ServiceKey, &out.Name, &out.Type, &out.Description, &out.Lifecycle, &out.Criticality,
		&out.Environment, &out.Scope, &out.BusinessOwnerID, &out.BusinessOwnerName, &out.TechnicalOwnerID,
		&out.TechnicalOwnerName, &out.SupportGroupID, &out.SupportGroupName, &out.SupportGroupActive,
		&out.OrgUnitRefID, &out.OrgUnitName, &out.DepartmentRefID, &out.DepartmentName, &out.CostCenterRefID,
		&out.CostCenterName, &out.LocationID, &out.LocationName, &out.CIID, &out.CIName, &out.CIClass,
		&out.CIVersion, &out.DependencyMode, &out.DependencyQuorum, &out.Version, &out.ConfigurationVersion,
		&out.CreatedAt, &out.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Item{}, ErrNotFound
	}
	return out, err
}

func (s *Service) getInTx(ctx context.Context, exec database.Executor, a Actor, id uint64, lock bool) (Item, error) {
	access, args := portfolioAccess(a, "p")
	joins, joinArgs := itemJoins(a)
	query := "SELECT " + itemColumns + joins + " WHERE p.tenant_id=? AND p.id=? AND " + access
	params := append([]any{}, joinArgs...)
	params = append(params, a.TenantID, id)
	params = append(params, args...)
	if lock {
		query += " FOR UPDATE"
	}
	return scanItem(exec.QueryRowContext(ctx, query, params...))
}

func (s *Service) Get(ctx context.Context, a Actor, id uint64) (Item, error) {
	if !CanRead(a) {
		return Item{}, workrecords.ErrForbidden
	}
	return s.getInTx(ctx, s.DB, a, id, false)
}

func (s *Service) List(ctx context.Context, a Actor, f ItemFilter) (ItemPage, error) {
	page := ItemPage{Items: []Item{}}
	if !CanRead(a) {
		return page, workrecords.ErrForbidden
	}
	f.Query = strings.TrimSpace(f.Query)
	if f.Limit < 1 || f.Limit > 100 || len(f.Query) > 160 || f.Type != "" && !itemTypes[f.Type] ||
		f.Lifecycle != "" && !contains([]string{"DRAFT", "PUBLISHED", "ACTIVE", "RETIRED"}, f.Lifecycle) ||
		f.Environment != "" && !environments[f.Environment] {
		return page, ErrInvalid
	}
	access, args := portfolioAccess(a, "p")
	joins, joinArgs := itemJoins(a)
	params := append([]any{}, joinArgs...)
	params = append(params, a.TenantID)
	params = append(params, args...)
	query := "SELECT " + itemColumns + joins + " WHERE p.tenant_id=? AND " + access
	if f.Query != "" {
		pattern := strings.NewReplacer("=", "==", "%", "=%", "_", "=_").Replace(strings.ToLower(f.Query)) + "%"
		query += " AND (LOWER(p.service_key) LIKE ? ESCAPE '=' OR LOWER(p.name) LIKE ? ESCAPE '=' OR LOWER(p.description) LIKE ? ESCAPE '=')"
		params = append(params, pattern, pattern, pattern)
	}
	for _, filter := range []struct{ column, value string }{{"p.item_type", f.Type}, {"p.lifecycle", f.Lifecycle}, {"p.environment", f.Environment}} {
		if filter.value != "" {
			query += " AND " + filter.column + "=?"
			params = append(params, filter.value)
		}
	}
	if f.OwnerID > 0 {
		query += " AND (p.business_owner_id=? OR p.technical_owner_id=?)"
		params = append(params, f.OwnerID, f.OwnerID)
	}
	if f.Before > 0 {
		query += " AND p.id<?"
		params = append(params, f.Before)
	}
	query += " ORDER BY p.id DESC LIMIT ?"
	params = append(params, f.Limit+1)
	rows, err := s.DB.QueryContext(ctx, query, params...)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		item, scanErr := scanItem(rows)
		if scanErr != nil {
			return page, scanErr
		}
		page.Items = append(page.Items, item)
	}
	if err := rows.Err(); err != nil {
		return page, err
	}
	if len(page.Items) > f.Limit {
		page.Items = page.Items[:f.Limit]
		page.Next = page.Items[len(page.Items)-1].ID
	}
	return page, nil
}

func (s *Service) Save(ctx context.Context, a Actor, id uint64, in ItemInput, idempotencyKey string) (Item, error) {
	var out Item
	if !CanManage(a) {
		return out, workrecords.ErrForbidden
	}
	var err error
	in, err = normalizeItem(in)
	if err != nil {
		return out, err
	}
	isCreate := id == 0
	if isCreate && in.ClearCILink {
		return out, ErrInvalid
	}
	err = s.actorTx(ctx, a, func(exec database.Executor, current Actor) error {
		if !CanManage(current) {
			return workrecords.ErrForbidden
		}
		var before *Item
		preserveCILink := false
		lifecycle := "DRAFT"
		if id > 0 {
			old, getErr := s.getInTx(ctx, exec, current, id, true)
			if getErr != nil {
				return getErr
			}
			if in.Version == 0 || old.Version != in.Version {
				return ErrConflict
			}
			if old.ServiceKey != in.ServiceKey || old.Type != in.Type || old.Lifecycle == "RETIRED" {
				return fmt.Errorf("%w: stabilny identyfikator, typ i wycofany cykl życia są niezmienne", ErrConflict)
			}
			var storedCI sql.Null[uint64]
			if err = exec.QueryRowContext(ctx, "SELECT ci_id FROM service_portfolio_items WHERE tenant_id=? AND id=? FOR UPDATE", current.TenantID, id).Scan(&storedCI); err != nil {
				return err
			}
			if in.ClearCILink && storedCI.Valid || in.CIID > 0 && storedCI.Valid && in.CIID != storedCI.V {
				if err = s.requireVisibleCI(ctx, exec, current, storedCI.V); err != nil {
					return err
				}
			}
			preserveCILink = in.CIID == 0 && !in.ClearCILink
			lifecycle = old.Lifecycle
			before = &old
		} else {
			if in.Version != 0 || strings.TrimSpace(idempotencyKey) == "" || len(idempotencyKey) > 190 {
				return ErrInvalid
			}
			raw, marshalErr := json.Marshal(in)
			if marshalErr != nil {
				return marshalErr
			}
			previous, reserveErr := workrecords.ReserveOperation(ctx, exec, current.Actor, idempotencyKey, "service_portfolio_item_create", sha256.Sum256(raw))
			if reserveErr != nil {
				return reserveErr
			}
			if previous > 0 {
				out, err = s.getInTx(ctx, exec, current, previous, false)
				return err
			}
		}
		if lifecycle != "DRAFT" {
			if err := s.validatePublication(ctx, exec, current, in, id); err != nil {
				return err
			}
		}
		if err := s.validateReferences(ctx, exec, current, in); err != nil {
			return err
		}
		arguments := []any{in.ServiceKey, in.Name, in.Type, in.Description, in.Criticality, in.Environment, in.Scope,
			nullableID(in.BusinessOwnerID), nullableID(in.TechnicalOwnerID), nullableID(in.SupportGroupID),
			nullableID(in.OrgUnitRefID), nullableID(in.DepartmentRefID), nullableID(in.CostCenterRefID), nullableID(in.LocationID),
			nullableID(in.CIID), in.DependencyMode, in.DependencyQuorum}
		if id == 0 {
			arguments = append([]any{current.TenantID}, arguments...)
			result, insertErr := exec.ExecContext(ctx, `INSERT INTO service_portfolio_items
 (tenant_id,service_key,name,item_type,description,criticality,environment,scope,business_owner_id,technical_owner_id,support_group_id,
  org_unit_ref_id,department_ref_id,cost_center_ref_id,location_id,ci_id,dependency_mode,dependency_quorum,created_by)
 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, append(arguments, current.UserID)...)
			if insertErr != nil {
				return missing(insertErr)
			}
			lastID, insertErr := result.LastInsertId()
			if insertErr != nil {
				return insertErr
			}
			id = uint64(lastID)
		} else {
			updateArgs := []any{in.Name, in.Description, in.Criticality, in.Environment, in.Scope,
				nullableID(in.BusinessOwnerID), nullableID(in.TechnicalOwnerID), nullableID(in.SupportGroupID),
				nullableID(in.OrgUnitRefID), nullableID(in.DepartmentRefID), nullableID(in.CostCenterRefID), nullableID(in.LocationID)}
			ciAssignment := "ci_id=?,"
			if preserveCILink {
				ciAssignment = ""
			} else {
				updateArgs = append(updateArgs, nullableID(in.CIID))
			}
			updateArgs = append(updateArgs, in.DependencyMode, in.DependencyQuorum, current.TenantID, id, in.Version)
			result, updateErr := exec.ExecContext(ctx, `UPDATE service_portfolio_items SET name=?,description=?,criticality=?,environment=?,scope=?,
 business_owner_id=?,technical_owner_id=?,support_group_id=?,org_unit_ref_id=?,department_ref_id=?,cost_center_ref_id=?,location_id=?,`+ciAssignment+`
				dependency_mode=?,dependency_quorum=?,version=version+1,configuration_version=configuration_version+1,updated_at=UTC_TIMESTAMP(6)
 WHERE tenant_id=? AND id=? AND version=?`, updateArgs...)
			if updateErr != nil {
				return missing(updateErr)
			}
			affected, updateErr := result.RowsAffected()
			if updateErr != nil || affected != 1 {
				if updateErr != nil {
					return updateErr
				}
				return ErrConflict
			}
		}
		out, err = s.getInTx(ctx, exec, current, id, false)
		if err != nil {
			return err
		}
		if err := s.changed(ctx, exec, current, "service_portfolio.item.changed", "service_portfolio_item", id, out.Version, before, out, []string{"data"}); err != nil {
			return err
		}
		if isCreate {
			return workrecords.CompleteOperation(ctx, exec, current.Actor, idempotencyKey, out.ID)
		}
		return nil
	})
	return out, err
}

func nullableID(id uint64) any {
	if id == 0 {
		return nil
	}
	return id
}

func missing(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
		return ErrDuplicate
	}
	return err
}

func (s *Service) requireVisibleCI(ctx context.Context, exec database.Executor, a Actor, id uint64) error {
	if id == 0 || !a.Has("cmdb.read") && !a.Has("cmdb.read_own") {
		return workrecords.ErrForbidden
	}
	access, args := ciAccess(a, "ci")
	params := []any{a.TenantID, id}
	params = append(params, args...)
	var visible bool
	err := exec.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM cmdb_cis ci WHERE ci.tenant_id=? AND ci.id=? AND "+access+")", params...).Scan(&visible)
	if err != nil {
		return err
	}
	if !visible {
		return workrecords.ErrForbidden
	}
	return nil
}

func (s *Service) validateReferences(ctx context.Context, exec database.Executor, a Actor, in ItemInput) error {
	for _, userID := range []uint64{in.BusinessOwnerID, in.TechnicalOwnerID} {
		if userID == 0 {
			continue
		}
		if err := workrecords.ValidateActiveUser(ctx, exec, a.TenantID, userID); err != nil {
			return fmt.Errorf("%w: opiekun nie jest aktywnym członkiem organizacji", ErrInvalid)
		}
	}
	if in.SupportGroupID > 0 {
		var found int
		err := exec.QueryRowContext(ctx, "SELECT active FROM `groups` WHERE tenant_id=? AND id=?", a.TenantID, in.SupportGroupID).Scan(&found)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: nie znaleziono grupy wsparcia w tej organizacji", ErrInvalid)
		}
		if err != nil {
			return err
		}
	}
	for _, ref := range []struct {
		id   uint64
		kind string
	}{{in.OrgUnitRefID, "ORG_UNIT"}, {in.DepartmentRefID, "DEPARTMENT"}, {in.CostCenterRefID, "COST_CENTER"}} {
		if ref.id == 0 {
			continue
		}
		var found int
		err := exec.QueryRowContext(ctx, "SELECT active FROM service_portfolio_org_refs WHERE tenant_id=? AND id=? AND kind=?", a.TenantID, ref.id, ref.kind).Scan(&found)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: niespójna jednostka organizacyjna", ErrInvalid)
		}
		if err != nil {
			return err
		}
	}
	if in.LocationID > 0 {
		var found int
		err := exec.QueryRowContext(ctx, "SELECT active FROM asset_catalog_values WHERE tenant_id=? AND id=? AND kind='LOCATION'", a.TenantID, in.LocationID).Scan(&found)
		if errors.Is(err, sql.ErrNoRows) || found == 0 {
			return fmt.Errorf("%w: lokalizacja nie istnieje lub jest nieaktywna w katalogu Assets", ErrInvalid)
		}
		if err != nil {
			return err
		}
	}
	if in.CIID > 0 {
		var class string
		var owner uint64
		var archived bool
		var version uint64
		access, args := ciAccess(a, "ci")
		params := []any{a.TenantID, in.CIID}
		params = append(params, args...)
		err := exec.QueryRowContext(ctx, "SELECT ci.class,ci.owner_id,ci.archived,ci.version FROM cmdb_cis ci WHERE ci.tenant_id=? AND ci.id=? AND "+access+" FOR UPDATE", params...).Scan(&class, &owner, &archived, &version)
		if errors.Is(err, sql.ErrNoRows) {
			return workrecords.ErrForbidden
		}
		if err != nil {
			return err
		}
		if version != in.CIVersion || archived || !ciClassAllowed(in.Type, class) {
			return ErrConflict
		}
	}
	return nil
}

func ciClassAllowed(itemType, ciClass string) bool {
	switch itemType {
	case "BUSINESS_APPLICATION":
		return ciClass == "APPLICATION"
	case "SERVICE_INSTANCE":
		return ciClass == "APPLICATION" || ciClass == "SERVICE"
	case "TECHNICAL_SERVICE", "BUSINESS_SERVICE":
		return ciClass == "SERVICE"
	default:
		return false
	}
}

func (s *Service) validatePublication(ctx context.Context, exec database.Executor, a Actor, in ItemInput, id uint64) error {
	if in.BusinessOwnerID == 0 || in.TechnicalOwnerID == 0 || in.SupportGroupID == 0 || in.Criticality == "" || in.Environment == "" {
		return fmt.Errorf("%w: publikacja wymaga właściciela biznesowego i technicznego, aktywnej grupy wsparcia, krytyczności i środowiska", ErrInvalid)
	}
	var active bool
	if err := exec.QueryRowContext(ctx, "SELECT active FROM `groups` WHERE tenant_id=? AND id=?", a.TenantID, in.SupportGroupID).Scan(&active); errors.Is(err, sql.ErrNoRows) || !active {
		return fmt.Errorf("%w: grupa wsparcia musi być aktywna", ErrInvalid)
	} else if err != nil {
		return err
	}
	for _, ref := range []struct {
		id   uint64
		kind string
	}{{in.OrgUnitRefID, "ORG_UNIT"}, {in.DepartmentRefID, "DEPARTMENT"}, {in.CostCenterRefID, "COST_CENTER"}} {
		if ref.id == 0 {
			continue
		}
		var enabled bool
		if err := exec.QueryRowContext(ctx, "SELECT active FROM service_portfolio_org_refs WHERE tenant_id=? AND id=? AND kind=?", a.TenantID, ref.id, ref.kind).Scan(&enabled); err != nil || !enabled {
			return fmt.Errorf("%w: dane organizacyjne muszą być aktywne przy publikacji", ErrInvalid)
		}
	}
	if in.LocationID > 0 {
		var enabled bool
		if err := exec.QueryRowContext(ctx, "SELECT active FROM asset_catalog_values WHERE tenant_id=? AND id=? AND kind='LOCATION'", a.TenantID, in.LocationID).Scan(&enabled); err != nil || !enabled {
			return fmt.Errorf("%w: lokalizacja musi być aktywna przy publikacji", ErrInvalid)
		}
	}
	if in.DependencyMode != "NOT_MODELED" {
		var count uint64
		if err := exec.QueryRowContext(ctx, "SELECT COUNT(*) FROM service_portfolio_relations WHERE tenant_id=? AND source_item_id=? AND relation_type='DEPENDS_ON'", a.TenantID, id).Scan(&count); err != nil {
			return err
		}
		if count == 0 {
			return fmt.Errorf("%w: wybierz jawny model redundancji dopiero po zdefiniowaniu zależności", ErrInvalid)
		}
		if in.DependencyMode == "QUORUM" && count < uint64(in.DependencyQuorum) {
			return fmt.Errorf("%w: próg quorum jest większy niż liczba zdefiniowanych zależności", ErrInvalid)
		}
	}
	return nil
}

func (s *Service) changed(ctx context.Context, exec database.Executor, a Actor, eventType, aggregateType string, id, version uint64, before, after any, fields []string) error {
	at := time.Now().UTC()
	payload := map[string]any{"actor_user_id": strconv.FormatUint(a.UserID, 10), "occurred_at": at, "visibility": "INTERNAL", "version": strconv.FormatUint(version, 10), "before": before, "after": after, "changed_fields": fields}
	if err := audit.NewMySQLRepository().Append(ctx, exec, audit.Event{TenantID: &a.TenantID, ActorUserID: &a.UserID, Action: eventType, ObjectType: aggregateType, ObjectID: strconv.FormatUint(id, 10), OldValues: before, NewValues: payload, RequestID: a.RequestID, IPAddress: a.IPAddress, Source: "service_portfolio"}); err != nil {
		return err
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, _, err = s.outbox.Append(ctx, exec, events.Input{TenantID: a.TenantID, Type: eventType, AggregateType: aggregateType, AggregateID: strconv.FormatUint(id, 10), Payload: raw, IdempotencyKey: fmt.Sprintf("%s:%d:%d", aggregateType, id, version), Version: 1})
	return err
}

func (s *Service) History(ctx context.Context, a Actor, id, before uint64, limit int) (HistoryPage, error) {
	if !CanRead(a) {
		return HistoryPage{}, workrecords.ErrForbidden
	}
	if limit < 1 || limit > 100 {
		return HistoryPage{}, ErrInvalid
	}
	if _, err := s.Get(ctx, a, id); err != nil {
		return HistoryPage{}, err
	}
	p, err := activity.NewRepository(s.DB).ListBySourceEvent(ctx, a.TenantID, "service_portfolio_item", strconv.FormatUint(id, 10), before, limit)
	if err != nil {
		return HistoryPage{}, err
	}
	out := HistoryPage{Page: p}
	err = s.DB.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM outbox_events o WHERE o.tenant_id=? AND o.aggregate_type='service_portfolio_item' AND o.aggregate_id=? AND (o.status<>'DELIVERED' OR EXISTS(SELECT 1 FROM event_deliveries d WHERE d.event_id=o.id AND d.consumer_id='activity.v1' AND d.status<>'DELIVERED')))", a.TenantID, strconv.FormatUint(id, 10)).Scan(&out.Pending)
	return out, err
}
