package incidents

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

type Option struct {
	Retired bool              `json:"retired,omitempty"`
	Value   string            `json:"value"`
	Label   string            `json:"label"`
	Labels  map[string]string `json:"labels,omitempty"`
}
type Category struct {
	Retired       bool              `json:"retired,omitempty"`
	Value         string            `json:"value"`
	Label         string            `json:"label"`
	Labels        map[string]string `json:"labels,omitempty"`
	Subcategories []Option          `json:"subcategories"`
}

var Categories = []Category{
	{Value: "hardware", Label: "Sprzęt", Labels: map[string]string{"pl": "Sprzęt", "en": "Hardware"}, Subcategories: []Option{{Value: "computer", Label: "Komputer", Labels: map[string]string{"pl": "Komputer", "en": "Computer"}}, {Value: "peripheral", Label: "Urządzenie peryferyjne", Labels: map[string]string{"pl": "Urządzenie peryferyjne", "en": "Peripheral"}}, {Value: "other", Label: "Inne", Labels: map[string]string{"pl": "Inne", "en": "Other"}}}},
	{Value: "software", Label: "Oprogramowanie", Labels: map[string]string{"pl": "Oprogramowanie", "en": "Software"}, Subcategories: []Option{{Value: "application", Label: "Aplikacja", Labels: map[string]string{"pl": "Aplikacja", "en": "Application"}}, {Value: "operating_system", Label: "System operacyjny", Labels: map[string]string{"pl": "System operacyjny", "en": "Operating system"}}, {Value: "other", Label: "Inne", Labels: map[string]string{"pl": "Inne", "en": "Other"}}}},
	{Value: "network", Label: "Sieć", Labels: map[string]string{"pl": "Sieć", "en": "Network"}, Subcategories: []Option{{Value: "connectivity", Label: "Połączenie", Labels: map[string]string{"pl": "Połączenie", "en": "Connectivity"}}, {Value: "vpn", Label: "VPN", Labels: map[string]string{"pl": "VPN", "en": "VPN"}}, {Value: "other", Label: "Inne", Labels: map[string]string{"pl": "Inne", "en": "Other"}}}},
	{Value: "access", Label: "Dostęp", Labels: map[string]string{"pl": "Dostęp", "en": "Access"}, Subcategories: []Option{{Value: "account", Label: "Konto", Labels: map[string]string{"pl": "Konto", "en": "Account"}}, {Value: "permissions", Label: "Uprawnienia", Labels: map[string]string{"pl": "Uprawnienia", "en": "Permissions"}}, {Value: "other", Label: "Inne", Labels: map[string]string{"pl": "Inne", "en": "Other"}}}},
	{Value: "other", Label: "Inne", Labels: map[string]string{"pl": "Inne", "en": "Other"}, Subcategories: []Option{{Value: "other", Label: "Inne", Labels: map[string]string{"pl": "Inne", "en": "Other"}}}},
}
var ResolutionCodes = []Option{{Value: "FIXED", Label: "Naprawiono", Labels: map[string]string{"pl": "Naprawiono", "en": "Fixed"}}, {Value: "WORKAROUND", Label: "Zastosowano obejście", Labels: map[string]string{"pl": "Zastosowano obejście", "en": "Workaround"}}, {Value: "DUPLICATE", Label: "Duplikat", Labels: map[string]string{"pl": "Duplikat", "en": "Duplicate"}}, {Value: "NOT_REPRODUCIBLE", Label: "Nie odtworzono", Labels: map[string]string{"pl": "Nie odtworzono", "en": "Not reproducible"}}}
var States = []Option{{Value: "NEW", Label: "Nowy", Labels: map[string]string{"pl": "Nowy", "en": "New"}}, {Value: "IN_PROGRESS", Label: "W trakcie", Labels: map[string]string{"pl": "W trakcie", "en": "In progress"}}, {Value: "ON_HOLD", Label: "Wstrzymany", Labels: map[string]string{"pl": "Wstrzymany", "en": "On hold"}}, {Value: "RESOLVED", Label: "Rozwiązany", Labels: map[string]string{"pl": "Rozwiązany", "en": "Resolved"}}, {Value: "CLOSED", Label: "Zamknięty", Labels: map[string]string{"pl": "Zamknięty", "en": "Closed"}}, {Value: "CANCELLED", Label: "Anulowany", Labels: map[string]string{"pl": "Anulowany", "en": "Cancelled"}}}
var PriorityMatrix = [3][3]int{{1, 2, 3}, {2, 3, 4}, {3, 4, 5}}

