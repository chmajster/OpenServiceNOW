package requests

import (
	"context"
	"encoding/csv"
	"fmt"
	"github.com/chmajster/OpenServiceNOW/internal/platform/httpapi"
	"github.com/chmajster/OpenServiceNOW/internal/recordlists"
	"net/http"
	"time"
)

type ExportRow struct {
	ID                                    uint64
	Number, Title, State, Group, Assigned string
	Updated                               time.Time
}

func (s *Service) Export(ctx context.Context, a Actor, tasks bool, q, state, view string, active bool, o ListOptions) ([]ExportRow, error) {
	where, args, err := listWhere(a, tasks, q, state, view, active)
	if err != nil {
		return nil, err
	}
	where, args, err = listScope(a, tasks, where, args, o)
	if err != nil {
		return nil, err
	}
	alias, columns, joins := "r", "r.id,r.number,u.display_name,r.state,'','',r.updated_at", " FROM service_requests r JOIN users u ON u.id=r.requester_id"
	if tasks {
		alias = "t"
		columns = "t.id,t.number,t.title,t.state,COALESCE(g.name,''),COALESCE(u.display_name,''),t.updated_at"
		joins = taskJoins
	}
	order, _, err := listOrder(alias, o, 0)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, "SELECT "+columns+joins+where+order+" LIMIT 10001", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ExportRow{}
	for rows.Next() {
		var row ExportRow
		if err = rows.Scan(&row.ID, &row.Number, &row.Title, &row.State, &row.Group, &row.Assigned, &row.Updated); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}
func (h *Handler) export(w http.ResponseWriter, r *http.Request, view string, o ListOptions) {
	rows, err := h.service.Export(r.Context(), actor(r), isTask(r), r.URL.Query().Get("q"), r.URL.Query().Get("state"), view, r.URL.Query().Get("active") == "true", o)
	if err != nil {
		writeError(w, r, err)
		return
	}
	if len(rows) > 10000 {
		httpapi.WriteError(w, r, 413, "EXPORT_LIMIT", "Eksport obejmuje najwyżej 10000 rekordów. Zawęź filtry.")
		return
	}
	name := "requests.csv"
	if isTask(r) {
		name = "fulfillment.csv"
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("Cache-Control", "no-store")
	writer := csv.NewWriter(w)
	_ = writer.Write([]string{"Id", "Numer", "Opis lub zgłaszający", "Stan", "Grupa", "Osoba przypisana", "Aktualizacja"})
	for _, row := range rows {
		values := []string{"'" + fmt.Sprint(row.ID), row.Number, row.Title, row.State, row.Group, row.Assigned, row.Updated.UTC().Format(time.RFC3339Nano)}
		for i, v := range values {
			values[i] = recordlists.CSVCell(v)
		}
		if writer.Write(values) != nil {
			return
		}
	}
	writer.Flush()
}
