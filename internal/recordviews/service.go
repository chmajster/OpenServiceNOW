package recordviews

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
	"github.com/chmajster/OpenServiceNOW/internal/recordlists"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
	mysql "github.com/go-sql-driver/mysql"
	"strings"
	"time"
)

var ErrNotFound = errors.New("widok nie istnieje lub nie masz do niego dostępu")
var ErrConflict = errors.New("widok zmienił się; zachowaj konfigurację i odczytaj aktualną wersję")
var ErrDuplicate = errors.New("masz już widok o tej nazwie")

type View struct {
	ID            uint64          `json:"id,string"`
	OwnerID       uint64          `json:"owner_user_id,string"`
	GroupID       *uint64         `json:"group_id,string,omitempty"`
	Domain        string          `json:"domain"`
	Name          string          `json:"name"`
	Configuration json.RawMessage `json:"configuration"`
	Version       uint64          `json:"version,string"`
	UpdatedAt     time.Time       `json:"updated_at"`
	CanEdit       bool            `json:"can_edit"`
}
type Input struct {
	Domain        string          `json:"domain"`
	Name          string          `json:"name"`
	GroupID       *uint64         `json:"group_id,string"`
	Configuration json.RawMessage `json:"configuration"`
	Version       uint64          `json:"version,string"`
}
type Domain struct {
	Read     func(workrecords.Principal) bool
	Validate func(workrecords.Principal, json.RawMessage) error
}
type Service struct {
	DB      *sql.DB
	Domains map[string]Domain
}

func (s *Service) domain(a workrecords.Principal, key string) (Domain, error) {
	d, ok := s.Domains[key]
	if !ok {
		return d, recordlists.ErrInvalid
	}
	if !d.Read(a) {
		return d, workrecords.ErrForbidden
	}
	return d, nil
}

const columns = "v.id,v.owner_user_id,v.group_id,v.domain,v.name,v.configuration,v.version,v.updated_at"
const visibility = `(v.owner_user_id=? OR (v.group_id IS NOT NULL AND EXISTS(SELECT 1 FROM group_memberships gm JOIN ` + "`groups`" + ` g ON g.tenant_id=gm.tenant_id AND g.id=gm.group_id WHERE gm.tenant_id=v.tenant_id AND gm.group_id=v.group_id AND gm.user_id=? AND g.active=TRUE)))`

