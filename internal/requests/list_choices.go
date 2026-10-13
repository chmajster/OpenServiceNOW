package requests

import (
	"github.com/chmajster/OpenServiceNOW/internal/catalog"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
	"github.com/go-chi/chi/v5"
	"net/http"
	"strconv"
)

func (h *Handler) listChoices(w http.ResponseWriter, r *http.Request) {
	selected, e := workrecords.ChoiceID(r.URL.Query().Get("id"))
	if e != nil {
		writeError(w, r, ErrInvalid)
		return
	}
	a := actor(r)
	kind := chi.URLParam(r, "kind")
	if kind != "users" && kind != "groups" {
		writeError(w, r, ErrInvalid)
		return
	}
	if !a.Has("request.manage") && !a.Has("request.fulfill") && (kind != "users" || !a.Has("request.read_own")) {
		writeError(w, r, ErrForbidden)
		return
	}
	q := r.URL.Query().Get("q")
	if !catalog.Text(q, 0, 200) {
		writeError(w, r, ErrInvalid)
		return
	}
	var after uint64
	var err error
	if raw := r.URL.Query().Get("after_id"); raw != "" {
		after, err = strconv.ParseUint(raw, 10, 64)
		if err != nil || after == 0 {
			writeError(w, r, ErrInvalid)
			return
		}
	}
	query := "SELECT g.id,g.name FROM `groups` g WHERE g.tenant_id=? AND g.active=TRUE AND g.id>? AND (?='' OR LOCATE(?,g.name)>0)"
	args := []any{a.TenantID, after, q, q}
	alias := "g"
	if kind == "users" {
		alias = "u"
		query = "SELECT u.id,u.display_name,u.email FROM users u JOIN tenant_memberships tm ON tm.user_id=u.id AND tm.tenant_id=? WHERE u.active=TRUE AND tm.active=TRUE AND u.id>? AND (?='' OR LOCATE(?,u.display_name)>0 OR LOCATE(?,u.email)>0)"
		args = append(args, q)
	}
	if !a.Has("request.manage") {
		if kind == "groups" {
			query += " AND EXISTS(SELECT 1 FROM group_memberships gm WHERE gm.tenant_id=g.tenant_id AND gm.group_id=g.id AND gm.user_id=?)"
			args = append(args, a.UserID)
		} else if !a.Has("request.fulfill") {
			query += " AND u.id=?"
			args = append(args, a.UserID)
		} else {
			query += " AND (u.id=? OR EXISTS(SELECT 1 FROM group_memberships target JOIN group_memberships actor ON actor.tenant_id=target.tenant_id AND actor.group_id=target.group_id JOIN `groups` g ON g.tenant_id=target.tenant_id AND g.id=target.group_id WHERE target.tenant_id=tm.tenant_id AND target.user_id=u.id AND actor.user_id=? AND g.active=TRUE))"
			args = append(args, a.UserID, a.UserID)
		}
	}
	query, args = workrecords.RestrictChoice(query, args, alias+".id", selected)
	query += " ORDER BY " + alias + ".id LIMIT 51"
	rows, err := h.service.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		writeError(w, r, err)
		return
	}
	defer rows.Close()
	type choice struct {
		ID               uint64 `json:"id,string"`
		Name             string `json:"name,omitempty"`
		DisplayName      string `json:"display_name,omitempty"`
		Email            string `json:"email,omitempty"`
		Status           string `json:"status,omitempty"`
		Active           bool   `json:"active"`
		MembershipActive bool   `json:"membership_active"`
		UserActive       bool   `json:"user_active"`
	}
	items := []choice{}
	for rows.Next() {
		v := choice{Active: true, MembershipActive: true, UserActive: true, Status: "ACTIVE"}
		if kind == "groups" {
			err = rows.Scan(&v.ID, &v.Name)
		} else {
			err = rows.Scan(&v.ID, &v.DisplayName, &v.Email)
		}
		if err != nil {
			writeError(w, r, err)
			return
		}
		items = append(items, v)
	}
	if err = rows.Err(); err != nil {
		writeError(w, r, err)
		return
	}
	var next uint64
	if len(items) > 50 {
		items = items[:50]
		next = items[49].ID
	}
	data := map[string]any{kind: items}
	if next > 0 {
		data["next_after_id"] = strconv.FormatUint(next, 10)
	}
	respond(w, r, 200, data, nil)
}
