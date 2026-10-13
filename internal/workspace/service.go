package workspace

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/chmajster/OpenServiceNOW/internal/incidents"
	"github.com/chmajster/OpenServiceNOW/internal/requests"
	"github.com/chmajster/OpenServiceNOW/internal/workflow"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
)

var ErrInvalid = errors.New("nieprawidłowe dane workspace")

type Metric struct {
	Key        string `json:"key"`
	Label      string `json:"label"`
	Count      uint64 `json:"count,string"`
	URL        string `json:"url"`
	Definition string `json:"definition"`
}
type Summary struct {
	Metrics      []Metric  `json:"metrics"`
	CalculatedAt time.Time `json:"calculated_at"`
}
type Service struct {
	DB        *sql.DB
	Incidents *incidents.Service
	Requests  *requests.Service
	Workflow  *workflow.Service
}

func incidentActor(a workrecords.Principal) incidents.Actor {
	return incidents.Actor{TenantID: a.TenantID, UserID: a.UserID, Permissions: a.Permissions}
}

func (s *Service) Summary(ctx context.Context, a workrecords.Principal) (Summary, error) {
	out := Summary{Metrics: []Metric{}, CalculatedAt: time.Now().UTC()}
	add := func(key, label, url, definition string, count uint64) {
		out.Metrics = append(out.Metrics, Metric{key, label, count, url, definition})
	}
	if a.Has("incident.read") || a.Has("incident.read_own") {
		for _, v := range []struct {
			key, label, view, definition string
			risk                         bool
		}{
			{"my_incidents", "Moje aktywne incydenty", "assigned", "Przypisane do mnie; bez rozwiązanych, zamkniętych i anulowanych.", false},
			{"group_incidents", "Incydenty moich grup", "my_group", "Przypisane do aktywnych grup, do których należę; wyłącznie aktywne.", false},
			{"sla_risk", "Zagrożone SLA incydentów", "all", "Aktywne incydenty z ostrzeżeniem lub przekroczeniem aktywnego/wstrzymanego SLA. Stan ostatnio zapisany przez worker; obejmuje cały dostępny zbiór.", true},
		} {
			f := incidents.Filter{View: v.view, Sort: "newest", Limit: 25, Active: true, SLARisk: v.risk}
			n, err := s.Incidents.Count(ctx, incidentActor(a), f)
			if err != nil {
				return Summary{}, err
			}
			url := "/incidents?active=true&view=" + v.view
			if v.risk {
				url += "&sla_risk=true"
			}
			add(v.key, v.label, url, v.definition, n)
		}
	}
	if a.Has("request.fulfill") || a.Has("request.manage") {
		for _, v := range []struct{ key, label, view, definition string }{
			{"my_tasks", "Moje aktywne zadania wniosków", "mine", "Otwarte, realizowane i wstrzymane zadania przypisane do mnie w aktywnych wnioskach."},
			{"group_tasks", "Zadania wniosków moich grup", "my_group", "Dostępne zadania aktywnych wniosków w aktywnych grupach, do których należę."},
			{"unassigned_tasks", "Nieprzypisane zadania wniosków", "unassigned", "Dostępne aktywne zadania bez wykonawcy w aktywnych wnioskach."},
		} {
			n, err := s.Requests.Count(ctx, a, true, v.view, true)
			if err != nil {
				return Summary{}, err
			}
			add(v.key, v.label, "/fulfillment?active=true&view="+v.view, v.definition, n)
		}
	}
	if a.Has("request.read_own") {
		n, err := s.Requests.Count(ctx, a, false, "mine", true)
		if err != nil {
			return Summary{}, err
		}
		add("my_requests", "Moje aktywne wnioski", "/requests?active=true&view=mine", "Moje wnioski w stanie Złożony lub W realizacji.", n)
	}
	if s.DB != nil {
		scopes, err := s.scopes(a)
		if err != nil {
			return Summary{}, err
		}
		for _, domain := range []struct{ kind, root, label string }{{"problem", "problems", "problemy"}, {"change", "changes", "zmiany"}} {
			scope, ok := scopes[domain.kind]
			if !ok {
				continue
			}
			for _, view := range []struct{ key, label, description string }{{"mine", "Moje aktywne ", "Aktywne dostępne rekordy przypisane do mnie."}, {"my_group", "Aktywne ", "Aktywne dostępne rekordy przypisane do aktualnie aktywnych grup, do których należę."}} {
				filtered, err := workrecords.AssignmentView(scope.Restricted(scope.Active), a, view.key)
				if err != nil {
					return Summary{}, err
				}
				n, err := countScope(ctx, s.DB, filtered)
				if err != nil {
					return Summary{}, err
				}
				label := view.label + domain.label
				if view.key == "my_group" {
					label += " moich grup"
				}
				add(view.key+"_"+domain.root, label, "/"+domain.root+"?active=true&view="+view.key, view.description, n)
			}
		}
	}
	if a.Has("workflow.approve") {
		n, err := s.Workflow.PendingCount(ctx, a)
		if err != nil {
			return Summary{}, err
		}
		add("approvals", "Oczekujące akceptacje", "/approvals", "Decyzje oczekujące na mnie w bieżącym kroku przed terminem, z aktualnym członkostwem i uprawnieniem.", n)
	}
	return out, nil
}

func ValidateQuery(q string) error {
	if !utf8.ValidString(q) || utf8.RuneCountInString(q) < 2 || utf8.RuneCountInString(q) > 200 {
		return fmt.Errorf("%w: wpisz od 2 do 200 znaków, bez znaków sterujących", ErrInvalid)
	}
	for _, c := range q {
		if unicode.IsControl(c) {
			return fmt.Errorf("%w: wpisz od 2 do 200 znaków, bez znaków sterujących", ErrInvalid)
		}
	}
	return nil
}
func (s *Service) Search(ctx context.Context, a workrecords.Principal, q string) ([]workrecords.Reference, error) {
	q = strings.TrimSpace(q)
	if err := ValidateQuery(q); err != nil {
		return nil, err
	}
	if s.DB != nil {
		scopes, err := s.scopes(a)
		if err != nil {
			return nil, err
		}
		out := []workrecords.Reference{}
		for _, kind := range referenceKinds {
			scope, ok := scopes[kind]
			if !ok {
				continue
			}
			items, err := workrecords.References(ctx, s.DB, scope, workrecords.ReferenceSelection{Query: q, Limit: 10})
			if err != nil {
				return nil, err
			}
			out = append(out, items...)
		}
		sort.SliceStable(out, func(i, j int) bool { return out[i].Exact && !out[j].Exact })
		return out, nil
	}
	out := []workrecords.Reference{}
	if a.Has("incident.read") || a.Has("incident.read_own") {
		r, err := s.Incidents.Search(ctx, incidentActor(a), q)
		if err != nil {
			return nil, err
		}
		out = append(out, r...)
	}
	if a.Has("request.read_own") || a.Has("request.fulfill") || a.Has("request.manage") {
		r, err := s.Requests.Search(ctx, a, q, false)
		if err != nil {
			return nil, err
		}
		out = append(out, r...)
	}
	if a.Has("request.fulfill") || a.Has("request.manage") {
		r, err := s.Requests.Search(ctx, a, q, true)
		if err != nil {
			return nil, err
		}
		out = append(out, r...)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Exact && !out[j].Exact })
	return out, nil
}
