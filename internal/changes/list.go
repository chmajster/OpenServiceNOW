package changes

import (
	"context"
	"encoding/csv"
	"fmt"
	"github.com/chmajster/OpenServiceNOW/internal/catalog"
	"github.com/chmajster/OpenServiceNOW/internal/platform/httpapi"
	"github.com/chmajster/OpenServiceNOW/internal/recordlists"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
	"net/http"
	"time"
)

var ListSorts = []string{"newest", "oldest", "title", "updated"}
var ListGroups = []string{"state", "impact", "assignment_group_id", "risk", "change_type"}
var ListColumns = []string{"number", "title", "state", "impact", "reporter", "assigned", "group", "risk", "change_type", "planned_start", "updated_at"}

type GroupCount struct {
	Value string `json:"value"`
	Count uint64 `json:"count,string"`
}
type Filter struct {
	Query, State, Impact, Active, Risk, Type, Sort, GroupBy, BeforeValue string
	Before, ProblemID                                                    uint64
	Limit                                                                int
	Expression                                                           *recordlists.Node
}
type Page struct {
	Items     []Record     `json:"items"`
	Next      uint64       `json:"next_before_id,string,omitempty"`
	NextValue string       `json:"next_before_value,omitempty"`
	Total     uint64       `json:"total,string"`
	Groups    []GroupCount `json:"groups"`
}

