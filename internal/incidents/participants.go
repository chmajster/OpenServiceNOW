package incidents

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"

	"github.com/chmajster/OpenServiceNOW/internal/identity"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
)

func (s *Service) Watchers(ctx context.Context, a Actor, id, after uint64, limit int) (WatcherList, error) {
	if limit < 1 || limit > 100 {
		return WatcherList{}, ErrInvalid
	}
	if _, err := s.Get(ctx, a, id); err != nil {
		return WatcherList{}, err
	}
	return s.repo.watchers(ctx, a, id, after, limit)
}
func (s *Service) Watch(ctx context.Context, a Actor, id, userID uint64, add bool) error {
	if userID == 0 {
		return ErrInvalid
	}
	if userID != a.UserID && (!a.Has("read") || !a.Has("update")) {
		return ErrForbidden
	}
	return s.communicationTx(ctx, a, id, func(e database.Executor, r Record) error {
		if add {
			if err := workrecords.ValidateActiveUser(ctx, e, a.TenantID, userID); err != nil {
				return assignmentError(err)
			}
			keys, err := identity.NewMySQLRepository(s.repo.db).ListPermissionsInTx(ctx, e, a.TenantID, userID)
			if err != nil {
				return err
			}
			target := Actor{TenantID: a.TenantID, UserID: userID, Permissions: map[string]bool{}}
			for _, key := range keys {
				target.Permissions[key] = true
			}
			if !readable(target, r) {
				return fmt.Errorf("%w: obserwator musi mieć dostęp do incydentu", ErrInvalid)
			}
			var exists bool
			if err := e.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM work_watchers WHERE tenant_id=? AND object_type='incident' AND object_id=? AND user_id=?)`, a.TenantID, fmt.Sprint(id), userID).Scan(&exists); err != nil {
				return err
			}
			if exists {
				return nil
			}
			var count int
			if err := e.QueryRowContext(ctx, `SELECT COUNT(*) FROM work_watchers WHERE tenant_id=? AND object_type='incident' AND object_id=?`, a.TenantID, fmt.Sprint(id)).Scan(&count); err != nil {
				return err
			}
			if count >= 100 {
				return fmt.Errorf("%w: limit 100 obserwatorów", ErrInvalid)
			}
		}
		kind := "work.watcher.removed"
		var changed bool
		var err error
		if add {
			kind = "work.watcher.added"
			changed, err = s.work.AddWatcherInTx(ctx, e, workActor(a), "incident", fmt.Sprint(id), userID)
		} else {
			changed, err = s.work.RemoveWatcherInTx(ctx, e, workActor(a), "incident", fmt.Sprint(id), userID)
		}
		if err != nil || !changed {
			return err
		}
		return s.collaborationEvent(ctx, e, a, r, kind, "INTERNAL", "", map[string]any{"resource_id": fmt.Sprint(userID), "user_id": fmt.Sprint(userID)})
	})
}
func (s *Service) Relations(ctx context.Context, a Actor, id, before uint64, limit int) (RelationList, error) {
	if limit < 1 || limit > 100 {
		return RelationList{}, ErrInvalid
	}
	if _, err := s.Get(ctx, a, id); err != nil {
		return RelationList{}, err
	}
	return s.repo.relations(ctx, a, id, before, limit)
}
func (s *Service) AddRelation(ctx context.Context, a Actor, id uint64, input RelationInput) (workrecords.Relation, error) {
	if !a.Has("read") || !a.Has("update") {
		return workrecords.Relation{}, ErrForbidden
	}
	if id == input.TargetID || input.TargetID == 0 || input.Type != "RELATED" && input.Type != "PARENT" {
		return workrecords.Relation{}, ErrInvalid
	}
	var result workrecords.Relation
	err := s.communicationTx(ctx, a, id, func(e database.Executor, r Record) error {
		target, err := s.repo.Get(ctx, e, a.TenantID, input.TargetID, true)
		if err != nil {
			return err
		}
		if !readable(a, target) {
			return ErrNotFound
		}
		source, destination := id, input.TargetID
		if input.Type == "RELATED" && source > destination {
			source, destination = destination, source
		}
		var existing uint64
		err = e.QueryRowContext(ctx, `SELECT id FROM work_relations WHERE tenant_id=? AND source_type='incident' AND target_type='incident' AND source_id=? AND target_id=? AND relation_type=?`, a.TenantID, fmt.Sprint(source), fmt.Sprint(destination), input.Type).Scan(&existing)
		if err == nil {
			result, err = workrecords.NewRepository(s.repo.db).GetRelation(ctx, e, a.TenantID, existing)
			return err
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		for _, end := range []uint64{id, input.TargetID} {
			var count int
			if err := e.QueryRowContext(ctx, `SELECT COUNT(*) FROM work_relations WHERE tenant_id=? AND ((source_type='incident' AND source_id=?) OR (target_type='incident' AND target_id=?))`, a.TenantID, fmt.Sprint(end), fmt.Sprint(end)).Scan(&count); err != nil {
				return err
			}
			if count >= 100 {
				return fmt.Errorf("%w: limit 100 relacji na incydent", ErrInvalid)
			}
		}
		if input.Type == "PARENT" {
			var parent bool
			if err := e.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM work_relations WHERE tenant_id=? AND source_type='incident' AND target_type='incident' AND relation_type='PARENT' AND source_id=?)`, a.TenantID, fmt.Sprint(source)).Scan(&parent); err != nil {
				return err
			}
			if parent {
				return fmt.Errorf("%w: incydent ma już nadrzędny rekord", ErrConflict)
			}
			var cycle bool
			err := e.QueryRowContext(ctx, `WITH RECURSIVE descendants(id) AS (SELECT CAST(? AS CHAR(128)) UNION DISTINCT SELECT r.target_id FROM work_relations r JOIN descendants d ON r.source_id=d.id WHERE r.tenant_id=? AND r.source_type='incident' AND r.target_type='incident' AND r.relation_type='PARENT') SELECT EXISTS(SELECT 1 FROM descendants WHERE id=?)`, fmt.Sprint(destination), a.TenantID, fmt.Sprint(source)).Scan(&cycle)
			if err != nil {
				return err
			}
			if cycle {
				return fmt.Errorf("%w: relacja tworzyłaby cykl", ErrInvalid)
			}
		}
		result, _, err = s.work.AddRelationInTx(ctx, e, workActor(a), fmt.Sprint(source), input.Type, fmt.Sprint(destination))
		if err != nil {
			return err
		}
		// Relation events are internal; read-own users see a relation only when both
		// ends still pass the SQL object predicate in Relations, never raw payloads.
		return s.collaborationEvent(ctx, e, a, r, "work.relation.added", "INTERNAL", fmt.Sprintf("incident-relation-added:%d", result.ID), map[string]any{"resource_id": fmt.Sprint(result.ID), "relation_id": fmt.Sprint(result.ID), "relation_type": input.Type, "source_id": fmt.Sprint(source), "target_id": fmt.Sprint(destination)})
	})
	return result, err
}
func (s *Service) RemoveRelation(ctx context.Context, a Actor, id, relationID uint64) error {
	if !a.Has("read") || !a.Has("update") {
		return ErrForbidden
	}
	return s.communicationTx(ctx, a, id, func(e database.Executor, r Record) error {
		rel, err := workrecords.NewRepository(s.repo.db).GetRelation(ctx, e, a.TenantID, relationID)
		if errors.Is(err, workrecords.ErrNotFound) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if rel.SourceType != "incident" || rel.TargetType != "incident" || rel.SourceID != fmt.Sprint(id) && rel.TargetID != fmt.Sprint(id) {
			return ErrNotFound
		}
		for _, raw := range []string{rel.SourceID, rel.TargetID} {
			end, err := strconv.ParseUint(raw, 10, 64)
			if err != nil {
				return ErrNotFound
			}
			other, err := s.repo.Get(ctx, e, a.TenantID, end, true)
			if err != nil {
				return err
			}
			if !readable(a, other) {
				return ErrNotFound
			}
		}
		_, changed, err := s.work.RemoveRelationInTx(ctx, e, workActor(a), relationID)
		if err != nil || !changed {
			return err
		}
		return s.collaborationEvent(ctx, e, a, r, "work.relation.removed", "INTERNAL", fmt.Sprintf("incident-relation-removed:%d", relationID), map[string]any{"resource_id": fmt.Sprint(relationID), "relation_id": fmt.Sprint(relationID), "relation_type": rel.Type})
	})
}
