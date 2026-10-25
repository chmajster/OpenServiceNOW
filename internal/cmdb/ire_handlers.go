package cmdb

import (
	"net/http"
	"strconv"

	"github.com/chmajster/OpenServiceNOW/internal/identity"
	"github.com/chmajster/OpenServiceNOW/internal/platform/httpapi"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
	"github.com/go-chi/chi/v5"
)

func registerIRERoutes(r chi.Router, h *Handler, m *identity.Middleware) {
	r.Route("/ire", func(r chi.Router) {
		r.Get("/sources", h.ire("sources.list"))
		r.With(m.RequireCSRF).Post("/sources", h.ire("sources.create"))
		r.With(m.RequireCSRF).Put("/sources/{id}", h.ire("sources.update"))
		r.Get("/rules", h.ire("rules.list"))
		r.With(m.RequireCSRF).Put("/rules/{class}", h.ire("rules.save"))
		r.Get("/policies", h.ire("policies.list"))
		r.With(m.RequireCSRF).Post("/policies", h.ire("policies.save"))
		r.With(m.RequireCSRF).Post("/match", h.ire("match"))
		r.Get("/runs", h.ire("runs.list"))
		r.With(m.RequireCSRF).Post("/runs", h.ire("runs.create"))
		r.Get("/runs/{id}", h.ire("runs.get"))
		r.Get("/runs/{id}/rows", h.ire("runs.rows"))
		r.With(m.RequireCSRF).Post("/runs/{id}/{action:apply|cancel}", h.ire("runs.action"))
		r.Get("/conflicts", h.ire("conflicts.list"))
		r.With(m.RequireCSRF).Post("/conflicts/{id}/{action:resolve|dismiss}", h.ire("conflicts.resolve"))
		r.With(m.RequireCSRF).Post("/merges/plan", h.ire("merge.plan"))
		r.With(m.RequireCSRF).Post("/merges/apply", h.ire("merge.apply"))
		r.Get("/quality/config", h.ire("quality.config"))
		r.With(m.RequireCSRF).Put("/quality/config", h.ire("quality.config.save"))
		r.Get("/quality/summary", h.ire("quality.summary"))
		r.Get("/quality/findings", h.ire("quality.findings"))
		r.With(m.RequireCSRF).Post("/quality/run", h.ire("quality.run"))
		r.Get("/cis/{id}/provenance", h.ire("ci.provenance"))
		r.With(m.RequireCSRF).Post("/cis/{id}/overrides", h.ire("ci.override.set"))
		r.With(m.RequireCSRF).Post("/cis/{id}/overrides/{field}/clear", h.ire("ci.override.clear"))
	})
}