func ListSchema() recordlists.Schema {
	return recordlists.Schema{
		"number": {SQL: "p.number", Kind: "text"}, "title": {SQL: "p.title", Kind: "text"}, "state": {SQL: "p.state", Kind: "enum", Values: []string{"NEW", "ASSESS", "AUTHORIZE", "SCHEDULED", "IMPLEMENT", "REVIEW", "CLOSED", "CANCELLED"}}, "impact": {SQL: "p.impact", Kind: "enum", Values: []string{"HIGH", "MEDIUM", "LOW"}}, "reporter_id": {SQL: "p.reporter_id", Kind: "id"}, "assigned_to": {SQL: "wa.assigned_to", Kind: "id", Nullable: true}, "assignment_group_id": {SQL: "wa.assignment_group_id", Kind: "id", Nullable: true}, "risk": {SQL: "p.risk", Kind: "enum", Values: []string{"HIGH", "MEDIUM", "LOW"}}, "change_type": {SQL: "p.change_type", Kind: "enum", Values: []string{"NORMAL", "STANDARD", "EMERGENCY"}}, "planned_start": {SQL: "p.planned_start", Kind: "time", Nullable: true}, "active": {Predicate: recordlists.BooleanPredicate("p.state NOT IN('CLOSED','CANCELLED')")}, "created_at": {SQL: "p.created_at", Kind: "time"}, "updated_at": {SQL: "p.updated_at", Kind: "time"},
	}
}
func listWhere(a Actor, f Filter) (string, []any, error) {
	if !a.Has("change.read") && !a.Has("change.read_own") {
		return "", nil, workrecords.ErrForbidden
	}
	if !catalog.Text(f.Query, 0, 160) || f.Sort != "" && !contains(ListSorts, f.Sort) || f.GroupBy != "" && !contains(ListGroups, f.GroupBy) {
		return "", nil, ErrInvalid
	}
	p, args := Access(a, "p")
	where := " WHERE p.tenant_id=? AND " + p
	args = append([]any{a.TenantID}, args...)
	if f.Query != "" {
		where += " AND (LOCATE(?,p.title)>0 OR LOCATE(?,p.number)>0)"
		args = append(args, f.Query, f.Query)
	}
	for key, value := range map[string]string{"state": f.State, "impact": f.Impact, "risk": f.Risk, "change_type": f.Type, "active": f.Active} {
		if value != "" {
			sql, values, err := recordlists.Compile(&recordlists.Node{Field: key, Operator: "eq", Value: value}, ListSchema())
			if err != nil {
				return "", nil, err
			}
			where += " AND (" + sql + ")"
			args = append(args, values...)
		}
	}
	if f.ProblemID > 0 {
		where += " AND EXISTS(SELECT 1 FROM change_problem_links l WHERE l.tenant_id=p.tenant_id AND l.change_id=p.id AND l.problem_id=?)"
		args = append(args, f.ProblemID)
	}
	sql, values, err := recordlists.Compile(f.Expression, ListSchema())
	if err != nil {
		return "", nil, err
	}
	if sql != "" {
		where += " AND (" + sql + ")"
		args = append(args, values...)
	}
	return where, args, nil
}
func listOrder(f Filter) (string, string, []any, error) {
	col, op, dir := "p.id", "<", "DESC"
	switch f.Sort {
	case "oldest":
		op, dir = ">", "ASC"
	case "title":
		col, op, dir = "p.title", ">", "ASC"
	case "updated":
		col = "p.updated_at"
	}
	cursor := ""
	args := []any{}
	if f.Before > 0 {
		if col == "p.id" {
			cursor = " AND p.id" + op + "?"
			args = append(args, f.Before)
		} else {
			var value any = f.BeforeValue
			if f.Sort == "updated" {
				stamp, err := time.Parse(time.RFC3339Nano, f.BeforeValue)
				if err != nil {
					return "", "", nil, ErrInvalid
				}
				value = stamp.UTC()
			} else if !catalog.Text(f.BeforeValue, 1, 160) {
				return "", "", nil, ErrInvalid
			}
			cursor = " AND (" + col + op + "? OR (" + col + "=? AND p.id<?))"
			args = append(args, value, value, f.Before)
		}
	}
	order := " ORDER BY " + col + " " + dir
	if col != "p.id" {
		order += ",p.id DESC"
	}
	return cursor, order, args, nil
}
func (s *Service) List(ctx context.Context, a Actor, f Filter) (Page, error) {
	out := Page{Items: []Record{}, Groups: []GroupCount{}}
	if f.Limit < 1 || f.Limit > 100 {
		return out, ErrInvalid
	}
	if f.ProblemID > 0 {
		if _, err := s.SourceProblem(ctx, a, f.ProblemID); err != nil {
			return out, err
		}
	}
	where, args, err := listWhere(a, f)
	if err != nil {
		return out, err
	}
	cursor, order, values, err := listOrder(f)
	if err != nil {
		return out, err
	}
	params := append(append([]any{}, args...), values...)
	params = append(params, f.Limit+1)
	rows, err := s.db.QueryContext(ctx, "SELECT "+listColumns+joins+where+cursor+order+" LIMIT ?", params...)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		r, err := scan(rows, a)
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
			out.NextValue = last.Title
		}
		if f.Sort == "updated" {
			out.NextValue = last.UpdatedAt.UTC().Format(time.RFC3339Nano)
		}
	}
	if err = s.db.QueryRowContext(ctx, "SELECT COUNT(*)"+joins+where, args...).Scan(&out.Total); err != nil {
		return out, err
	}
	if f.GroupBy != "" {
		col := ListSchema()[f.GroupBy].SQL
		if f.GroupBy == "risk" {
			col = "p.risk"
		}
		rows, err = s.db.QueryContext(ctx, "SELECT COALESCE(CAST("+col+" AS CHAR),''),COUNT(*)"+joins+where+" GROUP BY "+col+" ORDER BY "+col+" LIMIT 1001", args...)
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
	if f.ProblemID > 0 {
		if _, err := s.SourceProblem(ctx, a, f.ProblemID); err != nil {
			return nil, err
		}
	}
	where, args, err := listWhere(a, f)
	if err != nil {
		return nil, err
	}
	f.Before = 0
	_, order, _, err := listOrder(f)
	if err != nil {
		return nil, err
	}
	exportColumns := listColumns
	rows, err := s.db.QueryContext(ctx, "SELECT "+exportColumns+joins+where+order+" LIMIT 10001", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Record{}
	for rows.Next() {
		r, err := scan(rows, a)
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
		writeError(w, r, err)
		return
	}
	if len(items) > 10000 {
		httpapi.WriteError(w, r, 413, "EXPORT_LIMIT", "Zawęź filtry; limit eksportu to 10000 zmian.")
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="changes.csv"`)
	writer := csv.NewWriter(w)
	_ = writer.Write([]string{"Id", "Numer", "Opis skrócony", "Stan", "Wpływ", "Zgłaszający", "Przypisany", "Grupa", "Ryzyko"})
	for _, p := range items {
		values := []string{"'" + fmt.Sprint(p.ID), p.Number, p.Title, p.State, p.Impact, p.ReporterName, p.AssignedName, p.GroupName, fmt.Sprint(p.Risk)}
		for i, v := range values {
			values[i] = recordlists.CSVCell(v)
		}
		if writer.Write(values) != nil {
			return
		}
	}

	writer.Flush()
}

// List and export do not load analytical narratives or communication content.
const listColumns = "p.id,p.number,p.reporter_id,u.display_name,p.title,'',p.change_type,p.impact,p.risk,'','','','','','','',p.review_outcome,p.planned_start,p.planned_end,p.time_zone,p.workflow_id,p.workflow_version,p.template_id,p.template_version,p.assessment_revision,p.approved_revision,p.evaluated_version,p.execution_id,p.approval_state,p.actual_start,p.actual_end,p.state,'',p.version,p.created_at,p.updated_at,wa.assigned_to,wa.assignment_group_id,COALESCE(au.display_name,''),COALESCE(g.name,'')"
