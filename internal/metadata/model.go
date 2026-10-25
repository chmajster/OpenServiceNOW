package metadata

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"
)

var (
	ErrInvalid  = errors.New("invalid metadata configuration or value")
	ErrNotFound = errors.New("metadata configuration not found")
	ErrConflict = errors.New("metadata configuration version conflict")
)

type Actor struct {
	TenantID, UserID uint64
	Permissions      map[string]bool
	RequestID        string
	IPAddress        string
}

func (a Actor) Has(permission string) bool { return a.Permissions[permission] }

type Option struct {
	Value  string            `json:"value"`
	Labels map[string]string `json:"labels"`
}

type Expression struct {
	Op       string            `json:"op"`
	Field    string            `json:"field,omitempty"`
	Value    json.RawMessage   `json:"value,omitempty"`
	Children []Expression      `json:"children,omitempty"`
}

type Field struct {
	Key           string            `json:"key"`
	Labels        map[string]string `json:"labels"`
	Description   map[string]string `json:"description,omitempty"`
	Type          string            `json:"type"`
	MaxLength     int               `json:"max_length,omitempty"`
	Precision     int               `json:"precision,omitempty"`
	Scale         int               `json:"scale,omitempty"`
	Min           json.RawMessage   `json:"min,omitempty"`
	Max           json.RawMessage   `json:"max,omitempty"`
	Default       json.RawMessage   `json:"default,omitempty"`
	Required      bool              `json:"required"`
	Unique        bool              `json:"unique,omitempty"`
	Options       []Option          `json:"options,omitempty"`
	ReferenceType string            `json:"reference_type,omitempty"`
	ParentKey     string            `json:"parent_key,omitempty"`
	Active        bool              `json:"active"`
	VisibleWhen   *Expression       `json:"visible_when,omitempty"`
	RequiredWhen  *Expression       `json:"required_when,omitempty"`
	System        bool              `json:"system,omitempty"`
}

type FormSection struct {
	Key     string   `json:"key"`
	Label   string   `json:"label"`
	Labels  map[string]string `json:"labels,omitempty"`
	Fields  []string `json:"fields"`
	Columns int      `json:"columns"`
}

type FormLayout struct {
	Sections []FormSection `json:"sections"`
	Tabs     []string      `json:"tabs"`
	RoleViews map[string][]string `json:"role_views,omitempty"`
}

type ListLayout struct {
	Columns        []string          `json:"columns"`
	DefaultSort    string            `json:"default_sort"`
	SearchFields   []string          `json:"search_fields"`
	Filters        []string          `json:"filters"`
	RoleViews      map[string][]string `json:"role_views,omitempty"`
}

type RecordType struct {
	Key         string   `json:"key"`
	Labels      map[string]string `json:"labels"`
	Parent      string   `json:"parent,omitempty"`
	Fields      []Field  `json:"fields"`
	Form        FormLayout `json:"form"`
	List        ListLayout `json:"list"`
}

type Organization struct {
	Name          string `json:"name"`
	DefaultLocale string `json:"default_locale"`
	TimeZone      string `json:"time_zone"`
	DateFormat    string `json:"date_format"`
	NumberFormat  string `json:"number_format"`
	Currency      string `json:"currency"`
	BrandColor    string `json:"brand_color"`
	LogoURL       string `json:"logo_url,omitempty"`
}

type Configuration struct {
	Organization   Organization              `json:"organization"`
	RecordTypes    map[string]RecordType      `json:"record_types"`
	Dictionaries   map[string][]Option         `json:"dictionaries"`
	PriorityMatrix [3][3]int                  `json:"priority_matrix"`
}

type Version struct {
	Version   uint64         `json:"version,string"`
	State     string         `json:"state"`
	CreatedBy uint64         `json:"created_by,string"`
	CreatedAt time.Time      `json:"created_at"`
	PublishedAt *time.Time   `json:"published_at,omitempty"`
	Config    Configuration  `json:"config"`
}

type Head struct {
	Version          uint64        `json:"version,string"`
	DraftVersion     uint64        `json:"draft_version,string,omitempty"`
	PublishedVersion uint64        `json:"published_version,string,omitempty"`
	Draft            Configuration `json:"draft"`
	Published        Configuration `json:"published"`
}

