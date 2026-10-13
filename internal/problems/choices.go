package problems

import (
	"context"
	"github.com/chmajster/OpenServiceNOW/internal/catalog"
	"github.com/go-chi/chi/v5"
	"net/http"
)

type Choice struct {
	ID   uint64 `json:"id,string"`
	Name string `json:"name"`
}
type ChoicePage struct {
	Items []Choice `json:"items"`
	Next  uint64   `json:"next_before_id,string,omitempty"`
}

func (s *Service) Choices(ctx context.Context, a Actor, kind, q string, before uint64, limit int) (ChoicePage, error) {
	out := ChoicePage{Items: []Choice{}}
	if !a.Has("problem.read") && !a.Has("problem.read_own") {
		return out, ErrForbidden
	}
	if limit < 1 || limit > 100 || !catalog.Text(q, 0, 160) {
		return out, ErrInvalid
	}
	query := ""
	args := []any{a.TenantID}
	if kind == "users" {
		query = "SELECT u.id,u.display_name FROM users u JOIN tenant_memberships tm ON tm.user_id=u.id WHERE tm.tenant_id=? AND tm.active=TRUE AND u.active=TRUE"
	} else if kind == "groups" {
		query = "SELECT u.id,u.name FROM `groups` u WHERE u.tenant_id=? AND u.active=TRUE"
	} else {
		return out, ErrInvalid
	}
	if q != "" {
		if kind == "users" {
			query += " AND LOCATE(?,u.display_name)>0"
		} else {
			query += " AND LOCATE(?,u.name)>0"
		}
		args = append(args, q)
	}
	if before > 0 {
		query += " AND u.id<?"
		args = append(args, before)
	}
	query += " ORDER BY u.id DESC LIMIT ?"
	args = append(args, limit+1)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var c Choice
		if err = rows.Scan(&c.ID, &c.Name); err != nil {
			return out, err
		}
		out.Items = append(out.Items, c)
	}
	if len(out.Items) > limit {
		out.Items = out.Items[:limit]
		out.Next = out.Items[limit-1].ID
	}
	return out, rows.Err()
}
func (h *Handler) lookup(w http.ResponseWriter, r *http.Request) {
	before, limit, err := pagination(r, 20, "before_id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	kind := chi.URLParam(r, "kind")
	if kind == "incidents" {
		out, err := h.service.IncidentChoices(r.Context(), actor(r), r.URL.Query().Get("q"), 0, before, limit)
		respond(w, r, 200, out, err)
		return
	}
	out, err := h.service.Choices(r.Context(), actor(r), kind, r.URL.Query().Get("q"), before, limit)
	respond(w, r, 200, out, err)
}
