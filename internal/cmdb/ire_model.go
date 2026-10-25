package cmdb

import (
	"encoding/json"
	"time"
)

type DataSource struct {
	ID                 uint64     `json:"id,string"`
	Key                string     `json:"source_key"`
	Name               string     `json:"name"`
	Type               string     `json:"source_type"`
	OwnerID            uint64     `json:"owner_id,string,omitempty"`
	ScopeKey           string     `json:"scope_key"`
	ConnectionRef      string     `json:"connection_ref,omitempty"`
	SourceInstanceID   string     `json:"source_instance_id,omitempty"`
	State              string     `json:"state"`
	Priority           uint16     `json:"priority"`
	System             bool       `json:"is_system"`
	Version            uint64     `json:"version,string"`
	LastSuccessfulRead *time.Time `json:"last_successful_read_at,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

type DataSourceInput struct {
	Key              string `json:"source_key"`
	Name             string `json:"name"`
	Type             string `json:"source_type"`
	OwnerID          uint64 `json:"owner_id,string,omitempty"`
	ScopeKey         string `json:"scope_key"`
	ConnectionRef    string `json:"connection_ref,omitempty"`
	SourceInstanceID string `json:"source_instance_id,omitempty"`
	State            string `json:"state"`
	Priority         uint16 `json:"priority"`
	Version          uint64 `json:"version,string"`
}

type IdentityRule struct {
	Class              string            `json:"class"`
	Version            uint64            `json:"version,string"`
	MatchKeys          [][]string        `json:"match_keys"`
	RequiredAttributes []string          `json:"required_attributes"`
	Normalization      map[string]string `json:"normalization"`
	ParentDependent    bool              `json:"parent_dependent"`
	ParentRelationType string            `json:"parent_relation_type,omitempty"`
	ParentRole         string            `json:"parent_role,omitempty"`
	State              string            `json:"state"`
	CreatedAt          time.Time         `json:"created_at"`
}

type IdentityRuleInput struct {
	Class              string            `json:"class"`
	ExpectedVersion    uint64            `json:"expected_version,string"`
	MatchKeys          [][]string        `json:"match_keys"`
	RequiredAttributes []string          `json:"required_attributes"`
	Normalization      map[string]string `json:"normalization"`
	ParentDependent    bool              `json:"parent_dependent"`
	ParentRelationType string            `json:"parent_relation_type,omitempty"`
	ParentRole         string            `json:"parent_role,omitempty"`
}

type FieldPolicy struct {
	Class                 string `json:"class"`
	FieldKey              string `json:"field_key"`
	Strategy              string `json:"strategy"`
	AuthoritativeSourceID uint64 `json:"authoritative_source_id,string,omitempty"`
	AllowManualOverride   bool   `json:"allow_manual_override"`
	Version               uint64 `json:"version,string"`
}

type Observation struct {
	NativeKey  string                     `json:"source_native_key"`
	ScopeKey   string                     `json:"scope_key,omitempty"`
	ParentCIID uint64                     `json:"parent_ci_id,string,omitempty"`
	ObservedAt time.Time                  `json:"observed_at,omitempty"`
	Values     map[string]json.RawMessage `json:"values"`
}

type MatchResult struct {
	State       string   `json:"state"`
	CIID        uint64   `json:"ci_id,string,omitempty"`
	Candidates  []uint64 `json:"candidate_ci_ids"`
	RuleVersion uint64   `json:"rule_version,string"`
	Reason      string   `json:"reason_code,omitempty"`
	Message     string   `json:"reason_message,omitempty"`
}

type ReconciliationRunInput struct {
	SourceID         uint64        `json:"source_id,string"`
	IdempotencyKey   string        `json:"idempotency_key"`
	Mode             string        `json:"mode"`
	SnapshotScope    string        `json:"snapshot_scope,omitempty"`
	CompleteSnapshot bool          `json:"complete_snapshot"`
	Observations     []Observation `json:"observations"`
}

type ReconciliationRun struct {
	ID               uint64          `json:"id,string"`
	SourceID         uint64          `json:"source_id,string"`
	CreatedBy        uint64          `json:"created_by,string"`
	ExecutorID       uint64          `json:"executor_id,string"`
	Mode             string          `json:"mode"`
	SnapshotScope    string          `json:"snapshot_scope"`
	CompleteSnapshot bool            `json:"complete_snapshot"`
	State            string          `json:"state"`
	RuleVersions     json.RawMessage `json:"rule_versions"`
	Version          uint64          `json:"version,string"`
	ErrorCode        string          `json:"error_code,omitempty"`
	Counts           map[string]int  `json:"counts"`
	CreatedAt        time.Time       `json:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at"`
	FinishedAt       *time.Time      `json:"finished_at,omitempty"`
}