func scan(row interface{ Scan(...any) error }, a workrecords.Principal) (View, error) {
	var v View
	var group sql.Null[uint64]
	err := row.Scan(&v.ID, &v.OwnerID, &group, &v.Domain, &v.Name, &v.Configuration, &v.Version, &v.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return v, ErrNotFound
	}
	if group.Valid {
		v.GroupID = &group.V
	}
	v.CanEdit = v.OwnerID == a.UserID
	return v, err
}
func (s *Service) Get(ctx context.Context, a workrecords.Principal, id uint64) (View, error) {
	v, err := scan(s.DB.QueryRowContext(ctx, "SELECT "+columns+" FROM record_views v WHERE v.tenant_id=? AND v.id=? AND "+visibility, a.TenantID, id, a.UserID, a.UserID), a)
	if err != nil {
		return v, err
	}
	_, err = s.domain(a, v.Domain)
	if errors.Is(err, workrecords.ErrForbidden) {
		err = ErrNotFound
	}
	return v, err
}
func (s *Service) List(ctx context.Context, a workrecords.Principal, domain string, before uint64, limit int) ([]View, uint64, error) {
	if _, err := s.domain(a, domain); err != nil {
		return nil, 0, err
	}
	if limit < 1 || limit > 100 {
		return nil, 0, recordlists.ErrInvalid
	}
	where := " WHERE v.tenant_id=? AND v.domain=? AND " + visibility
	args := []any{a.TenantID, domain, a.UserID, a.UserID}
	if before > 0 {
		where += " AND v.id<?"
		args = append(args, before)
	}
	args = append(args, limit+1)
	rows, err := s.DB.QueryContext(ctx, "SELECT "+columns+" FROM record_views v"+where+" ORDER BY v.id DESC LIMIT ?", args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []View{}
	for rows.Next() {
		v, err := scan(rows, a)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, v)
	}
	if err = rows.Err(); err != nil {
		return nil, 0, err
	}
	var next uint64
	if len(out) > limit {
		out = out[:limit]
		next = out[limit-1].ID
	}
	return out, next, nil
}
func (s *Service) validate(ctx context.Context, e database.Executor, a workrecords.Principal, in Input) error {
	d, err := s.domain(a, in.Domain)
	if err != nil {
		return err
	}
	if err = d.Validate(a, in.Configuration); err != nil {
		return err
	}
	if strings.TrimSpace(in.Name) != in.Name || len([]rune(in.Name)) < 1 || len([]rune(in.Name)) > 120 || strings.ContainsAny(in.Name, "\r\n\x00") {
		return recordlists.ErrInvalid
	}
	if in.GroupID != nil {
		if *in.GroupID == 0 || !a.Has("workspace.views.share") {
			return workrecords.ErrForbidden
		}
		var member bool
		err = e.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM group_memberships gm JOIN `groups` g ON g.tenant_id=gm.tenant_id AND g.id=gm.group_id WHERE gm.tenant_id=? AND gm.user_id=? AND gm.group_id=? AND g.active=TRUE)", a.TenantID, a.UserID, *in.GroupID).Scan(&member)
		if err != nil {
			return err
		}
		if !member {
			return workrecords.ErrForbidden
		}
	}
	return nil
}
func (s *Service) Save(ctx context.Context, a workrecords.Principal, id uint64, in Input, key string) (View, error) {
	var out View
	err := database.NewTxManager(s.DB).WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(e database.Executor) error {
		current, err := workrecords.LockPrincipal(ctx, e, a)
		if err != nil {
			return err
		}
		a = current
		if err = s.validate(ctx, e, a, in); err != nil {
			return err
		}
		if id == 0 {
			raw, err := json.Marshal(in)
			if err != nil {
				return err
			}
			existing, err := workrecords.ReserveOperation(ctx, e, a.Actor, key, "record_view_create", sha256.Sum256(raw))
			if err != nil {
				return err
			}
			if existing > 0 {
				out, err = scan(e.QueryRowContext(ctx, "SELECT "+columns+" FROM record_views v WHERE v.tenant_id=? AND v.id=? AND v.owner_user_id=?", a.TenantID, existing, a.UserID), a)
				return err
			}
			result, err := e.ExecContext(ctx, "INSERT INTO record_views(tenant_id,owner_user_id,group_id,domain,name,configuration) VALUES(?,?,?,?,?,?)", a.TenantID, a.UserID, in.GroupID, in.Domain, in.Name, in.Configuration)
			if err != nil {
				return duplicate(err)
			}
			newID, err := result.LastInsertId()
			if err != nil {
				return err
			}
			id = uint64(newID)
			if err = workrecords.CompleteOperation(ctx, e, a.Actor, key, id); err != nil {
				return err
			}
		} else {
			old, err := scan(e.QueryRowContext(ctx, "SELECT "+columns+" FROM record_views v WHERE v.tenant_id=? AND v.id=? AND v.owner_user_id=? FOR UPDATE", a.TenantID, id, a.UserID), a)
			if err != nil {
				return err
			}
			if old.Domain != in.Domain {
				return recordlists.ErrInvalid
			}
			if old.Version != in.Version {
				return ErrConflict
			}
			result, err := e.ExecContext(ctx, "UPDATE record_views SET name=?,group_id=?,configuration=?,version=version+1,updated_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND id=? AND version=?", in.Name, in.GroupID, in.Configuration, a.TenantID, id, in.Version)
			if err != nil {
				return duplicate(err)
			}
			n, err := result.RowsAffected()
			if err != nil {
				return err
			}
			if n != 1 {
				return ErrConflict
			}
		}
		out, err = scan(e.QueryRowContext(ctx, "SELECT "+columns+" FROM record_views v WHERE v.tenant_id=? AND v.id=?", a.TenantID, id), a)
		if err != nil {
			return err
		}
		return changed(ctx, e, a, "saved", out)
	})
	return out, err
}
func (s *Service) Delete(ctx context.Context, a workrecords.Principal, id, version uint64) error {
	return database.NewTxManager(s.DB).WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(e database.Executor) error {
		current, err := workrecords.LockPrincipal(ctx, e, a)
		if err != nil {
			return err
		}
		a = current
		v, err := scan(e.QueryRowContext(ctx, "SELECT "+columns+" FROM record_views v WHERE v.tenant_id=? AND v.id=? AND v.owner_user_id=? FOR UPDATE", a.TenantID, id, a.UserID), a)
		if err != nil {
			return err
		}
		if _, err = s.domain(a, v.Domain); err != nil {
			return err
		}
		if v.Version != version {
			return ErrConflict
		}
		if _, err = e.ExecContext(ctx, "DELETE FROM record_views WHERE tenant_id=? AND id=?", a.TenantID, id); err != nil {
			return err
		}
		return changed(ctx, e, a, "deleted", v)
	})
}
func duplicate(err error) error {
	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
		return ErrDuplicate
	}
	return err
}
func changed(ctx context.Context, e database.Executor, a workrecords.Principal, action string, v View) error {
	data := map[string]any{"domain": v.Domain, "name": v.Name, "version": fmt.Sprint(v.Version), "group_id": v.GroupID}
	if err := audit.NewMySQLRepository().Append(ctx, e, audit.Event{TenantID: &a.TenantID, ActorUserID: &a.UserID, Action: "record_view." + action, ObjectType: "record_view", ObjectID: fmt.Sprint(v.ID), NewValues: data, RequestID: a.RequestID, IPAddress: a.IPAddress, Source: "api"}); err != nil {
		return err
	}
	payload, err := json.Marshal(map[string]any{"actor_user_id": fmt.Sprint(a.UserID), "visibility": "INTERNAL", "occurred_at": time.Now().UTC(), "version": fmt.Sprint(v.Version), "action": action})
	if err != nil {
		return err
	}
	_, _, err = events.NewRepository(nil).Append(ctx, e, events.Input{TenantID: a.TenantID, Type: "record_view.changed", AggregateType: "record_view", AggregateID: fmt.Sprint(v.ID), Payload: payload, IdempotencyKey: fmt.Sprintf("record_view:%d:%s:%d", v.ID, action, v.Version)})
	return err
}
