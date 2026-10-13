package incidents

import (
	"context"
	"encoding/csv"
	"fmt"
	"github.com/chmajster/OpenServiceNOW/internal/platform/httpapi"
	"github.com/chmajster/OpenServiceNOW/internal/recordlists"
	"net/http"
	"strings"
)

func (s *Service) Export(ctx context.Context, a Actor, f Filter) ([]Summary, error) {
	if !a.Has("read") && !a.Has("read_own") {
		return nil, ErrForbidden
	}
	if err := validateFilter(f); err != nil {
		return nil, err
	}
	where, args := listWhere(a, f)
	columns := strings.ReplaceAll(strings.ReplaceAll(recordColumns, "i.description,", "'',"), "i.resolution_description,", "'',")
	order := " ORDER BY i.id DESC"
	switch f.Sort {
	case "title":
		order = " ORDER BY i.title ASC,i.id DESC"
	case "state":
		order = " ORDER BY i.state ASC,i.id DESC"
	case "updated":
		order = " ORDER BY i.updated_at DESC,i.id DESC"
	case "oldest":
		order = " ORDER BY i.id ASC"
	case "priority":
		order = " ORDER BY i.priority ASC,i.id DESC"
	}
	rows, err := s.repo.db.QueryContext(ctx, "SELECT "+columns+recordJoins+where+order+" LIMIT 10001", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Summary{}
	for rows.Next() {
		r, err := scanRecord(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, summary(r))
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
func writeCSV(w http.ResponseWriter, r *http.Request, items []Summary, err error) {
	if err != nil {
		writeError(w, r, err)
		return
	}
	if len(items) > 10000 {
		httpapi.WriteError(w, r, 413, "EXPORT_LIMIT", "Eksport obejmuje najwyżej 10000 rekordów. Zawęź filtry.")
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="incidents.csv"`)
	w.Header().Set("Cache-Control", "no-store")
	writer := csv.NewWriter(w)
	_ = writer.Write([]string{"Id", "Numer", "Tytuł", "Stan", "Priorytet", "Zgłaszający", "Grupa", "Osoba przypisana", "Aktualizacja"})
	for _, i := range items {
		values := []string{"'" + fmt.Sprint(i.ID), i.Number, i.Title, i.State, fmt.Sprint(i.Priority), i.ReporterName, i.GroupName, i.AssignedName, i.UpdatedAt.UTC().Format("2006-01-02T15:04:05.000000Z")}
		for k, v := range values {
			values[k] = recordlists.CSVCell(v)
		}
		if err = writer.Write(values); err != nil {
			return
		}
	}
	writer.Flush()
}
