package cmdb

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/chmajster/OpenServiceNOW/internal/assets"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
	"strings"
)

const relationColumns = `r.id,r.version,r.source_id,r.target_id,r.relation_type,s.name,t.name,s.class,t.class`
const relationJoins = ` FROM cmdb_relations r JOIN cmdb_cis s ON s.tenant_id=r.tenant_id AND s.id=r.source_id JOIN cmdb_cis t ON t.tenant_id=r.tenant_id AND t.id=r.target_id `

func relationScan(row interface{ Scan(...any) error }) (Relation, error) {
	var r Relation
	err := row.Scan(&r.ID, &r.Version, &r.SourceID, &r.TargetID, &r.Type, &r.SourceName, &r.TargetName, &r.SourceClass, &r.TargetClass)
	return r, err
}
func relationAccess(a Actor) (string, []any) {
	sp, sa := Access(a, "s")
	tp, ta := Access(a, "t")
	return "(" + sp + ") AND (" + tp + ")", append(sa, ta...)
}
func (s *Service) Relations(ctx context.Context, a Actor, id, before uint64, limit int) (RelationPage, error) {
	page := RelationPage{Items: []Relation{}}
	if limit < 1 || limit > 100 {
		return page, ErrInvalid
	}
	if _, err := s.Get(ctx, a, id); err != nil {
		return page, err
	}
	p, args := relationAccess(a)
	params := []any{a.TenantID, id, id}
	params = append(params, args...)
	q := "SELECT " + relationColumns + relationJoins + " WHERE r.tenant_id=? AND (r.source_id=? OR r.target_id=?) AND " + p
	if before > 0 {
		q += " AND r.id<?"
		params = append(params, before)
	}
	q += " ORDER BY r.id DESC LIMIT ?"
	params = append(params, limit+1)
	rows, err := s.DB.QueryContext(ctx, q, params...)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		r, e := relationScan(rows)
		if e != nil {
			return page, e
		}
		page.Items = append(page.Items, r)
	}
	if len(page.Items) > limit {
		page.Items = page.Items[:limit]
		page.Next = page.Items[limit-1].ID
	}
	return page, rows.Err()
}
func (s *Service) AddRelation(ctx context.Context, a Actor, id uint64, in RelationInput, keys ...string) (Record, error) {
	var out Record
	if id == in.TargetID || in.Version == 0 || in.TargetVersion == 0 {
		return out, ErrInvalid
	}
	err := s.ActorTx(ctx, a, func(e database.Executor, a Actor) error {
		if !Writable(a) {
			return workrecords.ErrForbidden
		}
		key := ""
		if len(keys) > 0 {
			key = keys[0]
		}
		if key != "" {
			raw, _ := json.Marshal(struct {
				ID    uint64
				Input RelationInput
			}{id, in})
			old, err := workrecords.ReserveOperation(ctx, e, a.Actor, key, "ci_relation_add", sha256.Sum256(raw))
			if err != nil {
				return err
			}
			if old > 0 {
				out, err = s.GetInTx(ctx, e, a, old, false)
				return err
			}
		}
		source, err := s.GetInTx(ctx, e, a, id, true)
		if err != nil {
			return err
		}
		target, err := s.GetInTx(ctx, e, a, in.TargetID, true)
		if err != nil {
			return err
		}
		if source.Version != in.Version || target.Version != in.TargetVersion {
			return ErrConflict
		}
		if source.Archived || target.Archived {
			return fmt.Errorf("%w: archiwalne CI nie przyjmują nowych relacji", ErrConflict)
		}
		if !RelationType(in.Type, source.Class, target.Class) {
			return fmt.Errorf("%w: niedozwolony typ zależności dla tych klas", ErrInvalid)
		}
		var exists bool
		if err = e.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM cmdb_relations WHERE tenant_id=? AND source_id=? AND target_id=? AND relation_type=?)", a.TenantID, id, in.TargetID, in.Type).Scan(&exists); err != nil {
			return err
		}
		if exists {
			out = source
			if key != "" {
				return workrecords.CompleteOperation(ctx, e, a.Actor, key, id)
			}
			return nil
		}
		if in.Type == "PART_OF" {
			if err := s.checkContainment(ctx, e, a, id, in.TargetID); err != nil {
				return err
			}
		}
		for _, node := range []uint64{id, in.TargetID} {
			var count int
			if err = e.QueryRowContext(ctx, "SELECT COUNT(*) FROM cmdb_relations WHERE tenant_id=? AND (source_id=? OR target_id=?)", a.TenantID, node, node).Scan(&count); err != nil {
				return err
			}
			if count >= 100 {
				return fmt.Errorf("%w: CI może mieć najwyżej 100 zapisanych zależności", ErrInvalid)
			}
		}
		if _, err = e.ExecContext(ctx, "INSERT INTO cmdb_relations(tenant_id,source_id,target_id,relation_type) VALUES(?,?,?,?)", a.TenantID, id, in.TargetID, in.Type); err != nil {
			return assets.Missing(err)
		}
		if _, err = s.touch(ctx, e, a, target, "relations"); err != nil {
			return err
		}
		out, err = s.touch(ctx, e, a, source, "relations")
		if err == nil && key != "" {
			err = workrecords.CompleteOperation(ctx, e, a.Actor, key, id)
		}
		return err
	})
	return out, err
}
func (s *Service) RemoveRelation(ctx context.Context, a Actor, id, relation, version, targetVersion uint64) (Record, error) {
	var out Record
	if version == 0 || targetVersion == 0 {
		return out, ErrInvalid
	}
	err := s.ActorTx(ctx, a, func(e database.Executor, a Actor) error {
		if !Writable(a) {
			return workrecords.ErrForbidden
		}
		source, err := s.GetInTx(ctx, e, a, id, true)
		if err != nil {
			return err
		}
		var from, to uint64
		if err = e.QueryRowContext(ctx, "SELECT source_id,target_id FROM cmdb_relations WHERE tenant_id=? AND id=? AND (source_id=? OR target_id=?)", a.TenantID, relation, id, id).Scan(&from, &to); err != nil {
			return assets.Missing(err)
		}
		other := to
		if other == id {
			other = from
		}
		target, err := s.GetInTx(ctx, e, a, other, true)
		if err != nil {
			return err
		}
		if source.Version != version || target.Version != targetVersion {
			return ErrConflict
		}
		if source.Archived || target.Archived {
			return ErrConflict
		}
		if _, err = e.ExecContext(ctx, "DELETE FROM cmdb_relations WHERE tenant_id=? AND id=?", a.TenantID, relation); err != nil {
			return err
		}
		if _, err = s.touch(ctx, e, a, target, "relations"); err != nil {
			return err
		}
		out, err = s.touch(ctx, e, a, source, "relations")
		return err
	})
	return out, err
}

