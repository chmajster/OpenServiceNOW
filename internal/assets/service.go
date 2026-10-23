package assets

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/chmajster/OpenServiceNOW/internal/activity"
	"github.com/chmajster/OpenServiceNOW/internal/audit"
	"github.com/chmajster/OpenServiceNOW/internal/events"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
	"github.com/go-sql-driver/mysql"
	"strings"
	"time"
)

type Actor = workrecords.Principal
type Service struct {
	DB *sql.DB
	tx *database.TxManager
}

func NewService(db *sql.DB) *Service { return &Service{db, database.NewTxManager(db)} }
func Missing(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	var m *mysql.MySQLError
	if errors.As(err, &m) && m.Number == 1062 {
		return ErrDuplicate
	}
	return err
}
func Access(a Actor, alias string) (string, []any) {
	if a.Has("assets.read") {
		return "1=1", nil
	}
	if a.Has("assets.read_own") {
		return alias + ".owner_id=?", []any{a.UserID}
	}
	return "1=0", nil
}
func Writable(a Actor) bool { return a.Has("assets.manage") && a.Has("assets.read") }

const columns = `a.id,a.asset_tag,a.name,a.owner_id,u.display_name,COALESCE(a.serial_number,''),a.vendor,a.model,a.location,a.state,COALESCE(DATE_FORMAT(a.purchase_date,'%Y-%m-%d'),''),COALESCE(CAST(a.purchase_cost AS CHAR),''),a.currency,COALESCE(DATE_FORMAT(a.warranty_until,'%Y-%m-%d'),''),a.archived,a.version,a.created_at,a.updated_at`