type Change struct {
	Key      string          `json:"key"`
	Kind     string          `json:"kind"`
	Count    uint64          `json:"count,string"`
	Examples []string        `json:"examples,omitempty"`
	Message  string          `json:"message"`
}

type Impact struct {
	CanPublish bool     `json:"can_publish"`
	Changes    []Change `json:"changes"`
}

type Schema struct {
	TypeKey       string       `json:"type_key"`
	Version       uint64       `json:"version,string"`
	Fields        []Field      `json:"fields"`
	Form          FormLayout   `json:"form"`
	List          ListLayout   `json:"list"`
	Configuration Configuration `json:"configuration,omitempty"`
}

type FieldError struct {
	Field string `json:"field"`
	Code  string `json:"code"`
	Cause string `json:"message"`
}

func (e FieldError) Error() string { return fmt.Sprintf("%s: %s", e.Field, e.Cause) }
func (e FieldError) Unwrap() error { return ErrInvalid }

var fieldKey = regexp.MustCompile(`^[a-z][a-z0-9_]{0,47}$`)
var currencyCode = regexp.MustCompile(`^[A-Z]{3}$`)
var brandColor = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

const (
	TypeText       = "TEXT"
	TypeLongText   = "LONG_TEXT"
	TypeBoolean    = "BOOLEAN"
	TypeInteger    = "INTEGER"
	TypeDecimal    = "DECIMAL"
	TypeDate       = "DATE"
	TypeDateTime   = "DATETIME"
	TypeChoice     = "CHOICE"
	TypeMultiChoice = "MULTI_CHOICE"
	TypeReference  = "REFERENCE"
)

var referenceTypes = map[string]bool{
	"user": true, "group": true, "incident": true, "request": true,
	"ci": true, "asset": true,
}

