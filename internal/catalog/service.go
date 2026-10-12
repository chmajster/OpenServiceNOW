package catalog

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/chmajster/OpenServiceNOW/internal/audit"
	"github.com/chmajster/OpenServiceNOW/internal/events"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
	"strings"
	"time"
)

type Service struct {
	DB     *sql.DB
	tx     *database.TxManager
	outbox *events.Repository
}

func NewService(db *sql.DB) *Service {
	return &Service{db, database.NewTxManager(db), events.NewRepository(db)}
}
func Missing(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}
func (s *Service) AdminTx(ctx context.Context, a Actor, fn func(database.Executor, Actor) error) error {
	return s.tx.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(e database.Executor) error {
		current, err := workrecords.LockPrincipal(ctx, e, a)
		if err != nil {
			return err
		}
		if !current.Has("admin.catalog.manage") {
			return workrecords.ErrForbidden
		}
		return fn(e, current)
	})
}
func Audit(ctx context.Context, e database.Executor, a Actor, kind, object string, id uint64, data any) error {
	return audit.NewMySQLRepository().Append(ctx, e, audit.Event{TenantID: &a.TenantID, ActorUserID: &a.UserID, Action: kind, ObjectType: object, ObjectID: fmt.Sprint(id), NewValues: data, RequestID: a.RequestID, IPAddress: a.IPAddress, Source: "api"})
}
func (s *Service) changed(ctx context.Context, e database.Executor, a Actor, kind string, id uint64, data any) error {
	if err := Audit(ctx, e, a, kind, "catalog_item", id, data); err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]any{"actor_user_id": fmt.Sprint(a.UserID), "occurred_at": time.Now().UTC(), "visibility": "INTERNAL", "version": data})
	_, _, err := s.outbox.Append(ctx, e, events.Input{TenantID: a.TenantID, Type: "catalog.changed", AggregateType: "catalog_item", AggregateID: fmt.Sprint(id), Payload: payload, IdempotencyKey: fmt.Sprintf("catalog:%d:%s:%v", id, kind, data)})
	return err
}
func nodeTable(kind string) (string, error) {
	switch kind {
	case "catalogs":
		return "service_catalogs", nil
	case "categories":
		return "catalog_categories", nil
	}
	return "", ErrInvalid
}
func (s *Service) SaveNode(ctx context.Context, a Actor, kind string, id uint64, n Node, keys ...string) (Node, error) {
	table, err := nodeTable(kind)
	if err != nil {
		return n, err
	}
	n.Name = strings.TrimSpace(n.Name)
	if !Text(n.Name, 1, 160) || !Text(n.Description, 0, 10000) {
		return n, ErrInvalid
	}
	err = s.AdminTx(ctx, a, func(e database.Executor, a Actor) error {
		key := ""
		if len(keys) > 0 {
			key = keys[0]
		}
		if id == 0 && key != "" {
			raw, _ := json.Marshal(n)
			existing, err := workrecords.ReserveOperation(ctx, e, a.Actor, key, "catalog_"+kind, sha256.Sum256(raw))
			if err != nil {
				return err
			}
			if existing > 0 {
				n, err = GetNode(ctx, e, a.TenantID, kind, existing)
				return err
			}
		}
		if kind == "categories" {
			var exists bool
			if err := e.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM service_catalogs WHERE tenant_id=? AND id=?)", a.TenantID, n.CatalogID).Scan(&exists); err != nil {
				return err
			}
			if !exists {
				return ErrInvalid
			}
		}
		if id == 0 {
			q := "INSERT INTO " + table + "(tenant_id,name,description"
			args := []any{a.TenantID, n.Name, n.Description}
			if kind == "categories" {
				q += ",catalog_id"
				args = append(args, n.CatalogID)
			}
			q += ") VALUES(?,?,?"
			if kind == "categories" {
				q += ",?"
			}
			result, err := e.ExecContext(ctx, q+")", args...)
			if err != nil {
				return err
			}
			v, err := result.LastInsertId()
			n.ID = uint64(v)
			n.Version = 1
			if err != nil {
				return err
			}
		} else {
			if n.Version == 0 {
				return ErrInvalid
			}
			before, err := GetNode(ctx, e, a.TenantID, kind, id)
			if err != nil {
				return err
			}
			if kind == "categories" && n.CatalogID != before.CatalogID {
				return invalid("katalog istniejącej kategorii nie może się zmienić")
			}
			result, err := e.ExecContext(ctx, "UPDATE "+table+" SET name=?,description=?,version=version+1 WHERE tenant_id=? AND id=? AND version=?", n.Name, n.Description, a.TenantID, id, n.Version)
			if err != nil {
				return err
			}
			count, err := result.RowsAffected()
			if err != nil {
				return err
			}
			if count != 1 {
				return ErrConflict
			}
			n.ID = id
			n.Version++
		}
		if id == 0 && key != "" {
			if err := workrecords.CompleteOperation(ctx, e, a.Actor, key, n.ID); err != nil {
				return err
			}
		}
		return Audit(ctx, e, a, "catalog.node.saved", kind, n.ID, map[string]any{"name": n.Name, "version": fmt.Sprint(n.Version)})
	})
	return n, err
}
func GetNode(ctx context.Context, e database.Executor, tenant uint64, kind string, id uint64) (Node, error) {
	table, err := nodeTable(kind)
	if err != nil {
		return Node{}, err
	}
	columns := "id,name,description,version"
	var n Node
	dest := []any{&n.ID, &n.Name, &n.Description, &n.Version}
	if kind == "categories" {
		columns += ",catalog_id"
		dest = append(dest, &n.CatalogID)
	}
	err = e.QueryRowContext(ctx, "SELECT "+columns+" FROM "+table+" WHERE tenant_id=? AND id=?", tenant, id).Scan(dest...)
	return n, Missing(err)
}

