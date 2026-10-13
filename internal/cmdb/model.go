package cmdb

import (
	"context"
	"encoding/json"
	"github.com/chmajster/OpenServiceNOW/internal/assets"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/recordlists"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
	"time"
)

type Actor = workrecords.Principal

var (
	ErrInvalid   = assets.ErrInvalid
	ErrNotFound  = assets.ErrNotFound
	ErrConflict  = assets.ErrConflict
	ErrDuplicate = assets.ErrDuplicate
)

type Fields struct {
	Identifier     string                     `json:"identifier"`
	Name           string                     `json:"name"`
	Class          string                     `json:"class"`
	Status         string                     `json:"status"`
	OwnerID        uint64                     `json:"owner_id,string"`
	Environment    string                     `json:"environment"`
	Location       string                     `json:"location"`
	SupportGroupID *uint64                    `json:"support_group_id,string,omitempty"`
	Criticality    string                     `json:"criticality"`
	Lifecycle      string                     `json:"lifecycle"`
	Attributes     map[string]json.RawMessage `json:"attributes"`
}
type Record struct {
	Fields
	ID               uint64    `json:"id,string"`
	Version          uint64    `json:"version,string"`
	OwnerName        string    `json:"owner_name"`
	SupportGroupName string    `json:"support_group_name"`
	Archived         bool      `json:"archived"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}
type Page struct {
	Items     []Record     `json:"items"`
	Next      uint64       `json:"next_before_id,string,omitempty"`
	NextValue string       `json:"next_before_value,omitempty"`
	Total     uint64       `json:"total,string"`
	Groups    []GroupCount `json:"groups"`
}
type Filter struct {
	Query, Class, Status, Environment, Location, Archived string
	OwnerID, Before                                       uint64
	Limit                                                 int
	Expression                                            *recordlists.Node
	Sort, GroupBy, BeforeValue                            string
}
type Reference struct {
	ID      uint64 `json:"id,string"`
	Number  string `json:"number"`
	Title   string `json:"title"`
	State   string `json:"state"`
	Version uint64 `json:"version,string"`
	CanLink bool   `json:"can_link"`
}
type ReferencePage struct {
	Items []Reference `json:"items"`
	Next  uint64      `json:"next_before_id,string,omitempty"`
}

// RecordDomain owns the policy, fields and commands of its business record.
// CMDB never treats a client object_type/object_id as an authorization grant.
type RecordDomain interface {
	ReferenceInTx(context.Context, database.Executor, Actor, uint64, bool, uint64) (Reference, error)
	References(context.Context, Actor, string, uint64, uint64, int) (ReferencePage, error)
}
type Relation struct {
	ID          uint64 `json:"id,string"`
	Version     uint64 `json:"version,string"`
	SourceID    uint64 `json:"source_id,string"`
	TargetID    uint64 `json:"target_id,string"`
	Type        string `json:"type"`
	SourceName  string `json:"source_name"`
	TargetName  string `json:"target_name"`
	SourceClass string `json:"source_class"`
	TargetClass string `json:"target_class"`
}
type RelationPage struct {
	Items []Relation `json:"items"`
	Next  uint64     `json:"next_before_id,string,omitempty"`
}
type RelationInput struct {
	Version       uint64 `json:"version,string"`
	TargetID      uint64 `json:"target_id,string"`
	TargetVersion uint64 `json:"target_version,string"`
	Type          string `json:"type"`
}
type GraphNode struct {
	ID       uint64 `json:"id,string"`
	Name     string `json:"name"`
	Class    string `json:"class"`
	Status   string `json:"status"`
	Archived bool   `json:"archived"`
	Depth    int    `json:"depth"`
}
type Graph struct {
	Nodes     []GraphNode `json:"nodes"`
	Edges     []Relation  `json:"edges"`
	Truncated bool        `json:"truncated"`
	MaxNodes  int         `json:"max_nodes"`
	MaxDepth  int         `json:"max_depth"`
	MaxEdges  int         `json:"max_edges"`
}
type GraphFilter struct {
	Class, Environment, Type string
	MaxNodes, Depth          int
}