func DefaultConfiguration(tenantName string) Configuration {
	return Configuration{
		Organization: Organization{Name: tenantName, DefaultLocale: "pl", TimeZone: "UTC", DateFormat: "YYYY-MM-DD", NumberFormat: "1,234.56", Currency: "USD", BrandColor: "#0F766E"},
		RecordTypes: map[string]RecordType{
			"incident": {Key: "incident", Labels: map[string]string{"pl": "Incydent", "en": "Incident"}, Fields: []Field{}, Form: FormLayout{Sections: []FormSection{}, Tabs: []string{}}, List: ListLayout{Columns: []string{"number", "title", "state", "priority"}, DefaultSort: "newest", SearchFields: []string{"number", "title"}, Filters: []string{"state", "category"}}},
			"ci": {Key: "ci", Labels: map[string]string{"pl": "Element konfiguracji", "en": "Configuration item"}, Fields: []Field{}, Form: FormLayout{Sections: []FormSection{}, Tabs: []string{}}, List: ListLayout{Columns: []string{"identifier", "name", "class", "status"}, DefaultSort: "newest", SearchFields: []string{"identifier", "name"}, Filters: []string{"class", "status", "environment"}}},
		},
		Dictionaries: map[string][]Option{
			"incident.category": {
				{Value: "hardware", Labels: map[string]string{"pl": "Sprzęt", "en": "Hardware"}},
				{Value: "software", Labels: map[string]string{"pl": "Oprogramowanie", "en": "Software"}},
				{Value: "network", Labels: map[string]string{"pl": "Sieć", "en": "Network"}},
				{Value: "access", Labels: map[string]string{"pl": "Dostęp", "en": "Access"}},
				{Value: "other", Labels: map[string]string{"pl": "Inne", "en": "Other"}},
			},
			"incident.subcategory.hardware": {{Value: "computer", Labels: map[string]string{"pl": "Komputer", "en": "Computer"}}, {Value: "peripheral", Labels: map[string]string{"pl": "Urządzenie peryferyjne", "en": "Peripheral"}}, {Value: "other", Labels: map[string]string{"pl": "Inne", "en": "Other"}}},
			"incident.subcategory.software": {{Value: "application", Labels: map[string]string{"pl": "Aplikacja", "en": "Application"}}, {Value: "operating_system", Labels: map[string]string{"pl": "System operacyjny", "en": "Operating system"}}, {Value: "other", Labels: map[string]string{"pl": "Inne", "en": "Other"}}},
			"incident.subcategory.network": {{Value: "connectivity", Labels: map[string]string{"pl": "Połączenie", "en": "Connectivity"}}, {Value: "vpn", Labels: map[string]string{"pl": "VPN", "en": "VPN"}}, {Value: "other", Labels: map[string]string{"pl": "Inne", "en": "Other"}}},
			"incident.subcategory.access": {{Value: "account", Labels: map[string]string{"pl": "Konto", "en": "Account"}}, {Value: "permissions", Labels: map[string]string{"pl": "Uprawnienia", "en": "Permissions"}}, {Value: "other", Labels: map[string]string{"pl": "Inne", "en": "Other"}}},
			"incident.subcategory.other": {{Value: "other", Labels: map[string]string{"pl": "Inne", "en": "Other"}}},
			"incident.state": {{Value: "NEW", Labels: map[string]string{"pl": "Nowy", "en": "New"}}, {Value: "IN_PROGRESS", Labels: map[string]string{"pl": "W trakcie", "en": "In progress"}}, {Value: "ON_HOLD", Labels: map[string]string{"pl": "Wstrzymany", "en": "On hold"}}, {Value: "RESOLVED", Labels: map[string]string{"pl": "Rozwiązany", "en": "Resolved"}}, {Value: "CLOSED", Labels: map[string]string{"pl": "Zamknięty", "en": "Closed"}}, {Value: "CANCELLED", Labels: map[string]string{"pl": "Anulowany", "en": "Cancelled"}}},
			"incident.resolution_code": {{Value: "FIXED", Labels: map[string]string{"pl": "Naprawiono", "en": "Fixed"}}, {Value: "WORKAROUND", Labels: map[string]string{"pl": "Zastosowano obejście", "en": "Workaround"}}, {Value: "DUPLICATE", Labels: map[string]string{"pl": "Duplikat", "en": "Duplicate"}}, {Value: "NOT_REPRODUCIBLE", Labels: map[string]string{"pl": "Nie odtworzono", "en": "Not reproducible"}}},
			"incident.hold_reason": {{Value: "AWAITING_USER", Labels: map[string]string{"pl": "Oczekiwanie na zgłaszającego", "en": "Awaiting requester"}}, {Value: "AWAITING_VENDOR", Labels: map[string]string{"pl": "Oczekiwanie na dostawcę", "en": "Awaiting vendor"}}, {Value: "AWAITING_CHANGE", Labels: map[string]string{"pl": "Oczekiwanie na zmianę", "en": "Awaiting change"}}, {Value: "OTHER", Labels: map[string]string{"pl": "Inny powód", "en": "Other reason"}}},
			"ci.environment": {{Value: "DEV", Labels: map[string]string{"pl": "Deweloperskie", "en": "Development"}}, {Value: "TEST", Labels: map[string]string{"pl": "Testowe", "en": "Test"}}, {Value: "STAGING", Labels: map[string]string{"pl": "Przedprodukcyjne", "en": "Staging"}}, {Value: "PROD", Labels: map[string]string{"pl": "Produkcyjne", "en": "Production"}}},
			"ci.status": {{Value: "PLANNED", Labels: map[string]string{"pl": "Planowany", "en": "Planned"}}, {Value: "ACTIVE", Labels: map[string]string{"pl": "Aktywny", "en": "Active"}}, {Value: "MAINTENANCE", Labels: map[string]string{"pl": "Konserwacja", "en": "Maintenance"}}, {Value: "RETIRED", Labels: map[string]string{"pl": "Wycofany", "en": "Retired"}}},
		},
		PriorityMatrix: [3][3]int{{1, 2, 3}, {2, 3, 4}, {3, 4, 5}},
	}
}

