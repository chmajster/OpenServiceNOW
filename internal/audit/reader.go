package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

type Reader struct {
	db *sql.DB
}

func NewReader(db *sql.DB) *Reader {
	return &Reader{db: db}
}

func (r *Reader) List(ctx context.Context, tenantID, beforeID uint64, limit int) (Page, error) {
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
		var actorID sql.NullInt64
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
		if actorID.Valid && actorID.Int64 >= 0 {
			value := uint64(actorID.Int64)
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
