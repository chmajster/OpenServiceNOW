package incidents

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
)

type Repository struct{ db *sql.DB }

func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }
func (r *Repository) LockActor(ctx context.Context, exec database.Executor, a Actor) error {
	if a.TenantID == 0 || a.UserID == 0 {
		return ErrForbidden
	}
	var status string
	if err := exec.QueryRowContext(ctx, "SELECT status FROM tenants WHERE id=? FOR UPDATE", a.TenantID).Scan(&status); err != nil {
		return err
	}
	if status != "ACTIVE" {
		return ErrForbidden
	}
	if err := workrecords.ValidateActiveUser(ctx, exec, a.TenantID, a.UserID); err != nil {
		if errors.Is(err, workrecords.ErrNotFound) {
			return ErrForbidden
		}
		return err
	}
	return nil
}

// The reservation and record are committed together. The tenant/actor/key scope
// serializes retries without exposing another actor's incident.
func (r *Repository) Reserve(ctx context.Context, exec database.Executor, a Actor, key string, digest [32]byte) (uint64, error) {
	if _, err := exec.ExecContext(ctx, `INSERT INTO incident_requests(tenant_id,actor_user_id,request_key,request_hash) VALUES(?,?,?,?) ON DUPLICATE KEY UPDATE request_key=incident_requests.request_key`, a.TenantID, a.UserID, key, digest[:]); err != nil {
		return 0, err
	}
	var stored []byte
	var id sql.Null[uint64]
	if err := exec.QueryRowContext(ctx, `SELECT request_hash,incident_id FROM incident_requests WHERE tenant_id=? AND actor_user_id=? AND request_key=? FOR UPDATE`, a.TenantID, a.UserID, key).Scan(&stored, &id); err != nil {
		return 0, err
	}
	if subtle.ConstantTimeCompare(stored, digest[:]) != 1 {
		return 0, ErrIdempotency
	}
	return id.V, nil
}
func (r *Repository) CompleteRequest(ctx context.Context, exec database.Executor, a Actor, key string, id uint64) error {
	_, err := exec.ExecContext(ctx, `UPDATE incident_requests SET incident_id=? WHERE tenant_id=? AND actor_user_id=? AND request_key=?`, id, a.TenantID, a.UserID, key)
	return err
}
func (r *Repository) Insert(ctx context.Context, exec database.Executor, item Record) (uint64, error) {
	result, err := exec.ExecContext(ctx, `INSERT INTO incidents(tenant_id,number,title,description,reporter_id,affected_user_id,category,subcategory,impact,urgency,priority,resolution_description,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,'',?,?)`, item.TenantID, item.Number, item.Title, item.Description, item.ReporterID, item.AffectedUserID, item.Category, item.Subcategory, item.Impact, item.Urgency, item.Priority, item.CreatedAt, item.UpdatedAt)
	if err != nil {
		return 0, err
	}
	id, err := result.LastInsertId()
	return uint64(id), err
}
func (r *Repository) Save(ctx context.Context, exec database.Executor, item Record, expected uint64) error {
	result, err := exec.ExecContext(ctx, `UPDATE incidents SET title=?,description=?,affected_user_id=?,category=?,subcategory=?,impact=?,urgency=?,priority=?,priority_override=?,priority_reason=?,state=?,hold_reason=?,resolution_code=?,resolution_description=?,cancellation_reason=?,version=version+1,updated_at=?,started_at=?,resolved_at=?,closed_at=?,cancelled_at=? WHERE tenant_id=? AND id=? AND version=?`, item.Title, item.Description, item.AffectedUserID, item.Category, item.Subcategory, item.Impact, item.Urgency, item.Priority, item.PriorityOverride, item.PriorityReason, item.State, item.HoldReason, item.ResolutionCode, item.ResolutionDescription, item.CancellationReason, item.UpdatedAt, item.StartedAt, item.ResolvedAt, item.ClosedAt, item.CancelledAt, item.TenantID, item.ID, expected)
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
	return nil
}

const recordColumns = `i.id,i.tenant_id,i.number,i.title,i.description,i.reporter_id,reporter.display_name,i.affected_user_id,affected.display_name,i.category,i.subcategory,i.impact,i.urgency,i.priority,i.priority_override,i.priority_reason,i.state,i.hold_reason,i.resolution_code,i.resolution_description,i.cancellation_reason,i.version,i.created_at,i.updated_at,i.started_at,i.resolved_at,i.closed_at,i.cancelled_at,w.assigned_to,w.assignment_group_id,w.version,w.updated_at,COALESCE(assigned.display_name,''),COALESCE(g.name,'')`
const recordJoins = " FROM incidents i JOIN users reporter ON reporter.id=i.reporter_id JOIN users affected ON affected.id=i.affected_user_id JOIN work_assignments w ON w.tenant_id=i.tenant_id AND w.object_type='incident' AND w.object_id=CAST(i.id AS CHAR) LEFT JOIN users assigned ON assigned.id=w.assigned_to LEFT JOIN `groups` g ON g.tenant_id=w.tenant_id AND g.id=w.assignment_group_id "

