package metadata

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/big"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

var emailLocale = map[string]bool{"pl": true, "en": true}

func text(value string, min, max int) bool {
	return utf8.ValidString(value) && utf8.RuneCountInString(value) >= min && utf8.RuneCountInString(value) <= max && !strings.ContainsRune(value, 0)
}

func ValidateConfiguration(c Configuration) error {
	o := c.Organization
	if !text(strings.TrimSpace(o.Name), 1, 160) || !emailLocale[o.DefaultLocale] || !text(o.TimeZone, 1, 100) || !validTimeZone(o.TimeZone) ||
		(o.DateFormat != "YYYY-MM-DD" && o.DateFormat != "DD.MM.YYYY" && o.DateFormat != "MM/DD/YYYY") ||
		(o.NumberFormat != "1,234.56" && o.NumberFormat != "1.234,56") || !currencyCode.MatchString(o.Currency) || !brandColor.MatchString(o.BrandColor) {
		return fmt.Errorf("%w: organization preferences", ErrInvalid)
	}
	if o.LogoURL != "" {
		u, err := url.Parse(o.LogoURL)
		if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || len(o.LogoURL) > 2048 {
			return fmt.Errorf("%w: organization logo URL", ErrInvalid)
		}
	}
	if len(c.RecordTypes) == 0 || len(c.RecordTypes) > 32 {
		return fmt.Errorf("%w: record types", ErrInvalid)
	}
	for _, key := range []string{"incident.category","incident.state","incident.resolution_code","incident.hold_reason","ci.environment","ci.status"} {
		if len(c.Dictionaries[key]) == 0 { return fmt.Errorf("%w: required dictionary %q",ErrInvalid,key) }
	}
	for _, option := range c.Dictionaries["incident.state"] {
		if !map[string]bool{"NEW":true,"IN_PROGRESS":true,"ON_HOLD":true,"RESOLVED":true,"CLOSED":true,"CANCELLED":true}[option.Value] { return fmt.Errorf("%w: incident state %q is not supported by the lifecycle",ErrInvalid,option.Value) }
	}
	if len(c.Dictionaries["incident.state"]) != 6 { return fmt.Errorf("%w: the incident lifecycle requires all six supported states",ErrInvalid) }
	for _, category := range c.Dictionaries["incident.category"] {
		if len(c.Dictionaries["incident.subcategory."+category.Value]) == 0 { return fmt.Errorf("%w: category %q requires at least one subcategory",ErrInvalid,category.Value) }
	}
	for key, recordType := range c.RecordTypes {
		if key != recordType.Key || (key != "incident" && key != "ci") || len(recordType.Fields) > 200 {
			return fmt.Errorf("%w: unsupported record type %q", ErrInvalid, key)
		}
		labels, err := validateLabels(recordType.Labels, false)
		if err != nil || !labels {
			return fmt.Errorf("%w: record type labels %q", ErrInvalid, key)
		}
		if recordType.Parent != "" && recordType.Parent != key {
			if _, ok := c.RecordTypes[recordType.Parent]; !ok {
				return fmt.Errorf("%w: parent record type %q", ErrInvalid, recordType.Parent)
			}
		}
		base := map[string]Field{}
		for _, f := range builtInFields[key] {
			base[f.Key] = f
		}
		all := make(map[string]Field, len(base)+len(recordType.Fields))
		for fieldKey, field := range base {
			all[fieldKey] = field
		}
		seen := map[string]bool{}
		for _, field := range recordType.Fields {
			if !fieldKey.MatchString(field.Key) || field.System || seen[field.Key] {
				return fmt.Errorf("%w: custom field key %q is invalid, reserved, or duplicated", ErrInvalid, field.Key)
			}
			if _, reserved := base[field.Key]; reserved {
				return fmt.Errorf("%w: custom field key %q is reserved", ErrInvalid, field.Key)
			}
			seen[field.Key] = true
			if _, err := ValidateDefinition(field); err != nil {
				return err
			}
			if field.ParentKey != "" {
				parent, ok := all[field.ParentKey]
				if !ok || !fieldTypeCompatible(field, parent) {
					return fmt.Errorf("%w: invalid inherited field %q", ErrInvalid, field.Key)
				}
			}
			all[field.Key] = field
		}
		if err := validateDependencies(recordType.Fields, all); err != nil {
			return err
		}
		if err := validateLayout(recordType, all); err != nil {
			return err
		}
	}
	for key, options := range c.Dictionaries {
		if !fieldKey.MatchString(strings.ReplaceAll(key, ".", "_")) || len(options) == 0 || len(options) > 200 {
			return fmt.Errorf("%w: dictionary %q", ErrInvalid, key)
		}
		values := map[string]bool{}
		for _, option := range options {
			if !fieldKey.MatchString(strings.ToLower(option.Value)) || values[option.Value] {
				return fmt.Errorf("%w: dictionary value %q", ErrInvalid, option.Value)
			}
			values[option.Value] = true
			if _, err := validateLabels(option.Labels, true); err != nil {
				return fmt.Errorf("%w: dictionary %q: %v", ErrInvalid, key, err)
			}
		}
	}
	for row := range c.PriorityMatrix {
		for column, priority := range c.PriorityMatrix[row] {
			if priority < 1 || priority > 5 {
				return fmt.Errorf("%w: priority matrix cell %d,%d", ErrInvalid, row+1, column+1)
			}
		}
	}
	return nil
}

