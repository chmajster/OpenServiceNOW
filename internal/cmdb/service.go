package cmdb

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/chmajster/OpenServiceNOW/internal/activity"
	"github.com/chmajster/OpenServiceNOW/internal/assets"
	"github.com/chmajster/OpenServiceNOW/internal/attachments"
	"github.com/chmajster/OpenServiceNOW/internal/audit"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
	"strings"
)

type Service struct {
	DB      *sql.DB
	tx      *database.TxManager
	Assets  *assets.Service
	Files   *attachments.Service
	Domains map[string]RecordDomain
}

func NewService(db *sql.DB, files *attachments.Service, domains map[string]RecordDomain) *Service {
	return &Service{db, database.NewTxManager(db), assets.NewService(db), files, domains}
}
func Access(a Actor, alias string) (string, []any) {
	if a.Has("cmdb.read") {
		return "1=1", nil
	}
	if a.Has("cmdb.read_own") {
		return alias + ".owner_id=?", []any{a.UserID}
	}
	return "1=0", nil
}
func Writable(a Actor) bool { return a.Has("cmdb.manage") && a.Has("cmdb.read") }
func (s *Service) ActorTx(ctx context.Context, a Actor, fn func(database.Executor, Actor) error) error {
	return s.tx.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(e database.Executor) error {
		current, err := workrecords.LockPrincipal(ctx, e, a)
		if err != nil {
			return err
		}
		return fn(e, current)
	})
}

const columns = `c.id,c.identifier,c.name,c.class,c.status,c.owner_id,u.display_name,c.environment,c.location,c.attributes,c.archived,c.version,c.created_at,c.updated_at`

