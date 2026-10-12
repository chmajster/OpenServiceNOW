package activity

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
)

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) Append(ctx context.Context, exec database.Executor, input Input) (uint64, bool, error) {
	input.ObjectType = strings.TrimSpace(input.ObjectType)
	input.ObjectID = strings.TrimSpace(input.ObjectID)
	input.Type = strings.TrimSpace(input.Type)
	input.Summary = strings.TrimSpace(input.Summary)
	if exec == nil || input.TenantID == 0 ||
		input.ObjectType == "" || len(input.ObjectType) > 100 ||
		input.ObjectID == "" || len(input.ObjectID) > 128 ||
		input.Type == "" || len(input.Type) > 100 ||
		input.Summary == "" || len([]rune(input.Summary)) > 500 ||
		(input.ActorUserID != nil && *input.ActorUserID == 0) ||
		(input.SourceEventID != nil && *input.SourceEventID == 0) {
		return 0, false, ErrInvalid
	}
	if len(input.Data) > 0 && !json.Valid(input.Data) {
		return 0, false, ErrInvalid
	}
	if input.Visibility == "" {
		input.Visibility = "INTERNAL"
	}
	if input.Visibility != "PUBLIC" && input.Visibility != "INTERNAL" {
		return 0, false, ErrInvalid
	}
	if input.EventTime.IsZero() {
		input.EventTime = time.Now().UTC()
	}
	var data any
	if len(input.Data) > 0 {
		data = []byte(input.Data)
	}

	if input.SourceEventID == nil {
		result, err := exec.ExecContext(ctx, `
			INSERT INTO activity_entries (
				tenant_id, object_type, object_id, entry_type, actor_user_id,
				summary, data, event_time, visibility
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, input.TenantID, input.ObjectType, input.ObjectID, input.Type,
			nullableUint64(input.ActorUserID), input.Summary, data, input.EventTime.UTC(), input.Visibility)
		if err != nil {
			return 0, false, fmt.Errorf("append activity entry: %w", err)
		}
		id, err := result.LastInsertId()
		if err != nil {
			return 0, false, fmt.Errorf("read activity id: %w", err)
		}
		return uint64(id), true, nil
	}

	result, err := exec.ExecContext(ctx, `
		INSERT INTO activity_entries (
			tenant_id, object_type, object_id, entry_type, actor_user_id,
			summary, data, source_event_id, event_time, visibility
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE id = LAST_INSERT_ID(id)
	`, input.TenantID, input.ObjectType, input.ObjectID, input.Type,
		nullableUint64(input.ActorUserID), input.Summary, data, *input.SourceEventID, input.EventTime.UTC(), input.Visibility)
	if err != nil {
		return 0, false, fmt.Errorf("append event-backed activity entry: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, false, fmt.Errorf("read activity append result: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, false, fmt.Errorf("read activity id: %w", err)
	}
	return uint64(id), affected == 1, nil
}

func (r *Repository) List(
	ctx context.Context,
	tenantID uint64,
	objectType, objectID string,
	beforeID uint64,
	limit int,
) (Page, error) {
	return r.list(ctx, tenantID, objectType, objectID, beforeID, limit, false, true)
}

// ListBySourceEvent follows transactional event order even if workers project
// concurrent events out of order. Its cursor identifies the source event.
func (r *Repository) ListBySourceEvent(ctx context.Context, tenantID uint64, objectType, objectID string, beforeEventID uint64, limit int) (Page, error) {
	return r.list(ctx, tenantID, objectType, objectID, beforeEventID, limit, true, true)
}

func (r *Repository) list(ctx context.Context, tenantID uint64, objectType, objectID string, beforeID uint64, limit int, bySource, internal bool) (Page, error) {
	objectType = strings.TrimSpace(objectType)
	objectID = strings.TrimSpace(objectID)
	if tenantID == 0 || objectType == "" || objectID == "" || limit < 1 || limit > 200 {
		return Page{}, ErrInvalid
	}
	query := `
		SELECT id, tenant_id, object_type, object_id, entry_type, actor_user_id,
		       summary, data, source_event_id, event_time, COALESCE((SELECT display_name FROM users WHERE users.id=activity_entries.actor_user_id),'')
		FROM activity_entries
		WHERE tenant_id = ? AND object_type = ? AND object_id = ?
	`
	args := []any{tenantID, objectType, objectID}
	if !internal {
		query += " AND visibility = 'PUBLIC' AND entry_type <> 'work_note' AND COALESCE(JSON_UNQUOTE(JSON_EXTRACT(data,'$.message_type')),'') <> 'WORK_NOTE'"
	}
	orderColumn := "id"
	if bySource {
		orderColumn = "source_event_id"
		query += " AND source_event_id IS NOT NULL"
	}
	if beforeID > 0 {
		query += " AND " + orderColumn + " < ?"
		args = append(args, beforeID)
	}
	query += " ORDER BY " + orderColumn + " DESC LIMIT ?"
	args = append(args, limit+1)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return Page{}, fmt.Errorf("list activity entries: %w", err)
	}
	defer rows.Close()

	entries := make([]Entry, 0, limit+1)
	for rows.Next() {
		entry, err := scanEntry(rows)
		if err != nil {
			return Page{}, fmt.Errorf("scan activity entry: %w", err)
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return Page{}, fmt.Errorf("iterate activity entries: %w", err)
	}
	page := Page{Entries: entries}
	if len(entries) > limit {
		page.Entries = entries[:limit]
		page.NextBeforeID = page.Entries[len(page.Entries)-1].ID
		if bySource {
			page.NextBeforeID = *page.Entries[len(page.Entries)-1].SourceEventID
		}
	}
	return page, nil
}

// ListVisibleBySourceEvent never returns hidden payloads, summaries or cursors.
func (r *Repository) ListVisibleBySourceEvent(ctx context.Context, tenantID uint64, objectType, objectID string, before uint64, limit int, internal bool) (Page, error) {
	return r.list(ctx, tenantID, objectType, objectID, before, limit, true, internal)
}

type rowScanner interface {
	Scan(...any) error
}

func scanEntry(row rowScanner) (Entry, error) {
	var entry Entry
	var actor, source sql.Null[uint64]
	var data []byte
	if err := row.Scan(
		&entry.ID,
		&entry.TenantID,
		&entry.ObjectType,
		&entry.ObjectID,
		&entry.Type,
		&actor,
		&entry.Summary,
		&data,
		&source,
		&entry.EventTime,
		&entry.ActorName,
	); err != nil {
		return Entry{}, err
	}
	if actor.Valid {
		value := actor.V
		entry.ActorUserID = &value
	}
	if len(data) > 0 {
		entry.Data = append(json.RawMessage(nil), data...)
	}
	if source.Valid {
		value := source.V
		entry.SourceEventID = &value
	}
	return entry, nil
}

func nullableUint64(value *uint64) any {
	if value == nil {
		return nil
	}
	return *value
}
