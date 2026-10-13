package workflow

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

var nodeKey = regexp.MustCompile(`^[a-z][a-z0-9_]{0,47}$`)

func validText(s string, min, max int) bool {
	return utf8.ValidString(s) && utf8.RuneCountInString(strings.TrimSpace(s)) >= min && utf8.RuneCountInString(s) <= max && !strings.ContainsRune(s, 0)
}
func invalid(s string) error { return fmt.Errorf("%w: %s", ErrInvalid, s) }
func edges(n Node) []string {
	switch n.Type {
	case "END":
		return nil
	case "CONDITION":
		return []string{n.Next, n.Otherwise}
	case "APPROVAL":
		return []string{n.Next, n.Rejected, n.Timeout}
	case "WAIT_TASK":
		return []string{n.Next, n.Timeout}
	default:
		return []string{n.Next}
	}
}
func start(c Config) string {
	for _, n := range c.Nodes {
		if n.Type == "START" {
			return n.ID
		}
	}
	return ""
}
func find(c Config, id string) Node {
	for _, n := range c.Nodes {
		if n.ID == id {
			return n
		}
	}
	return Node{}
}

// Only a finite sequential DAG is executable. Branches are choices, never forks.
func Validate(c Config, publish bool) error {
	if c.Domain != "" && c.Domain != "REQUEST" && c.Domain != "CHANGE" {
		return invalid("nieobsługiwana domena procesu")
	}
	if c.Domain == "CHANGE" {
		return validateChange(c, publish)
	}
	if c.ChangeType != "" {
		return invalid("typ zmiany dotyczy wyłącznie domeny Change")
	}
	if !validText(c.Name, 1, 200) || len(c.Nodes) < 2 || len(c.Nodes) > 50 || c.SelfApproval != "DENY" && c.SelfApproval != "ALLOW" {
		return invalid("nazwa, 2–50 kroków lub polityka samodzielnej akceptacji")
	}
	nodes := map[string]Node{}
	starts := 0
	for _, n := range c.Nodes {
		if !nodeKey.MatchString(n.ID) || !validText(n.Label, 1, 160) || nodes[n.ID].ID != "" {
			return invalid("unikalne identyfikatory i etykiety kroków")
		}
		switch n.Type {
		case "START", "CONDITION", "APPROVAL", "CREATE_TASK", "WAIT_TASK", "WAIT_TIMER", "UPDATE_RECORD", "NOTIFY", "END":
		default:
			return invalid("nieobsługiwany typ kroku")
		}
		if n.Type == "START" {
			starts++
		}
		nodes[n.ID] = n
	}
	if !publish {
		return nil
	}
	if starts != 1 || c.ExecutorID == 0 || c.GroupID == 0 {
		return invalid("jeden start, tożsamość wykonawcza i grupa realizacji są wymagane")
	}
	for _, n := range c.Nodes {
		for _, to := range edges(n) {
			if nodes[to].ID == "" || nodes[to].Type == "START" {
				return invalid("nieprawidłowe odwołanie w kroku " + n.ID)
			}
		}
		if n.Type == "END" && (n.Next != "" || n.Otherwise != "" || n.Rejected != "" || n.Timeout != "") {
			return invalid("koniec nie ma wyjścia")
		}
		switch n.Type {
		case "CONDITION":
			var value any
			if !nodeKey.MatchString(n.Field) || json.Unmarshal(n.Equals, &value) != nil || value == nil {
				return invalid("warunek wymaga pola i wartości JSON")
			}
			switch value.(type) {
			case string, float64, bool:
			default:
				return invalid("warunek porównuje pojedynczą wartość, bez skryptów")
			}
		case "APPROVAL":
			if n.TargetType != "USER" && n.TargetType != "GROUP" || n.TargetID == 0 || n.Policy != "ANY" && n.Policy != "ALL" || n.Seconds < 1 || n.Seconds > 90*86400 {
				return invalid("akceptacja wymaga użytkownika/grupy, ANY/ALL i terminu 1 s–90 dni")
			}
		case "CREATE_TASK":
			if !validText(n.Title, 1, 200) || !validText(n.Instructions, 0, 10000) {
				return invalid("tytuł lub instrukcje zadania")
			}
		case "WAIT_TASK":
			if nodes[n.TaskRef].Type != "CREATE_TASK" || n.Seconds < 1 || n.Seconds > 365*86400 {
				return invalid("oczekiwanie wymaga kroku zadania i terminu 1 s–365 dni")
			}
		case "WAIT_TIMER":
			if n.Seconds < 1 || n.Seconds > 365*86400 {
				return invalid("termin oczekiwania 1 s–365 dni")
			}
		case "UPDATE_RECORD":
			if n.State != "IN_PROGRESS" && n.State != "FULFILLED" && n.State != "CLOSED" && n.State != "CANCELLED" || !validText(n.Reason, 1, 10000) {
				return invalid("dopuszczony stan rekordu i uzasadnienie są wymagane")
			}
		case "NOTIFY":
			if n.Audience != "REQUESTER" {
				return invalid("powiadomienie procesu jest przeznaczone dla zgłaszającego")
			}
		}
	}
	colors := map[string]int{}
	var visit func(string) error
	visit = func(id string) error {
		if colors[id] == 1 {
			return invalid("cykle nie są obsługiwane")
		}
		if colors[id] == 2 {
			return nil
		}
		colors[id] = 1
		for _, to := range edges(nodes[id]) {
			if err := visit(to); err != nil {
				return err
			}
		}
		colors[id] = 2
		return nil
	}
	if err := visit(start(c)); err != nil {
		return err
	}
	if len(colors) != len(nodes) {
		return invalid("każdy krok musi być osiągalny ze startu")
	}
	// Check every reachable path. Approval failure/timeout never grants task or
	// fulfillment authority. Task references must dominate their wait nodes.
	seen := map[string]bool{}
	var path func(string, bool, bool, string, uint64, uint64) error
	index := map[string]uint{}
	for i, n := range c.Nodes {
		index[n.ID] = uint(i)
	}
	path = func(id string, approved, cancelled bool, final string, tasks, waited uint64) error {
		key := fmt.Sprintf("%s:%t:%t:%s:%d:%d", id, approved, cancelled, final, tasks, waited)
		if seen[key] {
			return nil
		}
		seen[key] = true
		if len(seen) > 10000 {
			return invalid("graf ma zbyt wiele wariantów wykonania")
		}
		n := nodes[id]
		if final != "" && n.Type != "END" && n.Type != "NOTIFY" && n.Type != "WAIT_TIMER" && !(final == "FULFILLED" && n.Type == "UPDATE_RECORD" && n.State == "CLOSED") {
			return invalid("po realizacji dozwolone są tylko zamknięcie, termin, powiadomienie i koniec")
		}
		if cancelled && n.Type != "END" && n.Type != "NOTIFY" {
			return invalid("po anulowaniu dozwolone są tylko powiadomienie i koniec")
		}
		switch n.Type {
		case "CREATE_TASK":
			if !approved {
				return invalid("utworzenie zadania wymaga wcześniejszej akceptacji na każdej ścieżce")
			}
			tasks |= 1 << index[id]
		case "WAIT_TASK":
			bit := uint64(1) << index[n.TaskRef]
			if tasks&bit == 0 {
				return invalid("oczekiwanie musi następować po utworzeniu wskazanego zadania")
			}
			waited |= bit
		case "UPDATE_RECORD":
			if n.State == "CANCELLED" {
				cancelled = true
			} else if !approved {
				return invalid("aktualizacja stanu wymaga akceptacji")
			}
			if n.State == "FULFILLED" || n.State == "CLOSED" {
				if n.State == "CLOSED" && final != "FULFILLED" {
					return invalid("zamknięcie musi następować po realizacji wniosku")
				}
				if tasks == 0 || tasks != waited {
					return invalid("realizacja wymaga utworzenia i oczekiwania na wszystkie zadania")
				}
				final = n.State
			}
		case "END":
			if !cancelled && (!approved || tasks == 0 || tasks != waited) {
				return invalid("ścieżka końca musi anulować wniosek albo zaakceptować i ukończyć zadania")
			}
			return nil
		}
		if n.Type == "APPROVAL" {
			if err := path(n.Next, true, cancelled, final, tasks, waited); err != nil {
				return err
			}
			for _, to := range []string{n.Rejected, n.Timeout} {
				if err := path(to, false, cancelled, final, tasks, waited); err != nil {
					return err
				}
			}
			return nil
		}
		if n.Type == "WAIT_TASK" {
			if err := path(n.Timeout, false, cancelled, final, tasks, waited); err != nil {
				return err
			}
			return path(n.Next, approved, cancelled, final, tasks, waited)
		}
		for _, to := range edges(n) {
			if err := path(to, approved, cancelled, final, tasks, waited); err != nil {
				return err
			}
		}
		return nil
	}
	return path(start(c), false, false, "", 0, 0)
}
