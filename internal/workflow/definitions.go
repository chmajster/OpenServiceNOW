package workflow

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/chmajster/OpenServiceNOW/internal/audit"
	"github.com/chmajster/OpenServiceNOW/internal/identity"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
	"strconv"
	"time"
)

type Service struct {
	DB       *sql.DB
	tx       *database.TxManager
	commands Commands
	now      func() time.Time
}

func NewService(db *sql.DB, commands Commands) *Service {
	return &Service{db, database.NewTxManager(db), commands, func() time.Time { return time.Now().UTC() }}
}
func NewServiceWithClock(db *sql.DB, commands Commands, clock func() time.Time) *Service {
	s := NewService(db, commands)
	if clock != nil {
		s.now = clock
	}
	return s
}
func missing(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}
func Audit(ctx context.Context, e database.Executor, a Actor, action string, id uint64, data any) error {
	return audit.NewMySQLRepository().Append(ctx, e, audit.Event{TenantID: &a.TenantID, ActorUserID: &a.UserID, Action: action, ObjectType: "workflow", ObjectID: fmt.Sprint(id), NewValues: data, RequestID: a.RequestID, IPAddress: a.IPAddress, Source: "workflow"})
}
func (s *Service) actorTx(ctx context.Context, a Actor, permission string, fn func(database.Executor, Actor) error) error {
	return s.tx.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(e database.Executor) error {
		current, err := workrecords.LockPrincipal(ctx, e, a)
		if err != nil {
			return err
		}
		if !current.Has(permission) {
			return workrecords.ErrForbidden
		}
		return fn(e, current)
	})
}
func GetDefinition(ctx context.Context, e database.Executor, tenant, id uint64) (Definition, error) {
	var d Definition
	var raw []byte
	var pub sql.Null[uint64]
	err := e.QueryRowContext(ctx, "SELECT id,version,published_version,draft FROM workflow_definitions WHERE tenant_id=? AND id=?", tenant, id).Scan(&d.ID, &d.Version, &pub, &raw)
	if err != nil {
		return d, missing(err)
	}
	if pub.Valid {
		d.PublishedVersion = strconv.FormatUint(pub.V, 10)
	}
	err = json.Unmarshal(raw, &d.Draft)
	return d, err
}
func (s *Service) Definition(ctx context.Context, a Actor, id uint64) (Definition, error) {
	if !a.Has("workflow.author") && !a.Has("workflow.publish") {
		return Definition{}, workrecords.ErrForbidden
	}
	return GetDefinition(ctx, s.DB, a.TenantID, id)
}
func (s *Service) Save(ctx context.Context, a Actor, id uint64, d Definition, key string) (Definition, error) {
	if err := Validate(d.Draft, false); err != nil {
		return d, err
	}
	err := s.actorTx(ctx, a, "workflow.author", func(e database.Executor, a Actor) error {
		if id == 0 {
			raw, _ := json.Marshal(d.Draft)
			old, err := workrecords.ReserveOperation(ctx, e, a.Actor, key, "workflow_definition", sha256.Sum256(raw))
			if err != nil {
				return err
			}
			if old > 0 {
				d, err = GetDefinition(ctx, e, a.TenantID, old)
				return err
			}
			var count int
			if err = e.QueryRowContext(ctx, "SELECT COUNT(*) FROM workflow_definitions WHERE tenant_id=?", a.TenantID).Scan(&count); err != nil {
				return err
			}
			if count >= 100 {
				return invalid("organizacja może mieć najwyżej 100 definicji procesu")
			}
			result, err := e.ExecContext(ctx, "INSERT INTO workflow_definitions(tenant_id,name,draft) VALUES(?,?,?)", a.TenantID, d.Draft.Name, raw)
			if err != nil {
				return err
			}
			n, err := result.LastInsertId()
			if err != nil {
				return err
			}
			d.ID = uint64(n)
			d.Version = 1
			if err = workrecords.CompleteOperation(ctx, e, a.Actor, key, d.ID); err != nil {
				return err
			}
		} else {
			old, err := GetDefinition(ctx, e, a.TenantID, id)
			if err != nil {
				return err
			}
			if d.Version == 0 || d.Version != old.Version {
				return ErrConflict
			}
			raw, _ := json.Marshal(d.Draft)
			result, err := e.ExecContext(ctx, "UPDATE workflow_definitions SET name=?,draft=?,version=version+1 WHERE tenant_id=? AND id=? AND version=?", d.Draft.Name, raw, a.TenantID, id, d.Version)
			if err != nil {
				return err
			}
			n, err := result.RowsAffected()
			if err != nil {
				return err
			}
			if n != 1 {
				return ErrConflict
			}
			d.ID = id
			d.Version++
			d.PublishedVersion = old.PublishedVersion
		}
		return Audit(ctx, e, a, "workflow.draft.saved", d.ID, map[string]any{"version": fmt.Sprint(d.Version)})
	})
	return d, err
}
func ActiveExecutor(ctx context.Context, e database.Executor, tenant uint64, c Config) (Actor, error) {
	a, err := workrecords.LockPrincipal(ctx, e, Actor{Actor: workrecords.Actor{TenantID: tenant, UserID: c.ExecutorID}})
	if err != nil {
		return a, err
	}
	if !a.Has("workflow.execute") || !a.Has("request.fulfill") {
		return a, workrecords.ErrForbidden
	}
	var unrestricted bool
	if err = e.QueryRowContext(ctx, "SELECT password_change_required=FALSE FROM users WHERE id=?", c.ExecutorID).Scan(&unrestricted); err != nil {
		return a, err
	}
	if !unrestricted {
		return a, workrecords.ErrForbidden
	}
	var ok bool
	err = e.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM group_memberships gm JOIN `groups` g ON g.tenant_id=gm.tenant_id AND g.id=gm.group_id AND g.active=TRUE WHERE gm.tenant_id=? AND gm.group_id=? AND gm.user_id=?)", tenant, c.GroupID, c.ExecutorID).Scan(&ok)
	if err != nil {
		return a, err
	}
	if !ok {
		return a, workrecords.ErrForbidden
	}
	return a, nil
}
func (s *Service) Publish(ctx context.Context, a Actor, id, version uint64) (Definition, error) {
	var d Definition
	err := s.actorTx(ctx, a, "workflow.publish", func(e database.Executor, a Actor) error {
		var err error
		d, err = GetDefinition(ctx, e, a.TenantID, id)
		if err != nil {
			return err
		}
		if version == 0 || version != d.Version {
			return ErrConflict
		}
		if err = Validate(d.Draft, true); err != nil {
			return err
		}
		if _, err = ActiveExecutor(ctx, e, a.TenantID, d.Draft); err != nil {
			if errors.Is(err, workrecords.ErrForbidden) {
				return invalid("wykonawca musi być aktywnym członkiem grupy z workflow.execute i request.fulfill")
			}
			return err
		}
		for _, n := range d.Draft.Nodes {
			if n.Type == "APPROVAL" {
				if _, err = eligible(ctx, e, a.TenantID, n, 0, d.Draft.SelfApproval); err != nil {
					return err
				}
			}
		}
		raw, _ := json.Marshal(d.Draft)
		_, err = e.ExecContext(ctx, "INSERT INTO workflow_versions(tenant_id,definition_id,version,config,published_by,published_at) VALUES(?,?,?,?,?,?)", a.TenantID, id, version, raw, a.UserID, s.now())
		if err != nil {
			return err
		}
		_, err = e.ExecContext(ctx, "UPDATE workflow_definitions SET published_version=?,version=version+1 WHERE tenant_id=? AND id=?", version, a.TenantID, id)
		if err != nil {
			return err
		}
		d.PublishedVersion = fmt.Sprint(version)
		d.Version++
		return Audit(ctx, e, a, "workflow.published", id, map[string]any{"version": fmt.Sprint(version), "executor_id": fmt.Sprint(d.Draft.ExecutorID), "self_approval": d.Draft.SelfApproval, "membership_policy": "SNAPSHOT_AND_CURRENT"})
	})
	return d, err
}

