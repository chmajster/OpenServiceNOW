// Package recordlists validates presentation and query configuration. Domain
// adapters supply fixed SQL expressions and apply their own object authorization.
package recordlists

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

var ErrInvalid = errors.New("nieprawidłowa konfiguracja listy")

type Node struct {
	Logic    string `json:"logic,omitempty"`
	Children []Node `json:"children,omitempty"`
	Field    string `json:"field,omitempty"`
	Operator string `json:"operator,omitempty"`
	Value    string `json:"value,omitempty"`
}
type Field struct {
	SQL      string
	Kind     string
	Values   []string
	Nullable bool
	// Special predicates are fixed by the adapter, never supplied by the client.
	Predicate func(string) (string, []any, error)
}
type Schema map[string]Field

func Decode(raw string) (*Node, error) {
	if raw == "" {
		return nil, nil
	}
	if len(raw) > 16384 {
		return nil, ErrInvalid
	}
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	var node Node
	if err := dec.Decode(&node); err != nil {
		return nil, ErrInvalid
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return nil, ErrInvalid
	}
	return &node, nil
}
func Compile(node *Node, fields Schema) (string, []any, error) {
	if node == nil {
		return "", nil, nil
	}
	remaining := 40
	return compile(*node, fields, 0, &remaining)
}
func compile(n Node, fields Schema, depth int, remaining *int) (string, []any, error) {
	*remaining--
	if depth > 5 || *remaining < 0 {
		return "", nil, ErrInvalid
	}
	if n.Logic != "" {
		if (n.Logic != "AND" && n.Logic != "OR") || n.Field != "" || n.Operator != "" || n.Value != "" || len(n.Children) < 1 || len(n.Children) > 12 {
			return "", nil, ErrInvalid
		}
		parts := []string{}
		args := []any{}
		for _, child := range n.Children {
			sql, a, err := compile(child, fields, depth+1, remaining)
			if err != nil {
				return "", nil, err
			}
			parts = append(parts, sql)
			args = append(args, a...)
		}
		return "(" + strings.Join(parts, " "+n.Logic+" ") + ")", args, nil
	}
	field, ok := fields[n.Field]
	if !ok || len(n.Children) > 0 || !validText(n.Value, 200) {
		return "", nil, ErrInvalid
	}
	if field.Predicate != nil {
		if n.Operator != "eq" {
			return "", nil, ErrInvalid
		}
		return field.Predicate(n.Value)
	}
	if n.Operator == "empty" || n.Operator == "not_empty" {
		if !field.Nullable || n.Value != "" {
			return "", nil, ErrInvalid
		}
		op := " IS NULL"
		if n.Operator == "not_empty" {
			op = " IS NOT NULL"
		}
		return field.SQL + op, nil, nil
	}
	var value any = n.Value
	switch field.Kind {
	case "enum":
		found := false
		for _, v := range field.Values {
			if v == n.Value {
				found = true
			}
		}
		if !found {
			return "", nil, ErrInvalid
		}
	case "id":
		id, err := strconv.ParseUint(n.Value, 10, 64)
		if err != nil || id == 0 || strconv.FormatUint(id, 10) != n.Value {
			return "", nil, ErrInvalid
		}
		value = id
	case "number":
		number, err := strconv.Atoi(n.Value)
		if err != nil || number < 1 || number > 5 {
			return "", nil, ErrInvalid
		}
		value = number
	case "time":
		stamp, err := time.Parse(time.RFC3339, n.Value)
		if err != nil {
			return "", nil, ErrInvalid
		}
		value = stamp.UTC()
	case "text":
		if n.Value == "" {
			return "", nil, ErrInvalid
		}
	default:
		return "", nil, ErrInvalid
	}
	ops := map[string]string{"eq": "=", "ne": "<>"}
	if field.Kind == "number" || field.Kind == "time" {
		ops["gt"] = ">"
		ops["gte"] = ">="
		ops["lt"] = "<"
		ops["lte"] = "<="
	}
	if op, ok := ops[n.Operator]; ok {
		return field.SQL + op + "?", []any{value}, nil
	}
	if field.Kind == "text" && (n.Operator == "contains" || n.Operator == "starts_with") {
		escaped := strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(n.Value) + "%"
		if n.Operator == "contains" {
			escaped = "%" + escaped
		}
		return field.SQL + " LIKE ? ESCAPE '!'", []any{escaped}, nil
	}
	return "", nil, ErrInvalid
}
func validText(s string, max int) bool {
	if !utf8.ValidString(s) || utf8.RuneCountInString(s) > max {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
func BooleanPredicate(sql string) func(string) (string, []any, error) {
	return func(value string) (string, []any, error) {
		if value == "true" {
			return "(" + sql + ")", nil, nil
		}
		if value == "false" {
			return "NOT (" + sql + ")", nil, nil
		}
		return "", nil, ErrInvalid
	}
}

// CSVCell neutralizes spreadsheet formula prefixes, including whitespace/control
// characters before a formula. Numeric identifiers are also exported as text.
func CSVCell(s string) string {
	trimmed := strings.TrimLeftFunc(s, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) })
	if trimmed != "" && strings.ContainsRune("=+-@", rune(trimmed[0])) {
		return "'" + s
	}
	return s
}

type Column struct {
	Key   string `json:"key"`
	Width int    `json:"width"`
}
type Config struct {
	Context map[string]string `json:"context,omitempty"`
	Filter  *Node             `json:"filter,omitempty"`
	Sort    string            `json:"sort"`
	GroupBy string            `json:"group_by,omitempty"`
	Columns []Column          `json:"columns"`
}

func ValidateConfig(raw json.RawMessage, fields Schema, sorts, groups, columns []string) (Config, error) {
	if len(raw) > 32768 {
		return Config{}, ErrInvalid
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var c Config
	if dec.Decode(&c) != nil || dec.Decode(new(any)) != io.EOF {
		return c, ErrInvalid
	}
	if _, _, err := Compile(c.Filter, fields); err != nil {
		return c, err
	}
	for key, value := range c.Context {
		if !validText(value, 200) {
			return c, ErrInvalid
		}
		if key == "q" {
			continue
		}
		if key == "view" {
			if !strings.Contains("|all|mine|assigned|group|my_group|unassigned|", "|"+value+"|") {
				return c, ErrInvalid
			}
			continue
		}
		field, op := key, "eq"
		if key == "from" {
			field, op = "created_at", "gte"
		}
		if key == "to" {
			field, op = "created_at", "lte"
		}
		switch key {
		case "state", "category", "priority", "assigned_to", "group_id", "reporter_id", "from", "to", "active", "sla_risk", "class", "status", "environment", "location", "owner_id", "support_group_id", "archived", "criticality", "lifecycle", "impact", "known_error", "risk", "change_type", "assignment_group_id":
		default:
			return c, ErrInvalid
		}
		if _, _, err := Compile(&Node{Field: field, Operator: op, Value: value}, fields); err != nil {
			return c, ErrInvalid
		}
	}
	allowed := func(key string, keys []string) bool {
		for _, v := range keys {
			if v == key {
				return true
			}
		}
		return false
	}
	if !allowed(c.Sort, sorts) || (c.GroupBy != "" && !allowed(c.GroupBy, groups)) || len(c.Columns) < 1 || len(c.Columns) > 20 {
		return c, ErrInvalid
	}
	seen := map[string]bool{}
	for _, col := range c.Columns {
		if !allowed(col.Key, columns) || seen[col.Key] || col.Width < 80 || col.Width > 600 {
			return c, fmt.Errorf("%w: kolumny", ErrInvalid)
		}
		seen[col.Key] = true
	}
	return c, nil
}
