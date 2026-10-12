package notifications

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/chmajster/OpenServiceNOW/internal/audit"
	"github.com/chmajster/OpenServiceNOW/internal/events"
	"github.com/chmajster/OpenServiceNOW/internal/identity"
	"github.com/chmajster/OpenServiceNOW/internal/incidents"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
)

type Service struct {
	db            *sql.DB
	tx            *database.TxManager
	key           []byte
	allowInsecure bool
}

func NewService(db *sql.DB, key []byte, allowInsecure bool) *Service {
	return &Service{db: db, tx: database.NewTxManager(db), key: append([]byte(nil), key...), allowInsecure: allowInsecure}
}
func (s *Service) currentActor(ctx context.Context, e database.Executor, tenant, user uint64) (incidents.Actor, error) {
	a := incidents.Actor{TenantID: tenant, UserID: user, Permissions: map[string]bool{}}
	var active bool
	if err := e.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users u JOIN tenant_memberships tm ON tm.user_id=u.id JOIN tenants t ON t.id=tm.tenant_id WHERE tm.tenant_id=? AND u.id=? AND tm.active=TRUE AND u.active=TRUE AND t.status='ACTIVE' AND u.password_change_required=FALSE)`, tenant, user).Scan(&active); err != nil {
		return a, err
	}
	if !active {
		return a, nil
	}
	keys, err := identity.NewMySQLRepository(s.db).ListPermissionsInTx(ctx, e, tenant, user)
	for _, key := range keys {
		a.Permissions[key] = true
	}
	return a, err
}
func (s *Service) Register(d *events.Dispatcher) error {
	for _, kind := range []string{"incident.assigned", "work.comment.added", "work.work_note.added", "incident.state.changed", "incident.reopened", "incident.resolved", "incident.closed", "incident.cancelled"} {
		if err := d.RegisterConsumer(ConsumerID, kind, s.HandleEvent); err != nil {
			return err
		}
	}
	return nil
}
func notificationKind(kind string) string {
	switch kind {
	case "incident.assigned":
		return "assignment"
	case "work.comment.added", "work.work_note.added":
		return "reply"
	case "incident.resolved":
		return "resolution"
	default:
		return "state"
	}
}
func kindEnabled(p Preference, kind string) bool {
	switch kind {
	case "assignment":
		return p.Assignments
	case "reply":
		return p.Replies
	case "resolution":
		return p.Resolutions
	default:
		return p.StateChanges
	}
}

func (s *Service) HandleEvent(ctx context.Context, event events.Event) error {
	if !event.NotificationEligible {
		return nil
	}
	id, err := strconv.ParseUint(event.AggregateID, 10, 64)
	if err != nil || id == 0 || event.AggregateType != "incident" || event.TenantID == 0 || event.Version != 1 {
		return events.ErrInvalid
	}
	var payload struct {
		Actor       uint64             `json:"actor_user_id,string"`
		OccurredAt  time.Time          `json:"occurred_at"`
		Visibility  string             `json:"visibility"`
		MessageType string             `json:"message_type"`
		After       incidents.Snapshot `json:"after"`
	}
	if err = json.Unmarshal(event.Payload, &payload); err != nil {
		return events.ErrInvalid
	}
	if payload.Actor == 0 || payload.OccurredAt.IsZero() {
		return events.ErrInvalid
	}
	internal := payload.Visibility == "INTERNAL" || payload.MessageType == "WORK_NOTE" || event.Type == "work.work_note.added"
	kind := notificationKind(event.Type)
	return s.tx.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(e database.Executor) error {
		// Same organization serialization as incident/RBAC writes. Recipients and
		// all MySQL effects are committed together, including the real SMTP jobs.
		var tenantState string
		if err := e.QueryRowContext(ctx, `SELECT status FROM tenants WHERE id=? FOR UPDATE`, event.TenantID).Scan(&tenantState); err != nil {
			return err
		}
		if tenantState != "ACTIVE" {
			return nil
		}
		record, err := incidents.NewRepository(s.db).Get(ctx, e, event.TenantID, id, false)
		if errors.Is(err, incidents.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		recipients := map[uint64]bool{}
		if kind == "assignment" {
			if payload.After.AssignedTo != nil {
				recipients[*payload.After.AssignedTo] = true
			}
		} else {
			recipients[record.ReporterID] = true
			recipients[record.AffectedUserID] = true
			if record.Assignment.AssignedTo != nil {
				recipients[*record.Assignment.AssignedTo] = true
			}
		}
		rows, err := e.QueryContext(ctx, `SELECT user_id FROM work_watchers WHERE tenant_id=? AND object_type='incident' AND object_id=? ORDER BY user_id LIMIT 100`, event.TenantID, event.AggregateID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var user uint64
			if err = rows.Scan(&user); err != nil {
				rows.Close()
				return err
			}
			recipients[user] = true
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for user := range recipients {
			if user == payload.Actor || user == 0 {
				continue
			}
			a, err := s.currentActor(ctx, e, event.TenantID, user)
			if err != nil {
				return err
			}
			if !incidents.CanRead(a, record) || internal && !incidents.CanReadNotes(a) {
				continue
			}
			p, err := s.preference(ctx, e, event.TenantID, user)
			if err != nil {
				return err
			}
			if !kindEnabled(p, kind) || !p.InApp && !p.SMTP {
				continue
			}
			// Templates intentionally contain no body, title, attachment name or note
			// payload. Even a later access revocation cannot reveal them in email.
			label := map[string]string{"assignment": "Przypisano Ci incydent", "reply": "Nowa odpowiedź w incydencie", "state": "Zmieniono stan incydentu", "resolution": "Rozwiązano incydent"}[kind]
			if kind == "assignment" && (payload.After.AssignedTo == nil || *payload.After.AssignedTo != user) {
				label = "Zmieniono przypisanie incydentu"
			}
			visibility := "PUBLIC"
			if internal {
				label = "Nowa notatka wewnętrzna w incydencie"
				visibility = "INTERNAL"
			}
			result, err := e.ExecContext(ctx, `INSERT INTO notifications(tenant_id,user_id,source_event_id,incident_id,kind,visibility,summary,in_app_enabled,occurred_at) VALUES(?,?,?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE id=id`, event.TenantID, user, event.ID, id, kind, visibility, label, p.InApp, payload.OccurredAt.UTC())
			if err != nil {
				return err
			}
			n, err := result.RowsAffected()
			if err != nil {
				return err
			}
			if n == 0 {
				continue
			}
			notificationID, err := result.LastInsertId()
			if err != nil {
				return err
			}
			if p.SMTP {
				if _, err = s.createMail(ctx, e, event.TenantID, user, uint64(notificationID)); err != nil {
					return err
				}
			}
		}
		return nil
	})
}
func (s *Service) preference(ctx context.Context, e database.Executor, tenant, user uint64) (Preference, error) {
	p := defaultPreference()
	err := e.QueryRowContext(ctx, `SELECT in_app_enabled,smtp_enabled,assignments,replies,state_changes,resolutions,version FROM notification_preferences WHERE tenant_id=? AND user_id=?`, tenant, user).Scan(&p.InApp, &p.SMTP, &p.Assignments, &p.Replies, &p.StateChanges, &p.Resolutions, &p.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return defaultPreference(), nil
	}
	return p, err
}
func (s *Service) Preference(ctx context.Context, tenant, user uint64) (Preference, error) {
	return s.preference(ctx, s.db, tenant, user)
}
func (s *Service) SavePreference(ctx context.Context, tenant, user uint64, p Preference) error {
	return s.tx.WithTx(ctx, nil, func(e database.Executor) error {
		_, err := e.ExecContext(ctx, `INSERT IGNORE INTO notification_preferences(tenant_id,user_id,version) VALUES(?,?,0)`, tenant, user)
		if err != nil {
			return err
		}
		result, err := e.ExecContext(ctx, `UPDATE notification_preferences SET in_app_enabled=?,smtp_enabled=?,assignments=?,replies=?,state_changes=?,resolutions=?,version=version+1 WHERE tenant_id=? AND user_id=? AND version=?`, p.InApp, p.SMTP, p.Assignments, p.Replies, p.StateChanges, p.Resolutions, tenant, user, p.Version)
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err == nil && n != 1 {
			return ErrConflict
		}
		return err
	})
}

// Filtering is performed before pagination and counting. A notification is not
// an access grant; current permissions apply to every later read or mutation.
func notificationAccess(a incidents.Actor) (string, []any) {
	clause := ` n.tenant_id=? AND n.user_id=? AND n.in_app_enabled=TRUE AND i.tenant_id=n.tenant_id `
	args := []any{a.TenantID, a.UserID}
	if !a.Has("read") {
		if !a.Has("read_own") {
			clause += " AND FALSE "
		} else {
			clause += ` AND (i.reporter_id=? OR i.affected_user_id=?) `
			args = append(args, a.UserID, a.UserID)
		}
	}
	if !incidents.CanReadNotes(a) {
		clause += ` AND n.visibility='PUBLIC' `
	}
	return clause, args
}
func (s *Service) List(ctx context.Context, tenant, user, before uint64, limit int) (Page, error) {
	if limit < 1 || limit > 100 {
		return Page{}, ErrInvalid
	}
	a, err := s.currentActor(ctx, s.db, tenant, user)
	if err != nil {
		return Page{}, err
	}
	clause, args := notificationAccess(a)
	p := Page{Notifications: []Notification{}}
	if err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM notifications n JOIN incidents i ON i.id=n.incident_id WHERE `+clause+` AND n.read_at IS NULL`, args...).Scan(&p.Unread); err != nil {
		return p, err
	}
	query := `SELECT n.id,n.source_event_id,n.incident_id,n.kind,n.summary,n.occurred_at,n.read_at FROM notifications n JOIN incidents i ON i.id=n.incident_id WHERE ` + clause
	if before > 0 {
		query += ` AND n.id<?`
		args = append(args, before)
	}
	query += ` ORDER BY n.id DESC LIMIT ?`
	args = append(args, limit+1)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return p, err
	}
	defer rows.Close()
	for rows.Next() {
		var n Notification
		var read sql.NullTime
		if err = rows.Scan(&n.ID, &n.SourceEventID, &n.IncidentID, &n.Kind, &n.Summary, &n.OccurredAt, &read); err != nil {
			return p, err
		}
		if read.Valid {
			n.ReadAt = &read.Time
		}
		p.Notifications = append(p.Notifications, n)
	}
	if err = rows.Err(); err != nil {
		return p, err
	}
	if len(p.Notifications) > limit {
		p.Notifications = p.Notifications[:limit]
		p.NextBeforeID = p.Notifications[limit-1].ID
	}
	return p, nil
}
func (s *Service) MarkRead(ctx context.Context, tenant, user, id uint64) error {
	a, err := s.currentActor(ctx, s.db, tenant, user)
	if err != nil {
		return err
	}
	clause, args := notificationAccess(a)
	if id > 0 {
		clause += ` AND n.id=?`
		args = append(args, id)
	}
	// A repeat is successful when the accessible row already has read_at.
	var exists bool
	if id > 0 {
		if err = s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM notifications n JOIN incidents i ON i.id=n.incident_id WHERE `+clause+`)`, args...).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return ErrNotFound
		}
	}
	_, err = s.db.ExecContext(ctx, `UPDATE notifications n JOIN incidents i ON i.id=n.incident_id SET n.read_at=COALESCE(n.read_at,UTC_TIMESTAMP(6)) WHERE `+clause, args...)
	return err
}
func auditChange(ctx context.Context, e database.Executor, tenant, user uint64, action string, id uint64) error {
	return audit.NewMySQLRepository().Append(ctx, e, audit.Event{TenantID: &tenant, ActorUserID: &user, Action: action, ObjectType: "notification", ObjectID: fmt.Sprint(id), Source: "api"})
}
