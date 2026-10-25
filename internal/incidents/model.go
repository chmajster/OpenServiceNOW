package incidents

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/chmajster/OpenServiceNOW/internal/metadata"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
)

var (
	ErrInvalid     = errors.New("Nieprawidłowe dane incydentu")
	ErrNotFound    = errors.New("incident not found")
	ErrForbidden   = errors.New("incident access denied")
	ErrConflict    = errors.New("incident version conflict")
	ErrIdempotency = errors.New("idempotency key used for different input")
	ErrTransition  = errors.New("incident transition not allowed")
)

type Actor struct {
	Source               string
	TenantID, UserID     uint64
	Permissions          map[string]bool
	RequestID, IPAddress string
}

func (a Actor) Has(key string) bool { return a.Permissions["incident."+key] }

type Record struct {
	ServiceID       *uint64 `json:"service_id,string"`
	Location        string  `json:"location"`
	TemplateKey     string  `json:"template_key,omitempty"`
	TemplateVersion uint64  `json:"template_version,string,omitempty"`

	ProcessVersion   uint64                   `json:"process_version,string"`
	ProcessPolicy    metadata.IncidentProcess `json:"-"`
	AssignmentOrigin string                   `json:"assignment_origin"`
	AssignmentReason string                   `json:"assignment_reason"`

	ID                    uint64                     `json:"id,string"`
	TenantID              uint64                     `json:"tenant_id,string"`
	Number                string                     `json:"number"`
	Title                 string                     `json:"title"`
	Description           string                     `json:"description"`
	ReporterID            uint64                     `json:"reporter_id,string"`
	ReporterName          string                     `json:"reporter_name"`
	AffectedUserID        uint64                     `json:"affected_user_id,string"`
	AffectedUserName      string                     `json:"affected_user_name"`
	Category              string                     `json:"category"`
	Subcategory           string                     `json:"subcategory"`
	Impact                int                        `json:"impact"`
	Urgency               int                        `json:"urgency"`
	Priority              int                        `json:"priority"`
	CalculatedPriority    int                        `json:"calculated_priority"`
	PriorityOverride      *int                       `json:"priority_override"`
	PriorityReason        string                     `json:"priority_reason"`
	State                 string                     `json:"state"`
	HoldReason            string                     `json:"hold_reason"`
	ResolutionCode        string                     `json:"resolution_code"`
	ResolutionDescription string                     `json:"resolution_description"`
	CancellationReason    string                     `json:"cancellation_reason"`
	Version               uint64                     `json:"version,string"`
	CustomFieldVersion    uint64                     `json:"custom_fields_version,string"`
	CustomFields          map[string]json.RawMessage `json:"custom_fields,omitempty"`
	CreatedAt             time.Time                  `json:"created_at"`
	UpdatedAt             time.Time                  `json:"updated_at"`
	StartedAt             *time.Time                 `json:"started_at"`
	ResolvedAt            *time.Time                 `json:"resolved_at"`
	ClosedAt              *time.Time                 `json:"closed_at"`
	CancelledAt           *time.Time                 `json:"cancelled_at"`
	Assignment            workrecords.Assignment     `json:"assignment"`
	AssignedName          string                     `json:"assigned_name"`
	GroupName             string                     `json:"group_name"`
	Actions               Actions                    `json:"actions"`
}
type Actions struct {
	Edit             bool     `json:"edit"`
	Assign           bool     `json:"assign"`
	OverridePriority bool     `json:"override_priority"`
	Transitions      []string `json:"transitions"`
}
type Fields struct {
	ServiceID       *uint64 `json:"service_id,string"`
	Location        string  `json:"location"`
	TemplateKey     string  `json:"template_key,omitempty"`
	TemplateVersion uint64  `json:"template_version,string,omitempty"`

	Title              string                     `json:"title"`
	Description        string                     `json:"description"`
	AffectedUserID     uint64                     `json:"affected_user_id,string"`
	Category           string                     `json:"category"`
	Subcategory        string                     `json:"subcategory"`
	Impact             int                        `json:"impact"`
	Urgency            int                        `json:"urgency"`
	CustomFields       map[string]json.RawMessage `json:"custom_fields,omitempty"`
	CustomFieldVersion uint64                     `json:"custom_fields_version,string,omitempty"`
}
type UpdateInput struct {
	Fields
	Version uint64 `json:"version,string"`
}
type AssignmentInput struct {
	Reason            string  `json:"reason"`
	Version           uint64  `json:"version,string"`
	AssignedTo        *uint64 `json:"assigned_to,string"`
	AssignmentGroupID *uint64 `json:"assignment_group_id,string"`
}
type TransitionInput struct {
	Version               uint64 `json:"version,string"`
	State                 string `json:"state"`
	Reason                string `json:"reason"`
	HoldReason            string `json:"hold_reason"`
	ResolutionCode        string `json:"resolution_code"`
	ResolutionDescription string `json:"resolution_description"`
}
type PriorityInput struct {
	Version  uint64 `json:"version,string"`
	Priority *int   `json:"priority"`
	Reason   string `json:"reason"`
}
type Filter struct {
	Query, State, Category, View, Sort string
	Priority                           int
	AssignedTo, GroupID, ReporterID    uint64
	From, To                           time.Time
	AfterID                            uint64
	AfterPriority                      int
	Limit                              int
	CustomFields                       []metadata.QueryFilter
	SearchFields                       []string
	SearchableTypes                    map[string]string
}
type Page struct {
	Incidents    []Summary `json:"incidents"`
	NextAfterID  uint64    `json:"next_after_id,string,omitempty"`
	NextPriority int       `json:"next_after_priority,string,omitempty"`
}
type Summary struct {
	ID                 uint64                     `json:"id,string"`
	Number             string                     `json:"number"`
	Title              string                     `json:"title"`
	State              string                     `json:"state"`
	Priority           int                        `json:"priority"`
	ReporterID         uint64                     `json:"reporter_id,string"`
	ReporterName       string                     `json:"reporter_name"`
	AffectedUserID     uint64                     `json:"affected_user_id,string"`
	AffectedUserName   string                     `json:"affected_user_name"`
	Category           string                     `json:"category"`
	Subcategory        string                     `json:"subcategory"`
	Impact             int                        `json:"impact"`
	Urgency            int                        `json:"urgency"`
	Assignment         workrecords.Assignment     `json:"assignment"`
	AssignedName       string                     `json:"assigned_name"`
	GroupName          string                     `json:"group_name"`
	Version            uint64                     `json:"version,string"`
	CustomFields       map[string]json.RawMessage `json:"custom_fields,omitempty"`
	CustomFieldVersion uint64                     `json:"custom_fields_version,string"`
	CreatedAt          time.Time                  `json:"created_at"`
	UpdatedAt          time.Time                  `json:"updated_at"`
}