func validTimeZone(value string) bool { _, err := time.LoadLocation(value); return err == nil }

func validateLabels(labels map[string]string, required bool) (bool, error) {
	if !required && len(labels) == 0 {
		return false, nil
	}
	if len(labels) < 2 || len(labels) > 2 {
		return false, ErrInvalid
	}
	for _, locale := range []string{"pl", "en"} {
		label, ok := labels[locale]
		if !ok || !text(strings.TrimSpace(label), 1, 160) {
			return false, ErrInvalid
		}
	}
	return true, nil
}

func ValidateDefinition(f Field) (Field, error) {
	if !fieldKey.MatchString(f.Key) {
		return f, FieldError{Field: f.Key, Code: "INVALID_KEY", Cause: "invalid stable key"}
	}
	if ok, err := validateLabels(f.Labels, true); err != nil || !ok {
		return f, FieldError{Field: f.Key, Code: "INVALID_LABEL", Cause: "Polish and English labels are required"}
	}
	if f.Description != nil {
		if _, err := validateLabels(f.Description, false); err != nil {
			return f, FieldError{Field: f.Key, Code: "INVALID_DESCRIPTION", Cause: "description labels must use pl and en"}
		}
	}
	switch f.Type {
	case TypeText:
		if f.MaxLength < 1 || f.MaxLength > 4000 { return f, FieldError{Field:f.Key, Code:"INVALID_LENGTH", Cause:"TEXT length must be 1–4000"} }
	case TypeLongText:
		if f.MaxLength < 1 || f.MaxLength > 100000 { return f, FieldError{Field:f.Key, Code:"INVALID_LENGTH", Cause:"LONG_TEXT length must be 1–100000"} }
	case TypeBoolean, TypeDate, TypeDateTime:
	case TypeInteger:
		if !validBounds(f) { return f, FieldError{Field:f.Key, Code:"INVALID_RANGE", Cause:"integer bounds must be ordered numbers"} }
	case TypeDecimal:
		if f.Precision < 1 || f.Precision > 20 || f.Scale < 0 || f.Scale > 6 || f.Scale > f.Precision { return f, FieldError{Field:f.Key, Code:"INVALID_PRECISION", Cause:"DECIMAL supports precision 1–20 and scale 0–6"} }
		if !validBounds(f) { return f, FieldError{Field:f.Key, Code:"INVALID_RANGE", Cause:"decimal bounds must be ordered numbers"} }
	case TypeChoice, TypeMultiChoice:
		if len(f.Options) < 1 || len(f.Options) > 200 { return f, FieldError{Field:f.Key, Code:"INVALID_OPTIONS", Cause:"choice requires 1–200 options"} }
		seen := map[string]bool{}
		for _, o := range f.Options {
			if !fieldKey.MatchString(strings.ToLower(o.Value)) || seen[o.Value] { return f, FieldError{Field:f.Key, Code:"INVALID_OPTIONS", Cause:"choice values must be unique stable keys"} }
			seen[o.Value] = true
			if _, err := validateLabels(o.Labels, true); err != nil { return f, FieldError{Field:f.Key, Code:"INVALID_OPTIONS", Cause:"choice labels require pl and en"} }
		}
	case TypeReference:
		if !referenceTypes[f.ReferenceType] { return f, FieldError{Field:f.Key, Code:"INVALID_REFERENCE", Cause:"reference target is not supported"} }
	default:
		return f, FieldError{Field:f.Key, Code:"INVALID_TYPE", Cause:"unsupported data type"}
	}
	if f.Type != TypeChoice && f.Type != TypeMultiChoice && len(f.Options) > 0 { return f, FieldError{Field:f.Key, Code:"UNEXPECTED_OPTIONS", Cause:"options apply only to choice fields"} }
	if f.Type != TypeReference && f.ReferenceType != "" { return f, FieldError{Field:f.Key, Code:"UNEXPECTED_REFERENCE", Cause:"reference target applies only to reference fields"} }
	if f.Type != TypeText && f.Type != TypeLongText && f.MaxLength != 0 { return f, FieldError{Field:f.Key, Code:"UNEXPECTED_LENGTH", Cause:"length applies only to text fields"} }
	if f.Type != TypeDecimal && (f.Precision != 0 || f.Scale != 0) { return f, FieldError{Field:f.Key, Code:"UNEXPECTED_PRECISION", Cause:"precision applies only to decimal fields"} }
	if f.Default != nil {
		if f.Unique { return f, FieldError{Field:f.Key, Code:"UNIQUE_DEFAULT", Cause:"a static default cannot satisfy uniqueness for multiple records"} }
		if _, err := NormalizeValue(f, f.Default); err != nil { return f, FieldError{Field:f.Key, Code:"INVALID_DEFAULT", Cause:"default value does not match the field type"} }
	}
	return f, nil
}

