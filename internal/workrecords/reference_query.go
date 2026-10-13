package workrecords

import (
	"context"
	"fmt"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"strings"
)

// ReferenceScope is constructed only by a domain's read policy. Its SQL fields
// never contain client expressions. Every metadata read retains tenant/object ACL.
type ReferenceScope struct {
	ParentTab, ParentQuery                                          string
	Class                                                           string
	Kind, Root                                                      string
	ID, Number, Title, State, Version, Parent, CreatedAt, UpdatedAt string
	GroupID, GroupName, AssignedTo, ReporterID, Active              string
	From, Where                                                     string
	Args                                                            []any
}

func (s ReferenceScope) Restricted(clause string, args ...any) ReferenceScope {
	s.Where += " AND (" + clause + ")"
	s.Args = append(append([]any{}, s.Args...), args...)
	return s
}

type ReferenceSelection struct {
	Query  string
	IDs    []uint64
	Before uint64
	Limit  int
}

func References(ctx context.Context, db database.Executor, s ReferenceScope, f ReferenceSelection) ([]Reference, error) {
	if f.Limit < 1 || f.Limit > 150 || len(f.IDs) > 150 || s.Where == "" {
		return nil, ErrInvalid
	}
	class := s.Class
	if class == "" {
		class = "''"
	}
	parent := s.Parent
	if parent == "" {
		parent = "0"
	}
	args := append([]any{}, s.Args...)
	where := s.Where
	if f.Query != "" {
		where += " AND (LOCATE(?," + s.Number + ")>0 OR LOCATE(?," + s.Title + ")>0)"
		args = append(args, f.Query, f.Query)
	}
	if len(f.IDs) > 0 {
		placeholders := make([]string, len(f.IDs))
		for i, id := range f.IDs {
			if id == 0 {
				return nil, ErrInvalid
			}
			placeholders[i] = "?"
			args = append(args, id)
		}
		where += " AND " + s.ID + " IN (" + strings.Join(placeholders, ",") + ")"
	}
	if f.Before > 0 {
		where += " AND " + s.ID + "<?"
		args = append(args, f.Before)
	}
	order := " ORDER BY " + s.ID + " DESC"
	if f.Query != "" {
		order = " ORDER BY (" + s.Number + "=?) DESC," + s.ID + " DESC"
		args = append(args, f.Query)
	}
	args = append(args, f.Limit)
	rows, err := db.QueryContext(ctx, "SELECT "+strings.Join([]string{s.ID, s.Number, s.Title, s.State, s.Version, parent, class}, ",")+s.From+where+order+" LIMIT ?", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Reference{}
	for rows.Next() {
		var r Reference
		var parentID uint64
		if err = rows.Scan(&r.ID, &r.Number, &r.Title, &r.State, &r.Version, &parentID, &r.Class); err != nil {
			return nil, err
		}
		r.Type = s.Kind
		r.URL = fmt.Sprintf("/%s/%d", s.Root, r.ID)
		if s.Parent != "" {
			tab, key := s.ParentTab, s.ParentQuery
			if tab == "" {
				tab = "tasks"
			}
			if key == "" {
				key = "task_id"
			}
			r.URL = fmt.Sprintf("/%s/%d?tab=%s&%s=%d", s.Root, parentID, tab, key, r.ID)
		}
		r.Exact = f.Query != "" && strings.EqualFold(r.Number, f.Query)
		out = append(out, r)
	}
	return out, rows.Err()
}

// AssignmentView narrows an already authorized scope; it never grants read access.
func AssignmentView(s ReferenceScope, a Principal, view string) (ReferenceScope, error) {
	switch view {
	case "", "all":
		return s, nil
	case "mine":
		return s.Restricted(s.AssignedTo+"=?", a.UserID), nil
	case "my_group":
		return s.Restricted("EXISTS(SELECT 1 FROM group_memberships queue_gm JOIN `groups` queue_g ON queue_g.tenant_id=queue_gm.tenant_id AND queue_g.id=queue_gm.group_id AND queue_g.active=TRUE WHERE queue_gm.tenant_id=? AND queue_gm.group_id="+s.GroupID+" AND queue_gm.user_id=?)", a.TenantID, a.UserID), nil
	case "unassigned":
		return s.Restricted(s.AssignedTo + " IS NULL"), nil
	default:
		return s, ErrInvalid
	}
}
