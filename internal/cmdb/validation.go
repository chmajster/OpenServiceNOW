package cmdb

import (
	"encoding/json"
	"fmt"
	"github.com/chmajster/OpenServiceNOW/internal/assets"
	"math"
	"net"
	"net/url"
	"strconv"
	"strings"
)

type Attribute struct {
	Key       string   `json:"key"`
	Label     string   `json:"label"`
	Type      string   `json:"type"`
	Required  bool     `json:"required"`
	MaxLength int      `json:"max_length,omitempty"`
	Min       float64  `json:"min"`
	Max       float64  `json:"max"`
	Options   []string `json:"options,omitempty"`
}
type Class struct {
	Key        string      `json:"key"`
	Label      string      `json:"label"`
	Attributes []Attribute `json:"attributes"`
}

type RelationPair struct {
	Source string `json:"source"`
	Target string `json:"target"`
}

type RelationDefinition struct {
	Key   string         `json:"key"`
	Pairs []RelationPair `json:"pairs"`
}

type Contract struct {
	Classes                   []Class              `json:"classes"`
	CIFields                  []Attribute          `json:"ci_fields"`
	Statuses                  []string             `json:"statuses"`
	Environments              []string             `json:"environments"`
	RelationTypes             []RelationDefinition `json:"relation_types"`
	CIImportFields            []string             `json:"ci_import_fields"`
	CIImportRequiredFields    []string             `json:"ci_import_required_fields"`
	CIImportIdentityKeys      []string             `json:"ci_import_identity_keys"`
	AssetImportFields         []string             `json:"asset_import_fields"`
	AssetImportRequiredFields []string             `json:"asset_import_required_fields"`
	AssetImportIdentityKeys   []string             `json:"asset_import_identity_keys"`
	MaxCSVBytes               int                  `json:"max_csv_bytes"`
	MaxCSVRows                int                  `json:"max_csv_rows"`
	MaxCSVColumns             int                  `json:"max_csv_columns"`
}

var Classes = []Class{
	{"PHYSICAL_SERVER", "Serwer fizyczny", []Attribute{{Key: "manufacturer", Label: "Producent", Type: "TEXT", MaxLength: 160}, {Key: "model", Label: "Model", Type: "TEXT", MaxLength: 160}, {Key: "cpu_cores", Label: "Rdzenie CPU", Type: "INTEGER", Min: 1, Max: 4096}, {Key: "memory_gb", Label: "Pamięć RAM (GB)", Type: "INTEGER", Min: 1, Max: 1048576}, {Key: "os", Label: "System operacyjny", Type: "TEXT", MaxLength: 160}, {Key: "management_ip", Label: "Adres zarządzania", Type: "IP"}}},
	{"VM", "Maszyna wirtualna", []Attribute{{Key: "vcpu", Label: "vCPU", Type: "INTEGER", Min: 1, Max: 1024}, {Key: "memory_gb", Label: "Pamięć RAM (GB)", Type: "INTEGER", Min: 1, Max: 1048576}, {Key: "os", Label: "System operacyjny", Type: "TEXT", MaxLength: 160}, {Key: "ip_address", Label: "Adres IP", Type: "IP"}}},
	{"NETWORK_DEVICE", "Urządzenie sieciowe", []Attribute{{Key: "vendor", Label: "Producent", Type: "TEXT", MaxLength: 160}, {Key: "model", Label: "Model", Type: "TEXT", MaxLength: 160}, {Key: "management_ip", Label: "Adres zarządzania", Type: "IP"}, {Key: "port_count", Label: "Liczba portów", Type: "INTEGER", Min: 1, Max: 65535}}},
	{"APPLICATION", "Aplikacja", []Attribute{{Key: "version", Label: "Wersja aplikacji", Type: "TEXT", MaxLength: 160}, {Key: "url", Label: "Adres aplikacji", Type: "URL"}}},
	{"SERVICE", "Usługa", []Attribute{{Key: "tier", Label: "Ważność usługi", Type: "SELECT", Options: []string{"CRITICAL", "HIGH", "NORMAL"}}, {Key: "availability_target", Label: "Docelowa dostępność (%)", Type: "NUMBER", Min: 0, Max: 100}}},
}

var statuses = []string{"PLANNED", "ACTIVE", "MAINTENANCE", "RETIRED"}
var environments = []string{"DEV", "TEST", "STAGING", "PROD"}

var relationDefinitions = []RelationDefinition{
	{Key: "RUNS_ON", Pairs: []RelationPair{{Source: "APPLICATION", Target: "VM"}}},
	{Key: "HOSTED_ON", Pairs: []RelationPair{{Source: "VM", Target: "PHYSICAL_SERVER"}}},
	{Key: "DEPENDS_ON", Pairs: []RelationPair{{Source: "SERVICE", Target: "APPLICATION"}, {Source: "SERVICE", Target: "SERVICE"}, {Source: "APPLICATION", Target: "APPLICATION"}}},
	{Key: "CONNECTS_TO", Pairs: []RelationPair{{Source: "NETWORK_DEVICE", Target: "NETWORK_DEVICE"}, {Source: "NETWORK_DEVICE", Target: "PHYSICAL_SERVER"}, {Source: "NETWORK_DEVICE", Target: "VM"}}},
}

