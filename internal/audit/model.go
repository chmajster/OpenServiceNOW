package audit

import (
	"encoding/json"
	"time"
)

type Event struct {
	TenantID    *uint64
	ActorUserID *uint64
	Time        time.Time
	Action      string
	ObjectType  string
	ObjectID    string
	OldValues   any
	NewValues   any
	RequestID   string
	Source      string
	IPAddress   string
}

type Record struct {
	ID          uint64          `json:"id"`
	ActorUserID *uint64         `json:"actor_user_id,omitempty"`
	Time        time.Time       `json:"time"`
	Action      string          `json:"action"`
	ObjectType  string          `json:"object_type"`
	ObjectID    string          `json:"object_id,omitempty"`
	OldValues   json.RawMessage `json:"old_values,omitempty"`
	NewValues   json.RawMessage `json:"new_values,omitempty"`
	RequestID   string          `json:"request_id,omitempty"`
	Source      string          `json:"source"`
	IPAddress   string          `json:"ip_address,omitempty"`
}

type Page struct {
	Events       []Record `json:"events"`
	NextBeforeID uint64   `json:"next_before_id,omitempty"`
}