type NodePage struct {
	Items []Node `json:"items"`
	Next  uint64 `json:"next_after_id,string,omitempty"`
}

func (s *Service) Nodes(ctx context.Context, a Actor, kind string, after uint64, limit int) (NodePage, error) {
	table, err := nodeTable(kind)
	if err != nil {
		return NodePage{}, err
	}
	if !a.Has("admin.catalog.manage") && !a.Has("catalog.read") {
		return NodePage{}, workrecords.ErrForbidden
	}
	if limit < 1 || limit > 100 {
		return NodePage{}, ErrInvalid
	}
	columns := "id,name,description,version"
	if kind == "categories" {
		columns += ",catalog_id"
	}
	q := "SELECT " + columns + " FROM " + table + " n WHERE tenant_id=? AND id>?"
	args := []any{a.TenantID, after}
	if !a.Has("admin.catalog.manage") { // Empty/draft-only branches are not exposed to the portal.
		parent := "c.id=n.id"
		if kind == "catalogs" {
			parent = "c.catalog_id=n.id"
		}
		q += " AND EXISTS(SELECT 1 FROM catalog_categories c JOIN catalog_items i ON i.tenant_id=c.tenant_id AND i.category_id=c.id JOIN catalog_item_versions v ON v.tenant_id=i.tenant_id AND v.item_id=i.id AND v.version=i.published_version WHERE c.tenant_id=n.tenant_id AND " + parent + " AND " + AudienceSQL("v") + ")"
		args = append(args, a.UserID)
	}
	q += " ORDER BY id LIMIT ?"
	args = append(args, limit+1)
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return NodePage{}, err
	}
	defer rows.Close()
	p := NodePage{Items: []Node{}}
	for rows.Next() {
		var n Node
		dest := []any{&n.ID, &n.Name, &n.Description, &n.Version}
		if kind == "categories" {
			dest = append(dest, &n.CatalogID)
		}
		if err := rows.Scan(dest...); err != nil {
			return p, err
		}
		p.Items = append(p.Items, n)
	}
	if len(p.Items) > limit {
		p.Items = p.Items[:limit]
		p.Next = p.Items[limit-1].ID
	}
	return p, rows.Err()
}
func GetItem(ctx context.Context, e database.Executor, tenant, id uint64, lock bool) (Item, error) {
	var i Item
	var data []byte
	var published sql.Null[uint64]
	q := "SELECT id,category_id,version,published_version,draft FROM catalog_items WHERE tenant_id=? AND id=?"
	if lock {
		q += " FOR UPDATE"
	}
	err := e.QueryRowContext(ctx, q, tenant, id).Scan(&i.ID, &i.CategoryID, &i.Version, &published, &data)
	if err != nil {
		return i, Missing(err)
	}
	if published.Valid {
		i.PublishedVersion = &published.V
	}
	err = json.Unmarshal(data, &i.Draft)
	return i, err
}
func (s *Service) SaveItem(ctx context.Context, a Actor, id uint64, i Item, keys ...string) (Item, error) {
	i.Draft.Title = strings.TrimSpace(i.Draft.Title)
	if i.Draft.Fields == nil {
		i.Draft.Fields = []Field{}
	}
	if i.Draft.Steps == nil {
		i.Draft.Steps = []Step{}
	}
	for n := range i.Draft.Fields {
		i.Draft.Fields[n].Label = strings.TrimSpace(i.Draft.Fields[n].Label)
	}
	for n := range i.Draft.Steps {
		i.Draft.Steps[n].Title = strings.TrimSpace(i.Draft.Steps[n].Title)
	}
	if err := ValidateDefinition(i.Draft, false); err != nil {
		return i, err
	}
	err := s.AdminTx(ctx, a, func(e database.Executor, a Actor) error {
		key := ""
		if len(keys) > 0 {
			key = keys[0]
		}
		if id == 0 && key != "" {
			raw, _ := json.Marshal(i)
			existing, err := workrecords.ReserveOperation(ctx, e, a.Actor, key, "catalog_item", sha256.Sum256(raw))
			if err != nil {
				return err
			}
			if existing > 0 {
				i, err = GetItem(ctx, e, a.TenantID, existing, false)
				return err
			}
		}
		var exists bool
		if err := e.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM catalog_categories WHERE tenant_id=? AND id=?)", a.TenantID, i.CategoryID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return ErrInvalid
		}
		raw, _ := json.Marshal(i.Draft)
		if id == 0 {
			result, err := e.ExecContext(ctx, "INSERT INTO catalog_items(tenant_id,category_id,draft) VALUES(?,?,?)", a.TenantID, i.CategoryID, raw)
			if err != nil {
				return err
			}
			n, err := result.LastInsertId()
			if err != nil {
				return err
			}
			i.ID = uint64(n)
			i.Version = 1
		} else {
			before, err := GetItem(ctx, e, a.TenantID, id, true)
			if err != nil {
				return err
			}
			if i.Version == 0 || before.Version != i.Version {
				return ErrConflict
			}
			if i.CategoryID != before.CategoryID {
				return invalid("kategoria istniejącej pozycji nie może się zmienić")
			}
			if _, err := e.ExecContext(ctx, "UPDATE catalog_items SET draft=?,version=version+1 WHERE tenant_id=? AND id=?", raw, a.TenantID, id); err != nil {
				return err
			}
			i.ID = id
			i.Version++
			i.PublishedVersion = before.PublishedVersion
		}
		if id == 0 && key != "" {
			if err := workrecords.CompleteOperation(ctx, e, a.Actor, key, i.ID); err != nil {
				return err
			}
		}
		return s.changed(ctx, e, a, "catalog.draft.saved", i.ID, fmt.Sprint(i.Version))
	})
	return i, err
}
func ActiveGroup(ctx context.Context, e database.Executor, tenant, id uint64) error {
	var ok bool
	err := e.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM `groups` WHERE tenant_id=? AND id=? AND active=TRUE)", tenant, id).Scan(&ok)
	if err != nil {
		return err
	}
	if !ok {
		return invalid("grupa musi być aktywna i należeć do organizacji")
	}
	return nil
}
func (s *Service) Publish(ctx context.Context, a Actor, id, version uint64, withdraw bool) (Item, error) {
	var item Item
	err := s.AdminTx(ctx, a, func(e database.Executor, a Actor) error {
		var err error
		item, err = GetItem(ctx, e, a.TenantID, id, true)
		if err != nil {
			return err
		}
		if version == 0 || item.Version != version {
			return ErrConflict
		}
		var published any
		if !withdraw {
			if err := ValidateDefinition(item.Draft, true); err != nil {
				return err
			}
			if err := ActiveGroup(ctx, e, a.TenantID, item.Draft.GroupID); err != nil {
				return err
			}
			if item.Draft.AudienceGroupID > 0 {
				if err := ActiveGroup(ctx, e, a.TenantID, item.Draft.AudienceGroupID); err != nil {
					return err
				}
			}
			raw, _ := json.Marshal(item.Draft)
			if _, err := e.ExecContext(ctx, "INSERT INTO catalog_item_versions(tenant_id,item_id,version,definition,published_at,published_by) VALUES(?,?,?,?,?,?)", a.TenantID, id, item.Version, raw, time.Now().UTC(), a.UserID); err != nil {
				return err
			}
			published = item.Version
			v := item.Version
			item.PublishedVersion = &v
		} else {
			item.PublishedVersion = nil
		}
		if _, err := e.ExecContext(ctx, "UPDATE catalog_items SET published_version=?,version=version+1 WHERE tenant_id=? AND id=?", published, a.TenantID, id); err != nil {
			return err
		}
		item.Version++
		kind := "catalog.published"
		if withdraw {
			kind = "catalog.withdrawn"
		}
		return s.changed(ctx, e, a, kind, id, fmt.Sprint(item.Version))
	})
	return item, err
}
func AudienceSQL(alias string) string {
	return "(COALESCE(CAST(JSON_UNQUOTE(JSON_EXTRACT(" + alias + ".definition,'$.audience_group_id')) AS UNSIGNED),0)=0 OR EXISTS(SELECT 1 FROM group_memberships gm JOIN `groups` g ON g.tenant_id=gm.tenant_id AND g.id=gm.group_id AND g.active=TRUE WHERE gm.tenant_id=" + alias + ".tenant_id AND gm.user_id=? AND gm.group_id=CAST(JSON_UNQUOTE(JSON_EXTRACT(" + alias + ".definition,'$.audience_group_id')) AS UNSIGNED)))"
}

