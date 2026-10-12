package catalog

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

var fieldKey = regexp.MustCompile(`^[a-z][a-z0-9_]{0,47}$`)

func Text(v string, min, max int) bool {
	return utf8.ValidString(v) && utf8.RuneCountInString(v) >= min && utf8.RuneCountInString(v) <= max && !strings.ContainsRune(v, 0)
}
func invalid(message string) error { return fmt.Errorf("%w: %s", ErrInvalid, message) }
func ValidateDefinition(d Definition, publishing bool) error {
	if !Text(strings.TrimSpace(d.Title), 1, 200) || !Text(d.Description, 0, 10000) || len(d.Fields) > 50 || len(d.Steps) > 20 {
		return invalid("tytuł, opis lub rozmiar formularza")
	}
	seen := map[string]Field{}
	for _, f := range d.Fields {
		if !fieldKey.MatchString(f.Key) || !Text(strings.TrimSpace(f.Label), 1, 160) {
			return invalid("klucz lub etykieta pola")
		}
		if _, ok := seen[f.Key]; ok {
			return invalid("powtórzony klucz pola")
		}
		switch f.Type {
		case "text", "longtext", "number", "date", "checkbox", "select":
		default:
			return invalid("nieobsługiwany typ pola")
		}
		if f.Type == "select" {
			if len(f.Options) < 1 || len(f.Options) > 100 {
				return invalid("lista wyboru wymaga 1–100 opcji")
			}
			options := map[string]bool{}
			for _, v := range f.Options {
				if !Text(strings.TrimSpace(v), 1, 160) || options[v] {
					return invalid("nieprawidłowa opcja")
				}
				options[v] = true
			}
		} else if len(f.Options) > 0 {
			return invalid("opcje są dozwolone tylko dla listy wyboru")
		}
		// Conditions may only refer to earlier fields: no cycles, scripts or ambiguity.
		for _, c := range []*Condition{f.VisibleWhen, f.RequiredWhen} {
			if c == nil {
				continue
			}
			parent, ok := seen[c.Field]
			if !ok {
				return invalid("warunek musi wskazywać wcześniejsze pole")
			}
			if _, err := validateValue(parent, c.Equals, false); err != nil {
				return invalid("wartość warunku nie pasuje do pola")
			}
		}
		seen[f.Key] = f
	}
	for _, step := range d.Steps {
		if !Text(strings.TrimSpace(step.Title), 1, 200) || !Text(step.Instructions, 0, 10000) {
			return invalid("nieprawidłowy krok realizacji")
		}
	}
	if d.Method != "" && d.Method != "MANUAL_TASKS" {
		return invalid("obsługiwany sposób realizacji: MANUAL_TASKS")
	}
	if publishing && (d.Method != "MANUAL_TASKS" || d.GroupID == 0 || len(d.Steps) == 0) {
		return invalid("publikacja wymaga sposobu realizacji, aktywnej grupy i zadań")
	}
	if publishing {
		required := false
		for _, s := range d.Steps {
			required = required || s.Required
		}
		if !required {
			return invalid("publikacja wymaga co najmniej jednego obowiązkowego zadania")
		}
	}
	return nil
}
func equal(a, b json.RawMessage) bool {
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return false
	}
	ax, _ := json.Marshal(x)
	by, _ := json.Marshal(y)
	return bytes.Equal(ax, by)
}
func matches(c *Condition, values map[string]json.RawMessage) bool {
	return c == nil || equal(values[c.Field], c.Equals)
}
func ValidateValues(fields []Field, input map[string]json.RawMessage) (map[string]json.RawMessage, error) {
	if input == nil {
		input = map[string]json.RawMessage{}
	}
	allowed := map[string]bool{}
	out := map[string]json.RawMessage{}
	for _, f := range fields {
		allowed[f.Key] = true
		v, supplied := input[f.Key]
		if !matches(f.VisibleWhen, out) {
			if supplied {
				return nil, invalid("ukryte pole: " + f.Key)
			}
			continue
		}
		required := f.Required || f.RequiredWhen != nil && matches(f.RequiredWhen, out)
		if !supplied {
			if required {
				return nil, invalid("wymagane pole: " + f.Key)
			}
			continue
		}
		normalized, err := validateValue(f, v, required)
		if err != nil {
			return nil, invalid(f.Label + ": " + err.Error())
		}
		out[f.Key] = normalized
	}
	for key := range input {
		if !allowed[key] {
			return nil, invalid("niedozwolone pole: " + key)
		}
	}
	return out, nil
}
func validateValue(f Field, v json.RawMessage, required bool) (json.RawMessage, error) {
	if bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
		return nil, fmt.Errorf("wartość null nie jest dozwolona")
	}
	var value any
	switch f.Type {
	case "text", "longtext", "date", "select":
		var text string
		if json.Unmarshal(v, &text) != nil {
			return nil, fmt.Errorf("wymagany tekst")
		}
		max := 1000
		if f.Type == "longtext" {
			max = 10000
		}
		if !Text(text, 0, max) || required && strings.TrimSpace(text) == "" {
			return nil, fmt.Errorf("nieprawidłowa długość")
		}
		if f.Type == "date" && text != "" {
			if _, err := time.Parse("2006-01-02", text); err != nil {
				return nil, fmt.Errorf("wymagana rzeczywista data YYYY-MM-DD")
			}
		}
		if f.Type == "select" {
			found := false
			for _, o := range f.Options {
				found = found || o == text
			}
			if !found {
				return nil, fmt.Errorf("wybierz dozwoloną opcję")
			}
		}
		value = text
	case "number":
		var n float64
		if json.Unmarshal(v, &n) != nil || math.IsNaN(n) || math.IsInf(n, 0) || math.Abs(n) > 1e12 {
			return nil, fmt.Errorf("wymagana liczba od −10¹² do 10¹²")
		}
		value = n
	case "checkbox":
		var b bool
		if json.Unmarshal(v, &b) != nil {
			return nil, fmt.Errorf("wymagana wartość logiczna")
		}
		if required && !b {
			return nil, fmt.Errorf("wymagane zaznaczenie")
		}
		value = b
	default:
		return nil, ErrInvalid
	}
	return json.Marshal(value)
}