// WithDefaults upgrades configuration documents written before newer incident
// settings were introduced without changing their published version.
func WithDefaults(configuration Configuration) Configuration {
	defaults := DefaultConfiguration(configuration.Organization.Name)
	if configuration.RecordTypes == nil {
		configuration.RecordTypes = defaults.RecordTypes
	}
	if configuration.Dictionaries == nil {
		configuration.Dictionaries = defaults.Dictionaries
	} else {
		for key, options := range defaults.Dictionaries {
			if _, exists := configuration.Dictionaries[key]; !exists {
				configuration.Dictionaries[key] = options
			}
		}
	}
	if configuration.PriorityMatrix == [3][3]int{} {
		configuration.PriorityMatrix = defaults.PriorityMatrix
	}
	return configuration
}

var builtInFields = map[string][]Field{
	"incident": {
		{Key: "number", Labels: map[string]string{"pl": "Numer", "en": "Number"}, Type: TypeText, MaxLength: 32, Active: true, System: true},
		{Key: "title", Labels: map[string]string{"pl": "Tytuł", "en": "Title"}, Type: TypeText, MaxLength: 200, Required: true, Active: true, System: true},
		{Key: "description", Labels: map[string]string{"pl": "Opis", "en": "Description"}, Type: TypeLongText, MaxLength: 20000, Required: true, Active: true, System: true},
		{Key: "affected_user_id", Labels: map[string]string{"pl": "Osoba dotknięta", "en": "Affected user"}, Type: TypeReference, ReferenceType: "user", Required: true, Active: true, System: true},
		{Key: "category", Labels: map[string]string{"pl": "Kategoria", "en": "Category"}, Type: TypeChoice, Required: true, Active: true, System: true},
		{Key: "subcategory", Labels: map[string]string{"pl": "Podkategoria", "en": "Subcategory"}, Type: TypeChoice, Required: true, Active: true, System: true},
		{Key: "impact", Labels: map[string]string{"pl": "Wpływ", "en": "Impact"}, Type: TypeInteger, Min: json.RawMessage("1"), Max: json.RawMessage("3"), Required: true, Active: true, System: true},
		{Key: "urgency", Labels: map[string]string{"pl": "Pilność", "en": "Urgency"}, Type: TypeInteger, Min: json.RawMessage("1"), Max: json.RawMessage("3"), Required: true, Active: true, System: true},
		{Key: "priority", Labels: map[string]string{"pl": "Priorytet", "en": "Priority"}, Type: TypeInteger, Min: json.RawMessage("1"), Max: json.RawMessage("5"), Required: true, Active: true, System: true},
		{Key: "state", Labels: map[string]string{"pl": "Stan", "en": "State"}, Type: TypeChoice, Required: true, Active: true, System: true},
	},
	"ci": {
		{Key: "identifier", Labels: map[string]string{"pl": "Identyfikator", "en": "Identifier"}, Type: TypeText, MaxLength: 100, Required: true, Unique: true, Active: true, System: true},
		{Key: "name", Labels: map[string]string{"pl": "Nazwa", "en": "Name"}, Type: TypeText, MaxLength: 160, Required: true, Active: true, System: true},
		{Key: "class", Labels: map[string]string{"pl": "Klasa", "en": "Class"}, Type: TypeChoice, Required: true, Active: true, System: true},
		{Key: "status", Labels: map[string]string{"pl": "Stan", "en": "Status"}, Type: TypeChoice, Required: true, Active: true, System: true},
		{Key: "owner_id", Labels: map[string]string{"pl": "Właściciel", "en": "Owner"}, Type: TypeReference, ReferenceType: "user", Required: true, Active: true, System: true},
		{Key: "environment", Labels: map[string]string{"pl": "Środowisko", "en": "Environment"}, Type: TypeChoice, Required: true, Active: true, System: true},
		{Key: "location", Labels: map[string]string{"pl": "Lokalizacja", "en": "Location"}, Type: TypeText, MaxLength: 160, Active: true, System: true},
	},
}

func (c Configuration) Schema(typeKey string, version uint64) (Schema, error) {
	t, ok := c.RecordTypes[typeKey]
	if !ok {
		return Schema{}, ErrNotFound
	}
	fields := append([]Field{}, builtInFields[typeKey]...)
	fields = append(fields, t.Fields...)
	return Schema{TypeKey: typeKey, Version: version, Fields: fields, Form: t.Form, List: t.List}, nil
}