func scan(row interface{ Scan(...any) error }) (Record, error) {
	var r Record
	var raw []byte
	err := row.Scan(&r.ID, &r.Identifier, &r.Name, &r.Class, &r.Status, &r.OwnerID, &r.OwnerName, &r.Environment, &r.Location, &raw, &r.Archived, &r.Version, &r.CreatedAt, &r.UpdatedAt)
	if err != nil {
		return r, assets.Missing(err)
	}
	err = json.Unmarshal(raw, &r.Attributes)
	return r, err
}
func (s *Service) GetInTx(ctx context.Context, e database.Executor, a Actor, id uint64, lock bool) (Record, error) {
	p, args := Access(a, "c")
	params := []any{a.TenantID, id}
	params = append(params, args...)
	q := "SELECT " + columns + " FROM cmdb_cis c JOIN users u ON u.id=c.owner_id WHERE c.tenant_id=? AND c.id=? AND " + p
	if lock {
		q += " FOR UPDATE"
	}
	return scan(e.QueryRowContext(ctx, q, params...))
}
func (s *Service) Get(ctx context.Context, a Actor, id uint64) (Record, error) {
	return s.GetInTx(ctx, s.DB, a, id, false)
}
func (s *Service) List(ctx context.Context, a Actor, f Filter) (Page, error) {
	page := Page{Items: []Record{}}
	if f.Limit < 1 || f.Limit > 100 || !assets.Text(f.Query, 0, 160) || !assets.Text(f.Location, 0, 160) || f.Status != "" && !Status(f.Status) || f.Environment != "" && !Environment(f.Environment) || f.Archived != "" && f.Archived != "true" && f.Archived != "false" {
		return page, ErrInvalid
	}
	if f.Class != "" {
		if _, ok := ClassDefinition(f.Class); !ok {
			return page, ErrInvalid
		}
	}
	if !a.Has("cmdb.read") && !a.Has("cmdb.read_own") {
		return page, workrecords.ErrForbidden
	}
	p, args := Access(a, "c")
	params := []any{a.TenantID}
	params = append(params, args...)
	q := "SELECT " + columns + " FROM cmdb_cis c JOIN users u ON u.id=c.owner_id WHERE c.tenant_id=? AND " + p
	if f.Query != "" {
		q += " AND (c.name LIKE ? OR c.identifier LIKE ?)"
		v := "%" + f.Query + "%"
		params = append(params, v, v)
	}
	for _, filter := range []struct{ column, value string }{{"class", f.Class}, {"status", f.Status}, {"environment", f.Environment}, {"location", f.Location}} {
		if filter.value != "" {
			q += " AND c." + filter.column + "=?"
			params = append(params, filter.value)
		}
	}
	if f.OwnerID > 0 {
		q += " AND c.owner_id=?"
		params = append(params, f.OwnerID)
	}
	if f.Archived != "" {
		q += " AND c.archived=?"
		params = append(params, f.Archived == "true")
	}
	if f.Before > 0 {
		q += " AND c.id<?"
		params = append(params, f.Before)
	}
	q += " ORDER BY c.id DESC LIMIT ?"
	params = append(params, f.Limit+1)
	rows, err := s.DB.QueryContext(ctx, q, params...)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		r, e := scan(rows)
		if e != nil {
			return page, e
		}
		page.Items = append(page.Items, r)
	}
	if len(page.Items) > f.Limit {
		page.Items = page.Items[:f.Limit]
		page.Next = page.Items[f.Limit-1].ID
	}
	return page, rows.Err()
}
func (s *Service) SaveInTx(ctx context.Context, e database.Executor, a Actor, id uint64, in Record) (Record, error) {
	if !Writable(a) {
		return in, workrecords.ErrForbidden
	}
	f, err := Normalize(in.Fields)
	if err != nil {
		return in, err
	}
	in.Fields = f
	if err := workrecords.ValidateActiveUser(ctx, e, a.TenantID, in.OwnerID); err != nil {
		if !errors.Is(err, workrecords.ErrNotFound) {
			return in, err
		}
		return in, fmt.Errorf("%w: opiekun nie jest aktywnym członkiem organizacji", ErrInvalid)
	}
	var before *Record
	if id > 0 {
		old, err := s.GetInTx(ctx, e, a, id, true)
		if err != nil {
			return in, err
		}
		if old.Archived {
			return in, fmt.Errorf("%w: najpierw przywróć CI z archiwum", ErrConflict)
		}
		if in.Version == 0 || old.Version != in.Version {
			return in, ErrConflict
		}
		if old.Class != in.Class {
			return in, fmt.Errorf("%w: klasa istniejącego CI jest niezmienna", ErrConflict)
		}
		before = &old
	}
	raw, _ := json.Marshal(in.Attributes)
	args := []any{in.Identifier, in.Name, in.Class, in.Status, in.OwnerID, in.Environment, in.Location, raw}
	if id == 0 {
		args = append([]any{a.TenantID}, args...)
		r, err := e.ExecContext(ctx, "INSERT INTO cmdb_cis(tenant_id,identifier,name,class,status,owner_id,environment,location,attributes) VALUES(?,?,?,?,?,?,?,?,?)", args...)
		if err != nil {
			return in, assets.Missing(err)
		}
		n, err := r.LastInsertId()
		if err != nil {
			return in, err
		}
		id = uint64(n)
	} else {
		args = append(args, a.TenantID, id, in.Version)
		r, err := e.ExecContext(ctx, "UPDATE cmdb_cis SET identifier=?,name=?,class=?,status=?,owner_id=?,environment=?,location=?,attributes=?,version=version+1,updated_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND id=? AND version=?", args...)
		if err != nil {
			return in, assets.Missing(err)
		}
		n, err := r.RowsAffected()
		if err != nil {
			return in, err
		}
		if n != 1 {
			return in, ErrConflict
		}
	}
	out, err := s.GetInTx(ctx, e, a, id, false)
	if err != nil {
		return in, err
	}
	return out, assets.Changed(ctx, e, a, "ci", id, out.Version, before, out, []string{"data"})
}
func (s *Service) Save(ctx context.Context, a Actor, id uint64, in Record, key string) (Record, error) {
	var out Record
	err := s.ActorTx(ctx, a, func(e database.Executor, a Actor) error {
		if !Writable(a) {
			return workrecords.ErrForbidden
		}
		if id == 0 {
			f, err := Normalize(in.Fields)
			if err != nil {
				return err
			}
			in.Fields = f
			raw, _ := json.Marshal(in.Fields)
			old, err := workrecords.ReserveOperation(ctx, e, a.Actor, key, "ci_create", sha256.Sum256(raw))
			if err != nil {
				return err
			}
			if old > 0 {
				out, err = s.GetInTx(ctx, e, a, old, false)
				return err
			}
		}
		var err error
		out, err = s.SaveInTx(ctx, e, a, id, in)
		if err != nil {
			return err
		}
		if id == 0 {
			return workrecords.CompleteOperation(ctx, e, a.Actor, key, out.ID)
		}
		return nil
	})
	return out, err
}
func (s *Service) touch(ctx context.Context, e database.Executor, a Actor, r Record, field string) (Record, error) {
	before := r
	result, err := e.ExecContext(ctx, "UPDATE cmdb_cis SET version=version+1,updated_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND id=? AND version=?", a.TenantID, r.ID, r.Version)
	if err != nil {
		return r, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return r, err
	}
	if n != 1 {
		return r, ErrConflict
	}
	r, err = s.GetInTx(ctx, e, a, r.ID, false)
	if err != nil {
		return r, err
	}
	return r, assets.Changed(ctx, e, a, "ci", r.ID, r.Version, before, r, []string{field})
}
func (s *Service) Archive(ctx context.Context, a Actor, id, version uint64, flag bool, reason string) (Record, error) {
	var out Record
	if !assets.Text(strings.TrimSpace(reason), 1, 1000) || version == 0 {
		return out, ErrInvalid
	}
	err := s.ActorTx(ctx, a, func(e database.Executor, a Actor) error {
		if !Writable(a) {
			return workrecords.ErrForbidden
		}
		r, err := s.GetInTx(ctx, e, a, id, true)
		if err != nil {
			return err
		}
		if r.Version != version {
			return ErrConflict
		}
		if r.Archived == flag {
			out = r
			return nil
		}
		if r.Version != version {
			return ErrConflict
		}
		if _, err = e.ExecContext(ctx, "UPDATE cmdb_cis SET archived=? WHERE tenant_id=? AND id=?", flag, a.TenantID, id); err != nil {
			return err
		}
		out, err = s.touch(ctx, e, a, r, "archived")
		if err != nil {
			return err
		}
		return audit.NewMySQLRepository().Append(ctx, e, audit.Event{TenantID: &a.TenantID, ActorUserID: &a.UserID, Action: "ci.archive", ObjectType: "ci", ObjectID: fmt.Sprint(id), NewValues: map[string]any{"archived": flag, "reason": reason}, RequestID: a.RequestID, IPAddress: a.IPAddress, Source: "inventory"})
	})
	return out, err
}

