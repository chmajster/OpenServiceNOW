package activity

import (
	"encoding/json"
	"errors"
	"time"
)

var (
	ErrInvalid = errors.New("invalid activity input")
)

type Entry struct {
	ActorName     string          `json:"actor_name"`
	ID            uint64          `json:"id,string"`
	TenantID      uint64          `json:"tenant_id,string"`
	ObjectType    string          `json:"object_type"`
	ObjectID      string          `json:"object_id"`
	Type          string          `json:"type"`
	ActorUserID   *uint64         `json:"actor_user_id,string,omitempty"`
	Summary       string          `json:"summary"`
	Data          json.RawMessage `json:"data,omitempty"`
	SourceEventID *uint64         `json:"source_event_id,string,omitempty"`
	EventTime     time.Time       `json:"event_time"`
}

type Input struct {
	Visibility    string
	TenantID      uint64
	ObjectType    string
	ObjectID      string
	Type          string
	ActorUserID   *uint64
	Summary       string
	Data          json.RawMessage
	SourceEventID *uint64
	EventTime     time.Time
}

type Page struct {
	Entries      []Entry `json:"entries"`
	NextBeforeID uint64  `json:"next_before_id,string,omitempty"`
}
