package incidents

import (
	"context"
	"strings"
)

type Choice struct {
	ID               uint64 `json:"id,string"`
	Name             string `json:"name,omitempty"`
	DisplayName      string `json:"display_name,omitempty"`
	Email            string `json:"email,omitempty"`
	UserActive       bool   `json:"user_active"`
	MembershipActive bool   `json:"membership_active"`
}
type Choices struct {
	Users       []Choice `json:"users"`
	Groups      []Choice `json:"groups"`
	NextAfterID uint64   `json:"next_after_id,string,omitempty"`
}

func (s *Service) Choices(ctx context.Context, a Actor, kind, purpose, q string, after, group uint64, limit int) (Choices, error) {
	if !a.Has("read") && !a.Has("read_own") {
		return Choices{}, ErrForbidden
	}
	if limit < 1 || limit > 100 || !validText(q, 0, 200) {
		return Choices{}, ErrInvalid
	}
	escaped := strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(q)
	var query string
	args := []any{a.TenantID, after}
	switch kind {
	case "incidents":
		access, acl := readPredicate(a, "i")
		query = `SELECT i.id,CONCAT(i.number,' · ',i.title),'' FROM incidents i WHERE i.tenant_id=? AND i.id>? AND ` + access
		args = append(args, acl...)
		if q != "" {
			query += ` AND (i.number LIKE ? ESCAPE '!' OR i.title LIKE ? ESCAPE '!')`
			args = append(args, "%"+escaped+"%", "%"+escaped+"%")
		}
		query += " ORDER BY i.id"
	case "users":
		if purpose != "affected" && purpose != "assignment" {
			return Choices{}, ErrInvalid
		}
		if purpose == "assignment" && !a.Has("assign") {
			return Choices{}, ErrForbidden
		}
		query = `SELECT u.id,u.display_name,u.email FROM users u JOIN tenant_memberships tm ON tm.user_id=u.id WHERE tm.tenant_id=? AND u.id>? AND u.active=TRUE AND tm.active=TRUE`
		if purpose == "affected" && !a.Has("read") {
			query += " AND u.id=?"
			args = append(args, a.UserID)
		}
		if group > 0 {
			query += " AND EXISTS(SELECT 1 FROM group_memberships gm JOIN `groups` g ON g.tenant_id=gm.tenant_id AND g.id=gm.group_id AND g.active=TRUE WHERE gm.tenant_id=tm.tenant_id AND gm.group_id=? AND gm.user_id=u.id)"
			args = append(args, group)
		}
		if q != "" {
			query += " AND (u.display_name LIKE ? ESCAPE '!' OR u.email LIKE ? ESCAPE '!')"
			args = append(args, "%"+escaped+"%", "%"+escaped+"%")
		}
		query += " ORDER BY u.id"
	case "groups":
		query = "SELECT g.id,g.name,'' FROM `groups` g WHERE g.tenant_id=? AND g.id>? AND g.active=TRUE"
		if !a.Has("read") && !a.Has("assign") {
			query += " AND EXISTS(SELECT 1 FROM group_memberships gm WHERE gm.tenant_id=g.tenant_id AND gm.group_id=g.id AND gm.user_id=?)"
			args = append(args, a.UserID)
		}
		if q != "" {
			query += " AND g.name LIKE ? ESCAPE '!'"
			args = append(args, "%"+escaped+"%")
		}
		query += " ORDER BY g.id"
	default:
		return Choices{}, ErrInvalid
	}
	query += " LIMIT ?"
	args = append(args, limit+1)
	rows, err := s.repo.db.QueryContext(ctx, query, args...)
	if err != nil {
		return Choices{}, err
	}
	defer rows.Close()
	choices := []Choice{}
	for rows.Next() {
		var c Choice
		var name, email string
		if err := rows.Scan(&c.ID, &name, &email); err != nil {
			return Choices{}, err
		}
		if kind == "users" {
			c.DisplayName = name
			c.Email = email
			c.UserActive = true
			c.MembershipActive = true
		} else {
			c.Name = name
		}
		choices = append(choices, c)
	}
	if err := rows.Err(); err != nil {
		return Choices{}, err
	}
	result := Choices{Users: []Choice{}, Groups: []Choice{}}
	if len(choices) > limit {
		choices = choices[:limit]
		result.NextAfterID = choices[limit-1].ID
	}
	if kind == "users" || kind == "incidents" {
		result.Users = choices
	} else {
		result.Groups = choices
	}
	return result, nil
}
func (s *Service) Metadata(a Actor) (any, error) {
	if !a.Has("read") && !a.Has("read_own") {
		return nil, ErrForbidden
	}
	return struct {
		Categories      []Category `json:"categories"`
		States          []Option   `json:"states"`
		ResolutionCodes []Option   `json:"resolution_codes"`
		PriorityMatrix  [3][3]int  `json:"priority_matrix"`
		CanCreate       bool       `json:"can_create"`
	}{Categories, States, ResolutionCodes, PriorityMatrix, a.Has("create")}, nil
}
