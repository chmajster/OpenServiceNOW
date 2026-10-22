package knowledge

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/chmajster/OpenServiceNOW/internal/audit"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
	mysql "github.com/go-sql-driver/mysql"
)

type Service struct {
	db    *sql.DB
	tx    *database.TxManager
	audit *audit.MySQLRepository
}

func NewService(db *sql.DB) *Service {
	return &Service{db: db, tx: database.NewTxManager(db), audit: audit.NewMySQLRepository()}
}

func translate(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	var mysqlError *mysql.MySQLError
	if errors.As(err, &mysqlError) && mysqlError.Number == 1062 {
		return ErrConflict
	}
	return err
}

func (s *Service) write(ctx context.Context, actor Actor, allowed func(Actor) bool, fn func(database.Executor, Actor) error) error {
	return s.tx.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(exec database.Executor) error {
		current, err := workrecords.LockPrincipal(ctx, exec, actor)
		if err != nil {
			if errors.Is(err, workrecords.ErrForbidden) {
				return ErrForbidden
			}
			return err
		}
		if !allowed(current) {
			return ErrForbidden
		}
		return fn(exec, current)
	})
}

func requireManage(actor Actor) bool { return actor.Has(PermissionManage) }

func (s *Service) appendAudit(ctx context.Context, exec database.Executor, actor Actor, action, object string, id uint64, before, after any) error {
	tenant, user := actor.TenantID, actor.UserID
	return s.audit.Append(ctx, exec, audit.Event{
		TenantID: &tenant, ActorUserID: &user, Action: action, ObjectType: object,
		ObjectID: fmt.Sprint(id), OldValues: before, NewValues: after,
		RequestID: actor.RequestID, IPAddress: actor.IPAddress, Source: "api",
	})
}

func spaceAuditValue(space Space) map[string]any {
	return map[string]any{
		"key": space.Key, "name": space.Name, "status": space.Status,
		"review_interval_days": space.ReviewIntervalDays, "version": fmt.Sprint(space.Version),
	}
}

func pageAuditValue(page Page) map[string]any {
	digest := sha256.Sum256([]byte(page.Body))
	value := map[string]any{
		"base_id": fmt.Sprint(page.SpaceID), "title": page.Title, "status": page.Status,
		"version": fmt.Sprint(page.Version), "body_bytes": len(page.Body),
		"body_sha256": hex.EncodeToString(digest[:]),
	}
	if page.ReviewDueAt != nil {
		value["review_due_at"] = page.ReviewDueAt.UTC()
	}
	return value
}

func (s *Service) CreateSpace(ctx context.Context, actor Actor, input SpaceInput, key string) (Space, bool, error) {
	input, err := normalizeSpace(input, true)
	if err != nil {
		return Space{}, false, err
	}
	raw, _ := json.Marshal(input)
	digest := sha256.Sum256(raw)
	var id uint64
	created := false
	err = s.write(ctx, actor, requireManage, func(exec database.Executor, current Actor) error {
		existing, err := workrecords.ReserveOperation(ctx, exec, current.Actor, key, "knowledge_base", digest)
		if err != nil {
			if errors.Is(err, workrecords.ErrInvalid) {
				return ErrInvalid
			}
			return err
		}
		if existing > 0 {
			id = existing
			return nil
		}
		now := time.Now().UTC()
		result, err := exec.ExecContext(ctx, `
			INSERT INTO knowledge_spaces(
				tenant_id,space_key,name,description,status,review_interval_days,
				created_by,updated_by,created_at,updated_at
			) VALUES(?,?,?,?,?,?,?,?,?,?)`,
			current.TenantID, input.Key, input.Name, input.Description, input.Status,
			input.ReviewIntervalDays, current.UserID, current.UserID, now, now)
		if err != nil {
			return translate(err)
		}
		inserted, err := result.LastInsertId()
		if err != nil {
			return err
		}
		id, created = uint64(inserted), true
		space := Space{ID: id, Key: input.Key, Name: input.Name, Description: input.Description, Status: input.Status, ReviewIntervalDays: input.ReviewIntervalDays, Version: 1, CreatedAt: now, UpdatedAt: now}
		if err := s.appendAudit(ctx, exec, current, "knowledge.base.created", "knowledge_base", id, nil, spaceAuditValue(space)); err != nil {
			return err
		}
		return workrecords.CompleteOperation(ctx, exec, current.Actor, key, id)
	})
	if err != nil {
		return Space{}, false, err
	}
	space, err := s.Space(ctx, actor, id)
	return space, created, err
}

