package serviceportfolio

import (
	"context"
	"database/sql"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
)

const impactMaxNodes = 100
const impactMaxDepth = 6
const impactMaxEdges = 200

type impactVisit struct {
	node     ImpactNode
	parent   string
	pathRule string
}

func (s *Service) ImpactFromCI(ctx context.Context, a Actor, id uint64) (ImpactResult, error) {
	if !CanRead(a) || (!a.Has("cmdb.read") && !a.Has("cmdb.read_own")) {
		return ImpactResult{}, workrecords.ErrForbidden
	}
	access, args := ciAccess(a, "ci")
	params := []any{a.TenantID, id}
	params = append(params, args...)
	var root ImpactNode
	root.ID = impactID("CI", id)
	root.Kind = "CI"
	err := s.DB.QueryRowContext(ctx, "SELECT ci.name,ci.class,ci.status,ci.environment FROM cmdb_cis ci WHERE ci.tenant_id=? AND ci.id=? AND "+access, params...).Scan(&root.Name, &root.Class, &root.Lifecycle, &root.Environment)
	if err == sql.ErrNoRows {
		return ImpactResult{}, workrecords.ErrForbidden
	}
	if err != nil {
		return ImpactResult{}, err
	}
	root.Availability, root.AvailabilityRule, err = s.observationStateWithRule(ctx, s.DB, a.TenantID, "CI", id)
	if err != nil {
		return ImpactResult{}, err
	}
	return s.impact(ctx, a, root)
}

func (s *Service) ImpactFromItem(ctx context.Context, a Actor, id uint64) (ImpactResult, error) {
	item, err := s.Get(ctx, a, id)
	if err != nil {
		return ImpactResult{}, err
	}
	availability, availabilityRule, err := s.itemAvailability(ctx, s.DB, a, id, item.DependencyMode, item.DependencyQuorum)
	if err != nil {
		return ImpactResult{}, err
	}
	root := ImpactNode{ID: impactID("SERVICE", id), Kind: "SERVICE", Name: item.Name, Lifecycle: item.Lifecycle, Environment: item.Environment, Availability: availability, AvailabilityRule: availabilityRule}
	return s.impact(ctx, a, root)
}

func impactID(kind string, id uint64) string {
	return strings.ToLower(kind) + ":" + strconv.FormatUint(id, 10)
}

func parseImpactID(value string) (string, uint64, error) {
	parts := strings.Split(value, ":")
	if len(parts) != 2 {
		return "", 0, ErrInvalid
	}
	kind := strings.ToUpper(parts[0])
	id, err := strconv.ParseUint(parts[1], 10, 64)
	if err != nil || id == 0 || kind != "CI" && kind != "SERVICE" && kind != "OFFER" {
		return "", 0, ErrInvalid
	}
	return kind, id, nil
}

