package workspace

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
	"net/url"
	"strconv"
	"time"
)

type ReportPeriod struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}
type ReportBucket struct {
	Value  string                 `json:"value"`
	Label  string                 `json:"label"`
	Count  uint64                 `json:"count,string"`
	URL    string                 `json:"url"`
	Record *workrecords.Reference `json:"record,omitempty"`
}
type Report struct {
	Key        string         `json:"key"`
	Domain     string         `json:"domain"`
	Label      string         `json:"label"`
	Definition string         `json:"definition"`
	TimeScope  string         `json:"time_scope"`
	Eligible   uint64         `json:"eligible,string"`
	Total      uint64         `json:"total,string"`
	Items      []ReportBucket `json:"items"`
}
type Reports struct {
	Items        []Report     `json:"items"`
	Period       ReportPeriod `json:"period"`
	CalculatedAt time.Time    `json:"calculated_at"`
}

func ValidatePeriod(p ReportPeriod) error {
	if p.From.IsZero() || p.To.IsZero() || !p.To.After(p.From) || p.To.Sub(p.From) > 366*24*time.Hour {
		return fmt.Errorf("%w: podaj przedział od/do o długości do 366 dni", ErrInvalid)
	}
	return nil
}

var reportDomains = []string{"incident", "request", "problem", "change"}
var reportStates = map[string][]string{
	"incident": {"NEW", "IN_PROGRESS", "ON_HOLD", "RESOLVED", "CLOSED", "CANCELLED"},
	"request":  {"SUBMITTED", "IN_PROGRESS", "FULFILLED", "CLOSED", "CANCELLED"},
	"problem":  {"NEW", "ASSESS", "ROOT_CAUSE", "FIX_IN_PROGRESS", "RESOLVED", "CLOSED", "CANCELLED"},
	"change":   {"NEW", "ASSESS", "AUTHORIZE", "SCHEDULED", "IMPLEMENT", "REVIEW", "CLOSED", "CANCELLED"},
}

