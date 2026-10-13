package changes

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/chmajster/OpenServiceNOW/internal/catalog"
	"github.com/chmajster/OpenServiceNOW/internal/cmdb"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"time"
)

type CalendarRule struct {
	ID       uint64    `json:"id,string"`
	Version  uint64    `json:"version,string"`
	CIID     *uint64   `json:"ci_id,string"`
	Kind     string    `json:"kind"`
	Title    string    `json:"title"`
	StartsAt time.Time `json:"starts_at"`
	EndsAt   time.Time `json:"ends_at"`
	TimeZone string    `json:"time_zone"`
	Active   bool      `json:"active"`
}
type Collision struct {
	Items              []cmdb.Reference `json:"items"`
	RestrictedConflict bool             `json:"restricted_conflict"`
	Blackout           bool             `json:"blackout"`
	OutsideWindow      bool             `json:"outside_window"`
}

func (c Collision) blocked() bool {
	return len(c.Items) > 0 || c.RestrictedConflict || c.Blackout || c.OutsideWindow
}
func interval(from, to time.Time, max time.Duration) error {
	if !to.After(from) || to.Sub(from) > max || from.Year() < 2000 || to.Year() > 2100 {
		return ErrInvalid
	}
	return nil
}
func (s *Service) collisions(ctx context.Context, e database.Executor, a Actor, r Record) (Collision, error) {
	out := Collision{Items: []cmdb.Reference{}}
	if r.PlannedStart == nil || r.PlannedEnd == nil {
		return out, ErrInvalid
	}
	// The safety predicate considers every Change in this tenant. The projection
	// applies resource ACL before returning any number, title or identifier.
	rows, err := e.QueryContext(ctx, "SELECT DISTINCT ch.id FROM changes ch JOIN cmdb_change_links other ON other.tenant_id=ch.tenant_id AND other.change_id=ch.id JOIN cmdb_change_links own ON own.tenant_id=other.tenant_id AND own.ci_id=other.ci_id AND own.change_id=? WHERE ch.tenant_id=? AND ch.id<>? AND ch.state IN('SCHEDULED','IMPLEMENT','REVIEW') AND ch.planned_start<? AND ch.planned_end>? ORDER BY ch.id LIMIT 1001", r.ID, a.TenantID, r.ID, r.PlannedEnd, r.PlannedStart)
	if err != nil {
		return out, err
	}
	ids := []uint64{}
	for rows.Next() {
		var id uint64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return out, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	if len(ids) > 1000 {
		return out, fmt.Errorf("%w: przedział obejmuje zbyt wiele kolizji", ErrInvalid)
	}
	for _, id := range ids {
		p, args := Access(a, "p")
		params := append([]any{a.TenantID, id}, args...)
		var ref cmdb.Reference
		err = e.QueryRowContext(ctx, "SELECT p.id,p.number,p.title,p.state,p.version"+joins+" WHERE p.tenant_id=? AND p.id=? AND "+p, params...).Scan(&ref.ID, &ref.Number, &ref.Title, &ref.State, &ref.Version)
		if err == nil {
			out.Items = append(out.Items, ref)
		} else if missing(err) == ErrNotFound {
			out.RestrictedConflict = true
		} else {
			return out, err
		}
	}
	err = e.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM change_calendar_rules cr WHERE cr.tenant_id=? AND cr.active=TRUE AND cr.kind='BLACKOUT' AND cr.starts_at<? AND cr.ends_at>? AND (cr.ci_id IS NULL OR EXISTS(SELECT 1 FROM cmdb_change_links l WHERE l.tenant_id=cr.tenant_id AND l.change_id=? AND l.ci_id=cr.ci_id)))", a.TenantID, r.PlannedEnd, r.PlannedStart, r.ID).Scan(&out.Blackout)
	if err != nil {
		return out, err
	}
	// Once windows are configured for a scope, the whole half-open interval must
	// fit one of its windows. Global and per-CI restrictions both apply.
	err = e.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM change_calendar_rules cr WHERE cr.tenant_id=? AND cr.active=TRUE AND cr.kind='WINDOW' AND (cr.ci_id IS NULL OR EXISTS(SELECT 1 FROM cmdb_change_links l WHERE l.tenant_id=cr.tenant_id AND l.change_id=? AND l.ci_id=cr.ci_id)) AND NOT EXISTS(SELECT 1 FROM change_calendar_rules allowed WHERE allowed.tenant_id=cr.tenant_id AND allowed.active=TRUE AND allowed.kind='WINDOW' AND allowed.ci_id<=>cr.ci_id AND allowed.starts_at<=? AND allowed.ends_at>=?))", a.TenantID, r.ID, r.PlannedStart, r.PlannedEnd).Scan(&out.OutsideWindow)
	return out, err
}
func (s *Service) Collisions(ctx context.Context, a Actor, id uint64) (Collision, error) {
	r, err := s.Get(ctx, a, id)
	if err != nil {
		return Collision{}, err
	}
	return s.collisions(ctx, s.db, a, r)
}
func (s *Service) reserveSchedule(ctx context.Context, e database.Executor, a Actor, r Record, override bool, reason string) error {
	// actorTx holds the tenant row shared with all Change, CI and rule writes.
	// Checking and changing SCHEDULED therefore cannot race another reservation.
	if r.PlannedStart == nil || !r.PlannedStart.After(time.Now().UTC()) {
		return fmt.Errorf("%w: planowany początek musi być w przyszłości", ErrTransition)
	}
	c, err := s.collisions(ctx, e, a, r)
	if err != nil {
		return err
	}
	if override {
		if !a.Has("change.schedule_override") {
			return ErrForbidden
		}
		if !catalog.Text(reason, 1, 10000) {
			return ErrInvalid
		}
		return catalog.Audit(ctx, e, a, "change.schedule.override", "change", r.ID, map[string]any{"reason": reason, "restricted_conflict": c.RestrictedConflict, "blackout": c.Blackout, "outside_window": c.OutsideWindow})
	}
	if c.blocked() {
		return ErrCollision
	}
	return nil
}
func (s *Service) SaveRule(ctx context.Context, a Actor, id uint64, in CalendarRule, key string) (CalendarRule, error) {
	var out CalendarRule
	if in.ID != 0 && in.ID != id || !catalog.Text(in.Title, 1, 160) || !contains([]string{"WINDOW", "BLACKOUT"}, in.Kind) || in.CIID != nil && *in.CIID == 0 {
		return out, ErrInvalid
	}
	if err := interval(in.StartsAt, in.EndsAt, 366*24*time.Hour); err != nil {
		return out, err
	}
	if _, err := time.LoadLocation(in.TimeZone); err != nil || in.TimeZone == "Local" {
		return out, ErrInvalid
	}
	in.StartsAt = in.StartsAt.UTC()
	in.EndsAt = in.EndsAt.UTC()
	raw, _ := json.Marshal(struct {
		ID    uint64
		Input CalendarRule
	}{id, in})
	err := s.actorTx(ctx, a, func(e database.Executor, a Actor) error {
		if !a.Has("change.calendar") {
			return ErrForbidden
		}
		old, err := reserve(ctx, e, a, key, "change_calendar_rule", sha256.Sum256(raw))
		if err != nil {
			return err
		}
		if old > 0 {
			out, err = rule(ctx, e, a.TenantID, old)
			return err
		}
		if in.CIID != nil {
			service := cmdb.NewService(s.db, nil, nil)
			if _, err = service.GetInTx(ctx, e, a, *in.CIID, false); err != nil {
				return err
			}
		}
		if id == 0 {
			if in.Version != 0 {
				return ErrInvalid
			}
			var count int
			if err = e.QueryRowContext(ctx, "SELECT COUNT(*) FROM change_calendar_rules WHERE tenant_id=?", a.TenantID).Scan(&count); err != nil {
				return err
			}
			if count >= 1000 {
				return ErrInvalid
			}
			res, err := e.ExecContext(ctx, "INSERT INTO change_calendar_rules(tenant_id,ci_id,kind,title,starts_at,ends_at,time_zone,active) VALUES(?,?,?,?,?,?,?,?)", a.TenantID, in.CIID, in.Kind, in.Title, in.StartsAt, in.EndsAt, in.TimeZone, in.Active)
			if err != nil {
				return err
			}
			n, err := res.LastInsertId()
			if err != nil {
				return err
			}
			id = uint64(n)
		} else {
			before, err := rule(ctx, e, a.TenantID, id)
			if err != nil {
				return err
			}
			if in.Version == 0 || in.Version != before.Version {
				return ErrConflict
			}
			if _, err = e.ExecContext(ctx, "UPDATE change_calendar_rules SET ci_id=?,kind=?,title=?,starts_at=?,ends_at=?,time_zone=?,active=?,version=version+1 WHERE tenant_id=? AND id=? AND version=?", in.CIID, in.Kind, in.Title, in.StartsAt, in.EndsAt, in.TimeZone, in.Active, a.TenantID, id, in.Version); err != nil {
				return err
			}
		}
		if err = catalog.Audit(ctx, e, a, "change.calendar.rule.saved", "change_calendar_rule", id, map[string]any{"kind": in.Kind, "starts_at": in.StartsAt, "ends_at": in.EndsAt, "active": in.Active}); err != nil {
			return err
		}
		out, err = rule(ctx, e, a.TenantID, id)
		if err != nil {
			return err
		}
		return complete(ctx, e, a, key, id)
	})
	return out, err
}
func rule(ctx context.Context, e database.Executor, tenant, id uint64) (CalendarRule, error) {
	var r CalendarRule
	err := e.QueryRowContext(ctx, "SELECT id,version,ci_id,kind,title,starts_at,ends_at,time_zone,active FROM change_calendar_rules WHERE tenant_id=? AND id=?", tenant, id).Scan(&r.ID, &r.Version, &r.CIID, &r.Kind, &r.Title, &r.StartsAt, &r.EndsAt, &r.TimeZone, &r.Active)
	return r, missing(err)
}