type ReconciliationObservation struct {
	ID          uint64                     `json:"id,string"`
	Row         uint32                     `json:"row_number"`
	Observation Observation                `json:"observation"`
	State       string                     `json:"state"`
	CIID        uint64                     `json:"ci_id,string,omitempty"`
	Candidates  []uint64                   `json:"candidate_ci_ids"`
	RuleVersion uint64                     `json:"rule_version,string"`
	Accepted    map[string]json.RawMessage `json:"accepted_changes"`
	Rejected    map[string]string          `json:"rejected_changes"`
	Reason      string                     `json:"reason_code,omitempty"`
	Message     string                     `json:"reason_message,omitempty"`
}

type IREConflict struct {
	ID             uint64          `json:"id,string"`
	SourceID       uint64          `json:"source_id,string"`
	RunID          uint64          `json:"run_id,string,omitempty"`
	ObservationID  uint64          `json:"observation_id,string,omitempty"`
	Type           string          `json:"conflict_type"`
	State          string          `json:"state"`
	SourceKey      string          `json:"source_key"`
	Candidates     []uint64        `json:"candidate_ci_ids"`
	ValueDiff      json.RawMessage `json:"value_diff"`
	ReferenceDiff  json.RawMessage `json:"reference_diff"`
	ResolutionNote string          `json:"resolution_note"`
	Version        uint64          `json:"version,string"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

type ProvenanceEntry struct {
	FieldKey        string     `json:"field_key"`
	SourceID        uint64     `json:"source_id,string"`
	SourceKey       string     `json:"source_key"`
	SourceName      string     `json:"source_name"`
	RunType         string     `json:"run_type"`
	RunID           uint64     `json:"run_id,string,omitempty"`
	RuleVersion     uint64     `json:"rule_version,string"`
	ObservedAt      time.Time  `json:"observed_at"`
	AcceptedAt      time.Time  `json:"accepted_at"`
	OverrideType    string     `json:"override_type,omitempty"`
	OverrideReason  string     `json:"override_reason,omitempty"`
	OverrideUntil   *time.Time `json:"override_until,omitempty"`
	OverrideAllowed bool       `json:"override_allowed"`
}

type ProvenancePage struct {
	CIID       uint64            `json:"ci_id,string"`
	Version    uint64            `json:"version,string"`
	LastSeenAt *time.Time        `json:"last_seen_at,omitempty"`
	Items      []ProvenanceEntry `json:"items"`
}

type ManualOverrideInput struct {
	FieldKey  string    `json:"field_key"`
	Type      string    `json:"override_type"`
	Reason    string    `json:"reason"`
	ExpiresAt time.Time `json:"expires_at,omitempty"`
}

type MergePlan struct {
	SourceCIID            uint64         `json:"source_ci_id,string"`
	CanonicalCIID         uint64         `json:"canonical_ci_id,string"`
	SourceVersion         uint64         `json:"source_version,string"`
	CanonicalVersion      uint64         `json:"canonical_version,string"`
	References            map[string]int `json:"references"`
	Blockers              []string       `json:"blockers"`
	UnsupportedReferences []string       `json:"unsupported_references"`
	Token                 string         `json:"plan_token"`
}

type MergeInput struct {
	SourceCIID       uint64 `json:"source_ci_id,string"`
	CanonicalCIID    uint64 `json:"canonical_ci_id,string"`
	SourceVersion    uint64 `json:"source_version,string"`
	CanonicalVersion uint64 `json:"canonical_version,string"`
	PlanToken        string `json:"plan_token"`
	Reason           string `json:"reason"`
}

type QualityConfig struct {
	Enabled        bool                `json:"enabled"`
	ScheduleHours  uint16              `json:"schedule_hours"`
	StaleDays      uint16              `json:"stale_days"`
	RequiredFields map[string][]string `json:"required_fields"`
	Version        uint64              `json:"version,string"`
	NextRunAt      time.Time           `json:"next_run_at"`
}

type QualityFinding struct {
	ID         uint64          `json:"id,string"`
	CIClass    string          `json:"class"`
	RuleKey    string          `json:"rule_key"`
	Severity   string          `json:"severity"`
	CIID       uint64          `json:"ci_id,string"`
	FieldKey   string          `json:"field_key"`
	Details    json.RawMessage `json:"details"`
	State      string          `json:"state"`
	DetectedAt time.Time       `json:"detected_at"`
	CheckedAt  time.Time       `json:"checked_at"`
}

type QualitySummary struct {
	RuleKey     string `json:"rule_key"`
	Valid       uint64 `json:"valid"`
	Denominator uint64 `json:"denominator"`
	Open        uint64 `json:"open"`
}

type IREPage[T any] struct {
	Items []T    `json:"items"`
	Next  uint64 `json:"next_before_id,string,omitempty"`
}