func (s *Service) impact(ctx context.Context, a Actor, root ImpactNode) (ImpactResult, error) {
	out := ImpactResult{Root: root, Nodes: []ImpactNode{}, Edges: []ImpactEdge{}, ImpactedServices: []ImpactTarget{}, ImpactedOffers: []ImpactTarget{}, RulesUsed: []string{}, MaxNodes: impactMaxNodes, MaxDepth: impactMaxDepth, RuleVersion: impactRuleVersion, CalculatedAt: time.Now().UTC()}
	seen := map[string]bool{root.ID: true}
	visits := map[string]impactVisit{root.ID: {node: root}}
	queue := []string{root.ID}
	rules := map[string]bool{}
	if root.AvailabilityRule != "" {
		rules[root.AvailabilityRule] = true
	}
	for len(queue) > 0 {
		currentID := queue[0]
		queue = queue[1:]
		current := visits[currentID]
		if current.node.Depth >= impactMaxDepth {
			more, err := s.hasVisibleNeighbors(ctx, a, current.node)
			if err != nil {
				return out, err
			}
			if more {
				out.Truncated = true
			}
			continue
		}
		neighbors, used, err := s.neighbors(ctx, a, current.node)
		if err != nil {
			return out, err
		}
		for _, rule := range used {
			rules[rule] = true
		}
		for _, neighbor := range neighbors {
			if neighbor.node.AvailabilityRule != "" {
				rules[neighbor.node.AvailabilityRule] = true
			}
			if seen[neighbor.node.ID] {
				continue
			}
			if len(visits) >= impactMaxNodes || len(out.Edges) >= impactMaxEdges {
				out.Truncated = true
				continue
			}
			neighbor.node.Depth = current.node.Depth + 1
			neighbor.parent = currentID
			seen[neighbor.node.ID] = true
			visits[neighbor.node.ID] = neighbor
			queue = append(queue, neighbor.node.ID)
			out.Edges = append(out.Edges, ImpactEdge{From: currentID, To: neighbor.node.ID, Type: neighbor.pathRule})
		}
	}
	for _, visit := range visits {
		out.Nodes = append(out.Nodes, visit.node)
	}
	sort.Slice(out.Nodes, func(i, j int) bool {
		if out.Nodes[i].Depth == out.Nodes[j].Depth {
			return out.Nodes[i].ID < out.Nodes[j].ID
		}
		return out.Nodes[i].Depth < out.Nodes[j].Depth
	})
	for id, visit := range visits {
		if visit.node.Kind == "SERVICE" && id != root.ID && boolLifecycle(visit.node.Lifecycle) {
			out.ImpactedServices = append(out.ImpactedServices, ImpactTarget{ID: id, Name: visit.node.Name, Type: visit.node.Class, Path: impactPath(visits, id), Rule: "dependency_path_v1"})
		}
		if visit.node.Kind == "OFFER" {
			out.ImpactedOffers = append(out.ImpactedOffers, ImpactTarget{ID: id, Name: visit.node.Name, Type: "SERVICE_OFFER", Path: impactPath(visits, id), Rule: "active_offer_of_impacted_service"})
		}
	}
	sort.Slice(out.ImpactedServices, func(i, j int) bool { return out.ImpactedServices[i].ID < out.ImpactedServices[j].ID })
	sort.Slice(out.ImpactedOffers, func(i, j int) bool { return out.ImpactedOffers[i].ID < out.ImpactedOffers[j].ID })
	for rule := range rules {
		out.RulesUsed = append(out.RulesUsed, rule)
	}
	sort.Strings(out.RulesUsed)
	return out, nil
}

func impactPath(visits map[string]impactVisit, id string) []string {
	path := []string{}
	for id != "" {
		path = append(path, id)
		visit, ok := visits[id]
		if !ok {
			break
		}
		id = visit.parent
	}
	for left, right := 0, len(path)-1; left < right; left, right = left+1, right-1 {
		path[left], path[right] = path[right], path[left]
	}
	return path
}

