package requests

import (
	"github.com/chmajster/OpenServiceNOW/internal/attachments"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
	"time"
)

func notesAllowed(a Actor) bool { return a.Has("request.operator") }

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
	return FileDTO{f.ID, f.FileName, f.ContentType, f.SizeBytes, f.Visibility, f.MessageID, f.UploadedBy, f.CreatedAt, a.Has("request.operator") || f.UploadedBy != nil && *f.UploadedBy == a.UserID}
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
	return CollaborationAccess{canReply(a), notesAllowed(a), notesAllowed(a), notesAllowed(a), attachments.MaxFileBytes}
}

func canReply(a Actor) bool { return a.Has("request.operator") || a.Has("request.read_own") }
