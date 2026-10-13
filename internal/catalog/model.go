package catalog

import (
	"encoding/json"
	"errors"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
	"strconv"
)

var (
	ErrInvalid  = errors.New("nieprawidłowe dane katalogu")
	ErrNotFound = errors.New("nie znaleziono rekordu")
	ErrConflict = errors.New("zmieniono wersję rekordu; porównaj aktualny stan ze swoją propozycją")
)

type Actor = workrecords.Principal
type Node struct {
	ID          uint64 `json:"id,string"`
	CatalogID   uint64 `json:"catalog_id,string,omitempty"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Version     uint64 `json:"version,string"`
}
type Condition struct {
	Field  string          `json:"field"`
	Equals json.RawMessage `json:"equals"`
}
type Field struct {
	Key          string     `json:"key"`
	Label        string     `json:"label"`
	Type         string     `json:"type"`
	Required     bool       `json:"required"`
	VisibleWhen  *Condition `json:"visible_when,omitempty"`
	RequiredWhen *Condition `json:"required_when,omitempty"`
	Options      []string   `json:"options,omitempty"`
}
type Step struct {
	Title        string `json:"title"`
	Instructions string `json:"instructions"`
	Required     bool   `json:"required"`
}
type Definition struct {
	Title           string  `json:"title"`
	Description     string  `json:"description"`
	Fields          []Field `json:"fields"`
	Method          string  `json:"method"`
	GroupID         uint64  `json:"group_id,string"`
	AudienceGroupID uint64  `json:"audience_group_id,string,omitempty"`
	Steps           []Step  `json:"steps"`
	WorkflowID      uint64  `json:"workflow_id,string,omitempty"`
	WorkflowVersion uint64  `json:"workflow_version,string,omitempty"`
}
type Item struct {
	ID               uint64     `json:"id,string"`
	CategoryID       uint64     `json:"category_id,string"`
	Version          uint64     `json:"version,string"`
	PublishedVersion *uint64    `json:"published_version,omitempty"`
	Draft            Definition `json:"draft"`
}

// Publication deliberately excludes fulfillment instructions and audience rules.
type Publication struct {
	ID          uint64  `json:"id,string"`
	CategoryID  uint64  `json:"category_id,string"`
	Version     uint64  `json:"version,string"`
	Title       string  `json:"title"`
	Description string  `json:"description"`
	Fields      []Field `json:"fields"`
}

func Public(id, category, version uint64, d Definition) Publication {
	return Publication{id, category, version, d.Title, d.Description, d.Fields}
}
func (i Item) MarshalJSON() ([]byte, error) {
	type plain Item
	var version *string
	if i.PublishedVersion != nil {
		s := strconv.FormatUint(*i.PublishedVersion, 10)
		version = &s
	}
	return json.Marshal(struct {
		plain
		PublishedVersion *string `json:"published_version,omitempty"`
	}{plain(i), version})
}
