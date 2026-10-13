package problems

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/chmajster/OpenServiceNOW/internal/attachments"
	"github.com/chmajster/OpenServiceNOW/internal/catalog"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
)

func (s *Service) communicationActor(ctx context.Context, a Actor, id uint64) (Actor, error) {
	_, err := s.Get(ctx, a, id)
	return a, err
}
func (s *Service) communicationTx(ctx context.Context, a Actor, id uint64, fn func(database.Executor, Actor, Record) error) error {
	return s.withProblem(ctx, a, id, fn)
}
func (s *Service) collaborationEvent(ctx context.Context, e database.Executor, a Actor, r Record, kind, visibility, key string, data map[string]any) error {
	return s.event(ctx, e, a, r, kind, visibility, key, data)
}
func (s *Service) Messages(ctx context.Context, a Actor, id, before uint64, limit int) (workrecords.MessagePage, error) {
	var accessErr error
	a, accessErr = s.communicationActor(ctx, a, id)
	if accessErr != nil {
		err := accessErr
		return workrecords.MessagePage{}, err
	}
	return workrecords.NewRepository(s.db).ListMessagesVisible(ctx, a.TenantID, "problem", fmt.Sprint(id), before, limit, notesAllowed(a))
}
func (s *Service) AddMessage(ctx context.Context, a Actor, id uint64, input CommunicationInput, key string) (workrecords.Message, error) {
	var accessErr error
	a, accessErr = s.communicationActor(ctx, a, id)
	if accessErr != nil {
		err := accessErr
		return workrecords.Message{}, err
	}
	input.Body = strings.TrimSpace(input.Body)
	if !requestKeyPattern.MatchString(key) || !catalog.Text(input.Body, 1, 20000) || input.Type != workrecords.MessageComment && input.Type != workrecords.MessageWorkNote {
		return workrecords.Message{}, ErrInvalid
	}
	if input.Type == workrecords.MessageComment && !canReply(a) || input.Type == workrecords.MessageWorkNote && !notesAllowed(a) {
		return workrecords.Message{}, ErrForbidden
	}
	raw, _ := json.Marshal(struct {
		ID    string
		Input CommunicationInput
	}{fmt.Sprint(id), input})
	hash := sha256.Sum256(raw)
	var message workrecords.Message
	err := s.communicationTx(ctx, a, id, func(e database.Executor, a Actor, r Record) error {
		if input.Type == workrecords.MessageWorkNote && !notesAllowed(a) || input.Type == workrecords.MessageComment && !canReply(a) {
			return ErrForbidden
		}

		existing, err := reserve(ctx, e, a, key, "message", hash)
		if err != nil {
			return err
		}
		if existing > 0 {
			message, err = workrecords.NewRepository(s.db).GetMessage(ctx, e, a.TenantID, existing)
			return err
		}
		message, err = s.work.AppendMessage(ctx, e, a.TenantID, "problem", fmt.Sprint(id), input.Type, input.Body, a.UserID)
		if err != nil {
			return err
		}
		if err = complete(ctx, e, a, key, message.ID); err != nil {
			return err
		}
		kind, visibility := "problem.comment.added", "PUBLIC"
		if input.Type == workrecords.MessageWorkNote {
			kind = "problem.note.added"
			visibility = "INTERNAL"
		}
		if err := s.collaborationEvent(ctx, e, a, r, kind, visibility, fmt.Sprintf("problem-message:%d", message.ID), map[string]any{"resource_id": fmt.Sprint(message.ID), "message_id": fmt.Sprint(message.ID), "message_type": input.Type, "body": message.Body}); err != nil {
			return err
		}

		return nil
	})
	return message, err
}
func (s *Service) FileList(ctx context.Context, a Actor, id, before uint64, limit int) (FilePage, error) {
	var accessErr error
	a, accessErr = s.communicationActor(ctx, a, id)
	if accessErr != nil {
		err := accessErr
		return FilePage{}, err
	}
	p, err := attachments.NewRepository(s.db).ListVisible(ctx, a.TenantID, "problem", fmt.Sprint(id), before, limit, notesAllowed(a))
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
	f, err := attachments.NewRepository(s.db).GetInTx(ctx, e, a.TenantID, fileID)
	if errors.Is(err, attachments.ErrNotFound) {
		return f, ErrNotFound
	}
	if err != nil {
		return f, err
	}
	if f.ObjectType != "problem" || f.ObjectID != fmt.Sprint(id) || f.Visibility != "PUBLIC" && !notesAllowed(a) {
		return f, ErrNotFound
	}
	if f.MessageID != nil {
		m, err := workrecords.NewRepository(s.db).GetMessage(ctx, e, a.TenantID, *f.MessageID)
		if err != nil {
			if errors.Is(err, workrecords.ErrNotFound) {
				return f, ErrNotFound
			}
			return f, err
		}
		if m.ObjectType != "problem" || m.ObjectID != fmt.Sprint(id) || m.Type == workrecords.MessageWorkNote && !notesAllowed(a) {
			return f, ErrNotFound
		}
	}
	return f, nil
}
func (s *Service) OpenFile(ctx context.Context, a Actor, id, fileID uint64) (FileDTO, io.ReadCloser, error) {
	var accessErr error
	a, accessErr = s.communicationActor(ctx, a, id)
	if accessErr != nil {
		err := accessErr
		return FileDTO{}, nil, err
	}
	if s.files == nil {
		return FileDTO{}, nil, errors.New("storage not configured")
	}
	f, err := s.authorizedFile(ctx, s.db, a, id, fileID)
	if err != nil {
		return FileDTO{}, nil, err
	}
	_, reader, err := s.files.Open(ctx, a.TenantID, fileID)
	return fileDTO(a, f), reader, err
}
func (s *Service) UploadFile(ctx context.Context, a Actor, id uint64, name, visibility string, messageID *uint64, content io.Reader, key string) (FileDTO, error) {
	var accessErr error
	a, accessErr = s.communicationActor(ctx, a, id)
	if accessErr != nil {
		err := accessErr
		return FileDTO{}, err
	}
	if s.files == nil {
		return FileDTO{}, errors.New("storage not configured")
	}
	if !requestKeyPattern.MatchString(key) {
		return FileDTO{}, ErrInvalid
	}
	input := attachments.CreateInput{TenantID: a.TenantID, ObjectType: "problem", ObjectID: fmt.Sprint(id), FileName: strings.TrimSpace(name), UploadedBy: &a.UserID, Visibility: visibility, MessageID: messageID}
	// Authorization precedes reading/uploading the body and is rechecked under lock.
	authorize := func(e database.Executor, a Actor) error {
		if messageID != nil {
			m, err := workrecords.NewRepository(s.db).GetMessage(ctx, e, a.TenantID, *messageID)
			if err != nil {
				if errors.Is(err, workrecords.ErrNotFound) {
					return ErrNotFound
				}
				return err
			}
			if m.ObjectType != "problem" || m.ObjectID != fmt.Sprint(id) {
				return ErrNotFound
			}
			input.Visibility = "PUBLIC"
			if m.Type == workrecords.MessageWorkNote {
				input.Visibility = "INTERNAL"
			}
		}
		if input.Visibility == "INTERNAL" {
			if !notesAllowed(a) {
				return ErrForbidden
			}
		} else if input.Visibility == "PUBLIC" {
			if !canReply(a) {
				return ErrForbidden
			}
		} else {
			return ErrInvalid
		}
		return nil
	}
	if err := authorize(s.db, a); err != nil {
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
	err = s.communicationTx(ctx, a, id, func(e database.Executor, a Actor, r Record) error {
		if err := authorize(e, a); err != nil {
			return err
		}
		existing, err := reserve(ctx, e, a, key, "attachment", hash)
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
		if err = complete(ctx, e, a, key, f.ID); err != nil {
			return err
		}
		return s.collaborationEvent(ctx, e, a, r, "problem.attachment.added", input.Visibility, fmt.Sprintf("problem-file-added:%d", f.ID), map[string]any{"resource_id": fmt.Sprint(f.ID), "attachment_id": fmt.Sprint(f.ID), "file_name": f.FileName, "content_type": f.ContentType, "size_bytes": fmt.Sprint(f.SizeBytes), "message_id": messageIDString(messageID)})
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
	return s.communicationTx(ctx, a, id, func(e database.Executor, a Actor, r Record) error {
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
		changed, err := attachments.NewRepository(s.db).MarkDeletedTx(ctx, e, a.TenantID, fileID)
		if err != nil || !changed {
			return err
		}
		return s.collaborationEvent(ctx, e, a, r, "problem.attachment.removed", f.Visibility, fmt.Sprintf("problem-file-removed:%d", fileID), map[string]any{"resource_id": fmt.Sprint(fileID), "attachment_id": fmt.Sprint(fileID), "file_name": f.FileName})
	})
}
