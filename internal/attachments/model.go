package attachments

import (
	"errors"
	"time"
)

type Status string

const (
	StatusActive   Status = "ACTIVE"
	StatusDeleting Status = "DELETING"
	StatusDeleted  Status = "DELETED"
)

var (
	ErrInvalid          = errors.New("invalid attachment input")
	ErrNotFound         = errors.New("attachment not found")
	ErrConflict         = errors.New("attachment state conflict")
	ErrTooLarge         = errors.New("attachment exceeds size limit")
	ErrUnsafeStorageKey = errors.New("unsafe attachment storage key")
)

type Attachment struct {
	ID          uint64
	TenantID    uint64
	ObjectType  string
	ObjectID    string
	FileName    string
	ContentType string
	SizeBytes   uint64
	SHA256      [32]byte
	StorageKey  string
	Status      Status
	UploadedBy  *uint64
	CreatedAt   time.Time
	DeletedAt   *time.Time
}

type CreateInput struct {
	TenantID   uint64
	ObjectType string
	ObjectID   string
	FileName   string
	UploadedBy *uint64
}

type EventPayload struct {
	ActorUserID  *uint64 `json:"actor_user_id,string,omitempty"`
	AttachmentID uint64  `json:"attachment_id,string"`
	FileName     string  `json:"file_name"`
	ContentType  string  `json:"content_type"`
	SizeBytes    uint64  `json:"size_bytes,string"`
	SHA256       string  `json:"sha256"`
}
