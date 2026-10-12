package sla

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/chmajster/OpenServiceNOW/internal/activity"
	"github.com/chmajster/OpenServiceNOW/internal/audit"
	"github.com/chmajster/OpenServiceNOW/internal/events"
	"github.com/chmajster/OpenServiceNOW/internal/incidents"
	"github.com/chmajster/OpenServiceNOW/internal/jobs"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
)

var sourceTypes = []string{"incident.created", "incident.updated", "incident.assigned", "incident.state.changed", "incident.reopened", "incident.resolved", "incident.closed", "incident.cancelled", "incident.priority.changed", "incident.first_response"}

func (s *Service) Register(d *events.Dispatcher, p *activity.Projector) error {
	for _, kind := range sourceTypes {
		if err := d.RegisterConsumer(ConsumerID, kind, s.HandleEvent); err != nil {
			return err
		}
	}
	for kind, label := range map[string]string{"sla.warning": "Zbliża się termin SLA", "sla.breached": "Przekroczono termin SLA"} {
		if err := d.Register(kind, p.Handler(kind, label)); err != nil {
			return err
		}
	}
	return nil
}
func enqueue(ctx context.Context, e database.Executor, db *sql.DB, tenant, id uint64, key string, at time.Time) error {
	raw, _ := json.Marshal(struct {
		IncidentID uint64 `json:"incident_id,string"`
	}{id})
	_, _, err := jobs.NewRepository(db).EnqueueInTx(ctx, e, jobs.EnqueueInput{TenantID: tenant, Type: JobType, Payload: raw, MaxAttempts: 5, AvailableAt: at, IdempotencyKey: key})
	return err
}
func (s *Service) HandleEvent(ctx context.Context, event events.Event) error {
	id, err := strconv.ParseUint(event.AggregateID, 10, 64)
	if err != nil || id == 0 || event.TenantID == 0 || event.AggregateType != "incident" || event.Version != 1 {
		return events.ErrInvalid
	}
	return s.tx.WithTx(ctx, nil, func(e database.Executor) error {
		return enqueue(ctx, e, s.db, event.TenantID, id, fmt.Sprintf("sla:event:%d", event.ID), s.now().UTC())
	})
}

// A bounded durable sweep discovers new records even if event delivery is late,
// and autonomously visits active/paused instances. Each incident has its own job.
func (s *Service) Schedule(ctx context.Context, now time.Time) (int, error) {
	count := 0
	err := s.tx.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(e database.Executor) error {
		rows, err := e.QueryContext(ctx, `SELECT i.tenant_id,i.id FROM incidents i LEFT JOIN sla_work w ON w.tenant_id=i.tenant_id AND w.incident_id=i.id WHERE (w.next_check_at<=? OR w.incident_id IS NULL AND EXISTS(SELECT 1 FROM sla_definitions d WHERE d.tenant_id=i.tenant_id AND d.kind='policy' AND d.created_at<=i.created_at)) ORDER BY COALESCE(w.next_check_at,i.created_at),i.tenant_id,i.id LIMIT 100`, now.UTC())
		if err != nil {
			return err
		}
		type ref struct{ tenant, id uint64 }
		refs := []ref{}
		for rows.Next() {
			r := ref{}
			if err = rows.Scan(&r.tenant, &r.id); err != nil {
				rows.Close()
				return err
			}
			refs = append(refs, r)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, r := range refs {
			if err = enqueue(ctx, e, s.db, r.tenant, r.id, fmt.Sprintf("sla:tick:%d:%d:%d", r.tenant, r.id, now.UTC().Unix()/30), now.UTC()); err != nil {
				return err
			}
			count++
		}
		return nil
	})
	return count, err
}
func (s *Service) JobHandler(ctx context.Context, j jobs.Job) (json.RawMessage, error) {
	var p struct {
		IncidentID uint64 `json:"incident_id,string"`
	}
	if json.Unmarshal(j.Payload, &p) != nil || p.IncidentID == 0 || j.TenantID == 0 {
		return nil, jobs.ErrInvalid
	}
	err := s.Reconcile(ctx, j.TenantID, p.IncidentID)
	return json.RawMessage(`{"reconciled":true}`), err
}
func (s *Service) policyAt(ctx context.Context, e database.Executor, tenant uint64, r incidents.Snapshot) (*Policy, Calendar, error) {
	// Latest version of every definition at the business start, including disabled
	// versions, then matching. Disabling cannot resurrect an older enabled version.
	rows, err := e.QueryContext(ctx, `SELECT v.config FROM sla_definitions d JOIN sla_definition_versions v ON v.definition_id=d.id WHERE d.tenant_id=? AND d.kind='policy' AND v.effective_at<=? AND NOT EXISTS(SELECT 1 FROM sla_definition_versions later WHERE later.definition_id=d.id AND later.effective_at<=? AND later.version>v.version) ORDER BY d.id LIMIT 101`, tenant, r.CreatedAt, r.CreatedAt)
	if err != nil {
		return nil, Calendar{}, err
	}
	policies := []Policy{}
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			rows.Close()
			return nil, Calendar{}, err
		}
		p := Policy{}
		if err = json.Unmarshal(raw, &p); err != nil {
			rows.Close()
			return nil, Calendar{}, err
		}
		policies = append(policies, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, Calendar{}, err
	}
	if len(policies) > 100 {
		return nil, Calendar{}, ErrInvalid
	}
	p := choose(policies, r.Priority, r.Category)
	if p == nil {
		return nil, Calendar{}, nil
	}
	d, err := s.Definition(ctx, e, tenant, p.CalendarID, p.CalendarVersion)
	if err != nil {
		return nil, Calendar{}, err
	}
	c := Calendar{}
	err = json.Unmarshal(d.Config, &c)
	return p, c, err
}