func (s *Service) UpdateSpace(ctx context.Context, actor Actor, id uint64, input SpaceInput) (Space, error) {
	input, err := normalizeSpace(input, false)
	if err != nil || id == 0 {
		return Space{}, ErrInvalid
	}
	err = s.write(ctx, actor, requireManage, func(exec database.Executor, current Actor) error {
		before, err := getSpaceBase(ctx, exec, current.TenantID, id, true)
		if err != nil {
			return err
		}
		if before.Version != input.Version {
			return ErrConflict
		}
		now := time.Now().UTC()
		result, err := exec.ExecContext(ctx, `
			UPDATE knowledge_spaces SET space_key=?,name=?,description=?,status=?,
				review_interval_days=?,version=version+1,updated_by=?,updated_at=?
			WHERE tenant_id=? AND id=? AND version=?`,
			input.Key, input.Name, input.Description, input.Status, input.ReviewIntervalDays,
			current.UserID, now, current.TenantID, id, input.Version)
		if err != nil {
			return translate(err)
		}
		count, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if count != 1 {
			return ErrConflict
		}
		after := before
		after.Key, after.Name, after.Description, after.Status = input.Key, input.Name, input.Description, input.Status
		after.ReviewIntervalDays, after.Version, after.UpdatedAt = input.ReviewIntervalDays, input.Version+1, now
		return s.appendAudit(ctx, exec, current, "knowledge.base.updated", "knowledge_base", id, spaceAuditValue(before), spaceAuditValue(after))
	})
	if err != nil {
		return Space{}, err
	}
	return s.Space(ctx, actor, id)
}

func getSpaceBase(ctx context.Context, exec database.Executor, tenant, id uint64, lock bool) (Space, error) {
	query := `SELECT id,space_key,name,description,status,review_interval_days,version,created_at,updated_at
		FROM knowledge_spaces WHERE tenant_id=? AND id=?`
	if lock {
		query += " FOR UPDATE"
	}
	var space Space
	err := exec.QueryRowContext(ctx, query, tenant, id).Scan(
		&space.ID, &space.Key, &space.Name, &space.Description, &space.Status,
		&space.ReviewIntervalDays, &space.Version, &space.CreatedAt, &space.UpdatedAt)
	return space, translate(err)
}

func (s *Service) Space(ctx context.Context, actor Actor, id uint64) (Space, error) {
	if !canRead(actor) || actor.TenantID == 0 || actor.UserID == 0 || id == 0 {
		return Space{}, ErrForbidden
	}
	includeArchived := actor.Has(PermissionManage)
	includeDrafts := canWrite(actor)
	var space Space
	err := s.db.QueryRowContext(ctx, `
		SELECT s.id,s.space_key,s.name,s.description,s.status,s.review_interval_days,s.version,
		       (SELECT COUNT(*) FROM knowledge_pages p WHERE p.tenant_id=s.tenant_id AND p.space_id=s.id AND (? OR p.status='PUBLISHED')),
		       EXISTS(SELECT 1 FROM knowledge_base_subscriptions w WHERE w.tenant_id=s.tenant_id AND w.space_id=s.id AND w.user_id=?),
		       s.created_at,s.updated_at
		FROM knowledge_spaces s
		WHERE s.tenant_id=? AND s.id=? AND (? OR s.status='ACTIVE')`,
		includeDrafts, actor.UserID, actor.TenantID, id, includeArchived).Scan(
		&space.ID, &space.Key, &space.Name, &space.Description, &space.Status,
		&space.ReviewIntervalDays, &space.Version, &space.PageCount, &space.Subscribed,
		&space.CreatedAt, &space.UpdatedAt)
	return space, translate(err)
}

