package incidents

import (
	"context"
	"fmt"
	"strings"

	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
)

type MajorActionNoteInput struct {
	Version uint64 `json:"version,string"`
	Body    string `json:"body"`
}

func (s *Service) MajorActionNotes(ctx context.Context, a Actor, id, before uint64, limit int) (workrecords.MessagePage, error) {
	if limit < 1 || limit > 100 {
		return workrecords.MessagePage{}, ErrInvalid
	}
	if _, err := s.GetMajorAction(ctx, a, id); err != nil {
		return workrecords.MessagePage{}, err
	}
	return workrecords.NewRepository(s.repo.db).ListMessagesVisible(ctx, a.TenantID, ActionObjectType, fmt.Sprint(id), before, limit, true)
}

func (s *Service) AddMajorActionNote(ctx context.Context, a Actor, id uint64, input MajorActionNoteInput, key string) (workrecords.Message, error) {
	input.Body = strings.TrimSpace(input.Body)
	if input.Version == 0 || !validText(input.Body, 1, 20000) || !requestKeyPattern.MatchString(key) {
		return workrecords.Message{}, ErrInvalid
	}
	action, err := s.GetMajorAction(ctx, a, id)
	if err != nil {
		return workrecords.Message{}, err
	}
	digest, err := majorRequestHash(id, "action.note", input)
	if err != nil {
		return workrecords.Message{}, err
	}
	var result workrecords.Message
	err = s.majorTx(ctx, a, action.IncidentID, "incident.read|incident.read_own", func(e database.Executor, r Record, a Actor) error {
		var owner *uint64
		var version uint64
		if err := e.QueryRowContext(ctx, `SELECT owner_user_id,version FROM incident_major_actions WHERE tenant_id=? AND id=? FOR UPDATE`, a.TenantID, id).Scan(&owner, &version); err != nil {
			return err
		}
		if !a.Has("major.manage") && (owner == nil || *owner != a.UserID) {
			return ErrForbidden
		}
		existing, err := s.repo.reserveCollaboration(ctx, e, a, key, "major.action.note", digest)
		if err != nil {
			return err
		}
		if existing > 0 {
			result, err = workrecords.NewRepository(s.repo.db).GetMessage(ctx, e, a.TenantID, existing)
			return err
		}
		if version != input.Version {
			return ErrConflict
		}
		result, err = s.work.AppendMessageInTx(ctx, e, workActor(a), ActionObjectType, fmt.Sprint(id), workrecords.MessageWorkNote, input.Body)
		if err != nil {
			return err
		}
		if err = s.repo.completeCollaboration(ctx, e, a, key, result.ID); err != nil {
			return err
		}
		major, err := s.majorCase(ctx, e, a.TenantID, r.ID, false)
		if err != nil {
			return err
		}
		_, err = s.recordMajorEvent(ctx, e, a, r.ID, major.Version, "incident.major.action.note.added", "INTERNAL", "", result.ID, "", s.now().UTC())
		return err
	})
	return result, err
}
