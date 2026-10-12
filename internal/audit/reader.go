package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

type Reader struct {
	db *sql.DB
}

func NewReader(db *sql.DB) *Reader {
	return &Reader{db: db}
}

func (r *Reader) List(ctx context.Context, tenantID, beforeID uint64, limit int) (Page, error) {
	return r.ListFiltered(ctx, tenantID, beforeID, limit, Filter{})
}

// Filter uses exact matches and an inclusive time range. Tenant scope is always
// supplied separately by the authenticated transport, never by query parameters.
type Filter struct {
	From, To                                time.Time
	ActorUserID                             uint64
	Action, ObjectType, ObjectID, RequestID string
}

func (r *Reader) ListFiltered(ctx context.Context, tenantID, beforeID uint64, limit int, filter Filter) (Page, error) {
	if limit < 1 || limit > 200 {
		return Page{}, fmt.Errorf("audit limit out of range")
	}

	query := `
		SELECT
			id, actor_user_id, event_time, action, object_type, object_id,
			old_values, new_values, request_id, source, ip_address
		FROM audit_events
		WHERE tenant_id = ?
	`
	args := []any{tenantID}
	for _, condition := range []struct {
		clause  string
		value   any
		enabled bool
	}{
		{"event_time >= ?", filter.From.UTC(), !filter.From.IsZero()},
		{"event_time <= ?", filter.To.UTC(), !filter.To.IsZero()},
		{"actor_user_id = ?", filter.ActorUserID, filter.ActorUserID != 0},
		{"action = ?", filter.Action, filter.Action != ""},
		{"object_type = ?", filter.ObjectType, filter.ObjectType != ""},
		{"object_id = ?", filter.ObjectID, filter.ObjectID != ""},
		{"request_id = ?", filter.RequestID, filter.RequestID != ""},
	} {
		if condition.enabled {
			query += " AND " + condition.clause
			args = append(args, condition.value)
		}
	}
	if beforeID > 0 {
		query += " AND id < ?"
		args = append(args, beforeID)
	}
	query += " ORDER BY id DESC LIMIT ?"
	args = append(args, limit+1)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return Page{}, fmt.Errorf("list audit events: %w", err)
	}
	defer rows.Close()

	events := make([]Record, 0, limit+1)
	for rows.Next() {
		var record Record
		var actorID sql.NullString
		var objectID, requestID, ipAddress sql.NullString
		var oldValues, newValues []byte
		if err := rows.Scan(
			&record.ID,
			&actorID,
			&record.Time,
			&record.Action,
			&record.ObjectType,
			&objectID,
			&oldValues,
			&newValues,
			&requestID,
			&record.Source,
			&ipAddress,
		); err != nil {
			return Page{}, fmt.Errorf("scan audit event: %w", err)
		}
		if actorID.Valid {
			value, err := strconv.ParseUint(actorID.String, 10, 64)
			if err != nil {
				return Page{}, fmt.Errorf("parse audit actor id: %w", err)
			}
			record.ActorUserID = &value
		}
		if objectID.Valid {
			record.ObjectID = objectID.String
		}
		if requestID.Valid {
			record.RequestID = requestID.String
		}
		if ipAddress.Valid {
			record.IPAddress = ipAddress.String
		}
		if len(oldValues) > 0 {
			record.OldValues = json.RawMessage(append([]byte(nil), oldValues...))
		}
		if len(newValues) > 0 {
			record.NewValues = json.RawMessage(append([]byte(nil), newValues...))
		}
		events = append(events, record)
	}
	if err := rows.Err(); err != nil {
		return Page{}, fmt.Errorf("iterate audit events: %w", err)
	}

	page := Page{Events: events}
	if len(events) > limit {
		page.Events = events[:limit]
		page.NextBeforeID = page.Events[len(page.Events)-1].ID
	}
	return page, nil
}
