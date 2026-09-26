package attachments

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

type memoryStorage struct {
	data map[string][]byte
}

func (m *memoryStorage) Put(_ context.Context, key string, reader io.Reader) (int64, error) {
	if m.data == nil {
		m.data = map[string][]byte{}
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		return 0, err
	}
	m.data[key] = data
	return int64(len(data)), nil
}

func (m *memoryStorage) Open(_ context.Context, key string) (io.ReadCloser, error) {
	data, ok := m.data[key]
	if !ok {
		return nil, ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (m *memoryStorage) Delete(_ context.Context, key string) error {
	delete(m.data, key)
	return nil
}

type memoryMetadata struct {
	item Attachment
	nextID uint64
}

func (m *memoryMetadata) Create(_ context.Context, input CreateInput, key, contentType string, size uint64, sum [32]byte) (Attachment, error) {
	m.nextID++
	m.item = Attachment{
		ID: m.nextID, TenantID: input.TenantID, ObjectType: input.ObjectType,
		ObjectID: input.ObjectID, FileName: input.FileName, ContentType: contentType,
		SizeBytes: size, SHA256: sum, StorageKey: key, Status: StatusActive,
	}
	return m.item, nil
}

func (m *memoryMetadata) Get(_ context.Context, tenantID, id uint64) (Attachment, error) {
	if m.item.ID != id || m.item.TenantID != tenantID {
		return Attachment{}, ErrNotFound
	}
	return m.item, nil
}

func (m *memoryMetadata) BeginDelete(ctx context.Context, tenantID, id uint64) (Attachment, error) {
	item, err := m.Get(ctx, tenantID, id)
	if err != nil {
		return Attachment{}, err
	}
	if item.Status != StatusActive {
		return Attachment{}, ErrConflict
	}
	m.item.Status = StatusDeleting
	return m.item, nil
}

func (m *memoryMetadata) RevertDelete(context.Context, uint64, uint64) error {
	m.item.Status = StatusActive
	return nil
}

func (m *memoryMetadata) FinishDelete(context.Context, uint64, uint64) error {
	m.item.Status = StatusDeleted
	return nil
}

func TestServiceUploadOpenDelete(t *testing.T) {
	metadata := &memoryMetadata{}
	storage := &memoryStorage{}
	service, err := NewService(metadata, storage, 1024)
	if err != nil {
		t.Fatal(err)
	}
	item, err := service.Upload(context.Background(), CreateInput{
		TenantID: 1, ObjectType: "incident", ObjectID: "INC0001", FileName: "note.txt",
	}, strings.NewReader("hello"))
	if err != nil {
		t.Fatalf("Upload() error = %v", err)
	}
	if item.SizeBytes != 5 || item.ContentType == "" {
		t.Fatalf("attachment = %+v", item)
	}
	_, reader, err := service.Open(context.Background(), 1, item.ID)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	data, _ := io.ReadAll(reader)
	_ = reader.Close()
	if string(data) != "hello" {
		t.Fatalf("data = %q", data)
	}
	if err := service.Delete(context.Background(), 1, item.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, _, err := service.Open(context.Background(), 1, item.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Open() after delete error = %v", err)
	}
}

func TestServiceRejectsOversizedUpload(t *testing.T) {
	metadata := &memoryMetadata{}
	storage := &memoryStorage{}
	service, _ := NewService(metadata, storage, 4)
	_, err := service.Upload(context.Background(), CreateInput{
		TenantID: 1, ObjectType: "incident", ObjectID: "1", FileName: "large.bin",
	}, strings.NewReader("12345"))
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("error = %v", err)
	}
	if len(storage.data) != 0 {
		t.Fatalf("oversized payload remained in storage: %+v", storage.data)
	}
}
