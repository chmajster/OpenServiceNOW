package attachments

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/chmajster/OpenServiceNOW/internal/events"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
)

type Service struct {
	metadata MetadataStore
	transactional TransactionalMetadataStore
	storage Storage
	maxBytes int64
	tx *database.TxManager
	outbox *events.Repository
}

func NewService(metadata MetadataStore, storage Storage, maxBytes int64) (*Service, error) {
	if metadata == nil || storage == nil || maxBytes < 1 {
		return nil, ErrInvalid
	}
	return &Service{metadata: metadata, storage: storage, maxBytes: maxBytes}, nil
}

func NewEventingService(
	metadata TransactionalMetadataStore,
	storage Storage,
	maxBytes int64,
	tx *database.TxManager,
	outbox *events.Repository,
) (*Service, error) {
	if metadata == nil || storage == nil || maxBytes < 1 || tx == nil || outbox == nil {
		return nil, ErrInvalid
	}
	return &Service{
		metadata: metadata,
		transactional: metadata,
		storage: storage,
		maxBytes: maxBytes,
		tx: tx,
		outbox: outbox,
	}, nil
}

func (s *Service) Upload(
	ctx context.Context,
	input CreateInput,
	content io.Reader,
) (Attachment, error) {
	input.ObjectType = strings.TrimSpace(input.ObjectType)
	input.ObjectID = strings.TrimSpace(input.ObjectID)
	input.FileName = strings.TrimSpace(input.FileName)
	if err := validateCreateInput(input); err != nil || content == nil {
		return Attachment{}, ErrInvalid
	}

	reader := bufio.NewReader(content)
	sniff, err := reader.Peek(512)
	if err != nil && err != io.EOF && err != bufio.ErrBufferFull {
		return Attachment{}, fmt.Errorf("read attachment header: %w", err)
	}
	contentType := http.DetectContentType(sniff)

	key, err := newStorageKey(input.TenantID)
	if err != nil {
		return Attachment{}, err
	}
	hasher := sha256.New()
	limited := io.LimitReader(reader, s.maxBytes+1)
	written, err := s.storage.Put(ctx, key, io.TeeReader(limited, hasher))
	if err != nil {
		_ = s.storage.Delete(context.WithoutCancel(ctx), key)
		return Attachment{}, err
	}
	if written > s.maxBytes {
		_ = s.storage.Delete(context.WithoutCancel(ctx), key)
		return Attachment{}, ErrTooLarge
	}
	var sum [32]byte
	copy(sum[:], hasher.Sum(nil))

	var attachment Attachment
	if s.tx == nil {
		attachment, err = s.metadata.Create(ctx, input, key, contentType, uint64(written), sum)
	} else {
		err = s.tx.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(exec database.Executor) error {
			var createErr error
			attachment, createErr = s.transactional.CreateTx(
				ctx, exec, input, key, contentType, uint64(written), sum,
			)
			if createErr != nil {
				return createErr
			}
			payload, marshalErr := attachmentEventPayload(input.UploadedBy, attachment)
			if marshalErr != nil {
				return marshalErr
			}
			_, _, eventErr := s.outbox.Append(ctx, exec, events.Input{
				TenantID: attachment.TenantID,
				Type: "work.attachment.added",
				AggregateType: attachment.ObjectType,
				AggregateID: attachment.ObjectID,
				Payload: payload,
				IdempotencyKey: fmt.Sprintf("attachment-added:%d", attachment.ID),
			})
			return eventErr
		})
	}
	if err != nil {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cleanupCancel()
		_ = s.storage.Delete(cleanupCtx, key)
		return Attachment{}, err
	}
	return attachment, nil
}

func (s *Service) Open(
	ctx context.Context,
	tenantID, id uint64,
) (Attachment, io.ReadCloser, error) {
	attachment, err := s.metadata.Get(ctx, tenantID, id)
	if err != nil {
		return Attachment{}, nil, err
	}
	if attachment.Status != StatusActive {
		return Attachment{}, nil, ErrNotFound
	}
	reader, err := s.storage.Open(ctx, attachment.StorageKey)
	if err != nil {
		return Attachment{}, nil, err
	}
	return attachment, reader, nil
}

func (s *Service) Delete(ctx context.Context, tenantID, id uint64) error {
	return s.DeleteAs(ctx, tenantID, id, 0)
}

func (s *Service) DeleteAs(ctx context.Context, tenantID, id, actorUserID uint64) error {
	attachment, err := s.metadata.BeginDelete(ctx, tenantID, id)
	if err != nil {
		return err
	}
	if err := s.storage.Delete(ctx, attachment.StorageKey); err != nil {
		revertCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = s.metadata.RevertDelete(revertCtx, tenantID, id)
		return err
	}

	finalCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if s.tx == nil {
		return s.metadata.FinishDelete(finalCtx, tenantID, id)
	}
	return s.tx.WithTx(finalCtx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(exec database.Executor) error {
		if actorUserID > 0 {
			if err := s.transactional.ValidateActiveUserTx(finalCtx, exec, tenantID, actorUserID); err != nil {
				return err
			}
		}
		if err := s.transactional.FinishDeleteTx(finalCtx, exec, tenantID, id); err != nil {
			return err
		}
		var actor *uint64
		if actorUserID > 0 {
			value := actorUserID
			actor = &value
		}
		payload, err := attachmentEventPayload(actor, attachment)
		if err != nil {
			return err
		}
		_, _, err = s.outbox.Append(finalCtx, exec, events.Input{
			TenantID: attachment.TenantID,
			Type: "work.attachment.removed",
			AggregateType: attachment.ObjectType,
			AggregateID: attachment.ObjectID,
			Payload: payload,
			IdempotencyKey: fmt.Sprintf("attachment-removed:%d", attachment.ID),
		})
		return err
	})
}

func attachmentEventPayload(actor *uint64, attachment Attachment) (json.RawMessage, error) {
	payload, err := json.Marshal(EventPayload{
		ActorUserID: actor,
		AttachmentID: attachment.ID,
		FileName: attachment.FileName,
		ContentType: attachment.ContentType,
		SizeBytes: attachment.SizeBytes,
		SHA256: hex.EncodeToString(attachment.SHA256[:]),
	})
	if err != nil {
		return nil, fmt.Errorf("marshal attachment event: %w", err)
	}
	return payload, nil
}

func validateCreateInput(input CreateInput) error {
	if input.TenantID == 0 ||
		input.ObjectType == "" || len(input.ObjectType) > 100 ||
		input.ObjectID == "" || len(input.ObjectID) > 128 ||
		input.FileName == "" || len([]rune(input.FileName)) > 255 ||
		strings.ContainsAny(input.FileName, "/\\") ||
		(input.UploadedBy != nil && *input.UploadedBy == 0) {
		return ErrInvalid
	}
	return nil
}

func newStorageKey(tenantID uint64) (string, error) {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", fmt.Errorf("generate attachment storage key: %w", err)
	}
	return fmt.Sprintf("%d/%s", tenantID, hex.EncodeToString(random[:])), nil
}
