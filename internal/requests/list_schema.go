package requests

import (
	"context"
	"fmt"
	"github.com/chmajster/OpenServiceNOW/internal/recordlists"
	"strings"
	"time"
)

type ListOptions struct {
	Expression                *recordlists.Node
	GroupBy, Sort, AfterValue string
}
type GroupCount struct {
	Value string `json:"value"`
	Count uint64 `json:"count,string"`
}

var ListSorts = []string{"newest", "oldest", "number", "updated"}

func ListColumns(tasks bool) []string {
	if tasks {
		return []string{"number", "title", "state", "group_name", "assigned_name", "updated_at"}
	}
	return []string{"number", "requester_name", "state", "created_at", "updated_at"}
}
func ListGroups(tasks bool) []string {
	if tasks {
		return []string{"state", "group_id", "assigned_to"}
	}
	return []string{"state", "requester_id"}
}
func ListSchema(userID uint64, tasks bool) recordlists.Schema {
	alias, states, active := "r", []string{"SUBMITTED", "IN_PROGRESS", "FULFILLED", "CLOSED", "CANCELLED"}, "r.state IN ('SUBMITTED','IN_PROGRESS')"
	if tasks {
		alias = "t"
		states = []string{"OPEN", "IN_PROGRESS", "ON_HOLD", "COMPLETED", "CANCELLED"}
		active = "t.state NOT IN ('COMPLETED','CANCELLED') AND r.state IN ('SUBMITTED','IN_PROGRESS')"
	}
	fields := recordlists.Schema{"number": {SQL: alias + ".number", Kind: "text"}, "state": {SQL: alias + ".state", Kind: "enum", Values: states}, "updated_at": {SQL: alias + ".updated_at", Kind: "time"}, "active": {Predicate: recordlists.BooleanPredicate(active)}}
	if tasks {
		fields["title"] = recordlists.Field{SQL: "t.title", Kind: "text"}
		fields["group_id"] = recordlists.Field{SQL: "wa.assignment_group_id", Kind: "id", Nullable: true}
		fields["assigned_to"] = recordlists.Field{SQL: "wa.assigned_to", Kind: "id", Nullable: true}
		fields["my_groups"] = recordlists.Field{Predicate: func(value string) (string, []any, error) {
			if value != "true" && value != "false" {
				return "", nil, recordlists.ErrInvalid
			}
			prefix := ""
			if value == "false" {
				prefix = "NOT "
			}
			return prefix + "(g.active=TRUE AND EXISTS(SELECT 1 FROM group_memberships gm WHERE gm.tenant_id=t.tenant_id AND gm.group_id=wa.assignment_group_id AND gm.user_id=?))", []any{userID}, nil
		}}
	} else {
		fields["requester_id"] = recordlists.Field{SQL: "r.requester_id", Kind: "id"}
		fields["created_at"] = recordlists.Field{SQL: "r.created_at", Kind: "time"}
	}
	return fields
}
func listScope(a Actor, tasks bool, where string, args []any, o ListOptions) (string, []any, error) {
	if o.Sort != "" && !contains(ListSorts, o.Sort) {
		return "", nil, ErrInvalid
	}
	if o.GroupBy != "" {
		if _, err := groupColumn(tasks, o.GroupBy); err != nil {
			return "", nil, err
		}
	}
	sql, values, err := recordlists.Compile(o.Expression, ListSchema(a.UserID, tasks))
	if err != nil {
		return "", nil, ErrInvalid
	}
	if sql != "" {
		where += " AND (" + sql + ")"
		args = append(args, values...)
	}
	return where, args, nil
}
func groupColumn(tasks bool, key string) (string, error) {
	if key == "state" {
		if tasks {
			return "t.state", nil
		}
		return "r.state", nil
	}
	if !tasks && key == "requester_id" {
		return "CAST(r.requester_id AS CHAR)", nil
	}
	if tasks && key == "group_id" {
		return "CAST(wa.assignment_group_id AS CHAR)", nil
	}
	if tasks && key == "assigned_to" {
		return "CAST(wa.assigned_to AS CHAR)", nil
	}
	return "", ErrInvalid
}
func listOrder(alias string, o ListOptions, before uint64) (string, []any, error) {
	cursor := ""
	args := []any{}
	column, op, order := alias+".id", "<", "DESC"
	switch o.Sort {
	case "oldest":
		op, order = ">", "ASC"
	case "number":
		column, op, order = alias+".number", ">", "ASC"
	case "updated":
		column = alias + ".updated_at"
	}
	if before > 0 {
		if o.Sort == "number" || o.Sort == "updated" {
			var value any = o.AfterValue
			if o.Sort == "updated" {
				stamp, err := time.Parse(time.RFC3339Nano, o.AfterValue)
				if err != nil {
					return "", nil, ErrInvalid
				}
				value = stamp.UTC()
			} else if len([]rune(o.AfterValue)) < 1 || len([]rune(o.AfterValue)) > 80 || strings.ContainsAny(o.AfterValue, "\r\n\x00") {
				return "", nil, ErrInvalid
			}
			cursor = " AND (" + column + op + "? OR (" + column + "=? AND " + alias + ".id<?))"
			args = append(args, value, value, before)
		} else {
			cursor = " AND " + column + op + "?"
			args = append(args, before)
		}
	}
	orderSQL := " ORDER BY " + column + " " + order
	if column != alias+".id" {
		orderSQL += "," + alias + ".id DESC"
	}
	return cursor + orderSQL, args, nil
}
func (s *Service) listStats(ctx context.Context, tasks bool, where string, args []any, group string) (uint64, []GroupCount, error) {
	joins := " FROM service_requests r"
	if tasks {
		joins = taskJoins
	}
	var total uint64
	groups := []GroupCount{}
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*)"+joins+where, args...).Scan(&total); err != nil {
		return 0, nil, err
	}
	if group == "" {
		return total, groups, nil
	}
	col, err := groupColumn(tasks, group)
	if err != nil {
		return 0, nil, err
	}
	rows, err := s.db.QueryContext(ctx, "SELECT COALESCE("+col+",''),COUNT(*)"+joins+where+" GROUP BY "+col+" ORDER BY "+col+" LIMIT 1001", args...)
	if err != nil {
		return 0, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var bucket GroupCount
		if err = rows.Scan(&bucket.Value, &bucket.Count); err != nil {
			return 0, nil, err
		}
		if len(groups) >= 1000 {
			return 0, nil, fmt.Errorf("%w: zbyt wiele grup, zawęź filtr", ErrInvalid)
		}
		groups = append(groups, bucket)
	}
	return total, groups, rows.Err()
}
func nextValue(number string, updated time.Time, sort string) string {
	if sort == "number" {
		return number
	}
	if sort == "updated" {
		return updated.UTC().Format(time.RFC3339Nano)
	}
	return ""
}
func parseListOptions(raw, group, sort, value string) (ListOptions, error) {
	node, err := recordlists.Decode(raw)
	if err != nil {
		return ListOptions{}, fmt.Errorf("%w: filtr", ErrInvalid)
	}
	return ListOptions{node, group, sort, value}, nil
}
