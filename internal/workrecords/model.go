package workrecords

import (
	"errors"
	"time"
)

var (
	ErrInvalid  = errors.New("invalid work record input")
	ErrNotFound = errors.New("work record resource not found")
	ErrConflict = errors.New("work record conflict")
)

type MessageType string

const (
	MessageComment  MessageType = "COMMENT"
	MessageWorkNote MessageType = "WORK_NOTE"
)

type Actor struct {
	TenantID  uint64
	UserID    uint64
	RequestID string
}

type Assignment struct {
	TenantID          uint64    `json:"tenant_id,string"`
	ObjectType        string    `json:"object_type"`
	ObjectID          string    `json:"object_id"`
	AssignedTo        *uint64   `json:"assigned_to,string,omitempty"`
	AssignmentGroupID *uint64   `json:"assignment_group_id,string,omitempty"`
	Version           uint64    `json:"version,string"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type AssignmentChange struct {
	Before  *Assignment
	After   Assignment
	Changed bool
}

type Message struct {
	ID           uint64      `json:"id,string"`
	TenantID     uint64      `json:"tenant_id,string"`
	ObjectType   string      `json:"object_type"`
	ObjectID     string      `json:"object_id"`
	Type         MessageType `json:"type"`
	Body         string      `json:"body"`
	AuthorUserID uint64      `json:"author_user_id,string"`
	CreatedAt    time.Time   `json:"created_at"`
}

type MessagePage struct {
	Messages     []Message `json:"messages"`
	NextBeforeID uint64    `json:"next_before_id,string,omitempty"`
}

type Watcher struct {
	UserID    uint64    `json:"user_id,string"`
	CreatedBy uint64    `json:"created_by,string"`
	CreatedAt time.Time `json:"created_at"`
}

type WatcherPage struct {
	Watchers []Watcher `json:"watchers"`
}

type Relation struct {
	ID         uint64    `json:"id,string"`
	TenantID   uint64    `json:"tenant_id,string"`
	SourceType string    `json:"source_type"`
	SourceID   string    `json:"source_id"`
	Type       string    `json:"type"`
	TargetType string    `json:"target_type"`
	TargetID   string    `json:"target_id"`
	CreatedBy  uint64    `json:"created_by,string"`
	CreatedAt  time.Time `json:"created_at"`
}

type RelationPage struct {
	Relations    []Relation `json:"relations"`
	NextBeforeID uint64     `json:"next_before_id,string,omitempty"`
}

type AssignmentEventPayload struct {
	ActorUserID       uint64  `json:"actor_user_id,string"`
	AssignedTo        *uint64 `json:"assigned_to,omitempty"`
	AssignmentGroupID *uint64 `json:"assignment_group_id,omitempty"`
	Version           uint64  `json:"version,string"`
}

type MessageEventPayload struct {
	ActorUserID uint64      `json:"actor_user_id,string"`
	MessageID   uint64      `json:"message_id,string"`
	MessageType MessageType `json:"message_type"`
	Body        string      `json:"body"`
}

type WatcherEventPayload struct {
	ActorUserID uint64 `json:"actor_user_id,string"`
	UserID      uint64 `json:"user_id,string"`
}

type RelationEventPayload struct {
	ActorUserID  uint64 `json:"actor_user_id,string"`
	RelationID   uint64 `json:"relation_id,string"`
	RelationType string `json:"relation_type"`
	TargetType   string `json:"target_type"`
	TargetID     string `json:"target_id"`
}
