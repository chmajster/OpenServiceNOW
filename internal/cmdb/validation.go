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
	{"PHYSICAL_SERVER", "Serwer fizyczny", withAttributes(commonHardwareAttributes(), []Attribute{
		textAttribute("rack", "Szafa rack", 100), integerAttribute("rack_unit", "Jednostka rack (U)", 1, 60),
		integerAttribute("cpu_cores", "Rdzenie CPU", 1, 4096), integerAttribute("memory_gb", "Pamięć RAM (GB)", 1, 1048576),
		integerAttribute("storage_gb", "Pojemność dysków (GB)", 1, 1048576), textAttribute("os", "System operacyjny", 160), ipAttribute("management_ip", "Adres zarządzania"),
	})},
	{"VM", "Maszyna wirtualna", []Attribute{
		integerAttribute("vcpu", "vCPU", 1, 1024), integerAttribute("memory_gb", "Pamięć RAM (GB)", 1, 1048576),
		textAttribute("os", "System operacyjny", 160), ipAttribute("ip_address", "Adres IP"),
	}},
	{"NETWORK_DEVICE", "Urządzenie sieciowe", withAttributes([]Attribute{
		textAttribute("vendor", "Producent", 160), textAttribute("model", "Model", 160), textAttribute("serial_number", "Numer seryjny", 160), textAttribute("asset_tag", "Numer ewidencyjny", 100),
	}, []Attribute{
		selectAttribute("device_type", "Typ urządzenia", "ROUTER", "SWITCH", "FIREWALL", "ACCESS_POINT", "LOAD_BALANCER", "MODEM", "CONTROLLER", "OTHER"),
		textAttribute("firmware_version", "Wersja firmware", 160), ipAttribute("management_ip", "Adres zarządzania"), macAttribute(), integerAttribute("port_count", "Liczba portów", 1, 65535),
	})},
	{"WORKSTATION", "Komputer użytkownika", withAttributes(commonHardwareAttributes(), []Attribute{
		selectAttribute("form_factor", "Rodzaj komputera", "DESKTOP", "LAPTOP", "THIN_CLIENT"),
		integerAttribute("cpu_cores", "Rdzenie CPU", 1, 4096), integerAttribute("memory_gb", "Pamięć RAM (GB)", 1, 1048576),
		integerAttribute("storage_gb", "Pojemność dysku (GB)", 1, 1048576), textAttribute("os", "System operacyjny", 160), macAttribute(), ipAttribute("ip_address", "Adres IP"),
	})},
	{"PRINTER", "Drukarka", withAttributes(commonHardwareAttributes(), []Attribute{
		selectAttribute("printer_type", "Typ drukarki", "LASER", "INKJET", "THERMAL", "LABEL", "MULTIFUNCTION", "THREE_D"),
		ipAttribute("ip_address", "Adres IP"), macAttribute(),
	})},
	{"STORAGE_DEVICE", "Urządzenie pamięci masowej", withAttributes(commonHardwareAttributes(), []Attribute{
		selectAttribute("storage_type", "Rodzaj pamięci masowej", "DAS", "NAS", "SAN"),
		integerAttribute("capacity_gb", "Pojemność (GB)", 1, 1000000000),
		selectAttribute("protocol", "Protokół", "SAS", "SATA", "NVME", "FIBRE_CHANNEL", "ISCSI", "NFS", "SMB"), ipAttribute("management_ip", "Adres zarządzania"),
	})},
	{"MOBILE_DEVICE", "Urządzenie mobilne", withAttributes(commonHardwareAttributes(), []Attribute{
		selectAttribute("device_type", "Typ urządzenia", "SMARTPHONE", "TABLET"), textAttribute("os", "System operacyjny", 160),
	})},
	{"PERIPHERAL_DEVICE", "Urządzenie peryferyjne", withAttributes(commonHardwareAttributes(), []Attribute{
		selectAttribute("device_type", "Typ urządzenia", "MONITOR", "DOCK", "SCANNER", "WEBCAM", "KEYBOARD", "MOUSE", "HEADSET", "OTHER"),
		selectAttribute("connection_type", "Rodzaj połączenia", "USB", "USB_C", "BLUETOOTH", "HDMI", "DISPLAYPORT", "THUNDERBOLT", "OTHER"),
	})},
	{"UPS", "Zasilacz awaryjny UPS", withAttributes(commonHardwareAttributes(), []Attribute{
		integerAttribute("capacity_va", "Moc pozorna (VA)", 1, 1000000), ipAttribute("management_ip", "Adres zarządzania"),
	})},
	{"APPLICATION", "Aplikacja", []Attribute{
		textAttribute("version", "Wersja aplikacji", 160), {Key: "url", Label: "Adres aplikacji", Type: "URL"},
	}},
	{"SERVICE", "Usługa", []Attribute{
		selectAttribute("tier", "Ważność usługi", "CRITICAL", "HIGH", "NORMAL"), {Key: "availability_target", Label: "Docelowa dostępność (%)", Type: "NUMBER", Min: 0, Max: 100},
	}},
}

