package serviceportfolio

import (
	"context"
	"errors"
	"time"

	"github.com/chmajster/OpenServiceNOW/internal/activity"
	"github.com/chmajster/OpenServiceNOW/internal/cmdb"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
)

var (
	ErrInvalid   = errors.New("invalid service portfolio input")
	ErrNotFound  = errors.New("service portfolio record not found")
	ErrConflict  = errors.New("service portfolio version conflict")
	ErrDuplicate = errors.New("service portfolio key already exists")
)

type Actor = workrecords.Principal

type Item struct {
	ID                   uint64    `json:"id,string"`
	ServiceKey           string    `json:"service_key"`
	Name                 string    `json:"name"`
	Type                 string    `json:"type"`
	Description          string    `json:"description"`
	Lifecycle            string    `json:"lifecycle"`
	Criticality          string    `json:"criticality"`
	Environment          string    `json:"environment"`
	Scope                string    `json:"scope"`
	BusinessOwnerID      uint64    `json:"business_owner_id,string"`
	BusinessOwnerName    string    `json:"business_owner_name,omitempty"`
	TechnicalOwnerID     uint64    `json:"technical_owner_id,string"`
	TechnicalOwnerName   string    `json:"technical_owner_name,omitempty"`
	SupportGroupID       uint64    `json:"support_group_id,string"`
	SupportGroupName     string    `json:"support_group_name,omitempty"`
	SupportGroupActive   bool      `json:"support_group_active"`
	OrgUnitRefID         uint64    `json:"org_unit_ref_id,string"`
	OrgUnitName          string    `json:"org_unit_name,omitempty"`
	DepartmentRefID      uint64    `json:"department_ref_id,string"`
	DepartmentName       string    `json:"department_name,omitempty"`
	CostCenterRefID      uint64    `json:"cost_center_ref_id,string"`
	CostCenterName       string    `json:"cost_center_name,omitempty"`
	LocationID           uint64    `json:"location_id,string"`
	LocationName         string    `json:"location_name,omitempty"`
	CIID                 uint64    `json:"ci_id,string"`
	CIName               string    `json:"ci_name,omitempty"`
	CIClass              string    `json:"ci_class,omitempty"`
	CIVersion            uint64    `json:"ci_version,string,omitempty"`
	DependencyMode       string    `json:"dependency_mode"`
	DependencyQuorum     uint16    `json:"dependency_quorum"`
	Version              uint64    `json:"version,string"`
	ConfigurationVersion uint64    `json:"configuration_version,string"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}

type ItemInput struct {
	Version          uint64 `json:"version,string"`
	ServiceKey       string `json:"service_key"`
	Name             string `json:"name"`
	Type             string `json:"type"`
	Description      string `json:"description"`
	Criticality      string `json:"criticality"`
	Environment      string `json:"environment"`
	Scope            string `json:"scope"`
	BusinessOwnerID  uint64 `json:"business_owner_id,string"`
	TechnicalOwnerID uint64 `json:"technical_owner_id,string"`
	SupportGroupID   uint64 `json:"support_group_id,string"`
	OrgUnitRefID     uint64 `json:"org_unit_ref_id,string"`
	DepartmentRefID  uint64 `json:"department_ref_id,string"`
	CostCenterRefID  uint64 `json:"cost_center_ref_id,string"`
	LocationID       uint64 `json:"location_id,string"`
	CIID             uint64 `json:"ci_id,string"`
	CIVersion        uint64 `json:"ci_version,string"`
	ClearCILink      bool   `json:"clear_ci_link"`
	DependencyMode   string `json:"dependency_mode"`
	DependencyQuorum uint16 `json:"dependency_quorum"`
}

type ItemFilter struct {
	Query, Type, Lifecycle, Environment string
	OwnerID, Before                     uint64
	Limit                               int
}

type ItemPage struct {
	Items []Item `json:"items"`
	Next  uint64 `json:"next_before_id,string,omitempty"`
}

type Relation struct {
	ID            uint64 `json:"id,string"`
	Version       uint64 `json:"version,string"`
	SourceItemID  uint64 `json:"source_item_id,string"`
	TargetItemID  uint64 `json:"target_item_id,string"`
	TargetCIID    uint64 `json:"target_ci_id,string"`
	TargetVersion uint64 `json:"target_version,string"`
	Type          string `json:"type"`
	SourceName    string `json:"source_name"`
	SourceType    string `json:"source_type"`
	TargetName    string `json:"target_name"`
	TargetType    string `json:"target_type,omitempty"`
	TargetCIClass string `json:"target_ci_class,omitempty"`
}

type RelationInput struct {
	SourceVersion uint64 `json:"source_version,string"`
	TargetVersion uint64 `json:"target_version,string"`
	TargetItemID  uint64 `json:"target_item_id,string"`
	TargetCIID    uint64 `json:"target_ci_id,string"`
	Type          string `json:"type"`
}

type RemoveRelationInput struct {
	SourceVersion   uint64 `json:"source_version,string"`
	RelationVersion uint64 `json:"relation_version,string"`
	TargetVersion   uint64 `json:"target_version,string"`
}

type Offer struct {
	ID                     uint64   `json:"id,string"`
	OfferKey               string   `json:"offer_key"`
	ParentServiceID        uint64   `json:"parent_service_id,string"`
	ParentServiceName      string   `json:"parent_service_name,omitempty"`
	CatalogItemID          uint64   `json:"catalog_item_id,string"`
	CatalogItemTitle       string   `json:"catalog_item_title,omitempty"`
	Name                   string   `json:"name"`
	Description            string   `json:"description"`
	AvailabilityConditions string   `json:"availability_conditions"`
	SupportModel           string   `json:"support_model"`
	ObligationSummary      string   `json:"obligation_summary"`
	SLADefinitionID        uint64   `json:"sla_definition_id,string"`
	AudienceGroupIDs       []string `json:"audience_group_ids"`
	Lifecycle              string   `json:"lifecycle"`
	Version                uint64   `json:"version,string"`
}

type OfferInput struct {
	Version                uint64   `json:"version,string"`
	OfferKey               string   `json:"offer_key"`
	ParentServiceID        uint64   `json:"parent_service_id,string"`
	CatalogItemID          uint64   `json:"catalog_item_id,string"`
	Name                   string   `json:"name"`
	Description            string   `json:"description"`
	AvailabilityConditions string   `json:"availability_conditions"`
	SupportModel           string   `json:"support_model"`
	ObligationSummary      string   `json:"obligation_summary"`
	SLADefinitionID        uint64   `json:"sla_definition_id,string"`
	AudienceGroupIDs       []string `json:"audience_group_ids"`
}

type OfferPage struct {
	Items []Offer `json:"items"`
	Next  uint64  `json:"next_before_id,string,omitempty"`
}

type PublicOfferPage struct {
	Items []PublicOffer `json:"items"`
	Next  uint64        `json:"next_before_id,string,omitempty"`
}

type PublicOffer struct {
	ID                     uint64 `json:"id,string"`
	OfferKey               string `json:"offer_key"`
	Name                   string `json:"name"`
	Description            string `json:"description"`
	ServiceName            string `json:"service_name"`
	AvailabilityConditions string `json:"availability_conditions"`
	SupportModel           string `json:"support_model"`
	ObligationSummary      string `json:"obligation_summary"`
	CatalogItemID          uint64 `json:"catalog_item_id,string"`
	SLADefinitionID        uint64 `json:"sla_definition_id,string"`
	Version                uint64 `json:"version,string"`
}

type OrgRef struct {
	ID      uint64 `json:"id,string"`
	Kind    string `json:"kind"`
	Key     string `json:"key"`
	Name    string `json:"name"`
	Active  bool   `json:"active"`
	Version uint64 `json:"version,string"`
}

type Lookup struct {
	Items []map[string]any `json:"items"`
	Next  uint64           `json:"next_before_id,string,omitempty"`
}

type Violation struct {
	Rule       string `json:"rule"`
	Severity   string `json:"severity"`
	ItemID     uint64 `json:"item_id,string"`
	ServiceKey string `json:"service_key"`
	Name       string `json:"name"`
	Message    string `json:"message"`
}

type ViolationPage struct {
	Items []Violation `json:"items"`
	Next  uint64      `json:"next_before_id,string,omitempty"`
}

type AvailabilityInput struct {
	NodeKind string `json:"node_kind"`
	NodeID   uint64 `json:"node_id,string"`
	Version  uint64 `json:"version,string"`
	State    string `json:"state"`
	Note     string `json:"note"`
}

type Availability struct {
	NodeKind   string    `json:"node_kind"`
	NodeID     uint64    `json:"node_id,string"`
	State      string    `json:"state"`
	Source     string    `json:"source"`
	Note       string    `json:"note"`
	Version    uint64    `json:"version,string"`
	ObservedAt time.Time `json:"observed_at"`
}

type ImpactNode struct {
	ID               string `json:"id"`
	Kind             string `json:"kind"`
	Name             string `json:"name"`
	Class            string `json:"class,omitempty"`
	Lifecycle        string `json:"lifecycle,omitempty"`
	Environment      string `json:"environment,omitempty"`
	Depth            int    `json:"depth"`
	Availability     string `json:"availability,omitempty"`
	AvailabilityRule string `json:"availability_rule,omitempty"`
}

type ImpactEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Type string `json:"type"`
}

type ImpactTarget struct {
	ID   string   `json:"id"`
	Name string   `json:"name"`
	Type string   `json:"type"`
	Path []string `json:"path"`
	Rule string   `json:"rule"`
}

type ImpactResult struct {
	Root             ImpactNode     `json:"root"`
	Nodes            []ImpactNode   `json:"nodes"`
	Edges            []ImpactEdge   `json:"edges"`
	ImpactedServices []ImpactTarget `json:"impacted_services"`
	ImpactedOffers   []ImpactTarget `json:"impacted_offers"`
	RulesUsed        []string       `json:"rules_used"`
	Truncated        bool           `json:"truncated"`
	MaxNodes         int            `json:"max_nodes"`
	MaxDepth         int            `json:"max_depth"`
	RuleVersion      uint64         `json:"rule_version,string"`
	CalculatedAt     time.Time      `json:"calculated_at"`
}

type RetirementImpact struct {
	ServiceID          uint64 `json:"service_id,string"`
	ActiveOffers       int    `json:"active_offers"`
	ActiveCatalogItems int    `json:"active_catalog_items"`
	OpenIncidents      int    `json:"open_incidents"`
	IncidentsVisible   bool   `json:"incidents_visible"`
	OpenRequests       int    `json:"open_requests"`
	RequestsVisible    bool   `json:"requests_visible"`
	DependentItems     int    `json:"dependent_items"`
	RetirementAllowed  bool   `json:"retirement_allowed"`
}

type ClassificationCandidate struct {
	CIID       uint64 `json:"ci_id,string"`
	Name       string `json:"name"`
	Identifier string `json:"identifier"`
	Class      string `json:"class"`
	Version    uint64 `json:"version,string"`
}

type ClassificationInput struct {
	CIID       uint64 `json:"ci_id,string"`
	CIVersion  uint64 `json:"ci_version,string"`
	Type       string `json:"type"`
	ServiceKey string `json:"service_key"`
}

type ClassificationResult struct {
	Created []Item `json:"created"`
}

type RelatedRecordsResult struct {
	Kind      string           `json:"kind"`
	Available bool             `json:"available"`
	Message   string           `json:"message,omitempty"`
	Items     []cmdb.Reference `json:"items"`
}

type HistoryPage struct {
	activity.Page
	Pending bool `json:"pending"`
}

type CIReader interface {
	Get(context.Context, cmdb.Actor, uint64) (cmdb.Record, error)
	Records(context.Context, cmdb.Actor, string, string, uint64, uint64, int) (cmdb.ReferencePage, error)
}

type RelatedWorkAdapter interface {
	List(context.Context, Actor, []uint64, uint64, int) ([]cmdb.Reference, error)
}
