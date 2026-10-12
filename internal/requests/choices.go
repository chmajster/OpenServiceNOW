package requests

import (
	"github.com/chmajster/OpenServiceNOW/internal/catalog"
	"net/http"
)

type choice struct {
	ID               uint64 `json:"id,string"`
	Name             string `json:"name,omitempty"`
	DisplayName      string `json:"display_name,omitempty"`
	Email            string `json:"email,omitempty"`
	UserActive       bool   `json:"user_active"`
	MembershipActive bool   `json:"membership_active"`
}
type choicePage struct {
	Users  []choice `json:"users"`
	Groups []choice `json:"groups"`
	Next   uint64   `json:"next_after_id,string,omitempty"`
}

func (h *Handler) Groups(w http.ResponseWriter, r *http.Request) {
	if !actor(r).Has("admin.catalog.manage") {
		writeError(w, r, ErrForbidden)
		return
	}
	h.choices(w, r, 0)
}
func (h *Handler) TaskUsers(w http.ResponseWriter, r *http.Request) {
	id, err := objectID(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	t, err := h.service.Task(r.Context(), actor(r), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	h.choices(w, r, *t.Assignment.AssignmentGroupID)
}
func (h *Handler) choices(w http.ResponseWriter, r *http.Request, group uint64) {
	after, limit, err := pagination(r, 25, "after_id")
	q := r.URL.Query().Get("q")
	if err != nil || !catalog.Text(q, 0, 200) {
		writeError(w, r, ErrInvalid)
		return
	}
	a := actor(r)
	query := "SELECT id,name,'' FROM `groups` WHERE tenant_id=? AND id>? AND active=TRUE AND (?='' OR LOCATE(?,name)>0) ORDER BY id LIMIT ?"
	args := []any{a.TenantID, after, q, q, limit + 1}
	if group > 0 {
		query = "SELECT u.id,u.display_name,u.email FROM users u JOIN tenant_memberships tm ON tm.user_id=u.id JOIN group_memberships gm ON gm.tenant_id=tm.tenant_id AND gm.user_id=u.id JOIN `groups` g ON g.tenant_id=gm.tenant_id AND g.id=gm.group_id AND g.active=TRUE WHERE tm.tenant_id=? AND gm.group_id=? AND u.id>? AND u.active=TRUE AND tm.active=TRUE AND (?='' OR LOCATE(?,u.display_name)>0 OR LOCATE(?,u.email)>0) ORDER BY u.id LIMIT ?"
		args = []any{a.TenantID, group, after, q, q, q, limit + 1}
	}
	rows, err := h.service.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		writeError(w, r, err)
		return
	}
	defer rows.Close()
	items := []choice{}
	for rows.Next() {
		c := choice{UserActive: true, MembershipActive: true}
		if err := rows.Scan(&c.ID, &c.Name, &c.Email); err != nil {
			writeError(w, r, err)
			return
		}
		if group > 0 {
			c.DisplayName = c.Name
		}
		items = append(items, c)
	}
	if err := rows.Err(); err != nil {
		writeError(w, r, err)
		return
	}
	p := choicePage{Users: []choice{}, Groups: []choice{}}
	if len(items) > limit {
		items = items[:limit]
		p.Next = items[limit-1].ID
	}
	if group > 0 {
		p.Users = items
	} else {
		p.Groups = items
	}
	respond(w, r, 200, p, nil)
}