type source struct {
	id, version uint64
	kind        string
	at          time.Time
	raw         json.RawMessage
}

// All incident writes and this read hold the same tenant/incident locks. A later
// consumer may arrive first: we still fetch every committed preceding business
// transition, ordered by incident version, occurrence time and source ID.
func (s *Service) sources(ctx context.Context, e database.Executor, tenant, id uint64, c checkpoint) ([]source, error) {
	v := `CAST(JSON_UNQUOTE(JSON_EXTRACT(payload,'$.incident_version')) AS UNSIGNED)`
	t := `CAST(REPLACE(REPLACE(JSON_UNQUOTE(JSON_EXTRACT(payload,'$.occurred_at')),'T',' '),'Z','') AS DATETIME(6))`
	q := `SELECT id,event_type,payload,` + v + ` FROM outbox_events WHERE tenant_id=? AND aggregate_type='incident' AND aggregate_id=? AND event_type IN ('incident.created','incident.updated','incident.assigned','incident.state.changed','incident.reopened','incident.resolved','incident.closed','incident.cancelled','incident.priority.changed','incident.first_response')`
	args := []any{tenant, fmt.Sprint(id)}
	if c.CursorID > 0 {
		q += ` AND (` + v + `>? OR (` + v + `=? AND (` + t + `>? OR (` + t + `=? AND id>?))))`
		args = append(args, c.CursorVersion, c.CursorVersion, c.CursorTime, c.CursorTime, c.CursorID)
	}
	q += ` ORDER BY ` + v + `,` + t + `,id LIMIT 201`
	rows, err := e.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []source{}
	for rows.Next() {
		v := source{}
		if err = rows.Scan(&v.id, &v.kind, &v.raw, &v.version); err != nil {
			return nil, err
		}
		var p struct {
			At time.Time `json:"occurred_at"`
		}
		if err = json.Unmarshal(v.raw, &p); err != nil || p.At.IsZero() {
			return nil, events.ErrInvalid
		}
		v.at = p.At.UTC()
		result = append(result, v)
	}
	return result, rows.Err()
}
func (s *Service) Reconcile(ctx context.Context, tenant, id uint64) error {
	return s.tx.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(e database.Executor) error {
		var tenantStatus string
		if err := e.QueryRowContext(ctx, `SELECT status FROM tenants WHERE id=? FOR UPDATE`, tenant).Scan(&tenantStatus); err != nil {
			return err
		}
		if tenantStatus != "ACTIVE" {
			return nil
		}
		record, err := incidents.NewRepository(s.db).Get(ctx, e, tenant, id, true)
		if errors.Is(err, incidents.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if _, err = e.ExecContext(ctx, `INSERT INTO sla_work(tenant_id,incident_id,checkpoint) VALUES(?,?,'{}') ON DUPLICATE KEY UPDATE incident_id=incident_id`, tenant, id); err != nil {
			return err
		}
		var raw []byte
		if err = e.QueryRowContext(ctx, `SELECT checkpoint FROM sla_work WHERE tenant_id=? AND incident_id=? FOR UPDATE`, tenant, id).Scan(&raw); err != nil {
			return err
		}
		c := checkpoint{}
		if err = json.Unmarshal(raw, &c); err != nil {
			return err
		}
		sources, err := s.sources(ctx, e, tenant, id, c)
		if err != nil {
			return err
		}
		more := len(sources) > 200
		if more {
			sources = sources[:200]
		}
		for _, event := range sources {
			change := incidents.ChangeEvent{}
			if err = json.Unmarshal(event.raw, &change); err != nil {
				return err
			}
			if event.kind == "incident.created" {
				c.Policy, c.Calendar, err = s.policyAt(ctx, e, tenant, change.After)
				if err != nil {
					return err
				}
				if c.Policy != nil {
					c.Response, err = start(*c.Policy, "response", 1, id, 1, event.at, c.Calendar)
					if err != nil {
						return err
					}
					c.Resolution, err = start(*c.Policy, "resolution", 1, id, 1, event.at, c.Calendar)
					if err != nil {
						return err
					}
				}
			}
			if c.Policy != nil {
				if event.kind == "incident.reopened" && c.Resolution != nil && c.Resolution.FinishedAt != nil {
					c.Resolution, err = start(*c.Policy, "resolution", c.Resolution.Cycle+1, id, event.version, event.at, c.Calendar)
					if err != nil {
						return err
					}
				}
				for _, i := range []*Instance{c.Response, c.Resolution} {
					action := "started"
					if event.kind != "incident.created" && !(event.kind == "incident.reopened" && i.Kind == "resolution") {
						action, err = apply(i, c.Calendar, *c.Policy, event.kind, change.After, event.at, c.shouldPause(change.After))
						if err != nil {
							return err
						}
					}
					if err = s.saveInstance(ctx, e, tenant, i); err != nil {
						return err
					}
					if action == "" && change.Before != nil && (change.Before.Priority != change.After.Priority || change.Before.Category != change.After.Category) {
						action = "priority_preserved"
					}
					if action != "" {
						if err = s.history(ctx, e, tenant, i, event.id, action, event.at, change.After.HoldReason); err != nil {
							return err
						}
					}
					if err = s.thresholds(ctx, e, tenant, i); err != nil {
						return err
					}
				}
			}
			c.CursorID = event.id
			c.CursorVersion = event.version
			c.CursorTime = event.at
			c.IncidentVersion = event.version
			c.LastState = change.After.State
		}
		var next any
		now := s.now().UTC().Truncate(time.Microsecond)
		if c.Policy != nil && !more {
			for _, i := range []*Instance{c.Response, c.Resolution} {
				if now.Before(i.CalculatedAt) {
					now = i.CalculatedAt
				}
				if err = advance(i, c.Calendar, c.Policy.WarningPercent, now); err != nil {
					return err
				}
				if err = s.saveInstance(ctx, e, tenant, i); err != nil {
					return err
				}
				if err = s.thresholds(ctx, e, tenant, i); err != nil {
					return err
				}
				if i.Status == "ACTIVE" || i.Status == "PAUSED" {
					next = now.Add(30 * time.Second)
				}
			}
		}
		if more {
			next = now
			if err = enqueue(ctx, e, s.db, tenant, id, fmt.Sprintf("sla:continue:%d:%d", id, c.CursorID), now); err != nil {
				return err
			}
		}
		// With no matching policy there is deliberately no retroactive enrollment.
		raw, err = json.Marshal(c)
		if err != nil {
			return err
		}
		_, err = e.ExecContext(ctx, `UPDATE sla_work SET checkpoint=?,next_check_at=? WHERE tenant_id=? AND incident_id=?`, raw, next, tenant, record.ID)
		return err
	})
}
func (s *Service) saveInstance(ctx context.Context, e database.Executor, tenant uint64, i *Instance) error {
	raw, err := json.Marshal(i)
	if err != nil {
		return err
	}
	if i.ID == 0 {
		result, err := e.ExecContext(ctx, `INSERT INTO sla_instances(tenant_id,incident_id,kind,cycle,policy_id,policy_version,state) VALUES(?,?,?,?,?,?,?)`, tenant, i.IncidentID, i.Kind, i.Cycle, i.PolicyID, i.PolicyVersion, raw)
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
		i.Version++
	}
	raw, err = json.Marshal(i)
	if err != nil {
		return err
	}
	_, err = e.ExecContext(ctx, `UPDATE sla_instances SET state=?,version=? WHERE tenant_id=? AND id=?`, raw, i.Version, tenant, i.ID)
	return err
}
func (s *Service) history(ctx context.Context, e database.Executor, tenant uint64, i *Instance, eventID uint64, kind string, at time.Time, reason string) error {
	entry := History{InstanceID: i.ID, IncidentVersion: i.IncidentVersion, SourceEventID: eventID, Type: kind, OccurredAt: at.UTC(), AccruedSeconds: i.AccruedSeconds, Reason: reason, DueAt: i.DueAt}
	raw, _ := json.Marshal(entry)
	_, err := e.ExecContext(ctx, `INSERT INTO sla_history(tenant_id,instance_id,source_key,entry) VALUES(?,?,?,?) ON DUPLICATE KEY UPDATE id=id`, tenant, i.ID, fmt.Sprintf("%s:%d", kind, eventID), raw)
	return err
}
func (s *Service) thresholds(ctx context.Context, e database.Executor, tenant uint64, i *Instance) error {
	for _, v := range []struct {
		kind string
		at   *time.Time
	}{{"warning", i.WarnedAt}, {"breached", i.BreachedAt}} {
		if v.at == nil {
			continue
		}
		data := map[string]any{"occurred_at": v.at.UTC(), "visibility": "PUBLIC", "incident_version": fmt.Sprint(i.IncidentVersion), "sla_instance_id": fmt.Sprint(i.ID), "kind": i.Kind, "cycle": fmt.Sprint(i.Cycle), "policy_id": fmt.Sprint(i.PolicyID), "policy_version": fmt.Sprint(i.PolicyVersion), "calendar_id": fmt.Sprint(i.CalendarID), "calendar_version": fmt.Sprint(i.CalendarVersion), "due_at": i.DueAt, "target_seconds": i.TargetSeconds}
		raw, _ := json.Marshal(data)
		sourceID, created, err := events.NewRepository(s.db).Append(ctx, e, events.Input{TenantID: tenant, Type: "sla." + v.kind, AggregateType: "incident", AggregateID: fmt.Sprint(i.IncidentID), Payload: raw, IdempotencyKey: fmt.Sprintf("sla:%d:%s", i.ID, v.kind), AvailableAt: s.now().UTC()})
		if err != nil {
			return err
		}
		if created {
			if err = s.history(ctx, e, tenant, i, sourceID, v.kind, *v.at, ""); err != nil {
				return err
			}
			if err = audit.NewMySQLRepository().Append(ctx, e, audit.Event{TenantID: &tenant, Time: v.at.UTC(), Action: "sla." + v.kind, ObjectType: "incident", ObjectID: fmt.Sprint(i.IncidentID), NewValues: data, Source: "worker"}); err != nil {
				return err
			}
		}
	}
	return nil
}