func validBounds(f Field) bool {
	var minimum, maximum *big.Rat
	if len(f.Min) > 0 {
		var ok bool
		minimum, ok = new(big.Rat).SetString(string(f.Min))
		if !ok { return false }
	}
	if len(f.Max) > 0 {
		var ok bool
		maximum, ok = new(big.Rat).SetString(string(f.Max))
		if !ok { return false }
	}
	return minimum == nil || maximum == nil || minimum.Cmp(maximum) <= 0
}

func fieldTypeCompatible(child, parent Field) bool {
	return child.Type == parent.Type && child.ReferenceType == parent.ReferenceType && (child.Type != TypeChoice && child.Type != TypeMultiChoice || equalOptions(child.Options, parent.Options))
}

func equalOptions(a, b []Option) bool {
	if len(a) != len(b) { return false }
	values := map[string]bool{}
	for _, option := range a { values[option.Value] = true }
	for _, option := range b { if !values[option.Value] { return false } }
	return true
}

func validateLayout(recordType RecordType, fields map[string]Field) error {
	if len(recordType.Form.Sections)>20||len(recordType.Form.Tabs)>20||len(recordType.Form.RoleViews)>50||len(recordType.List.RoleViews)>50||len(recordType.List.Columns)<1||len(recordType.List.Columns)>50||len(recordType.List.SearchFields)<1||len(recordType.List.SearchFields)>20||len(recordType.List.Filters)>20{return fmt.Errorf("%w: form and list layouts exceed configured limits",ErrInvalid)}
	seen := map[string]bool{}
	sections := map[string]bool{}
	for _, section := range recordType.Form.Sections {
		if !fieldKey.MatchString(section.Key) || !text(strings.TrimSpace(section.Label), 1, 100) || section.Columns < 1 || section.Columns > 4 || len(section.Fields) > 100 { return fmt.Errorf("%w: invalid form section", ErrInvalid) }
		if section.Labels != nil {
			if _, err := validateLabels(section.Labels, true); err != nil { return fmt.Errorf("%w: form section labels must use pl and en", ErrInvalid) }
		}
		sections[section.Key] = true
		for _, key := range section.Fields {
			field, ok := fields[key]
			if !ok || !field.Active && !field.System || seen[key] { return fmt.Errorf("%w: form layout references missing, retired, or repeated field %q", ErrInvalid, key) }
			seen[key] = true
		}
	}
	tabs := map[string]bool{}
	for _, key := range recordType.Form.Tabs {
		if !sections[key] || tabs[key] { return fmt.Errorf("%w: form tab references a missing or repeated section", ErrInvalid) }
		tabs[key] = true
	}
	for key, columns := range recordType.Form.RoleViews {
		if !validRoleViewKey(key)||len(columns)>100 { return ErrInvalid }
		for _, field := range columns { if _, ok := fields[field]; !ok { return fmt.Errorf("%w: role view references unknown field %q", ErrInvalid, field) } }
	}
	if recordType.Key == "incident" && recordType.List.DefaultSort != "newest" && recordType.List.DefaultSort != "oldest" && recordType.List.DefaultSort != "priority" || recordType.Key == "ci" && recordType.List.DefaultSort != "newest" && recordType.List.DefaultSort != "oldest" { return fmt.Errorf("%w: unsupported list default sort", ErrInvalid) }
	for _, key := range append(append([]string{}, recordType.List.Columns...), append(recordType.List.SearchFields, recordType.List.Filters...)...) {
		if _, ok := fields[key]; !ok { return fmt.Errorf("%w: list layout references unknown field %q", ErrInvalid, key) }
	}
	for _, key := range recordType.List.Filters {
		field := fields[key]
		if !field.System { if _, err := filterColumn(field.Type); err != nil { return FieldError{Field:key,Code:"NOT_FILTERABLE",Cause:"list filters require an indexed field type"} } }
	}
	for _, key := range recordType.List.SearchFields {
		field := fields[key]
		if field.Type != TypeText && field.Type != TypeLongText && field.Type != TypeChoice { return FieldError{Field:key,Code:"NOT_SEARCHABLE",Cause:"list search supports text and choice fields"} }
	}
	for key, columns := range recordType.List.RoleViews {
		if !validRoleViewKey(key)||len(columns)>100 { return ErrInvalid }
		for _, field := range columns { if _, ok := fields[field]; !ok { return fmt.Errorf("%w: role view references unknown field %q", ErrInvalid, field) } }
	}
	return nil
}

