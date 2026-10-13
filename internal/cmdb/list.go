package cmdb

import (
	"context"
	"encoding/csv"
	"fmt"
	"github.com/chmajster/OpenServiceNOW/internal/assets"
	"github.com/chmajster/OpenServiceNOW/internal/platform/httpapi"
	"github.com/chmajster/OpenServiceNOW/internal/recordlists"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
	"net/http"
	"strings"
	"time"
)

const ciJoins = " FROM cmdb_cis c JOIN users u ON u.id=c.owner_id LEFT JOIN `groups` g ON g.tenant_id=c.tenant_id AND g.id=c.support_group_id"

var ListSorts = []string{"newest", "oldest", "title", "updated"}
var ListGroups = []string{"class", "status", "environment", "criticality", "lifecycle", "support_group_id"}
var ListColumns = []string{"number", "title", "class", "state", "environment", "owner", "group", "criticality", "lifecycle", "updated_at"}

type GroupCount struct {
	Value string `json:"value"`
	Count uint64 `json:"count,string"`
}

func ListSchema() recordlists.Schema {
	classes := []string{}
	for _, c := range Classes {
		classes = append(classes, c.Key)
	}
	return recordlists.Schema{
		"identifier": {SQL: "c.identifier", Kind: "text"}, "name": {SQL: "c.name", Kind: "text"}, "class": {SQL: "c.class", Kind: "enum", Values: classes},
		"status":      {SQL: "c.status", Kind: "enum", Values: []string{"PLANNED", "ACTIVE", "MAINTENANCE", "RETIRED"}},
		"environment": {SQL: "c.environment", Kind: "enum", Values: []string{"DEV", "TEST", "STAGING", "PROD"}},
		"owner_id":    {SQL: "c.owner_id", Kind: "id"}, "support_group_id": {SQL: "c.support_group_id", Kind: "id", Nullable: true}, "location": {SQL: "c.location", Kind: "text"},
		"criticality": {SQL: "c.criticality", Kind: "enum", Values: []string{"CRITICAL", "HIGH", "NORMAL", "LOW"}}, "lifecycle": {SQL: "c.lifecycle", Kind: "enum", Values: []string{"DESIGN", "BUILD", "OPERATE", "RETIRE"}},
		"archived": {Predicate: recordlists.BooleanPredicate("c.archived=TRUE")}, "created_at": {SQL: "c.created_at", Kind: "time"}, "updated_at": {SQL: "c.updated_at", Kind: "time"},
	}
}
func listWhere(a Actor, f Filter) (string, []any, error) {
	if !a.Has("cmdb.read") && !a.Has("cmdb.read_own") {
		return "", nil, workrecords.ErrForbidden
	}
	if !assets.Text(f.Query, 0, 160) || !assets.Text(f.Location, 0, 160) {
		return "", nil, ErrInvalid
	}
	schema := ListSchema()
	valid := func(value string, allowed []string) bool {
		if value == "" {
			return true
		}
		for _, v := range allowed {
			if value == v {
				return true
			}
		}
		return false
	}
	if !valid(f.Sort, ListSorts) || !valid(f.GroupBy, ListGroups) {
		return "", nil, ErrInvalid
	}
	p, args := Access(a, "c")
	where := " WHERE c.tenant_id=? AND " + p
	args = append([]any{a.TenantID}, args...)
	if f.Query != "" {
		v := "%" + strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(f.Query) + "%"
		where += " AND (c.name LIKE ? ESCAPE '!' OR c.identifier LIKE ? ESCAPE '!')"
		args = append(args, v, v)
	}
	for key, value := range map[string]string{"class": f.Class, "status": f.Status, "environment": f.Environment, "location": f.Location, "archived": f.Archived} {
		if value != "" {
			sql, values, err := recordlists.Compile(&recordlists.Node{Field: key, Operator: "eq", Value: value}, schema)
			if err != nil {
				return "", nil, ErrInvalid
			}
			where += " AND (" + sql + ")"
			args = append(args, values...)
		}
	}
	if f.OwnerID > 0 {
		where += " AND c.owner_id=?"
		args = append(args, f.OwnerID)
	}
	sql, values, err := recordlists.Compile(f.Expression, schema)
	if err != nil {
		return "", nil, ErrInvalid
	}
	if sql != "" {
		where += " AND (" + sql + ")"
		args = append(args, values...)
	}
	return where, args, nil
}
func ciOrder(f Filter) (string, string, []any, error) {
	col, op, dir := "c.id", "<", "DESC"
	switch f.Sort {
	case "oldest":
		op, dir = ">", "ASC"
	case "title":
		col, op, dir = "c.name", ">", "ASC"
	case "updated":
		col = "c.updated_at"
	}
	cursor := ""
	args := []any{}
	if f.Before > 0 {
		if col == "c.id" {
			cursor = " AND c.id" + op + "?"
			args = append(args, f.Before)
		} else {
			var value any = f.BeforeValue
			if f.Sort == "updated" {
				stamp, err := time.Parse(time.RFC3339Nano, f.BeforeValue)
				if err != nil {
					return "", "", nil, ErrInvalid
				}
				value = stamp.UTC()
			} else if !assets.Text(f.BeforeValue, 1, 160) {
				return "", "", nil, ErrInvalid
			}
			cursor = " AND (" + col + op + "? OR (" + col + "=? AND c.id<?))"
			args = append(args, value, value, f.Before)
		}
	}
	order := " ORDER BY " + col + " " + dir
	if col != "c.id" {
		order += ",c.id DESC"
	}
	return cursor, order, args, nil
}
func (s *Service) List(ctx context.Context, a Actor, f Filter) (Page, error) {
	out := Page{Items: []Record{}, Groups: []GroupCount{}}
	if f.Limit < 1 || f.Limit > 100 {
		return out, ErrInvalid
	}
	where, args, err := listWhere(a, f)
	if err != nil {
		return out, err
	}
	cursor, order, values, err := ciOrder(f)
	if err != nil {
		return out, err
	}
	params := append(append([]any{}, args...), values...)
	params = append(params, f.Limit+1)
	rows, err := s.DB.QueryContext(ctx, "SELECT "+columns+ciJoins+where+cursor+order+" LIMIT ?", params...)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		r, err := scan(rows)
		if err != nil {
			rows.Close()
			return out, err
		}
		out.Items = append(out.Items, r)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	if len(out.Items) > f.Limit {
		out.Items = out.Items[:f.Limit]
		last := out.Items[f.Limit-1]
		out.Next = last.ID
		if f.Sort == "title" {
			out.NextValue = last.Name
		}
		if f.Sort == "updated" {
			out.NextValue = last.UpdatedAt.UTC().Format(time.RFC3339Nano)
		}
	}
	if err = s.DB.QueryRowContext(ctx, "SELECT COUNT(*)"+ciJoins+where, args...).Scan(&out.Total); err != nil {
		return out, err
	}
	if f.GroupBy != "" {
		col := ListSchema()[f.GroupBy].SQL
		rows, err = s.DB.QueryContext(ctx, "SELECT COALESCE(CAST("+col+" AS CHAR),''),COUNT(*)"+ciJoins+where+" GROUP BY "+col+" ORDER BY "+col+" LIMIT 1001", args...)
		if err != nil {
			return out, err
		}
		for rows.Next() {
			var g GroupCount
			if err = rows.Scan(&g.Value, &g.Count); err != nil {
				rows.Close()
				return out, err
			}
			out.Groups = append(out.Groups, g)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return out, err
		}
		if len(out.Groups) > 1000 {
			return out, fmt.Errorf("%w: grupowanie przekracza 1000 grup; zawęź filtr", ErrInvalid)
		}
	}
	return out, nil
}
func (s *Service) Export(ctx context.Context, a Actor, f Filter) ([]Record, error) {
	where, args, err := listWhere(a, f)
	if err != nil {
		return nil, err
	}
	f.Before = 0
	_, order, _, err := ciOrder(f)
	if err != nil {
		return nil, err
	}
	exportColumns := strings.ReplaceAll(columns, "c.attributes,", "JSON_OBJECT(),")
	rows, err := s.DB.QueryContext(ctx, "SELECT "+exportColumns+ciJoins+where+order+" LIMIT 10001", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Record{}
	for rows.Next() {
		r, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
func (h *Handler) export(w http.ResponseWriter, r *http.Request, a Actor, f Filter) {
	items, err := h.service.Export(r.Context(), a, f)
	if err != nil {
		reply(w, r, nil, err)
		return
	}
	if len(items) > 10000 {
		httpapi.WriteError(w, r, 413, "EXPORT_LIMIT", "Zawęź filtry; limit eksportu to 10000 CI.")
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="cmdb.csv"`)
	writer := csv.NewWriter(w)
	_ = writer.Write([]string{"Id", "Identyfikator", "Nazwa", "Klasa", "Stan", "Środowisko", "Właściciel", "Grupa wsparcia", "Krytyczność", "Cykl życia"})
	for _, ci := range items {
		values := []string{"'" + fmt.Sprint(ci.ID), ci.Identifier, ci.Name, ci.Class, ci.Status, ci.Environment, ci.OwnerName, ci.SupportGroupName, ci.Criticality, ci.Lifecycle}
		for i, v := range values {
			values[i] = recordlists.CSVCell(v)
		}
		if writer.Write(values) != nil {
			return
		}
	}
	writer.Flush()
}
