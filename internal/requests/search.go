package requests

import (
	"context"
	"fmt"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
	"strings"
)

func (s *Service) Search(ctx context.Context, a Actor, q string, tasks bool) ([]workrecords.Reference, error) {
	alias, kind, root, joins, title, view := "r", "request", "requests", " FROM service_requests r", "r.number", "all"
	if tasks {
		alias, kind, root, joins, title, view = "t", "fulfillment_task", "fulfillment", taskJoins, "t.title", "group"
	}
	where, args, err := listWhere(a, tasks, q, "", view, false)
	if err != nil {
		return nil, err
	}
	args = append(args, q)
	rows, err := s.db.QueryContext(ctx, "SELECT "+alias+".id,"+alias+".number,"+title+","+alias+".state"+joins+where+" ORDER BY ("+alias+".number=?) DESC,"+alias+".id DESC LIMIT 10", args...)
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
		r.Type = kind
		r.URL = fmt.Sprintf("/%s/%d", root, r.ID)
		r.Exact = strings.EqualFold(r.Number, q)
		out = append(out, r)
	}
	return out, rows.Err()
}