func validRoleViewKey(key string) bool {
	if fieldKey.MatchString(key) { return true }
	parts := strings.Split(key, ".")
	if len(parts) < 2 { return false }
	for _, part := range parts {
		if !fieldKey.MatchString(part) { return false }
	}
	return true
}

func validateDependencies(fields []Field, all map[string]Field) error {
	graph := map[string][]string{}
	for _, f := range fields {
		for _, expression := range []*Expression{f.VisibleWhen, f.RequiredWhen} {
			if expression == nil { continue }
			refs, err := validateExpression(*expression, all, 0, new(int))
			if err != nil { return FieldError{Field: f.Key, Code:"INVALID_RULE", Cause:err.Error()} }
			for _, ref := range refs {
				if _, ok := all[ref]; !ok { return FieldError{Field:f.Key, Code:"UNKNOWN_RULE_FIELD", Cause:"rule references an unknown field"} }
				if ref == f.Key { return FieldError{Field:f.Key, Code:"RULE_CYCLE", Cause:"a field cannot depend on itself"} }
				if _, isCustom := all[ref]; isCustom { graph[f.Key] = append(graph[f.Key], ref) }
			}
		}
	}
	state := map[string]uint8{}
	var visit func(string) bool
	visit = func(key string) bool {
		if state[key] == 1 { return false }
		if state[key] == 2 { return true }
		state[key] = 1
		for _, dependency := range graph[key] { if !visit(dependency) { return false } }
		state[key] = 2
		return true
	}
	keys := make([]string, 0, len(graph)); for key := range graph { keys = append(keys, key) }; sort.Strings(keys)
	for _, key := range keys { if !visit(key) { return FieldError{Field:key, Code:"RULE_CYCLE", Cause:"field dependency rules contain a cycle"} } }
	return nil
}