func (s *Service) Spaces(ctx context.Context, actor Actor, after uint64, limit int) (SpacePage, error) {
	page := SpacePage{Spaces: []Space{}}
	if !canRead(actor) {
		return page, ErrForbidden
	}
	if limit < 1 || limit > 100 {
		return page, ErrInvalid
	}
	includeArchived := actor.Has(PermissionManage)
	includeDrafts := canWrite(actor)
	rows, err := s.db.QueryContext(ctx, `
		SELECT s.id,s.space_key,s.name,s.description,s.status,s.review_interval_days,s.version,
		       (SELECT COUNT(*) FROM knowledge_pages p WHERE p.tenant_id=s.tenant_id AND p.space_id=s.id AND (? OR p.status='PUBLISHED')),
		       EXISTS(SELECT 1 FROM knowledge_base_subscriptions w WHERE w.tenant_id=s.tenant_id AND w.space_id=s.id AND w.user_id=?),
		       s.created_at,s.updated_at
		FROM knowledge_spaces s
		WHERE s.tenant_id=? AND s.id>? AND (? OR s.status='ACTIVE')
		ORDER BY s.id LIMIT ?`,
		includeDrafts, actor.UserID, actor.TenantID, after, includeArchived, limit+1)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		var space Space
		if err := rows.Scan(&space.ID, &space.Key, &space.Name, &space.Description, &space.Status,
			&space.ReviewIntervalDays, &space.Version, &space.PageCount, &space.Subscribed,
			&space.CreatedAt, &space.UpdatedAt); err != nil {
			return page, err
		}
		page.Spaces = append(page.Spaces, space)
	}
	if err := rows.Err(); err != nil {
		return page, err
	}
	if len(page.Spaces) > limit {
		page.Spaces = page.Spaces[:limit]
		page.NextAfterID = page.Spaces[limit-1].ID
	}
	return page, nil
}

func (s *Service) CreatePage(ctx context.Context, actor Actor, spaceID uint64, input PageInput, key string) (Page, bool, error) {
	input, err := normalizePage(input, true)
	if err != nil || spaceID == 0 {
		return Page{}, false, ErrInvalid
	}
	raw, _ := json.Marshal(struct {
		SpaceID uint64
		Input   PageInput
	}{spaceID, input})
	digest := sha256.Sum256(raw)
	var id uint64
	created := false
	err = s.write(ctx, actor, canWrite, func(exec database.Executor, current Actor) error {
		existing, err := workrecords.ReserveOperation(ctx, exec, current.Actor, key, "knowledge_article", digest)
		if err != nil {
			if errors.Is(err, workrecords.ErrInvalid) {
				return ErrInvalid
			}
			return err
		}
		if existing > 0 {
			id = existing
			return nil
		}
		space, err := getSpaceBase(ctx, exec, current.TenantID, spaceID, true)
		if err != nil {
			return err
		}
		if space.Status != SpaceActive {
			return ErrConflict
		}
		applyReviewDefault(&input, space.ReviewIntervalDays)
		now := time.Now().UTC()
		result, err := exec.ExecContext(ctx, `
			INSERT INTO knowledge_pages(
				tenant_id,space_id,title,body,status,review_due_at,created_by,
				updated_by,created_at,updated_at
			) VALUES(?,?,?,?,?,?,?,?,?,?)`,
			current.TenantID, spaceID, input.Title, input.Body, input.Status,
			input.ReviewDueAt, current.UserID, current.UserID, now, now)
		if err != nil {
			return translate(err)
		}
		inserted, err := result.LastInsertId()
		if err != nil {
			return err
		}
		id, created = uint64(inserted), true
		page := Page{ID: id, SpaceID: spaceID, Title: input.Title, Body: input.Body, Status: input.Status, ReviewDueAt: input.ReviewDueAt, Version: 1, CreatedBy: current.UserID, UpdatedBy: current.UserID, CreatedAt: now, UpdatedAt: now}
		if err := insertVersion(ctx, exec, current.TenantID, page, current.UserID, now); err != nil {
			return err
		}
		if err := s.appendAudit(ctx, exec, current, "knowledge.article.created", "knowledge_article", id, nil, pageAuditValue(page)); err != nil {
			return err
		}
		return workrecords.CompleteOperation(ctx, exec, current.Actor, key, id)
	})
	if err != nil {
		return Page{}, false, err
	}
	page, err := s.Page(ctx, actor, id)
	return page, created, err
}

