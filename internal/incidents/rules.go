package incidents

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

type Option struct {
	Value string `json:"value"`
	Label string `json:"label"`
}
type Category struct {
	Value         string   `json:"value"`
	Label         string   `json:"label"`
	Subcategories []Option `json:"subcategories"`
}

var Categories = []Category{
	{"hardware", "Sprzęt", []Option{{"computer", "Komputer"}, {"peripheral", "Urządzenie peryferyjne"}, {"other", "Inne"}}},
	{"software", "Oprogramowanie", []Option{{"application", "Aplikacja"}, {"operating_system", "System operacyjny"}, {"other", "Inne"}}},
	{"network", "Sieć", []Option{{"connectivity", "Połączenie"}, {"vpn", "VPN"}, {"other", "Inne"}}},
	{"access", "Dostęp", []Option{{"account", "Konto"}, {"permissions", "Uprawnienia"}, {"other", "Inne"}}},
	{"other", "Inne", []Option{{"other", "Inne"}}},
}
var ResolutionCodes = []Option{{"FIXED", "Naprawiono"}, {"WORKAROUND", "Zastosowano obejście"}, {"DUPLICATE", "Duplikat"}, {"NOT_REPRODUCIBLE", "Nie odtworzono"}}
var States = []Option{{"NEW", "Nowy"}, {"IN_PROGRESS", "W trakcie"}, {"ON_HOLD", "Wstrzymany"}, {"RESOLVED", "Rozwiązany"}, {"CLOSED", "Zamknięty"}, {"CANCELLED", "Anulowany"}}
var PriorityMatrix = [3][3]int{{1, 2, 3}, {2, 3, 4}, {3, 4, 5}}

func CalculatePriority(impact, urgency int) (int, error) {
	if impact < 1 || impact > 3 || urgency < 1 || urgency > 3 {
		return 0, fmt.Errorf("%w: wpływ i pilność muszą mieć wartość 1–3", ErrInvalid)
	}
	return PriorityMatrix[impact-1][urgency-1], nil
}
func normalizeFields(input Fields) (Fields, error) {
	input.Title = strings.TrimSpace(input.Title)
	input.Description = strings.TrimSpace(input.Description)
	if !validText(input.Title, 1, 200) || !validText(input.Description, 1, 20000) {
		return input, fmt.Errorf("%w: tytuł wymaga 1–200 znaków, opis 1–20000 znaków", ErrInvalid)
	}
	if _, err := CalculatePriority(input.Impact, input.Urgency); err != nil {
		return input, err
	}
	for _, category := range Categories {
		if category.Value == input.Category {
			for _, sub := range category.Subcategories {
				if sub.Value == input.Subcategory {
					return input, nil
				}
			}
		}
	}
	return input, fmt.Errorf("%w: wybierz prawidłową kategorię i podkategorię", ErrInvalid)
}
func validText(value string, min, max int) bool {
	n := utf8.RuneCountInString(value)
	return utf8.ValidString(value) && n >= min && n <= max
}
func readable(a Actor, r Record) bool {
	return a.TenantID == r.TenantID && (a.Has("read") || (a.Has("read_own") && (r.ReporterID == a.UserID || r.AffectedUserID == a.UserID)))
}
func editable(r Record) bool { return r.State != "CLOSED" && r.State != "CANCELLED" }
func transitions(state string) []string {
	switch state {
	case "NEW":
		return []string{"IN_PROGRESS", "ON_HOLD", "CANCELLED"}
	case "IN_PROGRESS":
		return []string{"ON_HOLD", "RESOLVED", "CANCELLED"}
	case "ON_HOLD":
		return []string{"IN_PROGRESS", "RESOLVED", "CANCELLED"}
	case "RESOLVED":
		return []string{"CLOSED", "IN_PROGRESS"}
	case "CLOSED":
		return []string{"IN_PROGRESS"}
	default:
		return []string{}
	}
}
func transitionPermission(state string) string {
	if state == "RESOLVED" || state == "CLOSED" {
		return "resolve"
	}
	return "update"
}
func allowedActions(a Actor, r Record) Actions {
	result := Actions{Edit: editable(r) && a.Has("update"), Assign: editable(r) && a.Has("assign"), OverridePriority: editable(r) && a.Has("priority.override"), Transitions: []string{}}
	if !readable(a, r) {
		return Actions{Transitions: []string{}}
	}
	for _, target := range transitions(r.State) {
		if a.Has(transitionPermission(target)) {
			result.Transitions = append(result.Transitions, target)
		}
	}
	return result
}
func validateTransition(r Record, input TransitionInput) error {
	allowed := false
	for _, state := range transitions(r.State) {
		if state == input.State {
			allowed = true
		}
	}
	if !allowed {
		return ErrTransition
	}
	reopening := input.State == "IN_PROGRESS" && (r.State == "RESOLVED" || r.State == "CLOSED")
	if input.State == "ON_HOLD" || input.State == "CANCELLED" || reopening {
		if !validText(strings.TrimSpace(input.Reason), 1, 1000) {
			return fmt.Errorf("%w: podaj powód (1–1000 znaków)", ErrInvalid)
		}
	} else if input.Reason != "" {
		return fmt.Errorf("%w: powód nie dotyczy tego przejścia", ErrInvalid)
	}
	if input.State == "IN_PROGRESS" || input.State == "RESOLVED" {
		if r.Assignment.AssignedTo == nil {
			return fmt.Errorf("%w: najpierw przypisz incydent do aktywnego użytkownika", ErrInvalid)
		}
	}
	if input.State == "RESOLVED" {
		code := false
		for _, option := range ResolutionCodes {
			if option.Value == input.ResolutionCode {
				code = true
			}
		}
		if !code || !validText(strings.TrimSpace(input.ResolutionDescription), 1, 20000) {
			return fmt.Errorf("%w: wybierz kod i opisz rozwiązanie (1–20000 znaków)", ErrInvalid)
		}
	} else if input.ResolutionCode != "" || input.ResolutionDescription != "" {
		return fmt.Errorf("%w: rozwiązanie dotyczy wyłącznie przejścia do RESOLVED", ErrInvalid)
	}
	return nil
}
