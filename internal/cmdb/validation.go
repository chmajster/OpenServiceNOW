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

var Classes = []Class{
	{"PHYSICAL_SERVER", "Serwer fizyczny", []Attribute{{Key: "serial_number", Label: "Numer seryjny", Type: "TEXT", MaxLength: 128}, {Key: "manufacturer", Label: "Producent", Type: "TEXT", MaxLength: 160}, {Key: "model", Label: "Model", Type: "TEXT", MaxLength: 160}, {Key: "cpu_cores", Label: "Rdzenie CPU", Type: "INTEGER", Min: 1, Max: 4096}, {Key: "memory_gb", Label: "Pamięć RAM (GB)", Type: "INTEGER", Min: 1, Max: 1048576}, {Key: "os", Label: "System operacyjny", Type: "TEXT", MaxLength: 160}, {Key: "management_ip", Label: "Adres zarządzania", Type: "IP"}, {Key: "hostname", Label: "Hostname", Type: "HOSTNAME"}}},
	{"VM", "Maszyna wirtualna", []Attribute{{Key: "vcpu", Label: "vCPU", Type: "INTEGER", Min: 1, Max: 1024}, {Key: "memory_gb", Label: "Pamięć RAM (GB)", Type: "INTEGER", Min: 1, Max: 1048576}, {Key: "os", Label: "System operacyjny", Type: "TEXT", MaxLength: 160}, {Key: "ip_address", Label: "Adres IP", Type: "IP"}, {Key: "hostname", Label: "Hostname", Type: "HOSTNAME"}}},
	{"NETWORK_DEVICE", "Urządzenie sieciowe", []Attribute{{Key: "vendor", Label: "Producent", Type: "TEXT", MaxLength: 160}, {Key: "model", Label: "Model", Type: "TEXT", MaxLength: 160}, {Key: "management_ip", Label: "Adres zarządzania", Type: "IP"}, {Key: "serial_number", Label: "Numer seryjny", Type: "TEXT", MaxLength: 128}, {Key: "port_count", Label: "Liczba portów", Type: "INTEGER", Min: 1, Max: 65535}}},
	{"APPLICATION", "Aplikacja", []Attribute{{Key: "version", Label: "Wersja aplikacji", Type: "TEXT", MaxLength: 160}, {Key: "url", Label: "Adres aplikacji", Type: "URL"}}},
	{"DATABASE", "Baza danych", []Attribute{{Key: "engine", Label: "Silnik bazy", Type: "TEXT", MaxLength: 80}, {Key: "version", Label: "Wersja silnika", Type: "TEXT", MaxLength: 80}, {Key: "port", Label: "Port", Type: "INTEGER", Min: 1, Max: 65535}, {Key: "size_gb", Label: "Rozmiar (GB)", Type: "NUMBER", Min: 0, Max: 1099511627776}, {Key: "hostname", Label: "Hostname", Type: "HOSTNAME"}}},
	{"SERVICE", "Usługa biznesowa", []Attribute{{Key: "tier", Label: "Ważność usługi", Type: "SELECT", Options: []string{"CRITICAL", "HIGH", "NORMAL"}}, {Key: "availability_target", Label: "Docelowa dostępność (%)", Type: "NUMBER", Min: 0, Max: 100}}},
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
	return s == "PLANNED" || s == "ACTIVE" || s == "MAINTENANCE" || s == "RETIRED"
}
func Environment(s string) bool { return s == "DEV" || s == "TEST" || s == "STAGING" || s == "PROD" }
func Normalize(f Fields) (Fields, error) {
	f.Identifier = strings.ToLower(strings.TrimSpace(f.Identifier))
	f.Name = strings.TrimSpace(f.Name)
	f.Location = strings.TrimSpace(f.Location)
	if f.Criticality == "" {
		f.Criticality = "NORMAL"
	}
	if f.Lifecycle == "" {
		f.Lifecycle = "OPERATE"
		if f.Status == "PLANNED" {
			f.Lifecycle = "DESIGN"
		}
		if f.Status == "RETIRED" {
			f.Lifecycle = "RETIRE"
		}
	}
	if !oneOf(f.Criticality, "CRITICAL", "HIGH", "NORMAL", "LOW") || !oneOf(f.Lifecycle, "DESIGN", "BUILD", "OPERATE", "RETIRE") || (f.SupportGroupID != nil && *f.SupportGroupID == 0) {
		return f, ErrInvalid
	}
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
		case "HOSTNAME":
			if len(s) > 253 || s != strings.TrimSuffix(s, ".") {
				return ErrInvalid
			}
			for _, label := range strings.Split(s, ".") {
				if len(label) < 1 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
					return ErrInvalid
				}
				for _, r := range label {
					if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-') {
						return ErrInvalid
					}
				}
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
func oneOf(s string, values ...string) bool {
	for _, v := range values {
		if s == v {
			return true
		}
	}
	return false
}
func RelationType(kind, source, target string) bool {
	if _, ok := ClassDefinition(source); !ok {
		return false
	}
	if _, ok := ClassDefinition(target); !ok {
		return false
	}
	switch kind {
	case "RUNS_ON":
		return oneOf(source, "APPLICATION", "DATABASE") && oneOf(target, "VM", "PHYSICAL_SERVER")
	case "HOSTED_ON":
		return source == "VM" && target == "PHYSICAL_SERVER"
	case "DEPENDS_ON":
		return oneOf(source, "SERVICE", "APPLICATION", "DATABASE")
	case "PART_OF":
		return source == target && oneOf(source, "SERVICE", "APPLICATION", "NETWORK_DEVICE") || source == "APPLICATION" && target == "SERVICE"
	case "CONNECTS_TO":
		return source == "NETWORK_DEVICE" && oneOf(target, "NETWORK_DEVICE", "PHYSICAL_SERVER", "VM")
	}
	return false
}