// Schema exposes the same validation and import contract used by the services.
// Clients render from this response instead of maintaining their own field rules.
func Schema() Contract {
	classKeys := make([]string, 0, len(Classes))
	for _, class := range Classes {
		classKeys = append(classKeys, class.Key)
	}
	return Contract{
		Classes: Classes,
		CIFields: []Attribute{
			{Key: "identifier", Label: "Identyfikator", Type: "TEXT", Required: true, MaxLength: 100},
			{Key: "name", Label: "Nazwa", Type: "TEXT", Required: true, MaxLength: 160},
			{Key: "class", Label: "Klasa", Type: "SELECT", Required: true, Options: classKeys},
			{Key: "status", Label: "Stan operacyjny", Type: "SELECT", Required: true, Options: statuses},
			{Key: "owner_id", Label: "Opiekun techniczny", Type: "USER_REFERENCE", Required: true},
			{Key: "environment", Label: "Środowisko", Type: "SELECT", Required: true, Options: environments},
			{Key: "location", Label: "Lokalizacja", Type: "TEXT", MaxLength: 160},
		},
		Statuses:                   statuses,
		Environments:               environments,
		RelationTypes:              relationDefinitions,
		CIImportFields:             ImportFields("CI"),
		CIImportRequiredFields:     []string{"identifier", "name", "class", "status", "owner_id", "environment"},
		CIImportIdentityKeys:       []string{"identifier"},
		AssetImportFields:          ImportFields("ASSET"),
		AssetImportRequiredFields:  []string{"asset_tag", "name", "owner_id", "state", "currency"},
		AssetImportIdentityKeys:    []string{"asset_tag", "serial_number"},
		MaxCSVBytes:                MaxCSVBytes,
		MaxCSVRows:                 MaxCSVRows,
		MaxCSVColumns:              MaxCSVColumns,
	}
}

func ClassDefinition(key string) (Class, bool) {
	for _, c := range Classes {
		if c.Key == key {
			return c, true
		}
	}
	return Class{}, false
}
func Status(s string) bool {
	for _, value := range statuses {
		if value == s {
			return true
		}
	}
	return false
}
func Environment(s string) bool {
	for _, value := range environments {
		if value == s {
			return true
		}
	}
	return false
}
func Normalize(f Fields) (Fields, error) {
	f.Identifier = strings.ToLower(strings.TrimSpace(f.Identifier))
	f.Name = strings.TrimSpace(f.Name)
	f.Location = strings.TrimSpace(f.Location)
	c, ok := ClassDefinition(f.Class)
	if !ok || !assets.Identifier(f.Identifier) || !assets.Text(f.Name, 1, 160) || f.OwnerID == 0 || !Status(f.Status) || !Environment(f.Environment) || !assets.Text(f.Location, 0, 160) {
		return f, ErrInvalid
	}
	if f.Attributes == nil {
		f.Attributes = map[string]json.RawMessage{}
	}
	if len(f.Attributes) > len(c.Attributes) {
		return f, ErrInvalid
	}
	for _, rule := range c.Attributes {
		if rule.Required {
			if _, ok := f.Attributes[rule.Key]; !ok {
				return f, fmt.Errorf("%w: wymagany atrybut %s", ErrInvalid, rule.Key)
			}
		}
	}
	for key, raw := range f.Attributes {
		found := false
		for _, rule := range c.Attributes {
			if rule.Key != key {
				continue
			}
			found = true
			if err := validateAttribute(rule, raw); err != nil {
				return f, fmt.Errorf("%w: atrybut %s", err, key)
			}
			var value any
			decoder := json.NewDecoder(strings.NewReader(string(raw)))
			decoder.UseNumber()
			if err := decoder.Decode(&value); err != nil {
				return f, ErrInvalid
			}
			normalized, err := json.Marshal(value)
			if err != nil {
				return f, ErrInvalid
			}
			f.Attributes[key] = normalized
		}
		if !found {
			return f, fmt.Errorf("%w: niedozwolony atrybut %s dla klasy %s", ErrInvalid, key, f.Class)
		}
	}
	return f, nil
}
func validateAttribute(rule Attribute, raw json.RawMessage) error {
	switch rule.Type {
	case "INTEGER", "NUMBER":
		var n json.Number
		if json.Unmarshal(raw, &n) != nil || len(raw) == 0 || raw[0] == '"' {
			return ErrInvalid
		}
		v, err := strconv.ParseFloat(n.String(), 64)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) || v < rule.Min || v > rule.Max {
			return ErrInvalid
		}
		if rule.Type == "INTEGER" {
			if _, err = n.Int64(); err != nil {
				return ErrInvalid
			}
		}
	default:
		var s string
		if json.Unmarshal(raw, &s) != nil {
			return ErrInvalid
		}
		switch rule.Type {
		case "TEXT":
			if !assets.Text(s, 1, rule.MaxLength) {
				return ErrInvalid
			}
		case "IP":
			if net.ParseIP(s) == nil {
				return ErrInvalid
			}
		case "URL":
			u, err := url.Parse(s)
			if err != nil || !assets.Text(s, 1, 2048) || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil {
				return ErrInvalid
			}
		case "SELECT":
			for _, v := range rule.Options {
				if v == s {
					return nil
				}
			}
			return ErrInvalid
		default:
			return ErrInvalid
		}
	}
	return nil
}
func RelationType(kind, source, target string) bool {
	for _, definition := range relationDefinitions {
		if definition.Key != kind {
			continue
		}
		for _, pair := range definition.Pairs {
			if pair.Source == source && pair.Target == target {
				return true
			}
		}
	}
	return false
}