// Graph reads saved, currently authorized edges in bounded breadth-first batches.
// Visited node/edge sets retain cycles without recursing or inventing discovery.
func (s *Service) Graph(ctx context.Context, a Actor, id uint64, f GraphFilter) (Graph, error) {
	out := Graph{Nodes: []GraphNode{}, Edges: []Relation{}, MaxNodes: f.MaxNodes, MaxDepth: f.Depth, MaxEdges: 200}
	if f.MaxNodes < 1 || f.MaxNodes > 100 || f.Depth < 0 || f.Depth > 5 || f.Environment != "" && !Environment(f.Environment) || f.Type != "" && f.Type != "RUNS_ON" && f.Type != "HOSTED_ON" && f.Type != "DEPENDS_ON" && f.Type != "CONNECTS_TO" && f.Type != "PART_OF" {
		return out, ErrInvalid
	}
	if f.Class != "" {
		if _, ok := ClassDefinition(f.Class); !ok {
			return out, ErrInvalid
		}
	}
	root, err := s.Get(ctx, a, id)
	if err != nil {
		return out, err
	}
	out.Nodes = append(out.Nodes, GraphNode{root.ID, root.Name, root.Class, root.Status, root.Archived, 0})
	visited := map[uint64]bool{id: true}
	seen := map[uint64]bool{}
	frontier := []uint64{id}
	for depth := 0; depth <= f.Depth && len(frontier) > 0; depth++ {
		marks := strings.TrimSuffix(strings.Repeat("?,", len(frontier)), ",")
		p, args := relationAccess(a)
		params := []any{a.TenantID}
		for _, v := range frontier {
			params = append(params, v)
		}
		for _, v := range frontier {
			params = append(params, v)
		}
		params = append(params, args...)
		q := "SELECT " + relationColumns + ",s.status,t.status,s.archived,t.archived" + relationJoins + " WHERE r.tenant_id=? AND (r.source_id IN (" + marks + ") OR r.target_id IN (" + marks + ")) AND " + p
		if f.Type != "" {
			q += " AND r.relation_type=?"
			params = append(params, f.Type)
		}
		for _, filter := range []struct{ column, value string }{{"class", f.Class}, {"environment", f.Environment}} {
			if filter.value != "" {
				q += " AND (s.id=? OR s." + filter.column + "=?) AND (t.id=? OR t." + filter.column + "=?)"
				params = append(params, id, filter.value, id, filter.value)
			}
		}
		q += " ORDER BY r.id LIMIT 201"
		rows, err := s.DB.QueryContext(ctx, q, params...)
		if err != nil {
			return out, err
		}
		next := []uint64{}
		count := 0
		for rows.Next() {
			var r Relation
			var ss, ts string
			var sa, ta bool
			if err = rows.Scan(&r.ID, &r.Version, &r.SourceID, &r.TargetID, &r.Type, &r.SourceName, &r.TargetName, &r.SourceClass, &r.TargetClass, &ss, &ts, &sa, &ta); err != nil {
				rows.Close()
				return out, err
			}
			count++
			if count > 200 {
				out.Truncated = true
				continue
			}
			if seen[r.ID] {
				continue
			}
			if len(out.Edges) >= 200 {
				out.Truncated = true
				continue
			}
			nodes := []GraphNode{{r.SourceID, r.SourceName, r.SourceClass, ss, sa, depth + 1}, {r.TargetID, r.TargetName, r.TargetClass, ts, ta, depth + 1}}
			needed := 0
			for _, n := range nodes {
				if !visited[n.ID] {
					needed++
				}
			}
			if needed > 0 && (depth == f.Depth || len(out.Nodes)+needed > f.MaxNodes) {
				out.Truncated = true
				continue
			}
			for _, n := range nodes {
				if !visited[n.ID] {
					visited[n.ID] = true
					out.Nodes = append(out.Nodes, n)
					next = append(next, n.ID)
				}
			}
			seen[r.ID] = true
			out.Edges = append(out.Edges, r)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return out, err
		}
		frontier = next
	}
	return out, nil
}