func validateExpression(expression Expression, fields map[string]Field, depth int, count *int) ([]string, error) {
	*count++
	if depth > 8 || *count > 64 { return nil, fmt.Errorf("rule exceeds depth or size limit") }
	op := strings.ToUpper(expression.Op)
	if op == "AND" || op == "OR" {
		if expression.Field != "" || len(expression.Value) != 0 { return nil, fmt.Errorf("logical rules cannot include a field value") }
		if len(expression.Children) < 1 || len(expression.Children) > 16 { return nil, fmt.Errorf("AND/OR requires 1–16 conditions") }
		refs := []string{}
		for _, child := range expression.Children { values, err := validateExpression(child, fields, depth+1, count); if err != nil { return nil, err }; refs = append(refs, values...) }
		return refs, nil
	}
	if len(expression.Children) != 0 { return nil, fmt.Errorf("comparison rules cannot include child rules") }
	field, ok := fields[expression.Field]
	if !ok { return nil, fmt.Errorf("rule references an unknown field") }
	if op == "IS_EMPTY" || op == "IS_NOT_EMPTY" {
		if len(expression.Value) != 0 { return nil, fmt.Errorf("empty checks cannot include a comparison value") }
		return []string{expression.Field}, nil
	}
	if op != "EQ" && op != "NE" && op != "GT" && op != "GTE" && op != "LT" && op != "LTE" && op != "CONTAINS" { return nil, fmt.Errorf("unsupported rule operation") }
	if len(expression.Value) == 0 { return nil, fmt.Errorf("rule value is required") }
	if _, err := NormalizeValue(field, expression.Value); err != nil { return nil, fmt.Errorf("rule comparison does not match field type") }
	if (op == "GT" || op == "GTE" || op == "LT" || op == "LTE") && field.Type != TypeInteger && field.Type != TypeDecimal && field.Type != TypeDate && field.Type != TypeDateTime { return nil, fmt.Errorf("ordered comparison requires a numeric or date field") }
	if op == "CONTAINS" && field.Type != TypeText && field.Type != TypeLongText && field.Type != TypeMultiChoice { return nil, fmt.Errorf("contains requires text or multi-choice") }
	return []string{expression.Field}, nil
}

func NormalizeValue(field Field, value json.RawMessage) (json.RawMessage, error) {
	trimmed := bytes.TrimSpace(value)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) { return nil, ErrInvalid }
	var out any
	switch field.Type {
	case TypeText, TypeLongText:
		var textValue string
		if json.Unmarshal(trimmed, &textValue) != nil { return nil, ErrInvalid }
		max := field.MaxLength; if max == 0 { if field.Type == TypeLongText { max = 100000 } else { max = 4000 } }
		if !text(textValue, 0, max) { return nil, ErrInvalid }
		if field.Required && strings.TrimSpace(textValue) == "" { return nil, ErrInvalid }
		out = textValue
	case TypeBoolean:
		var value bool; if json.Unmarshal(trimmed, &value) != nil { return nil, ErrInvalid }; out = value
	case TypeInteger:
		var n json.Number
		if json.Unmarshal(trimmed, &n) != nil {
			var textValue string;if json.Unmarshal(trimmed,&textValue)!=nil{return nil,ErrInvalid};n=json.Number(textValue)
		}
		value, err := n.Int64(); if err != nil { return nil, ErrInvalid }
		if !withinNumericBounds(field, big.NewRat(value, 1)) { return nil, ErrInvalid }
		out = value
	case TypeDecimal:
		var n json.Number
		if json.Unmarshal(trimmed, &n) != nil {
			var textValue string;if json.Unmarshal(trimmed,&textValue)!=nil{return nil,ErrInvalid};n=json.Number(textValue)
		}
		canonical, err := canonicalDecimal(n.String(), field.Precision, field.Scale)
		if err != nil { return nil, err }
		rational, ok := new(big.Rat).SetString(canonical)
		if !ok || !withinNumericBounds(field, rational) {
			return nil, ErrInvalid
		}
		out = json.Number(canonical)
	case TypeDate:
		var value string; if json.Unmarshal(trimmed, &value) != nil { return nil, ErrInvalid }
		if _, err := time.Parse("2006-01-02", value); err != nil { return nil, ErrInvalid }
		out = value
	case TypeDateTime:
		var value string; if json.Unmarshal(trimmed, &value) != nil { return nil, ErrInvalid }
		parsed, err := time.Parse(time.RFC3339Nano, value); if err != nil { return nil, ErrInvalid }
		out = parsed.UTC().Truncate(time.Microsecond).Format(time.RFC3339Nano)
	case TypeChoice:
		var value string; if json.Unmarshal(trimmed, &value) != nil || !hasOption(field.Options, value) { return nil, ErrInvalid }; out = value
	case TypeMultiChoice:
		var values []string; if json.Unmarshal(trimmed, &values) != nil || len(values) > len(field.Options) { return nil, ErrInvalid }
		seen := map[string]bool{}; for _, value := range values { if !hasOption(field.Options, value) || seen[value] { return nil, ErrInvalid }; seen[value] = true }
		if field.Required && len(values) == 0 { return nil, ErrInvalid }
		sort.Strings(values); out = values
	case TypeReference:
		var value string
		if json.Unmarshal(trimmed, &value) != nil || len(value) == 0 || len(value) > 20 || strings.Trim(value, "0123456789") != "" || value[0] == '0' { return nil, ErrInvalid }
		if _, err := strconv.ParseUint(value, 10, 64); err != nil { return nil, ErrInvalid }
		out = value
	default:
		return nil, ErrInvalid
	}
	return json.Marshal(out)
}

