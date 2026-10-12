package incidents

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"fmt"

	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
)

func (r *Repository) reserveCollaboration(ctx context.Context, e database.Executor, a Actor, key, kind string, hash [32]byte) (uint64, error) {
	if _, err := e.ExecContext(ctx, `INSERT INTO incident_collaboration_requests(tenant_id,user_id,request_key,request_hash,resource_type) VALUES(?,?,?,?,?) ON DUPLICATE KEY UPDATE request_key=incident_collaboration_requests.request_key`, a.TenantID, a.UserID, key, hash[:], kind); err != nil {
		return 0, err
	}
	var stored []byte
	var id sql.Null[uint64]
	var storedKind string
	if err := e.QueryRowContext(ctx, `SELECT request_hash,resource_id,resource_type FROM incident_collaboration_requests WHERE tenant_id=? AND user_id=? AND request_key=? FOR UPDATE`, a.TenantID, a.UserID, key).Scan(&stored, &id, &storedKind); err != nil {
		return 0, err
	}
	if storedKind != kind || subtle.ConstantTimeCompare(stored, hash[:]) != 1 {
		return 0, ErrIdempotency
	}
	return id.V, nil
}
func (r *Repository) completeCollaboration(ctx context.Context, e database.Executor, a Actor, key string, id uint64) error {
	_, err := e.ExecContext(ctx, `UPDATE incident_collaboration_requests SET resource_id=? WHERE tenant_id=? AND user_id=? AND request_key=?`, id, a.TenantID, a.UserID, key)
	return err
}
func readPredicate(a Actor, alias string) (string, []any) {
	if a.Has("read") {
		return "1=1", nil
	}
	if a.Has("read_own") {
		return "(" + alias + ".reporter_id=? OR " + alias + ".affected_user_id=?)", []any{a.UserID, a.UserID}
	}
	return "1=0", nil
}
func (r *Repository) relations(ctx context.Context, a Actor, id, before uint64, limit int) (RelationList, error) {
	access, acl := readPredicate(a, "i")
	query := `SELECT r.id,r.relation_type,CAST(r.source_id AS UNSIGNED),CAST(r.target_id AS UNSIGNED),i.id,i.number,i.title FROM work_relations r JOIN incidents i ON i.tenant_id=r.tenant_id AND CAST(i.id AS CHAR)=IF(r.source_id=?,r.target_id,r.source_id) WHERE r.tenant_id=? AND r.source_type='incident' AND r.target_type='incident' AND (r.source_id=? OR r.target_id=?) AND ` + access
	args := []any{fmt.Sprint(id), a.TenantID, fmt.Sprint(id), fmt.Sprint(id)}
	args = append(args, acl...)
	if before > 0 {
		query += " AND r.id<?"
		args = append(args, before)
	}
	query += " ORDER BY r.id DESC LIMIT ?"
	args = append(args, limit+1)
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return RelationList{}, err
	}
	defer rows.Close()
	list := RelationList{Relations: []RelationDTO{}}
	for rows.Next() {
		var v RelationDTO
		if err := rows.Scan(&v.ID, &v.Type, &v.SourceID, &v.TargetID, &v.OtherID, &v.Number, &v.Title); err != nil {
			return list, err
		}
		list.Relations = append(list.Relations, v)
	}
	if len(list.Relations) > limit {
		list.Relations = list.Relations[:limit]
		list.NextBeforeID = list.Relations[limit-1].ID
	}
	return list, rows.Err()
}
func (r *Repository) watchers(ctx context.Context, a Actor, id, after uint64, limit int) (WatcherList, error) {
	var watching bool
	if err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM work_watchers WHERE tenant_id=? AND object_type='incident' AND object_id=? AND user_id=?)`, a.TenantID, fmt.Sprint(id), a.UserID).Scan(&watching); err != nil {
		return WatcherList{}, err
	}
	q := `SELECT w.user_id,u.display_name,w.created_at FROM work_watchers w JOIN users u ON u.id=w.user_id JOIN tenant_memberships tm ON tm.tenant_id=w.tenant_id AND tm.user_id=w.user_id WHERE w.tenant_id=? AND w.object_type='incident' AND w.object_id=? AND w.user_id>? AND u.active=TRUE AND tm.active=TRUE`
	args := []any{a.TenantID, fmt.Sprint(id), after}
	if !a.Has("read") {
		q += " AND w.user_id=?"
		args = append(args, a.UserID)
	}
	q += " ORDER BY w.user_id LIMIT ?"
	args = append(args, limit+1)
	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return WatcherList{}, err
	}
	defer rows.Close()
	list := WatcherList{Watchers: []WatcherDTO{}, Watching: watching}
	for rows.Next() {
		var w WatcherDTO
		if err := rows.Scan(&w.UserID, &w.Name, &w.CreatedAt); err != nil {
			return list, err
		}
		list.Watchers = append(list.Watchers, w)
	}
	if len(list.Watchers) > limit {
		list.Watchers = list.Watchers[:limit]
		list.NextAfterID = list.Watchers[limit-1].UserID
	}
	return list, rows.Err()
}