func (r *Repository) Get(ctx context.Context, exec database.Executor, tenantID, id uint64, lock bool) (Record, error) {
	if lock {
		var found uint64
		err := exec.QueryRowContext(ctx, "SELECT id FROM incidents WHERE tenant_id=? AND id=? FOR UPDATE", tenantID, id).Scan(&found)
		if errors.Is(err, sql.ErrNoRows) {
			return Record{}, ErrNotFound
		}
		if err != nil {
			return Record{}, err
		}
	}
	return scanRecord(exec.QueryRowContext(ctx, "SELECT "+recordColumns+recordJoins+" WHERE i.tenant_id=? AND i.id=?", tenantID, id))
}

type scanner interface{ Scan(...any) error }

func scanRecord(row scanner) (Record, error) {
	var item Record
	var assigned, group sql.Null[uint64]
	var override sql.Null[int]
	var started, resolved, closed, cancelled sql.NullTime
	err := row.Scan(&item.ID, &item.TenantID, &item.Number, &item.Title, &item.Description, &item.ReporterID, &item.ReporterName, &item.AffectedUserID, &item.AffectedUserName, &item.Category, &item.Subcategory, &item.Impact, &item.Urgency, &item.Priority, &override, &item.PriorityReason, &item.State, &item.HoldReason, &item.ResolutionCode, &item.ResolutionDescription, &item.CancellationReason, &item.Version, &item.CreatedAt, &item.UpdatedAt, &started, &resolved, &closed, &cancelled, &assigned, &group, &item.Assignment.Version, &item.Assignment.UpdatedAt, &item.AssignedName, &item.GroupName)
	if errors.Is(err, sql.ErrNoRows) {
		return Record{}, ErrNotFound
	}
	if err != nil {
		return Record{}, err
	}
	item.Assignment.TenantID = item.TenantID
	item.Assignment.ObjectType = "incident"
	item.Assignment.ObjectID = fmt.Sprint(item.ID)
	if assigned.Valid {
		item.Assignment.AssignedTo = &assigned.V
	}
	if group.Valid {
		item.Assignment.AssignmentGroupID = &group.V
	}
	if override.Valid {
		item.PriorityOverride = &override.V
	}
	if started.Valid {
		item.StartedAt = &started.Time
	}
	if resolved.Valid {
		item.ResolvedAt = &resolved.Time
	}
	if closed.Valid {
		item.ClosedAt = &closed.Time
	}
	if cancelled.Valid {
		item.CancelledAt = &cancelled.Time
	}
	item.CalculatedPriority, _ = CalculatePriority(item.Impact, item.Urgency)
	return item, nil
}
func (r *Repository) ValidatePeople(ctx context.Context, exec database.Executor, a Actor, reporter, affected uint64) error {
	if affected == 0 {
		return fmt.Errorf("%w: wybierz osobę dotkniętą problemem", ErrInvalid)
	}
	for _, id := range []uint64{reporter, affected} {
		if err := workrecords.ValidateActiveUser(ctx, exec, a.TenantID, id); err != nil {
			if errors.Is(err, workrecords.ErrNotFound) {
				return fmt.Errorf("%w: użytkownik musi mieć aktywne konto i członkostwo w tej organizacji", ErrInvalid)
			}
			return err
		}
	}
	return nil
}
func (r *Repository) ValidateGroupMember(ctx context.Context, exec database.Executor, tenantID uint64, assigned, group *uint64) error {
	if assigned == nil || group == nil {
		return nil
	}
	var found bool
	if err := exec.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM group_memberships WHERE tenant_id=? AND group_id=? AND user_id=?)`, tenantID, *group, *assigned).Scan(&found); err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("%w: przypisany użytkownik musi należeć do wybranej grupy", ErrInvalid)
	}
	return nil
}
func (r *Repository) List(ctx context.Context, a Actor, f Filter) (Page, error) {
	columns := strings.ReplaceAll(strings.ReplaceAll(recordColumns, "i.description,", "'',"), "i.resolution_description,", "'',")
	where, args := listWhere(a, f)
	query := "SELECT " + columns + recordJoins + where
	if f.AfterID > 0 {
		switch f.Sort {
		case "oldest":
			query += " AND i.id>?"
			args = append(args, f.AfterID)
		case "priority":
			query += " AND (i.priority>? OR (i.priority=? AND i.id<?))"
			args = append(args, f.AfterPriority, f.AfterPriority, f.AfterID)
		default:
			query += " AND i.id<?"
			args = append(args, f.AfterID)
		}
	}
	switch f.Sort {
	case "oldest":
		query += " ORDER BY i.id ASC"
	case "priority":
		query += " ORDER BY i.priority ASC,i.id DESC"
	default:
		query += " ORDER BY i.id DESC"
	}
	query += " LIMIT ?"
	args = append(args, f.Limit+1)
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return Page{}, err
	}
	defer rows.Close()
	page := Page{Incidents: []Summary{}}
	for rows.Next() {
		item, err := scanRecord(rows)
		if err != nil {
			return Page{}, err
		}
		page.Incidents = append(page.Incidents, summary(item))
	}
	if err := rows.Err(); err != nil {
		return Page{}, err
	}
	if len(page.Incidents) > f.Limit {
		page.Incidents = page.Incidents[:f.Limit]
		last := page.Incidents[f.Limit-1]
		page.NextAfterID = last.ID
		if f.Sort == "priority" {
			page.NextPriority = last.Priority
		}
	}
	return page, nil
}

// Counts and rows use the same object scope and filters, before pagination.
func listWhere(a Actor, f Filter) (string, []any) {
	query := " WHERE i.tenant_id=?"
	args := []any{a.TenantID}
	if !a.Has("read") {
		query += " AND (i.reporter_id=? OR i.affected_user_id=?)"
		args = append(args, a.UserID, a.UserID)
	}
	switch f.View {
	case "mine":
		query += " AND (i.reporter_id=? OR i.affected_user_id=? OR w.assigned_to=?)"
		args = append(args, a.UserID, a.UserID, a.UserID)
	case "my_group":
		query += " AND g.active=TRUE AND EXISTS(SELECT 1 FROM group_memberships gm WHERE gm.tenant_id=i.tenant_id AND gm.group_id=w.assignment_group_id AND gm.user_id=?)"
		args = append(args, a.UserID)
	case "assigned":
		query += " AND w.assigned_to=?"
		args = append(args, a.UserID)
	case "unassigned":
		query += " AND w.assigned_to IS NULL"
	}
	for _, condition := range []struct {
		sql     string
		value   any
		enabled bool
	}{
		{"i.state=?", f.State, f.State != ""}, {"i.category=?", f.Category, f.Category != ""}, {"i.priority=?", f.Priority, f.Priority != 0},
		{"w.assigned_to=?", f.AssignedTo, f.AssignedTo != 0}, {"w.assignment_group_id=?", f.GroupID, f.GroupID != 0}, {"i.reporter_id=?", f.ReporterID, f.ReporterID != 0},
		{"i.created_at>=?", f.From.UTC(), !f.From.IsZero()}, {"i.created_at<=?", f.To.UTC(), !f.To.IsZero()},
	} {
		if condition.enabled {
			query += " AND " + condition.sql
			args = append(args, condition.value)
		}
	}
	if f.Query != "" {
		query += " AND (i.number LIKE ? ESCAPE '!' OR i.title LIKE ? ESCAPE '!')"
		escaped := strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(f.Query)
		args = append(args, "%"+escaped+"%", "%"+escaped+"%")
	}
	if f.Active {
		query += " AND i.state NOT IN ('RESOLVED','CLOSED','CANCELLED')"
	}
	if f.SLARisk {
		query += ` AND EXISTS(SELECT 1 FROM sla_instances si WHERE si.tenant_id=i.tenant_id AND si.incident_id=i.id AND JSON_UNQUOTE(JSON_EXTRACT(si.state,'$.status')) IN ('ACTIVE','PAUSED') AND (JSON_EXTRACT(si.state,'$.warned_at') IS NOT NULL AND JSON_TYPE(JSON_EXTRACT(si.state,'$.warned_at'))<>'NULL' OR JSON_EXTRACT(si.state,'$.breached_at') IS NOT NULL AND JSON_TYPE(JSON_EXTRACT(si.state,'$.breached_at'))<>'NULL'))`
	}
	return query, args
}

func (r *Repository) Count(ctx context.Context, a Actor, f Filter) (uint64, error) {
	where, args := listWhere(a, f)
	var n uint64
	err := r.db.QueryRowContext(ctx, "SELECT COUNT(*)"+recordJoins+where, args...).Scan(&n)
	return n, err
}