func commonHardwareAttributes() []Attribute {
	return []Attribute{
		textAttribute("manufacturer", "Producent", 160), textAttribute("model", "Model", 160),
		textAttribute("serial_number", "Numer seryjny", 160), textAttribute("asset_tag", "Numer ewidencyjny", 100),
	}
}

func withAttributes(common, specific []Attribute) []Attribute {
	attributes := make([]Attribute, 0, len(common)+len(specific))
	attributes = append(attributes, common...)
	return append(attributes, specific...)
}

func textAttribute(key, label string, maxLength int) Attribute {
	return Attribute{Key: key, Label: label, Type: "TEXT", MaxLength: maxLength}
}

func integerAttribute(key, label string, min, max float64) Attribute {
	return Attribute{Key: key, Label: label, Type: "INTEGER", Min: min, Max: max}
}

func ipAttribute(key, label string) Attribute {
	return Attribute{Key: key, Label: label, Type: "IP"}
}

func macAttribute() Attribute {
	return Attribute{Key: "mac_address", Label: "Adres MAC", Type: "MAC", MaxLength: 17}
}

func selectAttribute(key, label string, options ...string) Attribute {
	return Attribute{Key: key, Label: label, Type: "SELECT", Options: options}
}

var statuses = []string{"PLANNED", "ACTIVE", "MAINTENANCE", "RETIRED"}
var environments = []string{"DEV", "TEST", "STAGING", "PROD"}

var relationDefinitions = []RelationDefinition{
	{Key: "RUNS_ON", Pairs: []RelationPair{{Source: "APPLICATION", Target: "VM"}, {Source: "APPLICATION", Target: "PHYSICAL_SERVER"}, {Source: "APPLICATION", Target: "WORKSTATION"}}},
	{Key: "HOSTED_ON", Pairs: []RelationPair{{Source: "VM", Target: "PHYSICAL_SERVER"}}},
	{Key: "DEPENDS_ON", Pairs: []RelationPair{{Source: "SERVICE", Target: "APPLICATION"}, {Source: "SERVICE", Target: "SERVICE"}, {Source: "APPLICATION", Target: "APPLICATION"}}},
	{Key: "CONNECTS_TO", Pairs: connectivityPairs()},
}

func connectivityPairs() []RelationPair {
	classes := []string{"NETWORK_DEVICE", "PHYSICAL_SERVER", "WORKSTATION", "PRINTER", "STORAGE_DEVICE", "MOBILE_DEVICE", "PERIPHERAL_DEVICE", "UPS", "VM"}
	pairs := make([]RelationPair, 0, len(classes)*len(classes))
	for _, source := range classes {
		for _, target := range classes {
			pairs = append(pairs, RelationPair{Source: source, Target: target})
		}
	}
	return pairs
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
		case "MAC":
			address, err := net.ParseMAC(s)
			if err != nil || len(address) != 6 || len(s) > rule.MaxLength {
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
