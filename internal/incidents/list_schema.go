package incidents

import (
	"fmt"
	"github.com/chmajster/OpenServiceNOW/internal/recordlists"
)

const slaRiskSQL = `EXISTS(SELECT 1 FROM sla_instances si WHERE si.tenant_id=i.tenant_id AND si.incident_id=i.id AND JSON_UNQUOTE(JSON_EXTRACT(si.state,'$.status')) IN ('ACTIVE','PAUSED') AND (JSON_TYPE(JSON_EXTRACT(si.state,'$.warned_at'))<>'NULL' OR JSON_TYPE(JSON_EXTRACT(si.state,'$.breached_at'))<>'NULL'))`

func ListSchema(userID uint64) recordlists.Schema {
	states := []string{}
	for _, v := range States {
		states = append(states, v.Value)
	}
	categories := []string{}
	for _, v := range Categories {
		categories = append(categories, v.Value)
	}
	return recordlists.Schema{
		"number": {SQL: "i.number", Kind: "text"}, "title": {SQL: "i.title", Kind: "text"},
		"state": {SQL: "i.state", Kind: "enum", Values: states}, "category": {SQL: "i.category", Kind: "enum", Values: categories}, "priority": {SQL: "i.priority", Kind: "number"},
		"reporter_id": {SQL: "i.reporter_id", Kind: "id"}, "assigned_to": {SQL: "w.assigned_to", Kind: "id", Nullable: true}, "group_id": {SQL: "w.assignment_group_id", Kind: "id", Nullable: true},
		"created_at": {SQL: "i.created_at", Kind: "time"}, "updated_at": {SQL: "i.updated_at", Kind: "time"},
		"active":   {Predicate: recordlists.BooleanPredicate("i.state NOT IN ('RESOLVED','CLOSED','CANCELLED')")},
		"sla_risk": {Predicate: recordlists.BooleanPredicate(slaRiskSQL)},
		"my_groups": {Predicate: func(v string) (string, []any, error) {
			if v != "true" && v != "false" {
				return "", nil, recordlists.ErrInvalid
			}
			prefix := ""
			if v == "false" {
				prefix = "NOT "
			}
			return prefix + "(g.active=TRUE AND EXISTS(SELECT 1 FROM group_memberships gm WHERE gm.tenant_id=i.tenant_id AND gm.group_id=w.assignment_group_id AND gm.user_id=?))", []any{userID}, nil
		}},
		"assigned_to_me": {Predicate: func(v string) (string, []any, error) {
			if v != "true" && v != "false" {
				return "", nil, recordlists.ErrInvalid
			}
			sql := "w.assigned_to=?"
			if v == "false" {
				sql = "(w.assigned_to IS NULL OR w.assigned_to<>?)"
			}
			return sql, []any{userID}, nil
		}},
	}
}

var ListSorts = []string{"newest", "oldest", "priority", "title", "state", "updated"}
var ListColumns = []string{"number", "title", "state", "priority", "reporter_name", "group_name", "assigned_name", "updated_at", "sla"}
var ListGroups = []string{"state", "priority", "category", "group_id", "assigned_to"}

type GroupCount struct {
	Value string `json:"value"`
	Count uint64 `json:"count,string"`
}

func groupColumn(key string) (string, error) {
	switch key {
	case "state":
		return "i.state", nil
	case "priority":
		return "CAST(i.priority AS CHAR)", nil
	case "category":
		return "i.category", nil
	case "group_id":
		return "CAST(w.assignment_group_id AS CHAR)", nil
	case "assigned_to":
		return "CAST(w.assigned_to AS CHAR)", nil
	}
	return "", fmt.Errorf("%w: grupowanie", ErrInvalid)
}
