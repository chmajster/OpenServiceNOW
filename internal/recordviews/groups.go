package recordviews

import (
	"context"
	"github.com/chmajster/OpenServiceNOW/internal/recordlists"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
	"net/http"
	"strconv"
)

type GroupChoice struct {
	ID     uint64 `json:"id,string"`
	Name   string `json:"name"`
	Active bool   `json:"active"`
}
type GroupPage struct {
	Groups []GroupChoice `json:"groups"`
	Next   uint64        `json:"next_after_id,string,omitempty"`
}

func (s *Service) Groups(ctx context.Context, a workrecords.Principal, q string, after uint64) (GroupPage, error) {
	out := GroupPage{Groups: []GroupChoice{}}
	if !a.Has("workspace.views.share") {
		return out, workrecords.ErrForbidden
	}
	if len([]rune(q)) > 200 {
		return out, recordlists.ErrInvalid
	}
	rows, err := s.DB.QueryContext(ctx, "SELECT g.id,g.name FROM `groups` g JOIN group_memberships gm ON gm.tenant_id=g.tenant_id AND gm.group_id=g.id WHERE g.tenant_id=? AND gm.user_id=? AND g.active=TRUE AND g.id>? AND (?='' OR LOCATE(?,g.name)>0) ORDER BY g.id LIMIT 51", a.TenantID, a.UserID, after, q, q)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var g GroupChoice
		g.Active = true
		if err = rows.Scan(&g.ID, &g.Name); err != nil {
			return out, err
		}
		out.Groups = append(out.Groups, g)
	}
	if len(out.Groups) > 50 {
		out.Groups = out.Groups[:50]
		out.Next = out.Groups[49].ID
	}
	return out, rows.Err()
}
func (h *Handler) groups(w http.ResponseWriter, r *http.Request) {
	a, err := h.actor(r)
	if err != nil {
		respond(w, r, 0, nil, err)
		return
	}
	var after uint64
	if raw := r.URL.Query().Get("after_id"); raw != "" {
		after, err = strconv.ParseUint(raw, 10, 64)
		if err != nil || after == 0 {
			respond(w, r, 0, nil, recordlists.ErrInvalid)
			return
		}
	}
	v, err := h.Service.Groups(r.Context(), a, r.URL.Query().Get("q"), after)
	respond(w, r, 200, v, err)
}
