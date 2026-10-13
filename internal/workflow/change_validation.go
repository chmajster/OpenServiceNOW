package workflow

import (
	"encoding/json"
	"fmt"
)

func validateChange(c Config, publish bool) error {
	if !validText(c.Name, 1, 200) || len(c.Nodes) < 2 || len(c.Nodes) > 50 || c.SelfApproval != "DENY" || c.ChangeType != "NORMAL" && c.ChangeType != "STANDARD" && c.ChangeType != "EMERGENCY" {
		return invalid("Change wymaga typu, 2–50 kroków i zakazu samodzielnej akceptacji")
	}
	nodes := map[string]Node{}
	starts := 0
	for _, n := range c.Nodes {
		if !nodeKey.MatchString(n.ID) || !validText(n.Label, 1, 160) || nodes[n.ID].ID != "" {
			return invalid("unikalne identyfikatory i etykiety kroków")
		}
		switch n.Type {
		case "START", "CONDITION", "APPROVAL", "UPDATE_RECORD", "NOTIFY", "END":
		default:
			return invalid("adapter Change obsługuje ocenę i akceptację; zadania realizuje proces zmiany")
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
		return invalid("wskaż jeden start, wykonawcę i grupę Change")
	}
	for _, n := range c.Nodes {
		for _, to := range edges(n) {
			if nodes[to].ID == "" || nodes[to].Type == "START" {
				return invalid("nieprawidłowe odwołanie kroku " + n.ID)
			}
		}
		if n.Type == "END" && (n.Next != "" || n.Otherwise != "" || n.Rejected != "" || n.Timeout != "") {
			return invalid("koniec nie ma wyjścia")
		}
		switch n.Type {
		case "CONDITION":
			var v string
			if json.Unmarshal(n.Equals, &v) != nil {
				return invalid("warunek Change porównuje jawną wartość słownika")
			}
			if n.Field == "risk" || n.Field == "impact" {
				if v != "HIGH" && v != "MEDIUM" && v != "LOW" {
					return invalid("nieprawidłowy poziom ryzyka lub wpływu")
				}
			} else if n.Field == "change_type" {
				if v != "NORMAL" && v != "STANDARD" && v != "EMERGENCY" {
					return invalid("nieprawidłowy typ zmiany")
				}
			} else {
				return invalid("warunek Change obsługuje ryzyko, wpływ i typ zmiany")
			}
		case "APPROVAL":
			if n.TargetType != "USER" && n.TargetType != "GROUP" || n.TargetID == 0 || n.Policy != "ANY" && n.Policy != "ALL" || n.Seconds < 1 || n.Seconds > 90*86400 || c.ChangeType == "EMERGENCY" && n.Seconds > 86400 {
				return invalid("akceptacja wymaga celu, polityki i terminu; Emergency najwyżej 24 godziny")
			}
		case "UPDATE_RECORD":
			if n.State != "AUTHORIZED" && n.State != "REJECTED" || !validText(n.Reason, 1, 10000) {
				return invalid("Change dopuszcza wyłącznie autoryzację lub odrzucenie z uzasadnieniem")
			}
		case "NOTIFY":
			if n.Audience != "REQUESTER" {
				return invalid("powiadomienie dotyczy zgłaszającego")
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
		return invalid("wszystkie kroki muszą być osiągalne")
	}
	seen := map[string]bool{}
	var path func(string, bool, string) error
	path = func(id string, approved bool, final string) error {
		key := fmt.Sprintf("%s:%t:%s", id, approved, final)
		if seen[key] {
			return nil
		}
		seen[key] = true
		n := nodes[id]
		if final != "" && n.Type != "END" && n.Type != "NOTIFY" {
			return invalid("po decyzji domenowej dozwolone są tylko powiadomienie i koniec")
		}
		if n.Type == "UPDATE_RECORD" {
			if n.State == "AUTHORIZED" && !approved || n.State == "REJECTED" && approved {
				return invalid("wynik domenowy musi odpowiadać decyzji akceptacji")
			}
			final = n.State
		}
		if n.Type == "END" {
			if final == "" {
				return invalid("każda ścieżka musi jawnie autoryzować lub odrzucić zmianę")
			}
			return nil
		}
		if n.Type == "APPROVAL" {
			if err := path(n.Next, true, final); err != nil {
				return err
			}
			for _, to := range []string{n.Rejected, n.Timeout} {
				if err := path(to, false, final); err != nil {
					return err
				}
			}
			return nil
		}
		for _, to := range edges(n) {
			if err := path(to, approved, final); err != nil {
				return err
			}
		}
		return nil
	}
	return path(start(c), false, "")
}
