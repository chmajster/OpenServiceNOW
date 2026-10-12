package incidents

import (
	"github.com/chmajster/OpenServiceNOW/internal/attachments"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
	"time"
)

func notesAllowed(a Actor) bool { return a.Has("read") && a.Has("notes.read") }

type CommunicationInput struct {
	Type workrecords.MessageType `json:"type"`
	Body string                  `json:"body"`
}
type FileDTO struct {
	ID         uint64    `json:"id,string"`
	Name       string    `json:"file_name"`
	MIME       string    `json:"content_type"`
	Size       uint64    `json:"size_bytes,string"`
	Visibility string    `json:"visibility"`
	MessageID  *uint64   `json:"message_id,string,omitempty"`
	UploadedBy *uint64   `json:"uploaded_by,string,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	CanDelete  bool      `json:"can_delete"`
}

func fileDTO(a Actor, f attachments.Attachment) FileDTO {
	return FileDTO{f.ID, f.FileName, f.ContentType, f.SizeBytes, f.Visibility, f.MessageID, f.UploadedBy, f.CreatedAt, a.Has("attachments.delete") || f.UploadedBy != nil && *f.UploadedBy == a.UserID}
}

type FilePage struct {
	Files        []FileDTO `json:"files"`
	NextBeforeID uint64    `json:"next_before_id,string,omitempty"`
}
type CollaborationAccess struct {
	Reply        bool  `json:"reply"`
	ReadNotes    bool  `json:"read_notes"`
	WriteNotes   bool  `json:"write_notes"`
	Manage       bool  `json:"manage"`
	MaxFileBytes int64 `json:"max_file_bytes"`
}

func collaborationAccess(a Actor) CollaborationAccess {
	return CollaborationAccess{a.Has("communicate"), notesAllowed(a), notesAllowed(a) && a.Has("notes.write"), a.Has("read") && a.Has("update"), attachments.MaxFileBytes}
}

type WatcherDTO struct {
	UserID    uint64    `json:"user_id,string"`
	Name      string    `json:"display_name"`
	CreatedAt time.Time `json:"created_at"`
}
type WatcherList struct {
	Watchers    []WatcherDTO `json:"watchers"`
	NextAfterID uint64       `json:"next_after_id,string,omitempty"`
	Watching    bool         `json:"watching"`
}
type RelationDTO struct {
	ID       uint64 `json:"id,string"`
	Type     string `json:"type"`
	SourceID uint64 `json:"source_id,string"`
	TargetID uint64 `json:"target_id,string"`
	OtherID  uint64 `json:"other_id,string"`
	Number   string `json:"number"`
	Title    string `json:"title"`
}
type RelationList struct {
	Relations    []RelationDTO `json:"relations"`
	NextBeforeID uint64        `json:"next_before_id,string,omitempty"`
}
type RelationInput struct {
	Type     string `json:"type"`
	TargetID uint64 `json:"target_id,string"`
}
type BatchItem struct {
	ID      uint64 `json:"id,string"`
	Version uint64 `json:"version,string"`
}
type BatchInput struct {
	Action string      `json:"action"`
	Items  []BatchItem `json:"items"`
}
type BatchResult struct {
	ID      uint64  `json:"id,string"`
	Status  int     `json:"status"`
	Message string  `json:"message"`
	Record  *Record `json:"record,omitempty"`
}