func canonicalDecimal(input string, precision, scale int) (string, error) {
	if strings.HasPrefix(input, "+") || strings.ContainsAny(input, "eE") { return "", ErrInvalid }
	if _, ok := new(big.Rat).SetString(input); !ok { return "", ErrInvalid }
	parts := strings.Split(input, ".")
	if len(parts) > 2 { return "", ErrInvalid }
	whole := parts[0]
	sign := ""
	if strings.HasPrefix(whole, "-") { sign, whole = "-", strings.TrimPrefix(whole, "-") }
	wholeDigits := len(strings.TrimLeft(whole, "0"))
	fraction := ""
	if len(parts) == 2 { fraction = parts[1] }
	if wholeDigits > precision-scale || len(fraction) > scale { return "", ErrInvalid }
	whole = strings.TrimLeft(whole, "0")
	if whole == "" { whole = "0" }
	fraction = strings.TrimRight(fraction, "0")
	canonical := whole
	if fraction != "" { canonical += "." + fraction }
	if sign != "" && canonical != "0" { canonical = sign + canonical }
	return canonical, nil
}

func withinNumericBounds(field Field, value *big.Rat) bool {
	for raw, compare := range map[string]bool{string(field.Min): true, string(field.Max): false} {
		if raw == "" { continue }
		bound, ok := new(big.Rat).SetString(raw); if !ok { return false }
		if compare && value.Cmp(bound) < 0 || !compare && value.Cmp(bound) > 0 { return false }
	}
	return true
}

func hasOption(options []Option, value string) bool { for _, option := range options { if option.Value == value { return true } }; return false }

func NormalizeValues(fields []Field, input, existing map[string]json.RawMessage, base map[string]json.RawMessage) (map[string]json.RawMessage, error) {
	if existing == nil { existing = map[string]json.RawMessage{} }
	if base == nil { base = map[string]json.RawMessage{} }
	result := make(map[string]json.RawMessage, len(existing)+len(input))
	for key, value := range existing { result[key] = append(json.RawMessage{}, value...) }
	active := map[string]bool{}
	for _, field := range fields {
		if !field.Active { continue }
		active[field.Key] = true
		if field.System { continue }
		values := make(map[string]json.RawMessage, len(base)+len(result)+len(input))
		for key, value := range base { values[key] = value }
		for key, value := range result { values[key] = value }
		for key, value := range input { values[key] = value }
		visible := field.VisibleWhen == nil || Evaluate(*field.VisibleWhen, values)
		required := field.Required || field.RequiredWhen != nil && Evaluate(*field.RequiredWhen, values)
		value, supplied := input[field.Key]
		if supplied && bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			if required && visible { return nil, FieldError{Field:field.Key, Code:"REQUIRED", Cause:"field is required"} }
			delete(result, field.Key)
			continue
		}
		if !supplied {
			if _, exists := result[field.Key]; exists { continue }
			if field.Default != nil { value = field.Default; supplied = true }
		}
		if !supplied {
			if required && visible { return nil, FieldError{Field:field.Key, Code:"REQUIRED", Cause:"field is required"} }
			continue
		}
		validationField := field
		validationField.Required = required && visible
		normalized, err := NormalizeValue(validationField, value)
		if err != nil { return nil, FieldError{Field:field.Key, Code:"INVALID_VALUE", Cause:"value does not match the published field definition"} }
		result[field.Key] = normalized
		if required && visible && isEmptyValue(normalized) { return nil, FieldError{Field:field.Key, Code:"REQUIRED", Cause:"field is required"} }
	}
	for key := range input {
		if !active[key] { return nil, FieldError{Field:key, Code:"UNKNOWN_FIELD", Cause:"field is not active in the published definition"} }
	}
	return result, nil
}

func isEmptyValue(value json.RawMessage) bool {
	trimmed := bytes.TrimSpace(value)
	return len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) || bytes.Equal(trimmed, []byte(`""`)) || bytes.Equal(trimmed, []byte("[]"))
}