func (s *Service) UpdatePage(ctx context.Context, actor Actor, id uint64, input PageInput) (Page, error) {
	input, err := normalizePage(input, false)
	if err != nil || id == 0 {
		return Page{}, ErrInvalid
	}
	err = s.write(ctx, actor, canWrite, func(exec database.Executor, current Actor) error {
		var spaceID uint64
		if err := exec.QueryRowContext(ctx, `SELECT space_id FROM knowledge_pages WHERE tenant_id=? AND id=?`, current.TenantID, id).Scan(&spaceID); err != nil {
			return translate(err)
		}
		space, err := getSpaceBase(ctx, exec, current.TenantID, spaceID, true)
		if err != nil {
			return err
		}
		before, err := getPageBase(ctx, exec, current.TenantID, id, true)
		if err != nil {
			return err
		}
		if before.SpaceID != spaceID {
			return ErrConflict
		}
		if space.Status != SpaceActive {
			return ErrConflict
		}
		if before.Version != input.Version {
			return ErrConflict
		}
		applyReviewDefault(&input, space.ReviewIntervalDays)
		now := time.Now().UTC()
		result, err := exec.ExecContext(ctx, `
			UPDATE knowledge_pages SET title=?,body=?,status=?,review_due_at=?,
				version=version+1,updated_by=?,updated_at=?
			WHERE tenant_id=? AND id=? AND version=?`,
			input.Title, input.Body, input.Status, input.ReviewDueAt,
			current.UserID, now, current.TenantID, id, input.Version)
		if err != nil {
			return translate(err)
		}
		count, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if count != 1 {
			return ErrConflict
		}
		after := before
		after.Title, after.Body, after.Status, after.ReviewDueAt = input.Title, input.Body, input.Status, input.ReviewDueAt
		after.Version, after.UpdatedBy, after.UpdatedAt = input.Version+1, current.UserID, now
		if err := insertVersion(ctx, exec, current.TenantID, after, current.UserID, now); err != nil {
			return err
		}
		return s.appendAudit(ctx, exec, current, "knowledge.article.updated", "knowledge_article", id, pageAuditValue(before), pageAuditValue(after))
	})
	if err != nil {
		return Page{}, err
	}
	return s.Page(ctx, actor, id)
}

func applyReviewDefault(input *PageInput, days uint16) {
	if input.Status != PagePublished {
		input.ReviewDueAt = nil
		return
	}
	if input.ReviewDueAt == nil && days > 0 {
		value := time.Now().UTC().AddDate(0, 0, int(days))
		input.ReviewDueAt = &value
	}
}

func insertVersion(ctx context.Context, exec database.Executor, tenant uint64, page Page, actor uint64, changed time.Time) error {
	_, err := exec.ExecContext(ctx, `
		INSERT INTO knowledge_page_versions(
			tenant_id,space_id,page_id,version,title,body,status,review_due_at,changed_by,changed_at
		) VALUES(?,?,?,?,?,?,?,?,?,?)`,
		tenant, page.SpaceID, page.ID, page.Version, page.Title, page.Body,
		page.Status, page.ReviewDueAt, actor, changed)
	return err
}

func getPageBase(ctx context.Context, exec database.Executor, tenant, id uint64, lock bool) (Page, error) {
	query := `SELECT id,space_id,title,body,status,review_due_at,version,created_by,updated_by,created_at,updated_at
		FROM knowledge_pages WHERE tenant_id=? AND id=?`
	if lock {
		query += " FOR UPDATE"
	}
	var page Page
	var review sql.NullTime
	err := exec.QueryRowContext(ctx, query, tenant, id).Scan(
		&page.ID, &page.SpaceID, &page.Title, &page.Body, &page.Status,
		&review, &page.Version, &page.CreatedBy, &page.UpdatedBy, &page.CreatedAt, &page.UpdatedAt)
	if review.Valid {
		page.ReviewDueAt = &review.Time
	}
	return page, translate(err)
}