// Published pins exactly the version which was offered to the requester. It is
// also called inside submission transactions, before creating any child record.
func Published(ctx context.Context, e database.Executor, a Actor, id, version uint64) (Publication, Definition, error) {
	var category, v uint64
	var data []byte
	err := e.QueryRowContext(ctx, "SELECT i.category_id,v.version,v.definition FROM catalog_items i JOIN catalog_item_versions v ON v.tenant_id=i.tenant_id AND v.item_id=i.id AND v.version=i.published_version WHERE i.tenant_id=? AND i.id=? AND "+AudienceSQL("v"), a.TenantID, id, a.UserID).Scan(&category, &v, &data)
	if err != nil {
		return Publication{}, Definition{}, Missing(err)
	}
	var d Definition
	if err := json.Unmarshal(data, &d); err != nil {
		return Publication{}, d, err
	}
	if version > 0 && v != version {
		return Publication{}, d, ErrConflict
	}
	return Public(id, category, v, d), d, nil
}

type ItemPage struct {
	Items []Item `json:"items"`
	Next  uint64 `json:"next_after_id,string,omitempty"`
}
type PublicationPage struct {
	Items []Publication `json:"items"`
	Next  uint64        `json:"next_after_id,string,omitempty"`
}

func (s *Service) Items(ctx context.Context, a Actor, admin bool, q string, category, catalogID, after uint64, limit int) (any, error) {
	if admin && !a.Has("admin.catalog.manage") || !admin && !a.Has("catalog.read") {
		return nil, workrecords.ErrForbidden
	}
	if limit < 1 || limit > 100 || !Text(q, 0, 200) {
		return nil, ErrInvalid
	}
	query := "SELECT i.id,i.category_id,i.version,i.published_version,i.draft FROM catalog_items i"
	if !admin {
		query = "SELECT i.id,i.category_id,v.version,i.published_version,v.definition FROM catalog_items i JOIN catalog_item_versions v ON v.tenant_id=i.tenant_id AND v.item_id=i.id AND v.version=i.published_version"
	}
	query += " WHERE i.tenant_id=? AND i.id>?"
	args := []any{a.TenantID, after}
	if catalogID > 0 {
		query += " AND EXISTS(SELECT 1 FROM catalog_categories c WHERE c.tenant_id=i.tenant_id AND c.id=i.category_id AND c.catalog_id=?)"
		args = append(args, catalogID)
	}
	if category > 0 {
		query += " AND i.category_id=?"
		args = append(args, category)
	}
	if !admin {
		query += " AND " + AudienceSQL("v")
		args = append(args, a.UserID)
	}
	col := "i.draft"
	if !admin {
		col = "v.definition"
	}
	query += " AND (?='' OR LOCATE(?,JSON_UNQUOTE(JSON_EXTRACT(" + col + ",'$.title')))>0) ORDER BY i.id LIMIT ?"
	args = append(args, q, q, limit+1)
	rows, err := s.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Item{}
	for rows.Next() {
		var i Item
		var data []byte
		var published sql.Null[uint64]
		if err := rows.Scan(&i.ID, &i.CategoryID, &i.Version, &published, &data); err != nil {
			return nil, err
		}
		if published.Valid {
			i.PublishedVersion = &published.V
		}
		if err := json.Unmarshal(data, &i.Draft); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var next uint64
	if len(items) > limit {
		items = items[:limit]
		next = items[limit-1].ID
	}
	if admin {
		return ItemPage{Items: items, Next: next}, nil
	}
	page := PublicationPage{Items: []Publication{}, Next: next}
	for _, i := range items {
		page.Items = append(page.Items, Public(i.ID, i.CategoryID, i.Version, i.Draft))
	}
	return page, nil
}
