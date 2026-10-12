package sla

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"time"

	"github.com/chmajster/OpenServiceNOW/internal/audit"
	"github.com/chmajster/OpenServiceNOW/internal/incidents"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
)

type Service struct {
	db  *sql.DB
	tx  *database.TxManager
	now func() time.Time
}

func NewService(db *sql.DB) *Service {
	return &Service{db: db, tx: database.NewTxManager(db), now: time.Now}
}

// The clock is a dependency, not an environment-controlled production switch.
func NewServiceWithClock(db *sql.DB, clock func() time.Time) *Service {
	s := NewService(db)
	if clock != nil {
		s.now = clock
	}
	return s
}

type Definition struct {
	ID          uint64          `json:"id,string"`
	Version     uint64          `json:"version,string"`
	Kind        string          `json:"kind"`
	EffectiveAt time.Time       `json:"effective_at"`
	Config      json.RawMessage `json:"config"`
}
type DefinitionPage struct {
	Items        []Definition `json:"items"`
	NextBeforeID uint64       `json:"next_before_id,string,omitempty"`
	Rules        string       `json:"rules"`
}

var keyPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{8,100}$`)

func decodeConfig(raw []byte, value any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(value); err != nil {
		return ErrInvalid
	}
	if err := d.Decode(new(any)); !errors.Is(err, io.EOF) {
		return ErrInvalid
	}
	return nil
}

func (s *Service) Definition(ctx context.Context, e database.Executor, tenant, id, version uint64) (Definition, error) {
	d := Definition{}
	q := `SELECT d.id,v.version,d.kind,v.effective_at,v.config FROM sla_definitions d JOIN sla_definition_versions v ON v.definition_id=d.id WHERE d.tenant_id=? AND d.id=? AND v.version=`
	args := []any{tenant, id}
	if version == 0 {
		q += `d.version`
	} else {
		q += `?`
		args = append(args, version)
	}
	err := e.QueryRowContext(ctx, q, args...).Scan(&d.ID, &d.Version, &d.Kind, &d.EffectiveAt, &d.Config)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return d, err
}
func (s *Service) ListDefinitions(ctx context.Context, tenant uint64, kind string, before uint64, limit int) (DefinitionPage, error) {
	p := DefinitionPage{Items: []Definition{}, Rules: Rules}
	if kind != "calendar" && kind != "policy" || limit < 1 || limit > 100 {
		return p, ErrInvalid
	}
	q := `SELECT d.id,v.version,d.kind,v.effective_at,v.config FROM sla_definitions d JOIN sla_definition_versions v ON v.definition_id=d.id AND v.version=d.version WHERE d.tenant_id=? AND d.kind=?`
	args := []any{tenant, kind}
	if before > 0 {
		q += ` AND d.id<?`
		args = append(args, before)
	}
	q += ` ORDER BY d.id DESC LIMIT ?`
	args = append(args, limit+1)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return p, err
	}
	defer rows.Close()
	for rows.Next() {
		d := Definition{}
		if err = rows.Scan(&d.ID, &d.Version, &d.Kind, &d.EffectiveAt, &d.Config); err != nil {
			return p, err
		}
		p.Items = append(p.Items, d)
	}
	if err = rows.Err(); err != nil {
		return p, err
	}
	if len(p.Items) > limit {
		p.Items = p.Items[:limit]
		p.NextBeforeID = p.Items[limit-1].ID
	}
	return p, nil
}
func (s *Service) SaveDefinition(ctx context.Context, a incidents.Actor, kind string, id, expected uint64, key string, raw json.RawMessage) (Definition, error) {
	out := Definition{}
	if !a.Permissions["admin.sla.manage"] {
		return out, incidents.ErrForbidden
	}
	if kind != "calendar" && kind != "policy" || id == 0 && (!keyPattern.MatchString(key) || expected != 0) || id > 0 && expected == 0 {
		return out, ErrInvalid
	}
	// Normalize all client-managed values; identity/version/effective time are server-owned.
	var c Calendar
	var p Policy
	if kind == "calendar" {
		if decodeConfig(raw, &c) != nil || ValidateCalendar(c) != nil {
			return out, ErrInvalid
		}
		c.ID = 0
		c.Version = 0
		if c.Holidays == nil {
			c.Holidays = []string{}
		}
		if c.Exceptions == nil {
			c.Exceptions = []Exception{}
		}
		for n := range c.Week {
			if c.Week[n].Windows == nil {
				c.Week[n].Windows = []Window{}
			}
		}
		for n := range c.Exceptions {
			if c.Exceptions[n].Windows == nil {
				c.Exceptions[n].Windows = []Window{}
			}
		}
		raw, _ = json.Marshal(c)
	} else {
		if decodeConfig(raw, &p) != nil || ValidatePolicy(p) != nil {
			return out, ErrInvalid
		}
		p.ID = 0
		p.Version = 0
		p.EffectiveAt = time.Time{}
		if p.PauseReasons == nil {
			p.PauseReasons = []string{}
		}
		raw, _ = json.Marshal(p)
	}
	hash := sha256.Sum256(raw)
	err := s.tx.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(e database.Executor) error {
		if err := incidents.NewRepository(s.db).LockActor(ctx, e, a); err != nil {
			return err
		}
		// Recheck live object-scoped administration permissions under the tenant lock.
		if err := s.authorizeAdmin(ctx, e, a); err != nil {
			return err
		}
		var old any
		now := s.now().UTC().Truncate(time.Microsecond)
		version := uint64(1)
		if id == 0 {
			var stored []byte
			var existing uint64
			err := e.QueryRowContext(ctx, `SELECT id,creation_hash FROM sla_definitions WHERE tenant_id=? AND kind=? AND request_key=?`, a.TenantID, kind, key).Scan(&existing, &stored)
			if err == nil {
				if subtle.ConstantTimeCompare(hash[:], stored) != 1 {
					return ErrConflict
				}
				out, err = s.Definition(ctx, e, a.TenantID, existing, 1)
				return err
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			var count int
			if err = e.QueryRowContext(ctx, `SELECT COUNT(*) FROM sla_definitions WHERE tenant_id=? AND kind=?`, a.TenantID, kind).Scan(&count); err != nil {
				return err
			}
			if count >= 100 {
				return fmt.Errorf("%w: limit 100 definicji każdego typu w organizacji", ErrInvalid)
			}
			result, err := e.ExecContext(ctx, `INSERT INTO sla_definitions(tenant_id,kind,request_key,creation_hash,created_at) VALUES(?,?,?,?,?)`, a.TenantID, kind, key, hash[:], now)
			if err != nil {
				return err
			}
			n, err := result.LastInsertId()
			if err != nil {
				return err
			}
			id = uint64(n)
		} else {
			current, err := s.Definition(ctx, e, a.TenantID, id, 0)
			if err != nil {
				return err
			}
			if current.Kind != kind {
				return ErrNotFound
			}
			if current.Version != expected {
				return ErrConflict
			}
			old = current
			version = expected + 1
			if !now.After(current.EffectiveAt) {
				now = current.EffectiveAt.Add(time.Microsecond)
			}
			if _, err = e.ExecContext(ctx, `UPDATE sla_definitions SET version=? WHERE tenant_id=? AND id=? AND version=?`, version, a.TenantID, id, expected); err != nil {
				return err
			}
		}
		if kind == "calendar" {
			c.ID = id
			c.Version = version
			raw, _ = json.Marshal(c)
		} else {
			cal, err := s.Definition(ctx, e, a.TenantID, p.CalendarID, p.CalendarVersion)
			if err != nil {
				return err
			}
			if cal.Kind != "calendar" {
				return ErrInvalid
			}
			var calendar Calendar
			if json.Unmarshal(cal.Config, &calendar) != nil {
				return ErrInvalid
			}
			// Verify both deadlines are representable before publishing a policy.
			if _, err = calendar.Add(now, p.ResponseSeconds); err != nil {
				return err
			}
			if _, err = calendar.Add(now, p.ResolutionSeconds); err != nil {
				return err
			}
			p.ID = id
			p.Version = version
			p.EffectiveAt = now
			raw, _ = json.Marshal(p)
		}
		if _, err := e.ExecContext(ctx, `INSERT INTO sla_definition_versions(tenant_id,definition_id,version,effective_at,config) VALUES(?,?,?,?,?)`, a.TenantID, id, version, now, raw); err != nil {
			return err
		}
		out = Definition{ID: id, Version: version, Kind: kind, EffectiveAt: now, Config: raw}
		return audit.NewMySQLRepository().Append(ctx, e, audit.Event{TenantID: &a.TenantID, ActorUserID: &a.UserID, Time: now, Action: "sla." + kind + ".saved", ObjectType: "sla_" + kind, ObjectID: fmt.Sprint(id), OldValues: old, NewValues: out, RequestID: a.RequestID, IPAddress: a.IPAddress, Source: "api"})
	})
	return out, err
}