func (s *Service) Page(ctx context.Context, actor Actor, id uint64) (Page, error) {
	if !canRead(actor) || id == 0 {
		return Page{}, ErrForbidden
	}
	var page Page
	var review sql.NullTime
	err := s.db.QueryRowContext(ctx, `
		SELECT p.id,p.space_id,p.title,p.body,p.status,p.review_due_at,p.version,
		       p.created_by,p.updated_by,COALESCE(u.display_name,''),p.created_at,p.updated_at
		FROM knowledge_pages p
		JOIN knowledge_spaces s ON s.tenant_id=p.tenant_id AND s.id=p.space_id
		LEFT JOIN users u ON u.id=p.updated_by
		WHERE p.tenant_id=? AND p.id=? AND (? OR s.status='ACTIVE') AND (? OR p.status='PUBLISHED')`,
		actor.TenantID, id, actor.Has(PermissionManage), canWrite(actor)).Scan(
		&page.ID, &page.SpaceID, &page.Title, &page.Body, &page.Status,
		&review, &page.Version, &page.CreatedBy, &page.UpdatedBy, &page.UpdatedByName,
		&page.CreatedAt, &page.UpdatedAt)
	if review.Valid {
		page.ReviewDueAt = &review.Time
	}
	return page, translate(err)
}

func (s *Service) Pages(ctx context.Context, actor Actor, spaceID, after uint64, limit int) (PagePage, error) {
	return s.pageSummaries(ctx, actor, spaceID, "", after, limit, false, "")
}

func (s *Service) Search(ctx context.Context, actor Actor, spaceID uint64, query string, after uint64, limit int) (PagePage, error) {
	query = strings.TrimSpace(query)
	if !text(query, 1, 200) {
		return PagePage{Pages: []PageSummary{}}, ErrInvalid
	}
	return s.pageSummaries(ctx, actor, spaceID, query, after, limit, false, "")
}

func (s *Service) Attention(ctx context.Context, actor Actor, spaceID, after uint64, limit int, reason string) (PagePage, error) {
	if !canWrite(actor) {
		return PagePage{Pages: []PageSummary{}}, ErrForbidden
	}
	if reason != "" && reason != AttentionReviewDue {
		return PagePage{Pages: []PageSummary{}}, ErrInvalid
	}
	return s.pageSummaries(ctx, actor, spaceID, "", after, limit, true, reason)
}

func (s *Service) pageSummaries(ctx context.Context, actor Actor, spaceID uint64, query string, after uint64, limit int, attention bool, attentionReason string) (PagePage, error) {
	page := PagePage{Pages: []PageSummary{}}
	if !canRead(actor) {
		return page, ErrForbidden
	}
	if limit < 1 || limit > 100 || (spaceID == 0 && (query == "" || attention)) {
		return page, ErrInvalid
	}
	includeDrafts := canWrite(actor)
	includeArchived := actor.Has(PermissionManage)
	statement := `
		SELECT p.id,p.space_id,s.name,p.title,p.status,p.review_due_at,p.version,
		       p.updated_by,COALESCE(u.display_name,''),p.updated_at,
		       CASE WHEN ?<>'' OR ? THEN LEFT(p.body,240) ELSE '' END,
		       CASE WHEN p.status='DRAFT' THEN 'DRAFT' WHEN p.review_due_at<=UTC_TIMESTAMP(6) THEN 'REVIEW_DUE' ELSE '' END
		FROM knowledge_pages p
		JOIN knowledge_spaces s ON s.tenant_id=p.tenant_id AND s.id=p.space_id
		LEFT JOIN users u ON u.id=p.updated_by
		WHERE p.tenant_id=? AND p.id>? AND (? OR s.status='ACTIVE') AND (? OR p.status='PUBLISHED')`
	args := []any{query, attention, actor.TenantID, after, includeArchived, includeDrafts}
	if spaceID > 0 {
		statement += " AND p.space_id=?"
		args = append(args, spaceID)
	}
	if query != "" {
		if utf8.RuneCountInString(query) < 3 {
			statement += " AND LOCATE(LOWER(?),LOWER(CONCAT(p.title,' ',p.body)))>0"
		} else {
			statement += " AND MATCH(p.title,p.body) AGAINST (? IN NATURAL LANGUAGE MODE)>0"
		}
		args = append(args, query)
	}
	if attention {
		if attentionReason == AttentionReviewDue {
			statement += " AND p.status='PUBLISHED' AND p.review_due_at<=UTC_TIMESTAMP(6)"
		} else {
			statement += " AND (p.status='DRAFT' OR (p.status='PUBLISHED' AND p.review_due_at<=UTC_TIMESTAMP(6)))"
		}
	}
	statement += " ORDER BY p.id LIMIT ?"
	args = append(args, limit+1)
	rows, err := s.db.QueryContext(ctx, statement, args...)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		var item PageSummary
		var review sql.NullTime
		if err := rows.Scan(&item.ID, &item.SpaceID, &item.SpaceName, &item.Title, &item.Status,
			&review, &item.Version, &item.UpdatedBy, &item.UpdatedByName, &item.UpdatedAt,
			&item.Excerpt, &item.Attention); err != nil {
			return page, err
		}
		if review.Valid {
			item.ReviewDueAt = &review.Time
		}
		page.Pages = append(page.Pages, item)
	}
	if err := rows.Err(); err != nil {
		return page, err
	}
	if len(page.Pages) > limit {
		page.Pages = page.Pages[:limit]
		page.NextAfterID = page.Pages[limit-1].ID
	}
	return page, nil
}