func summary(r Record) Summary {
	return Summary{ID: r.ID, Number: r.Number, Title: r.Title, State: r.State, Priority: r.Priority, ReporterID: r.ReporterID, ReporterName: r.ReporterName, AffectedUserID: r.AffectedUserID, AffectedUserName: r.AffectedUserName, Category: r.Category, Subcategory: r.Subcategory, Impact: r.Impact, Urgency: r.Urgency, Assignment: r.Assignment, AssignedName: r.AssignedName, GroupName: r.GroupName, Version: r.Version, CustomFields: r.CustomFields, CustomFieldVersion: r.CustomFieldVersion, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt}
}

// Snapshot is independent of read-time action metadata and user display names.
// Every event contains the complete business before/after state and version.
type Snapshot struct {
	AssignmentOrigin string `json:"assignment_origin,omitempty"`
	AssignmentReason string `json:"assignment_reason,omitempty"`
	Fields
	ID                    uint64     `json:"id,string"`
	Number                string     `json:"number"`
	ReporterID            uint64     `json:"reporter_id,string"`
	Priority              int        `json:"priority"`
	PriorityOverride      *int       `json:"priority_override"`
	PriorityReason        string     `json:"priority_reason"`
	State                 string     `json:"state"`
	HoldReason            string     `json:"hold_reason"`
	ResolutionCode        string     `json:"resolution_code"`
	ResolutionDescription string     `json:"resolution_description"`
	CancellationReason    string     `json:"cancellation_reason"`
	AssignedTo            *uint64    `json:"assigned_to,string"`
	AssignmentGroupID     *uint64    `json:"assignment_group_id,string"`
	Version               uint64     `json:"version,string"`
	CreatedAt             time.Time  `json:"created_at"`
	UpdatedAt             time.Time  `json:"updated_at"`
	StartedAt             *time.Time `json:"started_at"`
	ResolvedAt            *time.Time `json:"resolved_at"`
	ClosedAt              *time.Time `json:"closed_at"`
	CancelledAt           *time.Time `json:"cancelled_at"`
}
type ChangeEvent struct {
	ActorUserID     uint64    `json:"actor_user_id,string"`
	IncidentVersion uint64    `json:"incident_version,string"`
	OccurredAt      time.Time `json:"occurred_at"`
	Before          *Snapshot `json:"before"`
	After           Snapshot  `json:"after"`
	Reason          string    `json:"reason,omitempty"`
}

func snapshot(r Record) Snapshot {
	return Snapshot{AssignmentOrigin: r.AssignmentOrigin, AssignmentReason: r.AssignmentReason, Fields: fields(r), ID: r.ID, Number: r.Number, ReporterID: r.ReporterID, Priority: r.Priority, PriorityOverride: r.PriorityOverride, PriorityReason: r.PriorityReason, State: r.State, HoldReason: r.HoldReason, ResolutionCode: r.ResolutionCode, ResolutionDescription: r.ResolutionDescription, CancellationReason: r.CancellationReason, AssignedTo: r.Assignment.AssignedTo, AssignmentGroupID: r.Assignment.AssignmentGroupID, Version: r.Version, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt, StartedAt: r.StartedAt, ResolvedAt: r.ResolvedAt, ClosedAt: r.ClosedAt, CancelledAt: r.CancelledAt}
}
func fields(r Record) Fields {
	return Fields{ServiceID: r.ServiceID, Location: r.Location, TemplateKey: r.TemplateKey, TemplateVersion: r.TemplateVersion, Title: r.Title, Description: r.Description, AffectedUserID: r.AffectedUserID, Category: r.Category, Subcategory: r.Subcategory, Impact: r.Impact, Urgency: r.Urgency, CustomFields: r.CustomFields, CustomFieldVersion: r.CustomFieldVersion}
}

type HistoryEntry struct {
	ID            uint64          `json:"id,string"`
	Type          string          `json:"type"`
	Summary       string          `json:"summary"`
	ActorUserID   *uint64         `json:"actor_user_id,string,omitempty"`
	EventTime     time.Time       `json:"event_time"`
	Data          json.RawMessage `json:"data"`
	ActorName     string          `json:"actor_name"`
	SourceEventID *uint64         `json:"source_event_id,string,omitempty"`
}
type HistoryPage struct {
	Entries           []HistoryEntry `json:"entries"`
	NextBeforeEventID uint64         `json:"next_before_event_id,string,omitempty"`
	Pending           bool           `json:"pending"`
}