func reportURL(key, domain, bucket string, p ReportPeriod, asOf time.Time, problemID uint64) string {
	q := url.Values{"report": {key}, "domain": {domain}, "from": {p.From.UTC().Format(time.RFC3339Nano)}, "to": {p.To.UTC().Format(time.RFC3339Nano)}, "as_of": {asOf.UTC().Format(time.RFC3339Nano)}}
	if bucket != "" {
		q.Set("bucket", bucket)
	}
	if problemID > 0 {
		q.Set("problem_id", fmt.Sprint(problemID))
	}
	return "/reports/sources?" + q.Encode()
}
func sumBuckets(items []ReportBucket) uint64 {
	var total uint64
	for _, b := range items {
		total += b.Count
	}
	return total
}
func groupReport(ctx context.Context, e database.Executor, s workrecords.ReferenceScope, value, label string) ([]ReportBucket, error) {
	rows, err := e.QueryContext(ctx, "SELECT "+value+","+label+",COUNT(*)"+s.From+s.Where+" GROUP BY "+value+","+label+" ORDER BY "+value+" LIMIT 1001", s.Args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ReportBucket{}
	for rows.Next() {
		var b ReportBucket
		if err = rows.Scan(&b.Value, &b.Label, &b.Count); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	if len(out) > 1000 {
		return nil, fmt.Errorf("%w: raport przekracza 1000 grup; użyj listy z węższym filtrem", ErrInvalid)
	}
	return out, rows.Err()
}
func countScope(ctx context.Context, e database.Executor, s workrecords.ReferenceScope) (uint64, error) {
	var n uint64
	err := e.QueryRowContext(ctx, "SELECT COUNT(*)"+s.From+s.Where, s.Args...).Scan(&n)
	return n, err
}

const slaRiskSQL = `EXISTS(SELECT 1 FROM sla_instances si WHERE si.tenant_id=i.tenant_id AND si.incident_id=i.id AND JSON_UNQUOTE(JSON_EXTRACT(si.state,'$.status')) IN ('ACTIVE','PAUSED') AND (JSON_EXTRACT(si.state,'$.warned_at') IS NOT NULL AND JSON_TYPE(JSON_EXTRACT(si.state,'$.warned_at'))<>'NULL' OR JSON_EXTRACT(si.state,'$.breached_at') IS NOT NULL AND JSON_TYPE(JSON_EXTRACT(si.state,'$.breached_at'))<>'NULL'))`

func (s *Service) Reports(ctx context.Context, a workrecords.Principal, p ReportPeriod) (Reports, error) {
	out := Reports{Items: []Report{}, Period: p, CalculatedAt: time.Now().UTC()}
	if err := ValidatePeriod(p); err != nil {
		return out, err
	}
	scopes, err := s.scopes(a)
	if err != nil {
		return out, err
	}
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	for _, kind := range reportDomains {
		scope, ok := scopes[kind]
		if !ok {
			continue
		}
		active := scope.Restricted(scope.Active)
		states, err := groupReport(ctx, tx, active, scope.State, scope.State)
		if err != nil {
			return out, err
		}
		for i := range states {
			states[i].URL = reportURL("active_state", kind, states[i].Value, p, out.CalculatedAt, 0)
		}
		n := sumBuckets(states)
		out.Items = append(out.Items, Report{Key: "active_state", Domain: kind, Label: domainLabel(kind) + " — aktywne według stanu", Definition: "Dostępne rekordy w aktywnych stanach swojej domeny. Odczyt całego zbioru przed grupowaniem, bez ograniczenia daty utworzenia.", TimeScope: "snapshot", Eligible: n, Total: n, Items: states})
		groupID := "COALESCE(CAST(" + scope.GroupID + " AS CHAR),'')"
		groupName := scope.GroupName
		groups, err := groupReport(ctx, tx, active, groupID, groupName)
		if err != nil {
			return out, err
		}
		for i := range groups {
			if groups[i].Label == "" {
				groups[i].Label = "Bez grupy"
			}
			value := groups[i].Value
			if value == "" {
				value = "unassigned"
			}
			groups[i].URL = reportURL("active_group", kind, value, p, out.CalculatedAt, 0)
		}
		definition := "Dostępne aktywne rekordy według bieżącej grupy przypisania; brak grupy stanowi osobny koszyk. Pełny dostępny zbiór, niezależnie od daty utworzenia."
		if kind == "request" {
			definition = "Aktywne wnioski nie mają własnej grupy przypisania; realizacja jest przypisana na zadaniach SCTASK. Wnioski należą do koszyka Bez grupy."
		}
		out.Items = append(out.Items, Report{Key: "active_group", Domain: kind, Label: domainLabel(kind) + " — aktywne według grupy", Definition: definition, TimeScope: "snapshot", Eligible: n, Total: sumBuckets(groups), Items: groups})
		ageExpr := "CASE WHEN " + scope.CreatedAt + ">? THEN '0_7' WHEN " + scope.CreatedAt + ">? THEN '7_30' ELSE '30_plus' END"
		// Placeholders in SELECT precede the ACL parameters in WHERE.
		rows, err := tx.QueryContext(ctx, "SELECT "+ageExpr+",COUNT(*)"+active.From+active.Where+" GROUP BY 1 ORDER BY 1", append([]any{out.CalculatedAt.Add(-7 * 24 * time.Hour), out.CalculatedAt.Add(-30 * 24 * time.Hour)}, active.Args...)...)
		if err != nil {
			return out, err
		}
		ages := map[string]uint64{}
		for rows.Next() {
			var key string
			var count uint64
			if err = rows.Scan(&key, &count); err != nil {
				rows.Close()
				return out, err
			}
			ages[key] = count
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return out, err
		}
		buckets := []ReportBucket{}
		for _, v := range []struct{ key, label string }{{"0_7", "Mniej niż 7 dni"}, {"7_30", "Od 7 do mniej niż 30 dni"}, {"30_plus", "Co najmniej 30 dni"}} {
			buckets = append(buckets, ReportBucket{Value: v.key, Label: v.label, Count: ages[v.key], URL: reportURL("backlog_age", kind, v.key, p, out.CalculatedAt, 0)})
		}
		out.Items = append(out.Items, Report{Key: "backlog_age", Domain: kind, Label: domainLabel(kind) + " — wiek backlogu", Definition: "Czas od utworzenia do chwili obliczenia raportu, w pełnych przedziałach po 24 godziny UTC. Wyłącznie aktualnie aktywne dostępne rekordy; granice 7 i 30 dni są rozłączne.", TimeScope: "snapshot", Eligible: n, Total: sumBuckets(buckets), Items: buckets})
		if kind == "incident" {
			risk, err := countScope(ctx, tx, active.Restricted(slaRiskSQL))
			if err != nil {
				return out, err
			}
			out.Items = append(out.Items, Report{Key: "sla_risk", Domain: kind, Label: "Incydenty — zagrożone SLA", Definition: "Aktywne dostępne incydenty z ostrzeżeniem lub przekroczeniem SLA w stanie ACTIVE albo PAUSED, według ostatniego trwałego obliczenia workera. Jeden incydent liczony raz, niezależnie od liczby SLA. Pozostałe domeny nie mają wskaźnika SLA.", TimeScope: "snapshot", Eligible: n, Total: risk, Items: []ReportBucket{{Value: "risk", Label: "Ostrzeżenie lub przekroczenie", Count: risk, URL: reportURL("sla_risk", kind, "", p, out.CalculatedAt, 0)}}})
		}
	}
	if ch, ok := scopes["change"]; ok {
		for _, key := range []string{"planned_changes", "closed_changes"} {
			filtered, err := reportScope(scopes, ReportSourceFilter{Report: key, Domain: "change", Period: p, AsOf: out.CalculatedAt})
			if err != nil {
				return out, err
			}
			items, err := groupReport(ctx, tx, filtered, ch.State, ch.State)
			if err != nil {
				return out, err
			}
			for i := range items {
				items[i].URL = reportURL(key, "change", items[i].Value, p, out.CalculatedAt, 0)
			}
			label, definition := "Zmiany zaplanowane", "Dostępne zmiany zaplanowane, wdrażane albo w przeglądzie, których planowany przedział nakłada się na wybrany przedział [od, do). Są to rzeczywiste zarezerwowane terminy; szkice i samo oczekiwanie na akceptację nie są liczone."
			if key == "closed_changes" {
				label = "Zmiany zakończone"
				definition = "Dostępne zamknięte zmiany z rzeczywistym końcem wykonania w wybranym przedziale [od, do). Anulowane oraz niezamknięte zmiany nie są liczone."
			}
			n := sumBuckets(items)
			out.Items = append(out.Items, Report{Key: key, Domain: "change", Label: label, Definition: definition, TimeScope: "range", Eligible: n, Total: n, Items: items})
		}
	}
	if pr, ok := scopes["problem"]; ok {
		if inc, ok := scopes["incident"]; ok {
			inc = inc.Restricted("i.created_at>=? AND i.created_at<?", p.From.UTC(), p.To.UTC())
			inc = inc.Restricted("EXISTS(SELECT 1 FROM problem_incident_links l WHERE l.tenant_id=i.tenant_id AND l.incident_id=i.id AND l.problem_id=p.id)")
			count := "(SELECT COUNT(*)" + inc.From + inc.Where + ")"
			args := append(append([]any{}, inc.Args...), pr.Args...)
			rows, err := tx.QueryContext(ctx, "SELECT p.id,p.number,p.title,p.state,p.version,"+count+" AS incident_count"+pr.From+pr.Where+" HAVING incident_count>0 ORDER BY incident_count DESC,p.id DESC LIMIT 20", args...)
			if err != nil {
				return out, err
			}
			items := []ReportBucket{}
			for rows.Next() {
				var r workrecords.Reference
				var n uint64
				if err = rows.Scan(&r.ID, &r.Number, &r.Title, &r.State, &r.Version, &n); err != nil {
					rows.Close()
					return out, err
				}
				r.Type = "problem"
				r.URL = fmt.Sprintf("/problems/%d", r.ID)
				items = append(items, ReportBucket{Value: fmt.Sprint(r.ID), Label: r.Number + " · " + r.Title, Count: n, Record: &r, URL: reportURL("problem_incidents", "incident", "", p, out.CalculatedAt, r.ID)})
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return out, err
			}
			eligible, err := countScope(ctx, tx, pr)
			if err != nil {
				return out, err
			}
			out.Items = append(out.Items, Report{Key: "problem_incidents", Domain: "problem", Label: "Problemy z największą liczbą incydentów", Definition: "Do 20 dostępnych problemów z co najmniej jednym dostępnym powiązanym incydentem utworzonym w wybranym przedziale [od, do). Liczniki obejmują wyłącznie incydenty z prawem odczytu; stan problemu i incydentu nie ogranicza rankingu. Suma to powiązania w pokazanym rankingu, a nie liczba unikalnych incydentów w organizacji.", TimeScope: "range", Eligible: eligible, Total: sumBuckets(items), Items: items})
		}
	}
	if err = tx.Commit(); err != nil {
		return out, err
	}
	return out, nil
}

type ReportSourceFilter struct {
	Report, Domain, Bucket string
	Period                 ReportPeriod
	AsOf                   time.Time
	ProblemID, Before      uint64
	Limit                  int
}

func reportScope(scopes map[string]workrecords.ReferenceScope, f ReportSourceFilter) (workrecords.ReferenceScope, error) {
	scope, ok := scopes[f.Domain]
	if !ok {
		return scope, workrecords.ErrForbidden
	}
	switch f.Report {
	case "active_state":
		if !validState(f.Domain, f.Bucket) {
			return scope, ErrInvalid
		}
		return scope.Restricted(scope.Active).Restricted(scope.State+"=?", f.Bucket), nil
	case "active_group":
		scope = scope.Restricted(scope.Active)
		if f.Bucket == "unassigned" {
			return scope.Restricted(scope.GroupID + " IS NULL"), nil
		}
		id, err := strconv.ParseUint(f.Bucket, 10, 64)
		if err != nil || id == 0 {
			return scope, ErrInvalid
		}
		return scope.Restricted(scope.GroupID+"=?", id), nil
	case "backlog_age":
		if f.AsOf.IsZero() {
			return scope, ErrInvalid
		}
		scope = scope.Restricted(scope.Active)
		switch f.Bucket {
		case "0_7":
			return scope.Restricted(scope.CreatedAt+">?", f.AsOf.Add(-7*24*time.Hour)), nil
		case "7_30":
			return scope.Restricted(scope.CreatedAt+">? AND "+scope.CreatedAt+"<=?", f.AsOf.Add(-30*24*time.Hour), f.AsOf.Add(-7*24*time.Hour)), nil
		case "30_plus":
			return scope.Restricted(scope.CreatedAt+"<=?", f.AsOf.Add(-30*24*time.Hour)), nil
		}
		return scope, ErrInvalid
	case "sla_risk":
		if f.Domain != "incident" {
			return scope, ErrInvalid
		}
		return scope.Restricted(scope.Active).Restricted(slaRiskSQL), nil
	case "planned_changes", "closed_changes":
		if f.Domain != "change" {
			return scope, ErrInvalid
		}
		if f.Report == "planned_changes" {
			scope = scope.Restricted("p.state IN('SCHEDULED','IMPLEMENT','REVIEW') AND p.planned_start<? AND p.planned_end>?", f.Period.To.UTC(), f.Period.From.UTC())
		} else {
			scope = scope.Restricted("p.state='CLOSED' AND p.actual_end>=? AND p.actual_end<?", f.Period.From.UTC(), f.Period.To.UTC())
		}
		if f.Bucket != "" {
			if !validState("change", f.Bucket) {
				return scope, ErrInvalid
			}
			scope = scope.Restricted("p.state=?", f.Bucket)
		}
		return scope, nil
	case "problem_incidents":
		if f.Domain != "incident" || f.ProblemID == 0 {
			return scope, ErrInvalid
		}
		problem, ok := scopes["problem"]
		if !ok {
			return scope, workrecords.ErrForbidden
		}
		// The source problem must still be readable when a saved drilldown is opened.
		exists := "EXISTS(SELECT 1" + problem.From + problem.Where + " AND p.id=?)"
		scope = scope.Restricted(exists, append(append([]any{}, problem.Args...), f.ProblemID)...)
		return scope.Restricted("i.created_at>=? AND i.created_at<? AND EXISTS(SELECT 1 FROM problem_incident_links l WHERE l.tenant_id=i.tenant_id AND l.incident_id=i.id AND l.problem_id=?)", f.Period.From.UTC(), f.Period.To.UTC(), f.ProblemID), nil
	}
	return scope, ErrInvalid
}
func validState(domain, value string) bool {
	for _, v := range reportStates[domain] {
		if v == value {
			return true
		}
	}
	return false
}

type ReportSources struct {
	Items        []workrecords.Reference `json:"items"`
	Total        uint64                  `json:"total,string"`
	Next         uint64                  `json:"next_before_id,string,omitempty"`
	CalculatedAt time.Time               `json:"calculated_at"`
}

func (s *Service) ReportSources(ctx context.Context, a workrecords.Principal, f ReportSourceFilter) (ReportSources, error) {
	out := ReportSources{Items: []workrecords.Reference{}, CalculatedAt: time.Now().UTC()}
	if err := ValidatePeriod(f.Period); err != nil {
		return out, err
	}
	if f.Limit < 1 || f.Limit > 100 {
		return out, ErrInvalid
	}
	if !isReportDomain(f.Domain) {
		return out, ErrInvalid
	}
	scopes, err := s.scopes(a)
	if err != nil {
		return out, err
	}
	scope, err := reportScope(scopes, f)
	if err != nil {
		return out, err
	}
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	out.Total, err = countScope(ctx, tx, scope)
	if err != nil {
		return out, err
	}
	items, err := workrecords.References(ctx, tx, scope, workrecords.ReferenceSelection{Before: f.Before, Limit: f.Limit + 1})
	if err != nil {
		return out, err
	}
	if len(items) > f.Limit {
		items = items[:f.Limit]
		out.Next = items[len(items)-1].ID
	}
	out.Items = items
	if err = tx.Commit(); err != nil {
		return out, err
	}
	return out, nil
}
func isReportDomain(kind string) bool {
	for _, v := range reportDomains {
		if v == kind {
			return true
		}
	}
	return false
}