// PART_OF is containment; dependency cycles remain allowed. Tenant write lock
// covers the complete bounded traversal and insertion, including parallel edges.
func (s *Service) checkContainment(ctx context.Context, e database.Executor, a Actor, source, target uint64) error {
	visited := map[uint64]bool{target: true}
	frontier := []uint64{target}
	for len(frontier) > 0 {
		marks := strings.TrimSuffix(strings.Repeat("?,", len(frontier)), ",")
		args := []any{a.TenantID}
		for _, id := range frontier {
			args = append(args, id)
		}
		rows, err := e.QueryContext(ctx, "SELECT DISTINCT target_id FROM cmdb_relations WHERE tenant_id=? AND relation_type='PART_OF' AND source_id IN ("+marks+") LIMIT 1001", args...)
		if err != nil {
			return err
		}
		next := []uint64{}
		for rows.Next() {
			var id uint64
			if err = rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			if id == source {
				rows.Close()
				return fmt.Errorf("%w: relacja jest częścią nie może tworzyć cyklu", ErrInvalid)
			}
			if !visited[id] {
				visited[id] = true
				next = append(next, id)
			}
			if len(visited) > 1000 {
				rows.Close()
				return fmt.Errorf("%w: hierarchia przekracza limit 1000 elementów", ErrInvalid)
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		frontier = next
	}
	return nil
}
