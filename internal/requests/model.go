package requests

import (
	"encoding/json"
	"errors"
	"github.com/chmajster/OpenServiceNOW/internal/catalog"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
	"time"
)

type Actor = workrecords.Principal

var (
	ErrInvalid     = catalog.ErrInvalid
	ErrNotFound    = catalog.ErrNotFound
	ErrConflict    = catalog.ErrConflict
	ErrForbidden   = workrecords.ErrForbidden
	ErrIdempotency = errors.New("klucz ponowienia został użyty dla innej operacji lub treści")
	ErrTransition  = errors.New("niedozwolona zmiana stanu lub nieukończone wymagane zadania")
)

type SubmissionItem struct {
	ItemID  uint64                     `json:"item_id,string"`
	Version uint64                     `json:"version,string"`
	Values  map[string]json.RawMessage `json:"values"`
}
type Submission struct {
	Items []SubmissionItem `json:"items"`
}
type RequestedItem struct {
	ID                uint64                     `json:"id,string"`
	Number            string                     `json:"number"`
	Snapshot          catalog.Publication        `json:"snapshot"`
	Values            map[string]json.RawMessage `json:"values"`
	Total             int                        `json:"total_tasks"`
	Completed         int                        `json:"completed_tasks"`
	Required          int                        `json:"required_tasks"`
	RequiredCompleted int                        `json:"required_completed"`
}
type Request struct {
	ID            uint64          `json:"id,string"`
	Number        string          `json:"number"`
	RequesterID   uint64          `json:"requester_id,string"`
	RequesterName string          `json:"requester_name"`
	State         string          `json:"state"`
	Reason        string          `json:"reason"`
	Version       uint64          `json:"version,string"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
	Items         []RequestedItem `json:"items"`
	Actions       []string        `json:"actions"`
}
type Task struct {
	ID              uint64                 `json:"id,string"`
	RequestID       uint64                 `json:"request_id,string"`
	RequestedItemID uint64                 `json:"requested_item_id,string"`
	Number          string                 `json:"number"`
	RequestNumber   string                 `json:"request_number"`
	Title           string                 `json:"title"`
	Instructions    string                 `json:"instructions"`
	Required        bool                   `json:"required"`
	State           string                 `json:"state"`
	Result          string                 `json:"result"`
	Version         uint64                 `json:"version,string"`
	UpdatedAt       time.Time              `json:"updated_at"`
	Assignment      workrecords.Assignment `json:"assignment"`
	AssignedName    string                 `json:"assigned_name"`
	GroupName       string                 `json:"group_name"`
	Actions         []string               `json:"actions"`
}
type Change struct {
	Version    uint64  `json:"version,string"`
	State      string  `json:"state"`
	Reason     string  `json:"reason"`
	AssignedTo *uint64 `json:"assigned_to,string,omitempty"`
}
type RequestPage struct {
	Total     uint64       `json:"total,string"`
	Groups    []GroupCount `json:"groups"`
	NextValue string       `json:"next_before_value,omitempty"`
	Items     []Request    `json:"items"`
	Next      uint64       `json:"next_before_id,string,omitempty"`
}
type TaskPage struct {
	Total     uint64       `json:"total,string"`
	Groups    []GroupCount `json:"groups"`
	NextValue string       `json:"next_before_value,omitempty"`
	Items     []Task       `json:"items"`
	Next      uint64       `json:"next_before_id,string,omitempty"`
}
