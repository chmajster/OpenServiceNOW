package cmdb

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"github.com/chmajster/OpenServiceNOW/internal/assets"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"
)

const MaxCSVBytes = 1 << 20
const MaxCSVRows = 500
const MaxCSVColumns = 40

type ImportConfig struct {
	Target      string            `json:"target"`
	IdentityKey string            `json:"identity_key"`
	Mapping     map[string]string `json:"mapping"`
}
type CSVRow struct {
	Number   int
	Identity string
	Values   map[string]string
	Error    string
}

func ImportFields(target string) []string {
	if target == "ASSET" {
		return []string{"asset_tag", "name", "owner_id", "serial_number", "vendor", "model", "location", "state", "purchase_date", "purchase_cost", "currency", "warranty_until"}
	}
	out := []string{"identifier", "name", "class", "status", "owner_id", "environment", "location", "support_group_id", "criticality", "lifecycle"}
	seen := map[string]bool{}
	for _, c := range Classes {
		for _, a := range c.Attributes {
			key := "attributes." + a.Key
			if !seen[key] {
				out = append(out, key)
				seen[key] = true
			}
		}
	}
	return out
}
func ValidateImportConfig(c ImportConfig) error {
	if c.Target != "CI" && c.Target != "ASSET" || c.Target == "CI" && c.IdentityKey != "identifier" || c.Target == "ASSET" && c.IdentityKey != "asset_tag" && c.IdentityKey != "serial_number" {
		return ErrInvalid
	}
	required := []string{"name", "owner_id", c.IdentityKey}
	if c.Target == "CI" {
		required = append(required, "identifier", "class", "status", "environment")
	} else {
		required = append(required, "asset_tag", "state", "currency")
	}
	allowed := map[string]bool{}
	for _, key := range ImportFields(c.Target) {
		allowed[key] = true
	}
	seen := map[string]bool{}
	for key, header := range c.Mapping {
		if !allowed[key] || !assets.Text(header, 1, 160) || seen[header] {
			return fmt.Errorf("%w: niedozwolone lub powtórzone mapowanie", ErrInvalid)
		}
		seen[header] = true
	}
	for _, key := range required {
		if c.Mapping[key] == "" {
			return fmt.Errorf("%w: wymagane mapowanie %s", ErrInvalid, key)
		}
	}
	return nil
}

// ReadCSV is bounded and accepts UTF-8 RFC4180 comma-separated files only.
// Mapped fields update records; unmapped optional fields retain their values.
func ReadCSV(content []byte, c ImportConfig) ([]CSVRow, error) {
	if len(content) == 0 || len(content) > MaxCSVBytes || !utf8.Valid(content) {
		return nil, fmt.Errorf("%w: CSV UTF-8, limit 1 MiB", ErrInvalid)
	}
	if err := ValidateImportConfig(c); err != nil {
		return nil, err
	}
	r := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(content), "\ufeff")))
	headers, err := r.Read()
	if err != nil || len(headers) > MaxCSVColumns {
		return nil, fmt.Errorf("%w: niepoprawny nagłówek CSV (maks. 40 kolumn)", ErrInvalid)
	}
	index := map[string]int{}
	for i, h := range headers {
		if !assets.Text(h, 1, 160) {
			return nil, ErrInvalid
		}
		if _, ok := index[h]; ok {
			return nil, fmt.Errorf("%w: powtórzona kolumna", ErrInvalid)
		}
		index[h] = i
	}
	for _, h := range c.Mapping {
		if _, ok := index[h]; !ok {
			return nil, fmt.Errorf("%w: brak kolumny %s", ErrInvalid, h)
		}
	}
	out := []CSVRow{}
	identities := map[string]int{}
	for {
		values, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%w: niepoprawny CSV", ErrInvalid)
		}
		if len(out) >= MaxCSVRows {
			return nil, fmt.Errorf("%w: maksymalnie 500 wierszy", ErrInvalid)
		}
		row := CSVRow{Number: len(out) + 2, Values: map[string]string{}}
		for _, v := range values {
			if len(v) > 4000 {
				row.Error = "Pole przekracza 4000 bajtów"
			}
		}
		for field, h := range c.Mapping {
			row.Values[field] = strings.TrimSpace(values[index[h]])
		}
		row.Identity = strings.ToLower(row.Values[c.IdentityKey])
		if c.IdentityKey == "serial_number" {
			row.Identity = strings.ToUpper(row.Values[c.IdentityKey])
		}
		if row.Identity == "" || len(row.Identity) > 128 {
			row.Error = "Brak poprawnego klucza identyfikacji"
		}
		if previous, ok := identities[row.Identity]; ok {
			row.Error = "Powtórzony klucz w pliku"
			out[previous].Error = row.Error
		} else {
			identities[row.Identity] = len(out)
		}
		out = append(out, row)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: brak wierszy danych", ErrInvalid)
	}
	return out, nil
}
func csvFields(c ImportConfig, row CSVRow, old any) (json.RawMessage, error) {
	raw, err := json.Marshal(old)
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		fields = map[string]json.RawMessage{}
	}
	var attrs map[string]json.RawMessage
	if c.Target == "CI" {
		if json.Unmarshal(fields["attributes"], &attrs) != nil || attrs == nil {
			attrs = map[string]json.RawMessage{}
		}
	}
	for key, value := range row.Values {
		if strings.HasPrefix(key, "attributes.") {
			name := strings.TrimPrefix(key, "attributes.")
			if value == "" {
				delete(attrs, name)
				continue
			}
			var class string
			json.Unmarshal(fields["class"], &class)
			if v, ok := row.Values["class"]; ok {
				class = v
			}
			def, _ := ClassDefinition(class)
			found := false
			for _, rule := range def.Attributes {
				if rule.Key == name {
					found = true
					if rule.Type == "INTEGER" || rule.Type == "NUMBER" {
						if !json.Valid([]byte(value)) {
							return nil, ErrInvalid
						}
						attrs[name] = json.RawMessage(value)
					} else {
						attrs[name], _ = json.Marshal(value)
					}
				}
			}
			if !found {
				return nil, fmt.Errorf("%w: pole %s nie należy do klasy", ErrInvalid, name)
			}
			continue
		}
		if key == "owner_id" || key == "support_group_id" {
			n, err := strconv.ParseUint(value, 10, 64)
			if err != nil || n == 0 || strconv.FormatUint(n, 10) != value {
				return nil, fmt.Errorf("%w: owner_id jest ciągiem dziesiętnym", ErrInvalid)
			}
		}
		fields[key], _ = json.Marshal(value)
	}
	if c.Target == "CI" {
		fields["attributes"], err = json.Marshal(attrs)
		if err != nil {
			return nil, ErrInvalid
		}
		raw, err = json.Marshal(fields)
		if err != nil {
			return nil, ErrInvalid
		}
		var f Fields
		if json.Unmarshal(raw, &f) != nil {
			return nil, ErrInvalid
		}
		f, err = Normalize(f)
		if err != nil {
			return nil, err
		}
		return json.Marshal(f)
	}
	raw, _ = json.Marshal(fields)
	var f assets.Fields
	if json.Unmarshal(raw, &f) != nil {
		return nil, ErrInvalid
	}
	f, err = assets.Normalize(f)
	if err != nil {
		return nil, err
	}
	return json.Marshal(f)
}
