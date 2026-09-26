package attachments

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type Service struct {
	metadata MetadataStore
	storage Storage
	maxBytes int64
}

func NewService(metadata MetadataStore, storage Storage, maxBytes int64) (*Service, error) {
	if metadata == nil || storage == nil || maxBytes < 1 {
		return nil, ErrInvalid
	}
	return &Service{metadata: metadata, storage: storage, maxBytes: maxBytes}, nil
}

func (s *Service) Upload(
	ctx context.Context,
	input CreateInput,
	content io.Reader,
) (Attachment, error) {
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
	attachment, err := s.metadata.Create(ctx, input, key, contentType, uint64(written), sum)
	if err != nil {
		_ = s.storage.Delete(context.WithoutCancel(ctx), key)
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
	attachment, err := s.metadata.BeginDelete(ctx, tenantID, id)
	if err != nil {
		return err
	}
	if err := s.storage.Delete(ctx, attachment.StorageKey); err != nil {
		revertCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
		defer cancel()
		_ = s.metadata.RevertDelete(revertCtx, tenantID, id)
		return err
	}
	if err := s.metadata.FinishDelete(ctx, tenantID, id); err != nil {
		return err
	}
	return nil
}

func validateCreateInput(input CreateInput) error {
	input.ObjectType = strings.TrimSpace(input.ObjectType)
	input.ObjectID = strings.TrimSpace(input.ObjectID)
	input.FileName = strings.TrimSpace(input.FileName)
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
