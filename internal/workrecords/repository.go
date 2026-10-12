package workrecords

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
)

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) GetAssignment(
	ctx context.Context,
	tenantID uint64,
	objectType, objectID string,
) (Assignment, error) {
	return r.getAssignment(ctx, r.db, tenantID, objectType, objectID, false)
}

func (r *Repository) SetAssignment(
	ctx context.Context,
	exec database.Executor,
	tenantID uint64,
	objectType, objectID string,
	expectedVersion uint64,
	assignedTo, groupID *uint64,
) (AssignmentChange, error) {
	objectType = strings.TrimSpace(objectType)
	objectID = strings.TrimSpace(objectID)
	if exec == nil || !validObjectRef(tenantID, objectType, objectID) ||
		(assignedTo != nil && *assignedTo == 0) || (groupID != nil && *groupID == 0) {
		return AssignmentChange{}, ErrInvalid
	}
	if err := validateTargets(ctx, exec, tenantID, assignedTo, groupID); err != nil {
		return AssignmentChange{}, err
	}

	if expectedVersion == 0 {
		result, err := exec.ExecContext(ctx, `
			INSERT INTO work_assignments (
				tenant_id, object_type, object_id, assigned_to, assignment_group_id, version
			) VALUES (?, ?, ?, ?, ?, 1)
			ON DUPLICATE KEY UPDATE version = work_assignments.version
		`, tenantID, objectType, objectID, nullableUint64(assignedTo), nullableUint64(groupID))
		if err != nil {
			return AssignmentChange{}, fmt.Errorf("create work assignment: %w", err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return AssignmentChange{}, fmt.Errorf("read work assignment result: %w", err)
		}
		if affected != 1 {
			return AssignmentChange{}, ErrConflict
		}
		after, err := r.getAssignment(ctx, exec, tenantID, objectType, objectID, false)
		return AssignmentChange{After: after, Changed: err == nil}, err
	}

	before, err := r.getAssignment(ctx, exec, tenantID, objectType, objectID, true)
	if err != nil {
		return AssignmentChange{}, err
	}
	if before.Version != expectedVersion {
		return AssignmentChange{}, ErrConflict
	}
	if optionalUint64Equal(before.AssignedTo, assignedTo) &&
		optionalUint64Equal(before.AssignmentGroupID, groupID) {
		return AssignmentChange{Before: &before, After: before, Changed: false}, nil
	}
	result, err := exec.ExecContext(ctx, `
		UPDATE work_assignments
		SET assigned_to = ?, assignment_group_id = ?, version = version + 1
		WHERE tenant_id = ? AND object_type = ? AND object_id = ? AND version = ?
	`, nullableUint64(assignedTo), nullableUint64(groupID),
		tenantID, objectType, objectID, expectedVersion)
	if err != nil {
		return AssignmentChange{}, fmt.Errorf("update work assignment: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return AssignmentChange{}, fmt.Errorf("read assignment update result: %w", err)
	}
	if affected != 1 {
		return AssignmentChange{}, ErrConflict
	}
	after, err := r.getAssignment(ctx, exec, tenantID, objectType, objectID, false)
	if err != nil {
		return AssignmentChange{}, err
	}
	return AssignmentChange{Before: &before, After: after, Changed: true}, nil
}

func (r *Repository) AppendMessage(
	ctx context.Context,
	exec database.Executor,
	tenantID uint64,
	objectType, objectID string,
	messageType MessageType,
	body string,
	authorUserID uint64,
) (Message, error) {
	objectType = strings.TrimSpace(objectType)
	objectID = strings.TrimSpace(objectID)
	body = strings.TrimSpace(body)
	if exec == nil || !validObjectRef(tenantID, objectType, objectID) ||
		(messageType != MessageComment && messageType != MessageWorkNote) ||
		body == "" || len([]rune(body)) > 20000 || authorUserID == 0 {
		return Message{}, ErrInvalid
	}
	if err := validateActiveUser(ctx, exec, tenantID, authorUserID); err != nil {
		return Message{}, err
	}
	result, err := exec.ExecContext(ctx, `
		INSERT INTO work_messages (
			tenant_id, object_type, object_id, message_type, body, author_user_id
		) VALUES (?, ?, ?, ?, ?, ?)
	`, tenantID, objectType, objectID, messageType, body, authorUserID)
	if err != nil {
		return Message{}, fmt.Errorf("append work message: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return Message{}, fmt.Errorf("read work message id: %w", err)
	}
	return r.getMessage(ctx, exec, tenantID, uint64(id))
}

func (r *Repository) ListMessages(
	ctx context.Context,
	tenantID uint64,
	objectType, objectID string,
	beforeID uint64,
	limit int,
) (MessagePage, error) {
	objectType = strings.TrimSpace(objectType)
	objectID = strings.TrimSpace(objectID)
	if !validObjectRef(tenantID, objectType, objectID) || limit < 1 || limit > 200 {
		return MessagePage{}, ErrInvalid
	}
	query := `
		SELECT id, tenant_id, object_type, object_id, message_type, body, author_user_id, created_at
		FROM work_messages
		WHERE tenant_id = ? AND object_type = ? AND object_id = ?
	`
	args := []any{tenantID, objectType, objectID}
	if beforeID > 0 {
		query += " AND id < ?"
		args = append(args, beforeID)
	}
	query += " ORDER BY id DESC LIMIT ?"
	args = append(args, limit+1)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return MessagePage{}, fmt.Errorf("list work messages: %w", err)
	}
	defer rows.Close()
	messages := make([]Message, 0, limit+1)
	for rows.Next() {
		var item Message
		if err := rows.Scan(
			&item.ID, &item.TenantID, &item.ObjectType, &item.ObjectID,
			&item.Type, &item.Body, &item.AuthorUserID, &item.CreatedAt,
		); err != nil {
			return MessagePage{}, fmt.Errorf("scan work message: %w", err)
		}
		messages = append(messages, item)
	}
	if err := rows.Err(); err != nil {
		return MessagePage{}, fmt.Errorf("iterate work messages: %w", err)
	}
	page := MessagePage{Messages: messages}
	if len(messages) > limit {
		page.Messages = messages[:limit]
		page.NextBeforeID = page.Messages[len(page.Messages)-1].ID
	}
	return page, nil
}

func (r *Repository) AddWatcher(
	ctx context.Context,
	exec database.Executor,
	tenantID uint64,
	objectType, objectID string,
	userID, createdBy uint64,
) (bool, error) {
	objectType = strings.TrimSpace(objectType)
	objectID = strings.TrimSpace(objectID)
	if exec == nil || !validObjectRef(tenantID, objectType, objectID) || userID == 0 || createdBy == 0 {
		return false, ErrInvalid
	}
	if err := validateActiveUser(ctx, exec, tenantID, userID); err != nil {
		return false, err
	}
	if err := validateActiveUser(ctx, exec, tenantID, createdBy); err != nil {
		return false, err
	}
	result, err := exec.ExecContext(ctx, `
		INSERT INTO work_watchers (tenant_id, object_type, object_id, user_id, created_by)
		VALUES (?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE created_at = work_watchers.created_at
	`, tenantID, objectType, objectID, userID, createdBy)
	if err != nil {
		return false, fmt.Errorf("add work watcher: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("read watcher insert result: %w", err)
	}
	return affected == 1, nil
}

func (r *Repository) RemoveWatcher(
	ctx context.Context,
	exec database.Executor,
	tenantID uint64,
	objectType, objectID string,
	userID uint64,
) (bool, error) {
	if exec == nil || !validObjectRef(tenantID, strings.TrimSpace(objectType), strings.TrimSpace(objectID)) || userID == 0 {
		return false, ErrInvalid
	}
	result, err := exec.ExecContext(ctx, `
		DELETE FROM work_watchers
		WHERE tenant_id = ? AND object_type = ? AND object_id = ? AND user_id = ?
	`, tenantID, strings.TrimSpace(objectType), strings.TrimSpace(objectID), userID)
	if err != nil {
		return false, fmt.Errorf("remove work watcher: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("read watcher delete result: %w", err)
	}
	return affected == 1, nil
}

func (r *Repository) ListWatchers(
	ctx context.Context,
	tenantID uint64,
	objectType, objectID string,
) (WatcherPage, error) {
	objectType = strings.TrimSpace(objectType)
	objectID = strings.TrimSpace(objectID)
	if !validObjectRef(tenantID, objectType, objectID) {
		return WatcherPage{}, ErrInvalid
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT user_id, created_by, created_at
		FROM work_watchers
		WHERE tenant_id = ? AND object_type = ? AND object_id = ?
		ORDER BY user_id
	`, tenantID, objectType, objectID)
	if err != nil {
		return WatcherPage{}, fmt.Errorf("list work watchers: %w", err)
	}
	defer rows.Close()
	watchers := make([]Watcher, 0)
	for rows.Next() {
		var item Watcher
		if err := rows.Scan(&item.UserID, &item.CreatedBy, &item.CreatedAt); err != nil {
			return WatcherPage{}, fmt.Errorf("scan work watcher: %w", err)
		}
		watchers = append(watchers, item)
	}
	if err := rows.Err(); err != nil {
		return WatcherPage{}, fmt.Errorf("iterate work watchers: %w", err)
	}
	return WatcherPage{Watchers: watchers}, nil
}

func (r *Repository) AddRelation(
	ctx context.Context,
	exec database.Executor,
	tenantID uint64,
	sourceType, sourceID, relationType, targetType, targetID string,
	createdBy uint64,
) (Relation, bool, error) {
	sourceType = strings.TrimSpace(sourceType)
	sourceID = strings.TrimSpace(sourceID)
	relationType = strings.TrimSpace(relationType)
	targetType = strings.TrimSpace(targetType)
	targetID = strings.TrimSpace(targetID)
	if exec == nil ||
		!validObjectRef(tenantID, sourceType, sourceID) ||
		!validObjectRef(tenantID, targetType, targetID) ||
		relationType == "" || len(relationType) > 64 || createdBy == 0 ||
		(sourceType == targetType && sourceID == targetID) {
		return Relation{}, false, ErrInvalid
	}
	if err := validateActiveUser(ctx, exec, tenantID, createdBy); err != nil {
		return Relation{}, false, err
	}
	result, err := exec.ExecContext(ctx, `
		INSERT INTO work_relations (
			tenant_id, source_type, source_id, relation_type, target_type, target_id, created_by
		) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE id = LAST_INSERT_ID(id)
	`, tenantID, sourceType, sourceID, relationType, targetType, targetID, createdBy)
	if err != nil {
		return Relation{}, false, fmt.Errorf("add work relation: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return Relation{}, false, fmt.Errorf("read relation insert result: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return Relation{}, false, fmt.Errorf("read relation id: %w", err)
	}
	relation, err := r.getRelation(ctx, exec, tenantID, uint64(id))
	return relation, affected == 1, err
}

func (r *Repository) ListRelations(
	ctx context.Context,
	tenantID uint64,
	sourceType, sourceID string,
	beforeID uint64,
	limit int,
) (RelationPage, error) {
	sourceType = strings.TrimSpace(sourceType)
	sourceID = strings.TrimSpace(sourceID)
	if !validObjectRef(tenantID, sourceType, sourceID) || limit < 1 || limit > 200 {
		return RelationPage{}, ErrInvalid
	}
	query := `
		SELECT id, tenant_id, source_type, source_id, relation_type, target_type, target_id, created_by, created_at
		FROM work_relations
		WHERE tenant_id = ? AND source_type = ? AND source_id = ?
	`
	args := []any{tenantID, sourceType, sourceID}
	if beforeID > 0 {
		query += " AND id < ?"
		args = append(args, beforeID)
	}
	query += " ORDER BY id DESC LIMIT ?"
	args = append(args, limit+1)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return RelationPage{}, fmt.Errorf("list work relations: %w", err)
	}
	defer rows.Close()
	relations := make([]Relation, 0, limit+1)
	for rows.Next() {
		var item Relation
		if err := rows.Scan(
			&item.ID, &item.TenantID, &item.SourceType, &item.SourceID, &item.Type,
			&item.TargetType, &item.TargetID, &item.CreatedBy, &item.CreatedAt,
		); err != nil {
			return RelationPage{}, fmt.Errorf("scan work relation: %w", err)
		}
		relations = append(relations, item)
	}
	if err := rows.Err(); err != nil {
		return RelationPage{}, fmt.Errorf("iterate work relations: %w", err)
	}
	page := RelationPage{Relations: relations}
	if len(relations) > limit {
		page.Relations = relations[:limit]
		page.NextBeforeID = page.Relations[len(page.Relations)-1].ID
	}
	return page, nil
}

func (r *Repository) RemoveRelation(
	ctx context.Context,
	exec database.Executor,
	tenantID, relationID uint64,
) (Relation, bool, error) {
	if exec == nil || tenantID == 0 || relationID == 0 {
		return Relation{}, false, ErrInvalid
	}
	relation, err := r.getRelation(ctx, exec, tenantID, relationID)
	if errors.Is(err, ErrNotFound) {
		return Relation{}, false, nil
	}
	if err != nil {
		return Relation{}, false, err
	}
	result, err := exec.ExecContext(ctx, `
		DELETE FROM work_relations
		WHERE tenant_id = ? AND id = ?
	`, tenantID, relationID)
	if err != nil {
		return Relation{}, false, fmt.Errorf("remove work relation: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return Relation{}, false, fmt.Errorf("read relation delete result: %w", err)
	}
	return relation, affected == 1, nil
}

func (r *Repository) getAssignment(
	ctx context.Context,
	exec database.Executor,
	tenantID uint64,
	objectType, objectID string,
	forUpdate bool,
) (Assignment, error) {
	query := `
		SELECT tenant_id, object_type, object_id, assigned_to, assignment_group_id, version, updated_at
		FROM work_assignments
		WHERE tenant_id = ? AND object_type = ? AND object_id = ?
	`
	if forUpdate {
		query += " FOR UPDATE"
	}
	var item Assignment
	var assignedTo, groupID sql.Null[uint64]
	err := exec.QueryRowContext(ctx, query, tenantID, objectType, objectID).Scan(
		&item.TenantID, &item.ObjectType, &item.ObjectID,
		&assignedTo, &groupID, &item.Version, &item.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Assignment{}, ErrNotFound
	}
	if err != nil {
		return Assignment{}, fmt.Errorf("get work assignment: %w", err)
	}
	if assignedTo.Valid {
		value := assignedTo.V
		item.AssignedTo = &value
	}
	if groupID.Valid {
		value := groupID.V
		item.AssignmentGroupID = &value
	}
	return item, nil
}

func (r *Repository) getMessage(ctx context.Context, exec database.Executor, tenantID, id uint64) (Message, error) {
	var item Message
	if err := exec.QueryRowContext(ctx, `
		SELECT id, tenant_id, object_type, object_id, message_type, body, author_user_id, created_at
		FROM work_messages
		WHERE tenant_id = ? AND id = ?
	`, tenantID, id).Scan(
		&item.ID, &item.TenantID, &item.ObjectType, &item.ObjectID,
		&item.Type, &item.Body, &item.AuthorUserID, &item.CreatedAt,
	); err != nil {
		return Message{}, fmt.Errorf("get work message: %w", err)
	}
	return item, nil
}

func (r *Repository) getRelation(ctx context.Context, exec database.Executor, tenantID, id uint64) (Relation, error) {
	var item Relation
	if err := exec.QueryRowContext(ctx, `
		SELECT id, tenant_id, source_type, source_id, relation_type, target_type, target_id, created_by, created_at
		FROM work_relations
		WHERE tenant_id = ? AND id = ?
	`, tenantID, id).Scan(
		&item.ID, &item.TenantID, &item.SourceType, &item.SourceID, &item.Type,
		&item.TargetType, &item.TargetID, &item.CreatedBy, &item.CreatedAt,
	); errors.Is(err, sql.ErrNoRows) {
		return Relation{}, ErrNotFound
	} else if err != nil {
		return Relation{}, fmt.Errorf("get work relation: %w", err)
	}
	return item, nil
}

func validateTargets(
	ctx context.Context,
	exec database.Executor,
	tenantID uint64,
	assignedTo, groupID *uint64,
) error {
	if assignedTo != nil {
		if err := validateActiveUser(ctx, exec, tenantID, *assignedTo); err != nil {
			return err
		}
	}
	if groupID != nil {
		var found int
		if err := exec.QueryRowContext(ctx, `
			SELECT 1 FROM `+"`groups`"+` WHERE tenant_id = ? AND id = ? AND active = TRUE
		`, tenantID, *groupID).Scan(&found); errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return fmt.Errorf("validate assignment group: %w", err)
		}
	}
	return nil
}

func validateActiveUser(
	ctx context.Context,
	exec database.Executor,
	tenantID, userID uint64,
) error {
	var found int
	err := exec.QueryRowContext(ctx, `
		SELECT 1
		FROM tenant_memberships tm
		JOIN users u ON u.id = tm.user_id
		WHERE tm.tenant_id = ? AND tm.user_id = ? AND tm.active = TRUE AND u.active = TRUE
	`, tenantID, userID).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("validate tenant user: %w", err)
	}
	return nil
}

// ValidateActiveUser lets domains reuse the tenant membership invariant in their transaction.
func ValidateActiveUser(ctx context.Context, exec database.Executor, tenantID, userID uint64) error {
	return validateActiveUser(ctx, exec, tenantID, userID)
}

func validObjectRef(tenantID uint64, objectType, objectID string) bool {
	return tenantID > 0 && objectType != "" && len(objectType) <= 100 && objectID != "" && len(objectID) <= 128
}

func optionalUint64Equal(left, right *uint64) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func nullableUint64(value *uint64) any {
	if value == nil {
		return nil
	}
	return *value
}
