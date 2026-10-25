package incidents

import "time"

type MajorCase struct {
	IncidentID            uint64     `json:"incident_id,string"`
	Number                string     `json:"number"`
	Title                 string     `json:"title"`
	IncidentState         string     `json:"incident_state"`
	Priority              int        `json:"priority"`
	Status                string     `json:"status"`
	CandidateReason       string     `json:"candidate_reason,omitempty"`
	DecisionReason        string     `json:"decision_reason,omitempty"`
	NominatedBy           uint64     `json:"nominated_by,string,omitempty"`
	NominatedAt           time.Time  `json:"nominated_at"`
	DecidedBy             *uint64    `json:"decided_by,string,omitempty"`
	DecidedAt             *time.Time `json:"decided_at,omitempty"`
	CommanderUserID       *uint64    `json:"commander_user_id,string,omitempty"`
	DeputyUserID          *uint64    `json:"deputy_user_id,string,omitempty"`
	CommunicationsOwnerID *uint64    `json:"communications_owner_id,string,omitempty"`
	BridgeURL             string     `json:"bridge_url,omitempty"`
	ImpactSummary         string     `json:"impact_summary,omitempty"`
	ImpactConfidence      string     `json:"impact_confidence,omitempty"`
	Workaround            string     `json:"workaround,omitempty"`
	CommunicationAudience string     `json:"communication_audience,omitempty"`
	UpdateIntervalMinutes uint16     `json:"update_interval_minutes,omitempty"`
	NextUpdateAt          *time.Time `json:"next_update_at,omitempty"`
	DetectedAt            *time.Time `json:"detected_at,omitempty"`
	RestoredAt            *time.Time `json:"restored_at,omitempty"`
	ReviewStatus          string     `json:"review_status,omitempty"`
	OpenActions           uint64     `json:"open_actions,string,omitempty"`
	UnassignedActions     uint64     `json:"unassigned_actions,string,omitempty"`
	Version               uint64     `json:"version,string"`
	UpdatedAt             time.Time  `json:"updated_at"`
	Teams                 []MajorTeam `json:"teams,omitempty"`
	Links                 []MajorLink `json:"links,omitempty"`
}

type MajorTeam struct {
	GroupID   uint64 `json:"group_id,string"`
	GroupName string `json:"group_name"`
}

type MajorLink struct {
	Type string `json:"type"`
	ID   uint64 `json:"id,string"`
	Name string `json:"name"`
}

type MajorView struct {
	Case            *MajorCase     `json:"case,omitempty"`
	PublicUpdates   []MajorUpdate  `json:"public_updates"`
	InternalUpdates []MajorUpdate  `json:"internal_updates,omitempty"`
	Timeline        []MajorDecision `json:"timeline,omitempty"`
	Review          *MajorReview   `json:"review,omitempty"`
	Actions         []MajorAction  `json:"actions,omitempty"`
	Restricted      bool           `json:"restricted"`
}

type MajorDecision struct {
	ID          uint64    `json:"id,string"`
	Version     uint64    `json:"case_version,string"`
	Type        string    `json:"type"`
	Reason      string    `json:"reason"`
	ActorUserID uint64    `json:"actor_user_id,string"`
	ActorName   string    `json:"actor_name"`
	OccurredAt  time.Time `json:"occurred_at"`
}

type MajorUpdate struct {
	ID           uint64    `json:"id,string"`
	Audience     string    `json:"audience"`
	Body         string    `json:"body"`
	ActorUserID  uint64    `json:"actor_user_id,string"`
	ActorName    string    `json:"actor_name,omitempty"`
	Delivery     string    `json:"delivery_state"`
	OccurredAt   time.Time `json:"occurred_at"`
}

type MajorDecisionInput struct {
	Version uint64 `json:"version,string"`
	Reason  string `json:"reason"`
}

type MajorCoordinationInput struct {
	Version               uint64     `json:"version,string"`
	CommanderUserID       *uint64    `json:"commander_user_id,string"`
	DeputyUserID          *uint64    `json:"deputy_user_id,string"`
	CommunicationsOwnerID *uint64    `json:"communications_owner_id,string"`
	BridgeURL             string     `json:"bridge_url"`
	ImpactSummary         string     `json:"impact_summary"`
	ImpactConfidence      string     `json:"impact_confidence"`
	Workaround            string     `json:"workaround"`
	CommunicationAudience string     `json:"communication_audience"`
	UpdateIntervalMinutes uint16     `json:"update_interval_minutes"`
	NextUpdateAt          *time.Time `json:"next_update_at"`
	DetectedAt            *time.Time `json:"detected_at"`
	TeamGroupIDs          []uint64   `json:"team_group_ids"`
	ServiceIDs            []uint64   `json:"service_ids"`
	CIIDs                 []uint64   `json:"ci_ids"`
}

type MajorUpdateInput struct {
	Version  uint64 `json:"version,string"`
	Audience string `json:"audience"`
	Body     string `json:"body"`
}

type MajorRestoreInput struct {
	Version    uint64    `json:"version,string"`
	DetectedAt time.Time `json:"detected_at"`
	RestoredAt time.Time `json:"restored_at"`
	Reason     string    `json:"reason"`
}

type MajorReview struct {
	DetectedAt   time.Time `json:"detected_at"`
	RestoredAt   time.Time `json:"restored_at"`
	ImpactSummary string   `json:"impact_summary"`
	KnownCauses  string    `json:"known_causes"`
	WhatWorked   string    `json:"what_worked"`
	CompletedAt  *time.Time `json:"completed_at,omitempty"`
	Version      uint64    `json:"version,string"`
}

type MajorAction struct {
	ID          uint64     `json:"id,string"`
	IncidentID  uint64     `json:"incident_id,string"`
	Number      string     `json:"number,omitempty"`
	Description string     `json:"description"`
	OwnerUserID *uint64    `json:"owner_user_id,string,omitempty"`
	OwnerName   string     `json:"owner_name,omitempty"`
	DueAt       time.Time  `json:"due_at"`
	Status      string     `json:"status"`
	Version     uint64     `json:"version,string"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type MajorReviewInput struct {
	Version       uint64            `json:"version,string"`
	DetectedAt    time.Time         `json:"detected_at"`
	RestoredAt    time.Time         `json:"restored_at"`
	ImpactSummary string            `json:"impact_summary"`
	KnownCauses   string            `json:"known_causes"`
	WhatWorked    string            `json:"what_worked"`
	Complete      bool              `json:"complete"`
	Actions       []MajorActionInput `json:"actions"`
}

type MajorActionInput struct {
	ID          uint64     `json:"id,string,omitempty"`
	Version     uint64     `json:"version,string,omitempty"`
	Description string     `json:"description"`
	OwnerUserID *uint64    `json:"owner_user_id,string"`
	DueAt       time.Time  `json:"due_at"`
	Status      string     `json:"status"`
}

type MajorActionChangeInput struct {
	Version uint64 `json:"version,string"`
	Status  string `json:"status"`
}

type MajorChildrenInput struct {
	Version uint64              `json:"version,string"`
	Action  string              `json:"action"`
	Message string              `json:"message"`
	Items   []MajorChildTarget `json:"items"`
}

type MajorChildTarget struct {
	ID      uint64 `json:"id,string"`
	Version uint64 `json:"version,string"`
}

type MajorChildResult struct {
	ID      uint64 `json:"id,string"`
	Status  int    `json:"status"`
	Message string `json:"message"`
}