func Evaluate(expression Expression, values map[string]json.RawMessage) bool {
	op := strings.ToUpper(expression.Op)
	if op == "AND" { for _, child := range expression.Children { if !Evaluate(child, values) { return false } }; return true }
	if op == "OR" { for _, child := range expression.Children { if Evaluate(child, values) { return true } }; return false }
	left, ok := values[expression.Field]
	if op == "IS_EMPTY" { return !ok || len(bytes.TrimSpace(left)) == 0 || bytes.Equal(bytes.TrimSpace(left), []byte("null")) }
	if op == "IS_NOT_EMPTY" { return !Evaluate(Expression{Op:"IS_EMPTY", Field:expression.Field}, values) }
	if !ok { return false }
	if op == "EQ" { return jsonEqual(left, expression.Value) }
	if op == "NE" { return !jsonEqual(left, expression.Value) }
	var a,b string
	if json.Unmarshal(left,&a)==nil && json.Unmarshal(expression.Value,&b)==nil {
		if leftTime, leftErr := time.Parse(time.RFC3339Nano,a); leftErr == nil {
			if rightTime, rightErr := time.Parse(time.RFC3339Nano,b); rightErr == nil {
				return compareOrder(op,leftTime.Compare(rightTime))
			}
		}
		return compareOrder(op,strings.Compare(a,b))
	}
	if leftNumber, ok := rawNumber(left); ok {
		if rightNumber, rightOK := rawNumber(expression.Value); rightOK { return compareOrder(op,leftNumber.Cmp(rightNumber)) }
	}
	var leftText,rightText string
	if json.Unmarshal(left,&leftText)==nil && json.Unmarshal(expression.Value,&rightText)==nil && op=="CONTAINS" { return strings.Contains(leftText,rightText) }
	if op == "CONTAINS" { var items []string; var needle string; if json.Unmarshal(left,&items)==nil && json.Unmarshal(expression.Value,&needle)==nil { for _, item := range items { if item==needle{return true} } } }
	return false
}

type RuleTestResult struct {
	Valid  bool `json:"valid"`
	Result bool `json:"result"`
}

func TestRule(configuration Configuration, typeKey string, expression Expression, input map[string]json.RawMessage) (RuleTestResult, error) {
	if err := ValidateConfiguration(configuration); err != nil { return RuleTestResult{}, err }
	schema, err := configuration.Schema(typeKey, 0)
	if err != nil { return RuleTestResult{}, err }
	schema = enrichSchema(schema, configuration)
	fields := make(map[string]Field, len(schema.Fields))
	values := make(map[string]json.RawMessage, len(input))
	for _, field := range schema.Fields { fields[field.Key] = field }
	if _, err := validateExpression(expression, fields, 0, new(int)); err != nil { return RuleTestResult{}, FieldError{Code:"INVALID_RULE",Cause:"rule does not match the selected record type"} }
	for key, raw := range input {
		field, ok := fields[key]
		if !ok { return RuleTestResult{}, FieldError{Field:key,Code:"UNKNOWN_FIELD",Cause:"test input includes an unknown field"} }
		normalized, err := NormalizeValue(field, raw)
		if err != nil { return RuleTestResult{}, FieldError{Field:key,Code:"INVALID_TEST_VALUE",Cause:"test value does not match the field type"} }
		values[key] = normalized
	}
	return RuleTestResult{Valid:true,Result:Evaluate(expression,values)},nil
}

func compareOrder(op string, order int) bool {
	switch op { case "GT": return order>0; case "GTE": return order>=0; case "LT": return order<0; case "LTE": return order<=0 }
	return false
}

func rawNumber(raw json.RawMessage) (*big.Rat,bool) {
	trimmed:=bytes.TrimSpace(raw);if len(trimmed)==0{return nil,false}
	value,ok:=new(big.Rat).SetString(string(trimmed));return value,ok
}

func jsonEqual(a,b json.RawMessage) bool {
	if left,ok:=rawNumber(a);ok { if right,rightOK:=rawNumber(b);rightOK{return left.Cmp(right)==0} }
	var left,right any
	if json.Unmarshal(a,&left)!=nil || json.Unmarshal(b,&right)!=nil { return false }
	x,_:=json.Marshal(left); y,_:=json.Marshal(right); return bytes.Equal(x,y)
}