type Calendar struct {
	Changes []Record       `json:"changes"`
	Rules   []CalendarRule `json:"rules"`
}

func (s *Service) Calendar(ctx context.Context, a Actor, from, to time.Time) (Calendar, error) {
	out := Calendar{Changes: []Record{}, Rules: []CalendarRule{}}
	if !a.Has("change.read") && !a.Has("change.read_own") {
		return out, ErrForbidden
	}
	if err := interval(from, to, 93*24*time.Hour); err != nil {
		return out, err
	}
	p, args := Access(a, "p")
	params := append([]any{a.TenantID, from.UTC(), to.UTC()}, args...)
	rows, err := s.db.QueryContext(ctx, "SELECT "+listColumns+joins+" WHERE p.tenant_id=? AND p.planned_end>? AND p.planned_start<? AND p.state NOT IN('NEW','ASSESS','CANCELLED') AND "+p+" ORDER BY p.planned_start,p.id LIMIT 1001", params...)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		r, err := scan(rows, a)
		if err != nil {
			rows.Close()
			return out, err
		}
		out.Changes = append(out.Changes, r)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	if len(out.Changes) > 1000 {
		return out, fmt.Errorf("%w: skróć przedział kalendarza", ErrInvalid)
	}
	// CI-specific rules are visible only if that CI is readable.
	ci, args := cmdb.Access(a, "ci")
	params = append([]any{a.TenantID, from.UTC(), to.UTC()}, args...)
	rows, err = s.db.QueryContext(ctx, "SELECT cr.id,cr.version,cr.ci_id,cr.kind,cr.title,cr.starts_at,cr.ends_at,cr.time_zone,cr.active FROM change_calendar_rules cr LEFT JOIN cmdb_cis ci ON ci.tenant_id=cr.tenant_id AND ci.id=cr.ci_id WHERE cr.tenant_id=? AND cr.ends_at>? AND cr.starts_at<? AND cr.active=TRUE AND (cr.ci_id IS NULL OR "+ci+") ORDER BY cr.starts_at,cr.id LIMIT 1000", params...)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var r CalendarRule
		if err = rows.Scan(&r.ID, &r.Version, &r.CIID, &r.Kind, &r.Title, &r.StartsAt, &r.EndsAt, &r.TimeZone, &r.Active); err != nil {
			return out, err
		}
		out.Rules = append(out.Rules, r)
	}
	return out, rows.Err()
}
