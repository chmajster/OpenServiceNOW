package incidents

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/chmajster/OpenServiceNOW/internal/attachments"
	"github.com/chmajster/OpenServiceNOW/internal/audit"
	"github.com/chmajster/OpenServiceNOW/internal/events"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
)

func (s *Service) communicationTx(ctx context.Context, a Actor, id uint64, fn func(database.Executor, Record) error) error {
	return s.tx.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(e database.Executor) error {
		if err := s.repo.LockActor(ctx, e, a); err != nil {
			return err
		}
		r, err := s.repo.Get(ctx, e, a.TenantID, id, true)
		if err != nil {
			return err
		}
		if !readable(a, r) {
			return ErrNotFound
		}
		return fn(e, r)
	})
}
func (s *Service) collaborationEvent(ctx context.Context, e database.Executor, a Actor, r Record, kind, visibility, key string, data map[string]any) error {
	now := s.now().UTC().Truncate(time.Microsecond)
	data["actor_user_id"] = fmt.Sprint(a.UserID)
	data["occurred_at"] = now
	data["incident_version"] = fmt.Sprint(r.Version)
	data["visibility"] = visibility
	payload, err := json.Marshal(data)
	if err != nil {
		return err
	}
	// Audit records metadata, never a note body or a storage key.
	if err := s.audit.Append(ctx, e, audit.Event{TenantID: &a.TenantID, ActorUserID: &a.UserID, Time: now, Action: kind, ObjectType: "incident", ObjectID: fmt.Sprint(r.ID), NewValues: map[string]any{"visibility": visibility, "resource_id": data["resource_id"]}, RequestID: a.RequestID, Source: "api", IPAddress: a.IPAddress}); err != nil {
		return err
	}
	_, _, err = s.outbox.Append(ctx, e, events.Input{TenantID: a.TenantID, Type: kind, AggregateType: "incident", AggregateID: fmt.Sprint(r.ID), Payload: payload, IdempotencyKey: key, AvailableAt: now})
	return err
}
func (s *Service) Messages(ctx context.Context, a Actor, id, before uint64, limit int) (workrecords.MessagePage, error) {
	if _, err := s.Get(ctx, a, id); err != nil {
		return workrecords.MessagePage{}, err
	}
	return workrecords.NewRepository(s.repo.db).ListMessagesVisible(ctx, a.TenantID, "incident", fmt.Sprint(id), before, limit, notesAllowed(a))
}
func (s *Service) AddMessage(ctx context.Context, a Actor, id uint64, input CommunicationInput, key string) (workrecords.Message, error) {
	input.Body = strings.TrimSpace(input.Body)
	if !requestKeyPattern.MatchString(key) || !validText(input.Body, 1, 20000) || input.Type != workrecords.MessageComment && input.Type != workrecords.MessageWorkNote {
		return workrecords.Message{}, ErrInvalid
	}
	if input.Type == workrecords.MessageComment && !a.Has("communicate") || input.Type == workrecords.MessageWorkNote && (!notesAllowed(a) || !a.Has("notes.write")) {
		return workrecords.Message{}, ErrForbidden
	}
	raw, _ := json.Marshal(struct {
		ID    string
		Input CommunicationInput
	}{fmt.Sprint(id), input})
	hash := sha256.Sum256(raw)
	var message workrecords.Message
	err := s.communicationTx(ctx, a, id, func(e database.Executor, r Record) error {
		existing, err := s.repo.reserveCollaboration(ctx, e, a, key, "message", hash)
		if err != nil {
			return err
		}
		if existing > 0 {
			message, err = workrecords.NewRepository(s.repo.db).GetMessage(ctx, e, a.TenantID, existing)
			return err
		}
		message, err = s.work.AppendMessageInTx(ctx, e, workActor(a), "incident", fmt.Sprint(id), input.Type, input.Body)
		if err != nil {
			return err
		}
		if err = s.repo.completeCollaboration(ctx, e, a, key, message.ID); err != nil {
			return err
		}
		kind, visibility := "work.comment.added", "PUBLIC"
		if input.Type == workrecords.MessageWorkNote {
			kind = "work.work_note.added"
			visibility = "INTERNAL"
		}
		if err := s.collaborationEvent(ctx, e, a, r, kind, visibility, fmt.Sprintf("incident-message:%d", message.ID), map[string]any{"resource_id": fmt.Sprint(message.ID), "message_id": fmt.Sprint(message.ID), "message_type": input.Type, "body": message.Body}); err != nil {
			return err
		}
		if input.Type == workrecords.MessageComment && a.Has("read") && a.UserID != r.ReporterID {
			// One durable business event per incident; retries and later replies keep
			// the first occurrence. No private body enters the SLA contract.
			now := s.now().UTC().Truncate(time.Microsecond)
			payload, err := json.Marshal(ChangeEvent{ActorUserID: a.UserID, IncidentVersion: r.Version, OccurredAt: now, After: snapshot(r)})
			if err != nil {
				return err
			}
			_, created, err := s.outbox.Append(ctx, e, events.Input{TenantID: a.TenantID, Type: "incident.first_response", AggregateType: "incident", AggregateID: fmt.Sprint(id), Payload: payload, IdempotencyKey: fmt.Sprintf("incident-first-response:%d", id), AvailableAt: now})
			if err != nil || !created {
				return err
			}
			return s.audit.Append(ctx, e, audit.Event{TenantID: &a.TenantID, ActorUserID: &a.UserID, Time: now, Action: "incident.first_response", ObjectType: "incident", ObjectID: fmt.Sprint(id), NewValues: map[string]any{"message_id": fmt.Sprint(message.ID), "incident_version": fmt.Sprint(r.Version)}, RequestID: a.RequestID, Source: "api", IPAddress: a.IPAddress})
		}
		return nil
	})
	return message, err
}
func (s *Service) FileList(ctx context.Context, a Actor, id, before uint64, limit int) (FilePage, error) {
	if _, err := s.Get(ctx, a, id); err != nil {
		return FilePage{}, err
	}
	p, err := attachments.NewRepository(s.repo.db).ListVisible(ctx, a.TenantID, "incident", fmt.Sprint(id), before, limit, notesAllowed(a))
	if err != nil {
		return FilePage{}, err
	}
	result := FilePage{Files: []FileDTO{}, NextBeforeID: p.NextBeforeID}
	for _, f := range p.Files {
		result.Files = append(result.Files, fileDTO(a, f))
	}
	return result, nil
}
func (s *Service) authorizedFile(ctx context.Context, e database.Executor, a Actor, id, fileID uint64) (attachments.Attachment, error) {
	f, err := attachments.NewRepository(s.repo.db).GetInTx(ctx, e, a.TenantID, fileID)
	if errors.Is(err, attachments.ErrNotFound) {
		return f, ErrNotFound
	}
	if err != nil {
		return f, err
	}
	if f.ObjectType != "incident" || f.ObjectID != fmt.Sprint(id) || f.Visibility != "PUBLIC" && !notesAllowed(a) {
		return f, ErrNotFound
	}
	if f.MessageID != nil {
		m, err := workrecords.NewRepository(s.repo.db).GetMessage(ctx, e, a.TenantID, *f.MessageID)
		if err != nil {
			return f, ErrNotFound
		}
		if m.ObjectType != "incident" || m.ObjectID != fmt.Sprint(id) || m.Type == workrecords.MessageWorkNote && !notesAllowed(a) {
			return f, ErrNotFound
		}
	}
	return f, nil
}
func (s *Service) OpenFile(ctx context.Context, a Actor, id, fileID uint64) (FileDTO, io.ReadCloser, error) {
	if _, err := s.Get(ctx, a, id); err != nil {
		return FileDTO{}, nil, err
	}
	if s.files == nil {
		return FileDTO{}, nil, errors.New("storage not configured")
	}
	f, err := s.authorizedFile(ctx, s.repo.db, a, id, fileID)
	if err != nil {
		return FileDTO{}, nil, err
	}
	_, reader, err := s.files.Open(ctx, a.TenantID, fileID)
	return fileDTO(a, f), reader, err
}
func (s *Service) UploadFile(ctx context.Context, a Actor, id uint64, name, visibility string, messageID *uint64, content io.Reader, key string) (FileDTO, error) {
	if _, err := s.Get(ctx, a, id); err != nil {
		return FileDTO{}, err
	}
	if s.files == nil {
		return FileDTO{}, errors.New("storage not configured")
	}
	if !requestKeyPattern.MatchString(key) {
		return FileDTO{}, ErrInvalid
	}
	input := attachments.CreateInput{TenantID: a.TenantID, ObjectType: "incident", ObjectID: fmt.Sprint(id), FileName: strings.TrimSpace(name), UploadedBy: &a.UserID, Visibility: visibility, MessageID: messageID}
	// Authorization precedes reading/uploading the body and is rechecked under lock.
	authorize := func(e database.Executor) error {
		if messageID != nil {
			m, err := workrecords.NewRepository(s.repo.db).GetMessage(ctx, e, a.TenantID, *messageID)
			if err != nil || m.ObjectType != "incident" || m.ObjectID != fmt.Sprint(id) {
				return ErrNotFound
			}
			input.Visibility = "PUBLIC"
			if m.Type == workrecords.MessageWorkNote {
				input.Visibility = "INTERNAL"
			}
		}
		if input.Visibility == "INTERNAL" {
			if !notesAllowed(a) || !a.Has("notes.write") {
				return ErrForbidden
			}
		} else if input.Visibility == "PUBLIC" {
			if !a.Has("communicate") {
				return ErrForbidden
			}
		} else {
			return ErrInvalid
		}
		return nil
	}
	if err := authorize(s.repo.db); err != nil {
		return FileDTO{}, err
	}
	prepared, err := s.files.Prepare(ctx, input, content)
	if err != nil {
		return FileDTO{}, err
	}
	raw, _ := json.Marshal(struct {
		Input attachments.CreateInput
		Hash  [32]byte
	}{input, prepared.SHA256})
	hash := sha256.Sum256(raw)
	var f attachments.Attachment
	reused := false
	err = s.communicationTx(ctx, a, id, func(e database.Executor, r Record) error {
		if err := authorize(e); err != nil {
			return err
		}
		existing, err := s.repo.reserveCollaboration(ctx, e, a, key, "attachment", hash)
		if err != nil {
			return err
		}
		if existing > 0 {
			reused = true
			f, err = s.authorizedFile(ctx, e, a, id, existing)
			if err == nil && f.Status != attachments.StatusActive {
				return ErrConflict
			}
			return err
		}
		f, err = s.files.CreatePreparedInTx(ctx, e, input, prepared)
		if err != nil {
			return err
		}
		if err = s.repo.completeCollaboration(ctx, e, a, key, f.ID); err != nil {
			return err
		}
		return s.collaborationEvent(ctx, e, a, r, "work.attachment.added", input.Visibility, fmt.Sprintf("incident-file-added:%d", f.ID), map[string]any{"resource_id": fmt.Sprint(f.ID), "attachment_id": fmt.Sprint(f.ID), "file_name": f.FileName, "content_type": f.ContentType, "size_bytes": fmt.Sprint(f.SizeBytes), "message_id": messageIDString(messageID)})
	})
	if reused || err != nil && !errors.Is(err, database.ErrCommitUncertain) {
		_ = s.files.Discard(context.WithoutCancel(ctx), prepared)
	}
	return fileDTO(a, f), err
}
func messageIDString(id *uint64) any {
	if id == nil {
		return nil
	}
	return fmt.Sprint(*id)
}
func (s *Service) DeleteFile(ctx context.Context, a Actor, id, fileID uint64) error {
	return s.communicationTx(ctx, a, id, func(e database.Executor, r Record) error {
		f, err := s.authorizedFile(ctx, e, a, id, fileID)
		if err != nil {
			return err
		}
		if !fileDTO(a, f).CanDelete {
			return ErrForbidden
		}
		if f.Status != attachments.StatusActive {
			return nil
		}
		changed, err := attachments.NewRepository(s.repo.db).MarkDeletedTx(ctx, e, a.TenantID, fileID)
		if err != nil || !changed {
			return err
		}
		return s.collaborationEvent(ctx, e, a, r, "work.attachment.removed", f.Visibility, fmt.Sprintf("incident-file-removed:%d", fileID), map[string]any{"resource_id": fmt.Sprint(fileID), "attachment_id": fmt.Sprint(fileID), "file_name": f.FileName})
	})
}