func (s *Service) Versions(ctx context.Context, actor Actor, pageID, before uint64, limit int) (VersionPage, error) {
	result := VersionPage{Versions: []PageVersion{}}
	if limit < 1 || limit > 100 {
		return result, ErrInvalid
	}
	if _, err := s.Page(ctx, actor, pageID); err != nil {
		return result, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT v.page_id,v.version,v.title,v.status,v.review_due_at,
		       v.changed_by,COALESCE(u.display_name,''),v.changed_at
		FROM knowledge_page_versions v
		LEFT JOIN users u ON u.id=v.changed_by
		WHERE v.tenant_id=? AND v.page_id=? AND (?=0 OR v.version<?) AND (? OR v.status='PUBLISHED')
		ORDER BY v.version DESC LIMIT ?`, actor.TenantID, pageID, before, before, canWrite(actor), limit+1)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var item PageVersion
		var review sql.NullTime
		if err := rows.Scan(&item.PageID, &item.Version, &item.Title, &item.Status,
			&review, &item.ChangedBy, &item.ChangedByName, &item.ChangedAt); err != nil {
			return result, err
		}
		if review.Valid {
			item.ReviewDueAt = &review.Time
		}
		result.Versions = append(result.Versions, item)
	}
	if err := rows.Err(); err != nil {
		return result, err
	}
	if len(result.Versions) > limit {
		result.Versions = result.Versions[:limit]
		result.NextBeforeVersion = result.Versions[limit-1].Version
	}
	return result, nil
}

func (s *Service) Subscribe(ctx context.Context, actor Actor, spaceID uint64, enabled bool) (Space, error) {
	if spaceID == 0 {
		return Space{}, ErrInvalid
	}
	err := s.write(ctx, actor, canRead, func(exec database.Executor, current Actor) error {
		space, err := getSpaceBase(ctx, exec, current.TenantID, spaceID, true)
		if err != nil {
			return err
		}
		if space.Status != SpaceActive && !current.Has(PermissionManage) {
			return ErrNotFound
		}
		now := time.Now().UTC()
		var changed bool
		if enabled {
			result, err := exec.ExecContext(ctx, `
				INSERT IGNORE INTO knowledge_base_subscriptions(tenant_id,space_id,user_id,created_at)
				VALUES(?,?,?,?)`, current.TenantID, spaceID, current.UserID, now)
			if err != nil {
				return err
			}
			count, _ := result.RowsAffected()
			changed = count > 0
		} else {
			result, err := exec.ExecContext(ctx, `
				DELETE FROM knowledge_base_subscriptions WHERE tenant_id=? AND space_id=? AND user_id=?`,
				current.TenantID, spaceID, current.UserID)
			if err != nil {
				return err
			}
			count, _ := result.RowsAffected()
			changed = count > 0
		}
		if !changed {
			return nil
		}
		action := "knowledge.base.unsubscribed"
		if enabled {
			action = "knowledge.base.subscribed"
		}
		return s.appendAudit(ctx, exec, current, action, "knowledge_base", spaceID, nil, map[string]any{"subscribed": enabled})
	})
	if err != nil {
		return Space{}, err
	}
	return s.Space(ctx, actor, spaceID)
}