func (h *Handler) ire(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a, _ := r.Context().Value(actorKey{}).(Actor)
		ctx := r.Context()
		id, err := number(chi.URLParam(r, "id"), true)
		if err != nil {
			reply(w, r, nil, err)
			return
		}
		before, limit, err := pagination(r)
		if err != nil {
			reply(w, r, nil, err)
			return
		}
		q := r.URL.Query()
		var value any
		switch action {
		case "sources.list":
			value, err = h.service.ListSources(ctx, a, before, limit)
		case "sources.create", "sources.update":
			var body DataSourceInput
			if !decode(w, r, &body) {
				return
			}
			value, err = h.service.SaveSource(ctx, a, id, body)
		case "rules.list":
			value, err = h.service.ListIdentityRules(ctx, a)
		case "rules.save":
			var body IdentityRuleInput
			if !decode(w, r, &body) {
				return
			}
			body.Class = chi.URLParam(r, "class")
			value, err = h.service.SaveIdentityRule(ctx, a, body)
		case "policies.list":
			value, err = h.service.ListFieldPolicies(ctx, a)
		case "policies.save":
			var body FieldPolicy
			if !decode(w, r, &body) {
				return
			}
			value, err = h.service.SaveFieldPolicy(ctx, a, body)
		case "match":
			var body struct {
				SourceID    uint64      `json:"source_id,string"`
				Observation Observation `json:"observation"`
			}
			if !decode(w, r, &body) {
				return
			}
			value, err = h.service.MatchObservation(ctx, a, body.SourceID, body.Observation)
		case "runs.list":
			value, err = h.service.ListReconciliationRuns(ctx, a, before, limit)
		case "runs.create":
			var body ReconciliationRunInput
			if !decode(w, r, &body) {
				return
			}
			value, err = h.service.CreateReconciliationRun(ctx, a, body)
		case "runs.get":
			value, err = h.service.GetReconciliationRun(ctx, a, id)
		case "runs.rows":
			after := uint32(0)
			if raw := q.Get("after_row"); raw != "" {
				n, parseErr := strconv.ParseUint(raw, 10, 32)
				if parseErr != nil {
					err = ErrInvalid
				} else {
					after = uint32(n)
				}
			}
			if err == nil {
				var items []ReconciliationObservation
				var next uint32
				items, next, err = h.service.ReconciliationRows(ctx, a, id, after, limit)
				value = map[string]any{"items": items, "next_after_row": next}
			}
		case "runs.action":
			var body struct {
				Version uint64 `json:"version,string"`
			}
			if !decode(w, r, &body) {
				return
			}
			value, err = h.service.RunAction(ctx, a, id, body.Version, chi.URLParam(r, "action"))
		case "conflicts.list":
			value, err = h.service.ListIREConflicts(ctx, a, before, q.Get("state"), limit)
		case "conflicts.resolve":
			var body struct {
				Version uint64 `json:"version,string"`
				CIID    uint64 `json:"ci_id,string"`
				Note    string `json:"note"`
			}
			if !decode(w, r, &body) {
				return
			}
			value, err = h.service.ResolveIREConflict(ctx, a, id, body.Version, body.CIID, body.Note, chi.URLParam(r, "action"))
		case "merge.plan":
			var body struct {
				SourceCIID    uint64 `json:"source_ci_id,string"`
				CanonicalCIID uint64 `json:"canonical_ci_id,string"`
			}
			if !decode(w, r, &body) {
				return
			}
			value, err = h.service.MergePlan(ctx, a, body.SourceCIID, body.CanonicalCIID)
		case "merge.apply":
			var body MergeInput
			if !decode(w, r, &body) {
				return
			}
			value, err = h.service.MergeCIs(ctx, a, body)
		case "quality.config":
			value, err = h.service.QualityConfig(ctx, a)
		case "quality.config.save":
			var body QualityConfig
			if !decode(w, r, &body) {
				return
			}
			value, err = h.service.SaveQualityConfig(ctx, a, body)
		case "quality.summary":
			value, err = h.service.QualitySummary(ctx, a)
		case "quality.findings":
			var owner uint64
			owner, err = number(q.Get("owner_id"), true)
			if err == nil {
				value, err = h.service.QualityFindings(ctx, a, before, q.Get("class"), q.Get("rule"), q.Get("state"), owner, limit)
			}
		case "quality.run":
			if r.ContentLength > 0 {
				var empty struct{}
				if !decode(w, r, &empty) {
					return
				}
			}
			var jobID uint64
			jobID, err = h.service.RunQualityNow(ctx, a, r.Header.Get("Idempotency-Key"))
			value = map[string]any{"job_id": jobID}
		case "ci.provenance":
			value, err = h.service.Provenance(ctx, a, id)
		case "ci.override.set":
			var body struct {
				Version uint64 `json:"version,string"`
				ManualOverrideInput
			}
			if !decode(w, r, &body) {
				return
			}
			value, err = h.service.SetManualOverride(ctx, a, id, body.Version, body.ManualOverrideInput)
		case "ci.override.clear":
			var body struct {
				Version uint64 `json:"version,string"`
				Reason  string `json:"reason"`
			}
			if !decode(w, r, &body) {
				return
			}
			value, err = h.service.ClearManualOverride(ctx, a, id, body.Version, chi.URLParam(r, "field"), body.Reason)
		default:
			err = workrecords.ErrForbidden
		}
		if err == nil {
			httpapi.WriteJSON(w, http.StatusOK, value)
			return
		}
		reply(w, r, nil, err)
	}
}