func (s *Service) neighbors(ctx context.Context, a Actor, node ImpactNode) ([]impactVisit, []string, error) {
	neighbors := []impactVisit{}
	rules := []string{}
	var rows *sql.Rows
	switch node.Kind {
	case "CI":
		id, err := parseNodeID(node.ID, "ci")
		if err != nil {
			return nil, nil, err
		}
		ciAccessSQL, ciArgs := ciAccess(a, "t")
		sourceAccess, sourceArgs := ciAccess(a, "src")
		rows, err = s.DB.QueryContext(ctx, `SELECT r.source_id,r.target_id,r.relation_type,
 IF(r.source_id=? ,t.name,src.name),IF(r.source_id=? ,t.class,src.class),IF(r.source_id=? ,t.status,src.status),IF(r.source_id=? ,t.environment,src.environment)
 FROM cmdb_relations r JOIN cmdb_cis src ON src.tenant_id=r.tenant_id AND src.id=r.source_id JOIN cmdb_cis t ON t.tenant_id=r.tenant_id AND t.id=r.target_id
 WHERE r.tenant_id=? AND (r.source_id=? OR r.target_id=?) AND `+sourceAccess+" AND "+ciAccessSQL+" ORDER BY r.id LIMIT 201", append([]any{id, id, id, id, a.TenantID, id, id}, append(sourceArgs, ciArgs...)...)...)
		if err != nil {
			return nil, nil, err
		}
		for rows.Next() {
			var source, target uint64
			var typ, name, class, status, env string
			if err := rows.Scan(&source, &target, &typ, &name, &class, &status, &env); err != nil {
				rows.Close()
				return nil, nil, err
			}
			next := target
			rule := typ
			if source == id {
				next = target
			} else {
				next = source
				rule = typ + "_REVERSE"
			}
			neighbors = append(neighbors, impactVisit{node: ImpactNode{ID: impactID("CI", next), Kind: "CI", Name: name, Class: class, Lifecycle: status, Environment: env}, pathRule: "cmdb." + rule})
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, nil, err
		}
		if len(neighbors) > 0 {
			rules = append(rules, "cmdb_typed_relations_v1")
		}
		// A CI can be the backing record for one portfolio concept.
		portfolioSQL, portfolioArgs := portfolioAccess(a, "p")
		var serviceID uint64
		var serviceName, typ, lifecycle, env, mode string
		var quorum uint16
		p := append([]any{a.TenantID, id}, portfolioArgs...)
		err = s.DB.QueryRowContext(ctx, "SELECT p.id,p.name,p.item_type,p.lifecycle,p.environment,p.dependency_mode,p.dependency_quorum FROM service_portfolio_items p WHERE p.tenant_id=? AND p.ci_id=? AND "+portfolioSQL, p...).Scan(&serviceID, &serviceName, &typ, &lifecycle, &env, &mode, &quorum)
		if err == nil {
			state, availabilityRule, calcErr := s.itemAvailability(ctx, s.DB, a, serviceID, mode, quorum)
			if calcErr != nil {
				return nil, nil, calcErr
			}
			neighbors = append(neighbors, impactVisit{node: ImpactNode{ID: impactID("SERVICE", serviceID), Kind: "SERVICE", Name: serviceName, Class: typ, Lifecycle: lifecycle, Environment: env, Availability: state, AvailabilityRule: availabilityRule}, pathRule: "portfolio.ci_mapping"})
			rules = append(rules, "portfolio_ci_mapping_v1")
		} else if err != sql.ErrNoRows {
			return nil, nil, err
		}
		// Portfolio dependencies may point directly to a CI.
		sourcePortfolio, spArgs := portfolioAccess(a, "p")
		rows, err = s.DB.QueryContext(ctx, `SELECT p.id,p.name,p.item_type,p.lifecycle,p.environment,p.dependency_mode,p.dependency_quorum,r.relation_type
 FROM service_portfolio_relations r JOIN service_portfolio_items p ON p.tenant_id=r.tenant_id AND p.id=r.source_item_id
 WHERE r.tenant_id=? AND r.target_ci_id=? AND `+sourcePortfolio+" ORDER BY r.id LIMIT 101", append([]any{a.TenantID, id}, spArgs...)...)
		if err != nil {
			return nil, nil, err
		}
		for rows.Next() {
			var sid uint64
			var name, st, life, environment, depMode, rel string
			var q uint16
			if err := rows.Scan(&sid, &name, &st, &life, &environment, &depMode, &q, &rel); err != nil {
				rows.Close()
				return nil, nil, err
			}
			availability, availabilityRule, err := s.itemAvailability(ctx, s.DB, a, sid, depMode, q)
			if err != nil {
				rows.Close()
				return nil, nil, err
			}
			neighbors = append(neighbors, impactVisit{node: ImpactNode{ID: impactID("SERVICE", sid), Kind: "SERVICE", Name: name, Class: st, Lifecycle: life, Environment: environment, Availability: availability, AvailabilityRule: availabilityRule}, pathRule: "portfolio." + rel + "_REVERSE"})
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, nil, err
		}
	case "SERVICE":
		id, err := parseNodeID(node.ID, "service")
		if err != nil {
			return nil, nil, err
		}
		access, args := portfolioAccess(a, "p")
		var ciID uint64
		var ciName, ciClass, ciStatus, ciEnv string
		params := []any{a.TenantID, id}
		params = append(params, args...)
		err = s.DB.QueryRowContext(ctx, `SELECT ci.id,ci.name,ci.class,ci.status,ci.environment FROM service_portfolio_items p JOIN cmdb_cis ci ON ci.tenant_id=p.tenant_id AND ci.id=p.ci_id WHERE p.tenant_id=? AND p.id=? AND `+access+" AND "+func() string { q, _ := ciAccess(a, "ci"); return q }(), append(params, func() []any { _, q := ciAccess(a, "ci"); return q }()...)...).Scan(&ciID, &ciName, &ciClass, &ciStatus, &ciEnv)
		if err == nil {
			neighbors = append(neighbors, impactVisit{node: ImpactNode{ID: impactID("CI", ciID), Kind: "CI", Name: ciName, Class: ciClass, Lifecycle: ciStatus, Environment: ciEnv}, pathRule: "portfolio.ci_mapping"})
			rules = append(rules, "portfolio_ci_mapping_v1")
		} else if err != sql.ErrNoRows {
			return nil, nil, err
		}
		// Edges are traversable both ways for impact, while the response preserves which end owns the relation.
		sp, spArgs := portfolioAccess(a, "src")
		tp, tpArgs := portfolioAccess(a, "tgt")
		rows, err = s.DB.QueryContext(ctx, `SELECT r.source_item_id,COALESCE(r.target_item_id,0),COALESCE(r.target_ci_id,0),r.relation_type,
  src.name,src.item_type,src.lifecycle,src.environment,src.dependency_mode,src.dependency_quorum,
  COALESCE(tgt.name,ci.name,''),COALESCE(tgt.item_type,''),COALESCE(tgt.lifecycle,ci.status,''),COALESCE(tgt.environment,ci.environment,''),COALESCE(tgt.dependency_mode,'NOT_MODELED'),COALESCE(tgt.dependency_quorum,0)
 FROM service_portfolio_relations r JOIN service_portfolio_items src ON src.tenant_id=r.tenant_id AND src.id=r.source_item_id
 LEFT JOIN service_portfolio_items tgt ON tgt.tenant_id=r.tenant_id AND tgt.id=r.target_item_id
 LEFT JOIN cmdb_cis ci ON ci.tenant_id=r.tenant_id AND ci.id=r.target_ci_id
 WHERE r.tenant_id=? AND (r.source_item_id=? OR r.target_item_id=?) AND `+sp+" AND (r.target_item_id IS NULL OR "+tp+") ORDER BY r.id LIMIT 201", append([]any{a.TenantID, id, id}, append(spArgs, tpArgs...)...)...)
		if err != nil {
			return nil, nil, err
		}
		for rows.Next() {
			var source, targetItem, targetCI uint64
			var rel, sName, sType, sLife, sEnv, sMode, tName, tType, tLife, tEnv, tMode string
			var sQ, tQ uint16
			if err := rows.Scan(&source, &targetItem, &targetCI, &rel, &sName, &sType, &sLife, &sEnv, &sMode, &sQ, &tName, &tType, &tLife, &tEnv, &tMode, &tQ); err != nil {
				rows.Close()
				return nil, nil, err
			}
			if targetItem > 0 {
				next := targetItem
				name, typ, life, environment, mode, q, rule := tName, tType, tLife, tEnv, tMode, tQ, rel
				if source != id {
					next = source
					name, typ, life, environment, mode, q, rule = sName, sType, sLife, sEnv, sMode, sQ, rel+"_REVERSE"
				}
				availability, availabilityRule, err := s.itemAvailability(ctx, s.DB, a, next, mode, q)
				if err != nil {
					rows.Close()
					return nil, nil, err
				}
				neighbors = append(neighbors, impactVisit{node: ImpactNode{ID: impactID("SERVICE", next), Kind: "SERVICE", Name: name, Class: typ, Lifecycle: life, Environment: environment, Availability: availability, AvailabilityRule: availabilityRule}, pathRule: "portfolio." + rule})
			} else if targetCI > 0 && source == id {
				ciAccessSQL, ciArgs := ciAccess(a, "c")
				var name, class, status, environment string
				params := []any{a.TenantID, targetCI}
				params = append(params, ciArgs...)
				err := s.DB.QueryRowContext(ctx, "SELECT c.name,c.class,c.status,c.environment FROM cmdb_cis c WHERE c.tenant_id=? AND c.id=? AND "+ciAccessSQL, params...).Scan(&name, &class, &status, &environment)
				if err == nil {
					availability, availabilityRule, err := s.observationStateWithRule(ctx, s.DB, a.TenantID, "CI", targetCI)
					if err != nil {
						rows.Close()
						return nil, nil, err
					}
					neighbors = append(neighbors, impactVisit{node: ImpactNode{ID: impactID("CI", targetCI), Kind: "CI", Name: name, Class: class, Lifecycle: status, Environment: environment, Availability: availability, AvailabilityRule: availabilityRule}, pathRule: "portfolio." + rel})
				} else if err != sql.ErrNoRows {
					rows.Close()
					return nil, nil, err
				}
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, nil, err
		}
		if len(neighbors) > 0 {
			rules = append(rules, "portfolio_typed_relations_v1")
		}
		// Only active offers for active business services appear in impact output.
		if node.Lifecycle == "ACTIVE" {
			rows, err = s.DB.QueryContext(ctx, "SELECT id,name FROM service_portfolio_offers WHERE tenant_id=? AND parent_service_id=? AND lifecycle='ACTIVE' ORDER BY id LIMIT 101", a.TenantID, id)
			if err != nil {
				return nil, nil, err
			}
			for rows.Next() {
				var oid uint64
				var name string
				if err := rows.Scan(&oid, &name); err != nil {
					rows.Close()
					return nil, nil, err
				}
				neighbors = append(neighbors, impactVisit{node: ImpactNode{ID: impactID("OFFER", oid), Kind: "OFFER", Name: name, Lifecycle: "ACTIVE"}, pathRule: "portfolio.active_offer"})
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return nil, nil, err
			}
		}
	case "OFFER":
		return neighbors, rules, nil
	}
	return neighbors, rules, nil
}

func parseNodeID(value, prefix string) (uint64, error) {
	parts := strings.Split(value, ":")
	if len(parts) != 2 || parts[0] != prefix {
		return 0, ErrInvalid
	}
	id, err := strconv.ParseUint(parts[1], 10, 64)
	if err != nil || id == 0 {
		return 0, ErrInvalid
	}
	return id, nil
}

func (s *Service) hasVisibleNeighbors(ctx context.Context, a Actor, node ImpactNode) (bool, error) {
	neighbors, _, err := s.neighbors(ctx, a, node)
	if err != nil {
		return false, err
	}
	return len(neighbors) > 0, nil
}

func (s *Service) observationState(ctx context.Context, exec database.Executor, tenant uint64, kind string, id uint64) (string, error) {
	state, _, err := s.observationStateWithRule(ctx, exec, tenant, kind, id)
	return state, err
}

func (s *Service) observationStateWithRule(ctx context.Context, exec database.Executor, tenant uint64, kind string, id uint64) (string, string, error) {
	var state string
	err := exec.QueryRowContext(ctx, "SELECT state FROM service_portfolio_component_availability WHERE tenant_id=? AND node_kind=? AND node_id=?", tenant, kind, id).Scan(&state)
	if err == sql.ErrNoRows {
		return "UNKNOWN", "no_observation", nil
	}
	if err != nil {
		return "", "", err
	}
	return state, "manual_observation", nil
}

func (s *Service) itemAvailability(ctx context.Context, exec database.Executor, a Actor, id uint64, mode string, quorum uint16) (string, string, error) {
	manual, manualRule, err := s.observationStateWithRule(ctx, exec, a.TenantID, "ITEM", id)
	if err != nil {
		return "", "", err
	}
	if manual != "UNKNOWN" {
		return manual, manualRule, nil
	}
	if mode == "NOT_MODELED" {
		return "NOT_MODELED", "no_redundancy_model", nil
	}
	rows, err := exec.QueryContext(ctx, "SELECT COALESCE(target_item_id,0),COALESCE(target_ci_id,0) FROM service_portfolio_relations WHERE tenant_id=? AND source_item_id=? AND relation_type='DEPENDS_ON' ORDER BY id LIMIT 101", a.TenantID, id)
	if err != nil {
		return "", "", err
	}
	states := []string{}
	for rows.Next() {
		var itemID, ciID uint64
		if err := rows.Scan(&itemID, &ciID); err != nil {
			rows.Close()
			return "", "", err
		}
		kind, target := "ITEM", itemID
		if ciID > 0 {
			kind, target = "CI", ciID
			if !s.visibleCI(ctx, exec, a, target) {
				states = append(states, "UNKNOWN")
				continue
			}
		} else {
			visible, accessArgs := portfolioAccess(a, "target_item")
			params := []any{a.TenantID, target}
			params = append(params, accessArgs...)
			var found bool
			if err := exec.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM service_portfolio_items target_item WHERE target_item.tenant_id=? AND target_item.id=? AND "+visible+")", params...).Scan(&found); err != nil {
				rows.Close()
				return "", "", err
			}
			if !found {
				states = append(states, "UNKNOWN")
				continue
			}
		}
		state, err := s.observationState(ctx, exec, a.TenantID, kind, target)
		if err != nil {
			rows.Close()
			return "", "", err
		}
		states = append(states, state)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return "", "", err
	}
	value, rule := dependencyAvailability(mode, quorum, states)
	return value, rule, nil
}

func (s *Service) visibleCI(ctx context.Context, exec database.Executor, a Actor, id uint64) bool {
	access, args := ciAccess(a, "ci")
	params := []any{a.TenantID, id}
	params = append(params, args...)
	var found int
	return exec.QueryRowContext(ctx, "SELECT 1 FROM cmdb_cis ci WHERE ci.tenant_id=? AND ci.id=? AND "+access, params...).Scan(&found) == nil
}