func CalculatePriority(impact, urgency int) (int, error) {
	return CalculatePriorityWith(PriorityMatrix, impact, urgency)
}
func CalculatePriorityWith(matrix [3][3]int, impact, urgency int) (int, error) {
	if impact < 1 || impact > 3 || urgency < 1 || urgency > 3 {
		return 0, fmt.Errorf("%w: wpływ i pilność muszą mieć wartość 1–3", ErrInvalid)
	}
	value := matrix[impact-1][urgency-1]
	if value < 1 || value > 5 {
		return 0, ErrInvalid
	}
	return value, nil
}
func normalizeFields(input Fields) (Fields, error) {
	return normalizeFieldsWith(input, Categories, PriorityMatrix)
}
func normalizeFieldsWith(input Fields, categories []Category, matrix [3][3]int) (Fields, error) {
	input, err := normalizeFieldsBase(input)
	if err != nil {
		return input, err
	}
	if _, err := CalculatePriorityWith(matrix, input.Impact, input.Urgency); err != nil {
		return input, err
	}
	for _, category := range categories {
		if category.Value == input.Category && !category.Retired {
			for _, sub := range category.Subcategories {
				if sub.Value == input.Subcategory && !sub.Retired {
					return input, nil
				}
			}
		}
	}
	return input, fmt.Errorf("%w: wybierz prawidłową kategorię i podkategorię", ErrInvalid)
}
func normalizeFieldsBase(input Fields) (Fields, error) {
	input.Title = strings.TrimSpace(input.Title)
	input.Description = strings.TrimSpace(input.Description)
	if !validText(input.Title, 1, 200) || !validText(input.Description, 1, 20000) {
		return input, fmt.Errorf("%w: tytuł wymaga 1–200 znaków, opis 1–20000 znaków", ErrInvalid)
	}
	input.Location = strings.TrimSpace(input.Location)
	if !validText(input.Location, 0, 160) || input.ServiceID != nil && *input.ServiceID == 0 {
		return input, ErrInvalid
	}
	input.Category = strings.TrimSpace(input.Category)
	input.Subcategory = strings.TrimSpace(input.Subcategory)
	if !validText(input.Category, 1, 40) || !validText(input.Subcategory, 1, 40) {
		return input, fmt.Errorf("%w: wybierz prawidłową kategorię i podkategorię", ErrInvalid)
	}
	if input.Impact < 1 || input.Impact > 3 || input.Urgency < 1 || input.Urgency > 3 {
		return input, fmt.Errorf("%w: wpływ i pilność muszą mieć wartość 1–3", ErrInvalid)
	}
	return input, nil
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
	for _, target := range processTransitions(r) {
		if target == "IN_PROGRESS" && (r.State == "RESOLVED" || r.State == "CLOSED") && !reopenInWindow(r, time.Now()) {
			continue
		}
		if a.Has(transitionPermission(target)) || target == "IN_PROGRESS" && canRequesterReopen(a, r, time.Now()) {
			result.Transitions = append(result.Transitions, target)
		}
	}
	return result
}
func validateTransition(r Record, input TransitionInput) error {
	return validateTransitionWith(r, input, ResolutionCodes)
}
func validateTransitionWith(r Record, input TransitionInput, resolutionCodes []Option) error {
	return validateTransitionWithReasons(r, input, resolutionCodes, defaultHoldReasons)
}

var defaultHoldReasons = []Option{{Value: "AWAITING_USER"}, {Value: "AWAITING_VENDOR"}, {Value: "AWAITING_CHANGE"}, {Value: "OTHER"}}

func validateTransitionWithReasons(r Record, input TransitionInput, resolutionCodes, holdReasons []Option) error {
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
	if input.State == "ON_HOLD" {
		valid := false
		for _, option := range holdReasons {
			if option.Value == input.HoldReason && !option.Retired {
				valid = true
				break
			}
		}
		if !valid || input.Reason != "" {
			return fmt.Errorf("%w: wybierz aktywny powód wstrzymania", ErrInvalid)
		}
	} else if input.HoldReason != "" {
		return fmt.Errorf("%w: powód wstrzymania dotyczy wyłącznie stanu ON_HOLD", ErrInvalid)
	}
	if input.State == "CANCELLED" || reopening {
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
		for _, option := range resolutionCodes {
			if option.Value == input.ResolutionCode && !option.Retired {
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