// A catalog publication pins an existing immutable version, not a mutable pointer.
func Published(ctx context.Context, e database.Executor, tenant, id, version uint64) (Config, error) {
	var raw []byte
	var c Config
	err := e.QueryRowContext(ctx, "SELECT config FROM workflow_versions WHERE tenant_id=? AND definition_id=? AND version=?", tenant, id, version).Scan(&raw)
	if err != nil {
		return c, missing(err)
	}
	err = json.Unmarshal(raw, &c)
	return c, err
}
func userAllowed(ctx context.Context, e database.Executor, tenant, user uint64, n Node, requester uint64, self string) (bool, error) {
	if user == requester && self == "DENY" {
		return false, nil
	}
	if n.TargetType == "USER" && user != n.TargetID {
		return false, nil
	}
	if err := workrecords.ValidateActiveUser(ctx, e, tenant, user); err != nil {
		if errors.Is(err, workrecords.ErrNotFound) {
			return false, nil
		}
		return false, err
	}
	var unrestricted bool
	if err := e.QueryRowContext(ctx, "SELECT password_change_required=FALSE FROM users WHERE id=?", user).Scan(&unrestricted); err != nil {
		return false, err
	}
	if !unrestricted {
		return false, nil
	}
	keys, err := identity.NewMySQLRepository(nil).ListPermissionsInTx(ctx, e, tenant, user)
	if err != nil {
		return false, err
	}
	allowed := false
	for _, k := range keys {
		allowed = allowed || k == "workflow.approve"
	}
	if !allowed {
		return false, nil
	}
	if n.TargetType == "GROUP" {
		err = e.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM group_memberships gm JOIN `groups` g ON g.tenant_id=gm.tenant_id AND g.id=gm.group_id AND g.active=TRUE WHERE gm.tenant_id=? AND gm.group_id=? AND gm.user_id=?)", tenant, n.TargetID, user).Scan(&allowed)
	}
	return allowed, err
}
func eligible(ctx context.Context, e database.Executor, tenant uint64, n Node, requester uint64, self string) ([]uint64, error) {
	ids := []uint64{n.TargetID}
	if n.TargetType == "GROUP" {
		ids = []uint64{}
		rows, err := e.QueryContext(ctx, "SELECT gm.user_id FROM group_memberships gm JOIN `groups` g ON g.tenant_id=gm.tenant_id AND g.id=gm.group_id AND g.active=TRUE WHERE gm.tenant_id=? AND gm.group_id=? ORDER BY gm.user_id LIMIT 101", tenant, n.TargetID)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id uint64
			if err = rows.Scan(&id); err != nil {
				rows.Close()
				return nil, err
			}
			ids = append(ids, id)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
		if len(ids) > 100 {
			return nil, invalid("grupa akceptacji przekracza 100 członków")
		}
	}
	out := []uint64{}
	for _, id := range ids {
		ok, err := userAllowed(ctx, e, tenant, id, n, requester, self)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, id)
		}
	}
	if len(out) == 0 {
		return nil, invalid("brak uprawnionych akceptujących; sprawdź członkostwo, workflow.approve i politykę własnego wniosku")
	}
	return out, nil
}