type HistoryPage struct {
	activity.Page
	Pending bool `json:"pending"`
}

func (s *Service) History(ctx context.Context, a Actor, kind string, id, before uint64, limit int) (HistoryPage, error) {
	if limit < 1 || limit > 100 {
		return HistoryPage{}, ErrInvalid
	}
	if kind == "ci" {
		if _, err := s.Get(ctx, a, id); err != nil {
			return HistoryPage{}, err
		}
	} else if kind == "asset" {
		if _, err := s.Assets.Get(ctx, a, id); err != nil {
			return HistoryPage{}, err
		}
	} else {
		return HistoryPage{}, ErrInvalid
	}
	p, err := activity.NewRepository(s.DB).ListBySourceEvent(ctx, a.TenantID, kind, fmt.Sprint(id), before, limit)
	if err != nil {
		return HistoryPage{}, err
	}
	out := HistoryPage{Page: p}
	err = s.DB.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM outbox_events o WHERE o.tenant_id=? AND o.aggregate_type=? AND o.aggregate_id=? AND (o.status<>'DELIVERED' OR EXISTS(SELECT 1 FROM event_deliveries d WHERE d.event_id=o.id AND d.consumer_id='activity.v1' AND d.status<>'DELIVERED')))", a.TenantID, kind, fmt.Sprint(id)).Scan(&out.Pending)
	return out, err
}
