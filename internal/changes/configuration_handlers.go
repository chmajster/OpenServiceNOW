package changes

import (
	"github.com/chmajster/OpenServiceNOW/internal/cmdb"
	"github.com/go-chi/chi/v5"
	"net/http"
	"time"
)

func (h *Handler) calendar(w http.ResponseWriter, r *http.Request) {
	from, err := time.Parse(time.RFC3339, r.URL.Query().Get("from"))
	if err != nil {
		writeError(w, r, ErrInvalid)
		return
	}
	to, err := time.Parse(time.RFC3339, r.URL.Query().Get("to"))
	if err != nil {
		writeError(w, r, ErrInvalid)
		return
	}
	out, err := h.service.Calendar(r.Context(), actor(r), from, to)
	respond(w, r, 200, out, err)
}
func (h *Handler) collisions(w http.ResponseWriter, r *http.Request) {
	id, err := objectID(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	out, err := h.service.Collisions(r.Context(), actor(r), id)
	respond(w, r, 200, out, err)
}
func (h *Handler) ruleSave(w http.ResponseWriter, r *http.Request) {
	id, err := number(chi.URLParam(r, "ruleID"), true)
	if err != nil {
		writeError(w, r, err)
		return
	}
	var in CalendarRule
	if !decode(w, r, &in) {
		return
	}
	out, err := h.service.SaveRule(r.Context(), actor(r), id, in, r.Header.Get("Idempotency-Key"))
	respond(w, r, 200, out, err)
}

type RulePage struct {
	Items []CalendarRule `json:"items"`
	Next  uint64         `json:"next_before_id,string,omitempty"`
}

func (h *Handler) rules(w http.ResponseWriter, r *http.Request) {
	a := actor(r)
	if !a.Has("change.calendar") {
		writeError(w, r, ErrForbidden)
		return
	}
	before, limit, err := pagination(r, 25, "before_id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	p, args := cmdb.Access(a, "ci")
	params := append([]any{a.TenantID, before, before}, args...)
	params = append(params, limit+1)
	rows, err := h.service.db.QueryContext(r.Context(), "SELECT cr.id,cr.version,cr.ci_id,cr.kind,cr.title,cr.starts_at,cr.ends_at,cr.time_zone,cr.active FROM change_calendar_rules cr LEFT JOIN cmdb_cis ci ON ci.tenant_id=cr.tenant_id AND ci.id=cr.ci_id WHERE cr.tenant_id=? AND (?=0 OR cr.id<?) AND (cr.ci_id IS NULL OR "+p+") ORDER BY cr.id DESC LIMIT ?", params...)
	if err != nil {
		writeError(w, r, err)
		return
	}
	defer rows.Close()
	out := RulePage{Items: []CalendarRule{}}
	for rows.Next() {
		var item CalendarRule
		if err = rows.Scan(&item.ID, &item.Version, &item.CIID, &item.Kind, &item.Title, &item.StartsAt, &item.EndsAt, &item.TimeZone, &item.Active); err != nil {
			writeError(w, r, err)
			return
		}
		out.Items = append(out.Items, item)
	}
	if err = rows.Err(); err != nil {
		writeError(w, r, err)
		return
	}
	if len(out.Items) > limit {
		out.Items = out.Items[:limit]
		out.Next = out.Items[limit-1].ID
	}
	respond(w, r, 200, out, nil)
}
func (h *Handler) templateSave(w http.ResponseWriter, r *http.Request) {
	id, err := number(chi.URLParam(r, "templateID"), true)
	if err != nil {
		writeError(w, r, err)
		return
	}
	var in TemplateInput
	if !decode(w, r, &in) {
		return
	}
	out, err := h.service.SaveTemplate(r.Context(), actor(r), id, in, r.Header.Get("Idempotency-Key"))
	respond(w, r, 200, out, err)
}
func (h *Handler) templatePublish(w http.ResponseWriter, r *http.Request) {
	id, err := childID(r, "templateID")
	if err != nil {
		writeError(w, r, err)
		return
	}
	var in struct {
		Version  uint64 `json:"version,string"`
		Withdraw bool   `json:"withdraw"`
	}
	if !decode(w, r, &in) {
		return
	}
	out, err := h.service.PublishTemplate(r.Context(), actor(r), id, in.Version, in.Withdraw, r.Header.Get("Idempotency-Key"))
	respond(w, r, 200, out, err)
}
func (h *Handler) templateGet(w http.ResponseWriter, r *http.Request) {
	id, err := childID(r, "templateID")
	if err != nil {
		writeError(w, r, err)
		return
	}
	out, err := h.service.Template(r.Context(), actor(r), id)
	respond(w, r, 200, out, err)
}
func (h *Handler) templateVersion(w http.ResponseWriter, r *http.Request) {
	id, err := childID(r, "templateID")
	if err != nil {
		writeError(w, r, err)
		return
	}
	version, err := childID(r, "version")
	if err != nil {
		writeError(w, r, err)
		return
	}
	out, err := h.service.TemplateConfig(r.Context(), actor(r), id, version)
	respond(w, r, 200, out, err)
}

type TemplateSummary struct {
	ID               uint64  `json:"id,string"`
	Name             string  `json:"name"`
	Version          uint64  `json:"version,string"`
	PublishedVersion *uint64 `json:"published_version,string"`
	Withdrawn        bool    `json:"withdrawn"`
}

func (h *Handler) templates(w http.ResponseWriter, r *http.Request) {
	a := actor(r)
	if !a.Has("change.read") && !a.Has("change.read_own") && !a.Has("change.template_author") && !a.Has("change.template_approve") {
		writeError(w, r, ErrForbidden)
		return
	}
	before, limit, err := pagination(r, 25, "before_id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	query := "SELECT id,name,version,published_version,withdrawn FROM change_templates WHERE tenant_id=? AND (?=0 OR id<?)"
	params := []any{a.TenantID, before, before}
	if !a.Has("change.template_author") && !a.Has("change.template_approve") {
		query += " AND published_version IS NOT NULL AND withdrawn=FALSE"
	}
	if q := r.URL.Query().Get("q"); q != "" {
		if len([]rune(q)) > 160 {
			writeError(w, r, ErrInvalid)
			return
		}
		query += " AND LOCATE(?,name)>0"
		params = append(params, q)
	}
	query += " ORDER BY id DESC LIMIT ?"
	params = append(params, limit+1)
	rows, err := h.service.db.QueryContext(r.Context(), query, params...)
	if err != nil {
		writeError(w, r, err)
		return
	}
	defer rows.Close()
	out := struct {
		Items []TemplateSummary `json:"items"`
		Next  uint64            `json:"next_before_id,string,omitempty"`
	}{Items: []TemplateSummary{}}
	for rows.Next() {
		var item TemplateSummary
		if err = rows.Scan(&item.ID, &item.Name, &item.Version, &item.PublishedVersion, &item.Withdrawn); err != nil {
			writeError(w, r, err)
			return
		}
		out.Items = append(out.Items, item)
	}
	if err = rows.Err(); err != nil {
		writeError(w, r, err)
		return
	}
	if len(out.Items) > limit {
		out.Items = out.Items[:limit]
		out.Next = out.Items[limit-1].ID
	}
	respond(w, r, 200, out, nil)
}
func (h *Handler) approvalProcesses(w http.ResponseWriter, r *http.Request) {
	before, limit, err := pagination(r, 25, "before_id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	out, err := h.service.ApprovalProcesses(r.Context(), actor(r), r.URL.Query().Get("change_type"), before, limit)
	respond(w, r, 200, out, err)
}
func (h *Handler) approvalProcess(w http.ResponseWriter, r *http.Request) {
	id, err := childID(r, "definitionID")
	if err != nil {
		writeError(w, r, err)
		return
	}
	config, version, err := h.service.ApprovalProcess(r.Context(), actor(r), id)
	respond(w, r, 200, map[string]any{"config": config, "version": version}, err)
}

func (h *Handler) ruleGet(w http.ResponseWriter, r *http.Request) {
	id, err := childID(r, "ruleID")
	var out CalendarRule
	if err == nil {
		out, err = h.service.Rule(r.Context(), actor(r), id)
	}
	respond(w, r, 200, out, err)
}
