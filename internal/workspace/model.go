package workspace

import (
	"encoding/json"
	"errors"
	"strconv"
	"time"
)

var (
	ErrInvalid  = errors.New("invalid workspace input")
	ErrNotFound = errors.New("workspace resource not found")
	ErrForbidden = errors.New("workspace access denied")
	ErrConflict = errors.New("workspace version conflict")
)

const (
	IncidentType = "incident"
	TaskType     = "fulfillment_task"
)

type Actor struct {
	TenantID  uint64
	UserID    uint64
	RequestID string
	IPAddress string
	Permissions map[string]bool
}

func (a Actor) Has(permission string) bool { return a.Permissions[permission] }
func (a Actor) CanReadIncidents() bool { return a.Has("incident.read") || a.Has("incident.read_own") }
func (a Actor) CanReadTasks() bool { return a.Has("request.manage") || a.Has("request.fulfill") }
func (a Actor) CanUse() bool { return a.TenantID != 0 && a.UserID != 0 && (a.CanReadIncidents() || a.CanReadTasks()) }

type WorkReference struct {
	TenantID uint64 `json:"tenant_id,string"`
	ObjectType string `json:"object_type"`
	ObjectID string `json:"object_id"`
}

func (r WorkReference) Key() string { return strconv.FormatUint(r.TenantID, 10) + ":" + r.ObjectType + ":" + r.ObjectID }

type WorkItem struct {
	WorkReference
	Number string `json:"number"`
	Title string `json:"title"`
	State string `json:"state"`
	StateCategory string `json:"state_category"`
	Category string `json:"category"`
	Priority *int `json:"priority,omitempty"`
	AssignedTo *uint64 `json:"assigned_to,string,omitempty"`
	AssignedName string `json:"assigned_name"`
	AssignmentGroupID *uint64 `json:"assignment_group_id,string,omitempty"`
	GroupName string `json:"group_name"`
	Version uint64 `json:"version,string"`
	AssignmentVersion uint64 `json:"assignment_version,string"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	DueAt *time.Time `json:"due_at,omitempty"`
	Watched bool `json:"watched"`
	RecordPath string `json:"record_path"`
	Capabilities []string `json:"capabilities"`
	SearchRank int `json:"search_rank,omitempty"`
	CustomFields map[string]json.RawMessage `json:"custom_fields,omitempty"`
}

type FilterNode struct {
	Operator string `json:"operator,omitempty"`
	Children []FilterNode `json:"children,omitempty"`
	Field string `json:"field,omitempty"`
	Compare string `json:"compare,omitempty"`
	Value json.RawMessage `json:"value,omitempty"`
}

type Query struct {
	Types []string `json:"types,omitempty"`
	View string `json:"view,omitempty"`
	Query string `json:"query,omitempty"`
	Filters *FilterNode `json:"filters,omitempty"`
	Sort string `json:"sort,omitempty"`
	GroupBy string `json:"group_by,omitempty"`
	Columns []string `json:"columns,omitempty"`
	ColumnOrder []string `json:"column_order,omitempty"`
	ColumnWidths map[string]int `json:"column_widths,omitempty"`
	Limit int `json:"limit,omitempty"`
	Cursor string `json:"cursor,omitempty"`
}

type QueryPage struct {
	Items []WorkItem `json:"items"`
	NextCursor string `json:"next_cursor,omitempty"`
	Limit int `json:"limit"`
}

type FieldDefinition struct {
	Key string `json:"key"`
	Label string `json:"label"`
	Type string `json:"type"`
	Operators []string `json:"operators"`
	ReferenceType string `json:"reference_type,omitempty"`
	ObjectType string `json:"object_type,omitempty"`
	FilterOnly bool `json:"filter_only,omitempty"`
	Options []FieldOption `json:"options,omitempty"`
}

type FieldOption struct { Value string `json:"value"`;Label string `json:"label"` }

type ReferenceOption struct {
	ID string `json:"id"`
	Name string `json:"name"`
}

type ReferencePage struct {
	Items []ReferenceOption `json:"items"`
	NextAfterID uint64 `json:"next_after_id,string,omitempty"`
}

type View struct {
	ID uint64 `json:"id,string"`
	TenantID uint64 `json:"tenant_id,string"`
	OwnerUserID uint64 `json:"owner_user_id,string"`
	Name string `json:"name"`
	Shared bool `json:"shared"`
	Version uint64 `json:"version,string"`
	Definition Query `json:"definition"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type SaveViewInput struct {
	Name string `json:"name"`
	Shared bool `json:"shared"`
	Version uint64 `json:"version,string,omitempty"`
	Definition Query `json:"definition"`
}

type ActionTarget struct {
	WorkReference
	Version uint64 `json:"version,string"`
	AssignmentGroupID *uint64 `json:"assignment_group_id,string,omitempty"`
}

type ActionInput struct {
	Action string `json:"action"`
	Items []ActionTarget `json:"items"`
	AssignedTo *uint64 `json:"assigned_to,string,omitempty"`
	State string `json:"state,omitempty"`
	Reason string `json:"reason,omitempty"`
	ResolutionCode string `json:"resolution_code,omitempty"`
	ResolutionDescription string `json:"resolution_description,omitempty"`
}

type ActionResult struct {
	WorkReference
	Status string `json:"status"`
	Message string `json:"message,omitempty"`
	Item *WorkItem `json:"item,omitempty"`
}

type ActionPage struct { Results []ActionResult `json:"results"` }

type ExportStatus struct {
	JobID uint64 `json:"job_id,string"`
	Status string `json:"status"`
	Progress uint8 `json:"progress"`
	Error string `json:"error,omitempty"`
	DownloadPath string `json:"download_path,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type cursor struct {
	Sort string `json:"s"`
	Value string `json:"v,omitempty"`
	Number int `json:"n,omitempty"`
	UpdatedAt string `json:"u,omitempty"`
	ObjectType string `json:"t"`
	ObjectID string `json:"i"`
}