func scan(row interface{ Scan(...any) error }) (Record, error) {
	var r Record
	err := row.Scan(&r.ID, &r.AssetTag, &r.Name, &r.OwnerID, &r.OwnerName, &r.SerialNumber, &r.Vendor, &r.Model, &r.Location, &r.State, &r.PurchaseDate, &r.PurchaseCost, &r.Currency, &r.WarrantyUntil, &r.Archived, &r.Version, &r.CreatedAt, &r.UpdatedAt)
	return r, Missing(err)
}
func (s *Service) GetInTx(ctx context.Context, e database.Executor, a Actor, id uint64, lock bool) (Record, error) {
	p, args := Access(a, "a")
	params := []any{a.TenantID, id}
	params = append(params, args...)
	q := "SELECT " + columns + " FROM assets a JOIN users u ON u.id=a.owner_id WHERE a.tenant_id=? AND a.id=? AND " + p
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
	if f.Limit < 1 || f.Limit > 100 || !Text(f.Query, 0, 160) || !Text(f.Location, 0, 160) || f.State != "" && !State(f.State) || f.Archived != "" && f.Archived != "true" && f.Archived != "false" {
		return page, ErrInvalid
	}
	if !a.Has("assets.read") && !a.Has("assets.read_own") {
		return page, workrecords.ErrForbidden
	}
	p, args := Access(a, "a")
	params := []any{a.TenantID}
	params = append(params, args...)
	q := "SELECT " + columns + " FROM assets a JOIN users u ON u.id=a.owner_id WHERE a.tenant_id=? AND " + p
	if f.Query != "" {
		q += " AND (a.name LIKE ? OR a.asset_tag LIKE ? OR a.serial_number LIKE ?)"
		v := "%" + f.Query + "%"
		params = append(params, v, v, v)
	}
	if f.State != "" {
		q += " AND a.state=?"
		params = append(params, f.State)
	}
	if f.Location != "" {
		q += " AND a.location=?"
		params = append(params, f.Location)
	}
	if f.OwnerID > 0 {
		q += " AND a.owner_id=?"
		params = append(params, f.OwnerID)
	}
	if f.Archived != "" {
		q += " AND a.archived=?"
		params = append(params, f.Archived == "true")
	}
	if f.Before > 0 {
		q += " AND a.id<?"
		params = append(params, f.Before)
	}
	q += " ORDER BY a.id DESC LIMIT ?"
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
func Changed(ctx context.Context, e database.Executor, a Actor, kind string, id, version uint64, before, after any, fields []string) error {
	data := map[string]any{"actor_user_id": fmt.Sprint(a.UserID), "occurred_at": time.Now().UTC(), "visibility": "INTERNAL", "version": fmt.Sprint(version), "before": before, "after": after, "changed_fields": fields}
	if err := audit.NewMySQLRepository().Append(ctx, e, audit.Event{TenantID: &a.TenantID, ActorUserID: &a.UserID, Action: kind + ".changed", ObjectType: kind, ObjectID: fmt.Sprint(id), NewValues: data, RequestID: a.RequestID, IPAddress: a.IPAddress, Source: "inventory"}); err != nil {
		return err
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, _, err = events.NewRepository(nil).Append(ctx, e, events.Input{TenantID: a.TenantID, Type: kind + ".changed", AggregateType: kind, AggregateID: fmt.Sprint(id), Payload: raw, IdempotencyKey: fmt.Sprintf("%s:%d:%d", kind, id, version)})
	return err
}
func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// SaveInTx participates in both manual writes and per-row import transactions.
func (s *Service) SaveInTx(ctx context.Context, e database.Executor, a Actor, id uint64, in Record) (Record, error) {
	if !Writable(a) {
		return Record{}, workrecords.ErrForbidden
	}
	f, err := Normalize(in.Fields)
	if err != nil {
		return Record{}, err
	}
	in.Fields = f
	if err := workrecords.ValidateActiveUser(ctx, e, a.TenantID, in.OwnerID); err != nil {
		if !errors.Is(err, workrecords.ErrNotFound) {
			return in, err
		}
		return Record{}, fmt.Errorf("%w: właściciel nie jest aktywnym członkiem organizacji", ErrInvalid)
	}
	for _, item := range []struct {
		kind         CatalogKind
		value        string
		manufacturer string
	}{
		{CatalogManufacturer, in.Vendor, ""},
		{CatalogModel, in.Model, in.Vendor},
		{CatalogLocation, in.Location, ""},
	} {
		if err := ensureCatalogValue(ctx, e, a.TenantID, item.kind, item.value, item.manufacturer); err != nil {
			return Record{}, err
		}
	}
	var before *Record
	if id > 0 {
		old, err := s.GetInTx(ctx, e, a, id, true)
		if err != nil {
			return in, err
		}
		if old.Archived {
			return in, fmt.Errorf("%w: najpierw przywróć Asset z archiwum", ErrConflict)
		}
		if in.Version == 0 || old.Version != in.Version {
			return in, ErrConflict
		}
		if !Transition(old.State, in.State) {
			return in, fmt.Errorf("%w: niedozwolone przejście cyklu życia Asset", ErrInvalid)
		}
		before = &old
	}
	args := []any{in.AssetTag, in.Name, in.OwnerID, nullable(in.SerialNumber), in.Vendor, in.Model, in.Location, in.State, nullable(in.PurchaseDate), nullable(in.PurchaseCost), in.Currency, nullable(in.WarrantyUntil)}
	if id == 0 {
		args = append([]any{a.TenantID}, args...)
		r, err := e.ExecContext(ctx, "INSERT INTO assets(tenant_id,asset_tag,name,owner_id,serial_number,vendor,model,location,state,purchase_date,purchase_cost,currency,warranty_until) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)", args...)
		if err != nil {
			return in, Missing(err)
		}
		n, err := r.LastInsertId()
		if err != nil {
			return in, err
		}
		id = uint64(n)
	} else {
		args = append(args, a.TenantID, id, in.Version)
		r, err := e.ExecContext(ctx, "UPDATE assets SET asset_tag=?,name=?,owner_id=?,serial_number=?,vendor=?,model=?,location=?,state=?,purchase_date=?,purchase_cost=?,currency=?,warranty_until=?,version=version+1,updated_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND id=? AND version=?", args...)
		if err != nil {
			return in, Missing(err)
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
	return out, Changed(ctx, e, a, "asset", id, out.Version, before, out, []string{"data"})
}
func (s *Service) Save(ctx context.Context, a Actor, id uint64, in Record, key string) (Record, error) {
	var out Record
	err := s.tx.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(e database.Executor) error {
		current, err := workrecords.LockPrincipal(ctx, e, a)
		if err != nil {
			return err
		}
		if !Writable(current) {
			return workrecords.ErrForbidden
		}
		a = current
		if id == 0 {
			f, err := Normalize(in.Fields)
			if err != nil {
				return err
			}
			in.Fields = f
			raw, _ := json.Marshal(in.Fields)
			old, err := workrecords.ReserveOperation(ctx, e, a.Actor, key, "asset_create", sha256.Sum256(raw))
			if err != nil {
				return err
			}
			if old > 0 {
				out, err = s.GetInTx(ctx, e, a, old, false)
				return err
			}
		}
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
func (s *Service) TouchInTx(ctx context.Context, e database.Executor, a Actor, r Record, field string) (Record, error) {
	if !Writable(a) {
		return r, workrecords.ErrForbidden
	}
	before := r
	result, err := e.ExecContext(ctx, "UPDATE assets SET version=version+1,updated_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND id=? AND version=?", a.TenantID, r.ID, r.Version)
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
	return r, Changed(ctx, e, a, "asset", r.ID, r.Version, before, r, []string{field})
}
func (s *Service) Archive(ctx context.Context, a Actor, id, version uint64, flag bool, reason string) (Record, error) {
	var out Record
	if !Text(strings.TrimSpace(reason), 1, 1000) || version == 0 {
		return out, ErrInvalid
	}
	err := s.tx.WithTx(ctx, nil, func(e database.Executor) error {
		current, err := workrecords.LockPrincipal(ctx, e, a)
		if err != nil {
			return err
		}
		a = current
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
		if _, err = e.ExecContext(ctx, "UPDATE assets SET archived=? WHERE tenant_id=? AND id=?", flag, a.TenantID, id); err != nil {
			return err
		}
		out, err = s.TouchInTx(ctx, e, a, r, "archived")
		if err != nil {
			return err
		}
		return audit.NewMySQLRepository().Append(ctx, e, audit.Event{TenantID: &a.TenantID, ActorUserID: &a.UserID, Action: "asset.archive", ObjectType: "asset", ObjectID: fmt.Sprint(id), NewValues: map[string]any{"archived": flag, "reason": reason}, RequestID: a.RequestID, IPAddress: a.IPAddress, Source: "inventory"})
	})
	return out, err
}
func (s *Service) History(ctx context.Context, a Actor, id, before uint64, limit int) (activity.Page, error) {
	if _, err := s.Get(ctx, a, id); err != nil {
		return activity.Page{}, err
	}
	return activity.NewRepository(s.DB).ListBySourceEvent(ctx, a.TenantID, "asset", fmt.Sprint(id), before, limit)
}
