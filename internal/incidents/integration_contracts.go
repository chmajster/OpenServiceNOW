package incidents

import "context"

// IncidentReference keeps optional Problem, Change and Alert adapters tied to
// the original tenant and immutable incident identity. Adapters must enforce
// their own resource authorization; a reference never grants access.
type IncidentReference struct {
	TenantID uint64 `json:"tenant_id,string"`
	ID       uint64 `json:"id,string"`
	Number   string `json:"number"`
	Version  uint64 `json:"version,string"`
}

type RelatedWorkReference struct {
	TenantID   uint64 `json:"tenant_id,string"`
	ObjectType string `json:"object_type"`
	ObjectID   string `json:"object_id"`
	Number     string `json:"number"`
}

// Optional providers consume real domain events from the shared outbox and
// use these contracts when their modules are installed. No provider is enabled
// or invoked by the standalone incident process.
type ProblemLinkProvider interface {
	ProblemsForIncident(context.Context, Actor, IncidentReference) ([]RelatedWorkReference, error)
}
type ChangeLinkProvider interface {
	ChangesForIncident(context.Context, Actor, IncidentReference) ([]RelatedWorkReference, error)
}
type AlertLinkProvider interface {
	AlertsForIncident(context.Context, Actor, IncidentReference) ([]RelatedWorkReference, error)
}
