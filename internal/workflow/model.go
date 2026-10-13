package workflow

import (
	"encoding/json"
	"errors"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
	"time"
)

type Actor = workrecords.Principal

var (
	ErrInvalid  = errors.New("nieprawidłowa definicja procesu")
	ErrConflict = errors.New("stan procesu lub decyzji zmienił się; porównaj aktualny stan ze swoją propozycją")
	ErrNotFound = errors.New("nie znaleziono dostępnego procesu")
)

const JobType = "workflow.advance"

type Node struct {
	ID           string          `json:"id"`
	Label        string          `json:"label"`
	Type         string          `json:"type"`
	Next         string          `json:"next,omitempty"`
	Otherwise    string          `json:"otherwise,omitempty"`
	Rejected     string          `json:"rejected,omitempty"`
	Timeout      string          `json:"timeout,omitempty"`
	Field        string          `json:"field,omitempty"`
	Equals       json.RawMessage `json:"equals,omitempty"`
	TargetType   string          `json:"target_type,omitempty"`
	TargetID     uint64          `json:"target_id,string,omitempty"`
	Policy       string          `json:"policy,omitempty"`
	Seconds      int64           `json:"seconds,omitempty"`
	TaskRef      string          `json:"task_ref,omitempty"`
	Title        string          `json:"title,omitempty"`
	Instructions string          `json:"instructions,omitempty"`
	State        string          `json:"state,omitempty"`
	Reason       string          `json:"reason,omitempty"`
	Audience     string          `json:"audience,omitempty"`
}
type Config struct {
	Domain       string `json:"domain,omitempty"`
	ChangeType   string `json:"change_type,omitempty"`
	Name         string `json:"name"`
	ExecutorID   uint64 `json:"executor_id,string"`
	GroupID      uint64 `json:"group_id,string"`
	SelfApproval string `json:"self_approval"`
	Nodes        []Node `json:"nodes"`
}
type Definition struct {
	ID               uint64 `json:"id,string"`
	Version          uint64 `json:"version,string"`
	PublishedVersion string `json:"published_version,omitempty"`
	Draft            Config `json:"draft"`
}
type Execution struct {
	Domain            string          `json:"domain"`
	ChangeID          uint64          `json:"change_id,string,omitempty"`
	SubjectVersion    uint64          `json:"subject_version,string,omitempty"`
	SubjectRevision   uint64          `json:"subject_revision,string,omitempty"`
	SubjectSnapshot   json.RawMessage `json:"subject_snapshot,omitempty"`
	ChangeNumber      string          `json:"change_number,omitempty"`
	ID                uint64          `json:"id,string"`
	DefinitionID      uint64          `json:"definition_id,string"`
	DefinitionVersion uint64          `json:"definition_version,string"`
	RequestID         uint64          `json:"request_id,string"`
	RequestedItemID   uint64          `json:"requested_item_id,string"`
	RequesterID       uint64          `json:"-"`
	ExecutorID        uint64          `json:"executor_id,string"`
	GroupID           uint64          `json:"group_id,string"`
	CurrentNode       string          `json:"current_node"`
	State             string          `json:"state"`
	Version           uint64          `json:"version,string"`
	Approved          bool            `json:"approved"`
	ErrorCode         string          `json:"error_code,omitempty"`
	NextCheckAt       *time.Time      `json:"next_check_at,omitempty"`
	CreatedAt         time.Time       `json:"created_at"`
	UpdatedAt         time.Time       `json:"updated_at"`
	Name              string          `json:"name"`
	RequestNumber     string          `json:"request_number"`
	Snapshot          json.RawMessage `json:"snapshot,omitempty"`
	Values            json.RawMessage `json:"values,omitempty"`
	Steps             []Step          `json:"steps"`
	Attempts          []Attempt       `json:"attempts,omitempty"`
	Actions           []string        `json:"actions"`
}
type Step struct {
	NodeID     string          `json:"node_id"`
	Label      string          `json:"label"`
	Type       string          `json:"type"`
	State      string          `json:"state"`
	Version    uint64          `json:"version,string"`
	Result     json.RawMessage `json:"result,omitempty"`
	StartedAt  *time.Time      `json:"started_at,omitempty"`
	Deadline   *time.Time      `json:"deadline,omitempty"`
	FinishedAt *time.Time      `json:"finished_at,omitempty"`
	Decisions  []Decision      `json:"decisions,omitempty"`
	CanDecide  bool            `json:"can_decide"`
}
type Decision struct {
	UserID    uint64     `json:"user_id,string"`
	Name      string     `json:"name"`
	Decision  string     `json:"decision"`
	Reason    string     `json:"reason"`
	DecidedAt *time.Time `json:"decided_at,omitempty"`
}
type DecisionInput struct {
	Version  uint64 `json:"version,string"`
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
}
type Attempt struct {
	ID        uint64    `json:"id,string"`
	NodeID    string    `json:"node_id"`
	Version   uint64    `json:"execution_version,string"`
	Outcome   string    `json:"outcome"`
	ErrorCode string    `json:"error_code,omitempty"`
	At        time.Time `json:"occurred_at"`
}
