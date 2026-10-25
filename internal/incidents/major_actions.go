package incidents

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
)

const ActionObjectType = "incident_action"

type MajorActionCreateInput struct {
	Version uint64 `json:"version,string"`
	MajorActionInput
}

func (s *Service) createMajorActionInTx(ctx context.Context, e database.Executor, a Actor, incident uint64, input MajorActionInput, kind string, now time.Time) (uint64, error) {
	input.Description = strings.TrimSpace(input.Description)
	if input.Status == "" {
		input.Status = "OPEN"
	}
	if input.Status != "OPEN" && input.Status != "IN_PROGRESS" {
		return 0, ErrInvalid
	}
	if !validText(input.Description, 1, 4000) || input.DueAt.IsZero() {
		return 0, ErrInvalid
	}
	if err := s.validateActionOwner(ctx, e, a.TenantID, incident, input.OwnerUserID, false); err != nil {
		return 0, err
	}
	var count int
	if err := e.QueryRowContext(ctx, `SELECT COUNT(*) FROM incident_major_actions WHERE tenant_id=? AND incident_id=?`, a.TenantID, incident).Scan(&count); err != nil {
		return 0, err
	}
	if count >= 100 {
		return 0, fmt.Errorf("%w: at most 100 coordination and review actions per incident", ErrInvalid)
	}
	number, err := s.allocator.NextInTx(ctx, e, a.TenantID, ActionObjectType, "ACT", 7)
	if err != nil {
		return 0, err
	}
	result, err := e.ExecContext(ctx, `INSERT INTO incident_major_actions(tenant_id,incident_id,number,action_kind,description,owner_user_id,due_at,status,version,created_by,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,1,?,?,?)`, a.TenantID, incident, number, kind, input.Description, input.OwnerUserID, input.DueAt.UTC(), input.Status, a.UserID, now, now)
	if err != nil {
		return 0, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}
	if _, err = s.work.SetAssignmentInTx(ctx, e, workActor(a), ActionObjectType, fmt.Sprint(id), 0, input.OwnerUserID, nil); err != nil {
		return 0, err
	}
	return uint64(id), nil
}

func (s *Service) CreateMajorAction(ctx context.Context, a Actor, incident uint64, input MajorActionCreateInput, key string) (MajorAction, error) {
	if !a.Has("major.manage") {
		return MajorAction{}, ErrForbidden
	}
	if !requestKeyPattern.MatchString(key) || input.Version == 0 || input.ID != 0 {
		return MajorAction{}, ErrInvalid
	}
	digest, err := majorRequestHash(incident, "action.create", input)
	if err != nil {
		return MajorAction{}, err
	}
	var id uint64
	err = s.majorTx(ctx, a, incident, "incident.major.manage", func(e database.Executor, record Record, a Actor) error {
		existing, err := s.repo.reserveCollaboration(ctx, e, a, key, "major.action.create", digest)
		if err != nil {
			return err
		}
		if existing > 0 {
			id = existing
			return nil
		}
		major, err := s.majorCase(ctx, e, a.TenantID, incident, true)
		if err != nil {
			return err
		}
		if major.Version != input.Version {
			return ErrConflict
		}
		if major.Status != "ACTIVE" && major.Status != "RESTORED" {
			return ErrTransition
		}
		now := s.now().UTC().Truncate(time.Microsecond)
		id, err = s.createMajorActionInTx(ctx, e, a, incident, input.MajorActionInput, "COORDINATION", now)
		if err != nil {
			return err
		}
		if err = s.repo.completeCollaboration(ctx, e, a, key, id); err != nil {
			return err
		}
		if _, err = e.ExecContext(ctx, `UPDATE incident_major_cases SET version=version+1,updated_at=? WHERE tenant_id=? AND incident_id=?`, now, a.TenantID, incident); err != nil {
			return err
		}
		_, err = s.recordMajorEvent(ctx, e, a, incident, major.Version+1, "incident.major.action.changed", "INTERNAL", "created", id, "", now)
		return err
	})
	if err != nil {
		return MajorAction{}, err
	}
	return s.GetMajorAction(ctx, a, id)
}

func (s *Service) GetMajorAction(ctx context.Context, a Actor, id uint64) (MajorAction, error) {
	var item MajorAction
	err := s.repo.db.QueryRowContext(ctx, `SELECT ac.id,ac.incident_id,ac.number,ac.description,ac.owner_user_id,COALESCE(u.display_name,''),ac.due_at,ac.status,ac.version,ac.updated_at FROM incident_major_actions ac LEFT JOIN users u ON u.id=ac.owner_user_id WHERE ac.tenant_id=? AND ac.id=?`, a.TenantID, id).Scan(&item.ID, &item.IncidentID, &item.Number, &item.Description, &item.OwnerUserID, &item.OwnerName, &item.DueAt, &item.Status, &item.Version, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return item, ErrNotFound
	}
	if err != nil {
		return item, err
	}
	if _, err = s.Get(ctx, a, item.IncidentID); err != nil {
		return item, err
	}
	if !a.Has("major.read") && !a.Has("major.manage") && (item.OwnerUserID == nil || *item.OwnerUserID != a.UserID) {
		return MajorAction{}, ErrNotFound
	}
	return item, nil
}

// Coordination tasks use the workspace's shared assignment service. Reassigning
// a task is reserved for a coordinator and obeys the task's optimistic version.
func (s *Service) AssignMajorAction(ctx context.Context, a Actor, id, version uint64, owner *uint64) (MajorAction, error) {
	if !a.Has("major.manage") {
		return MajorAction{}, ErrForbidden
	}
	if version == 0 {
		return MajorAction{}, ErrInvalid
	}
	item, err := s.GetMajorAction(ctx, a, id)
	if err != nil {
		return item, err
	}
	err = s.majorTx(ctx, a, item.IncidentID, "incident.major.manage", func(e database.Executor, r Record, a Actor) error {
		var stored uint64
		if err := e.QueryRowContext(ctx, `SELECT version FROM incident_major_actions WHERE tenant_id=? AND id=? FOR UPDATE`, a.TenantID, id).Scan(&stored); err != nil {
			return err
		}
		if stored != version {
			return ErrConflict
		}
		if err := s.validateActionOwner(ctx, e, a.TenantID, item.IncidentID, owner, false); err != nil {
			return err
		}
		assignment, err := workrecords.NewRepository(s.repo.db).GetAssignment(ctx, a.TenantID, ActionObjectType, fmt.Sprint(id))
		if err != nil {
			return err
		}
		if _, err = s.work.SetAssignmentInTx(ctx, e, workActor(a), ActionObjectType, fmt.Sprint(id), assignment.Version, owner, nil); err != nil {
			return err
		}
		now := s.now().UTC().Truncate(time.Microsecond)
		if _, err = e.ExecContext(ctx, `UPDATE incident_major_actions SET owner_user_id=?,version=version+1,updated_at=? WHERE tenant_id=? AND id=?`, owner, now, a.TenantID, id); err != nil {
			return err
		}
		_, err = s.recordMajorEvent(ctx, e, a, r.ID, stored+1, "incident.major.action.changed", "INTERNAL", "assigned", id, "", now)
		return err
	})
	if err != nil {
		return MajorAction{}, err
	}
	return s.GetMajorAction(ctx, a, id)
}
