package incidents

import (
	"context"
	"fmt"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
	"strings"
)

func (s *Service) Search(ctx context.Context, a Actor, q string) ([]workrecords.Reference, error) {
	if !a.Has("read") && !a.Has("read_own") {
		return nil, ErrForbidden
	}
	f := Filter{Query: q, View: "all", Sort: "newest", Limit: 10}
	if err := validateFilter(f); err != nil {
		return nil, err
	}
	where, args := listWhere(a, f)
	args = append(args, q)
	rows, err := s.repo.db.QueryContext(ctx, "SELECT i.id,i.number,i.title,i.state"+recordJoins+where+" ORDER BY (i.number=?) DESC,i.id DESC LIMIT 10", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []workrecords.Reference{}
	for rows.Next() {
		var r workrecords.Reference
		if err = rows.Scan(&r.ID, &r.Number, &r.Title, &r.State); err != nil {
			return nil, err
		}
		r.Type = "incident"
		r.URL = fmt.Sprintf("/incidents/%d", r.ID)
		r.Exact = strings.EqualFold(r.Number, q)
		out = append(out, r)
	}
	return out, rows.Err()
}
