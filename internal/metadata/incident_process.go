package metadata

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

// IncidentProcess is published atomically with the organization's dictionaries.
// Stable keys and explicit template versions make configuration changes auditable.
type IncidentProcess struct {
	DefaultGroupID      string                   `json:"default_group_id"`
	AssignmentRules     []IncidentAssignmentRule `json:"assignment_rules"`
	Templates           []IncidentTemplate       `json:"templates"`
	Transitions         map[string][]string      `json:"transitions"`
	AutoCloseHours      int                      `json:"auto_close_hours"`
	ReopenHours         int                      `json:"reopen_hours"`
	RequesterCanReopen  bool                     `json:"requester_can_reopen"`
	CloseRequiredFields []string                 `json:"close_required_fields"`
}

type IncidentAssignmentRule struct {
	Key         string          `json:"key"`
	Name        string          `json:"name"`
	Active      bool            `json:"active"`
	GroupID     string          `json:"group_id"`
	ServiceID   string          `json:"service_id"`
	Category    string          `json:"category"`
	Location    string          `json:"location"`
	Priority    int             `json:"priority"`
	Contains    string          `json:"contains"`
	CustomField string          `json:"custom_field"`
	CustomValue json.RawMessage `json:"custom_value,omitempty"`
}

type IncidentTemplate struct {
	Key          string                     `json:"key"`
	Version      uint64                     `json:"version,string"`
	Labels       map[string]string          `json:"labels"`
	Retired      bool                       `json:"retired"`
	Title        string                     `json:"title"`
	Description  string                     `json:"description"`
	Category     string                     `json:"category"`
	Subcategory  string                     `json:"subcategory"`
	Impact       int                        `json:"impact"`
	Urgency      int                        `json:"urgency"`
	ServiceID    string                     `json:"service_id"`
	Location     string                     `json:"location"`
	CustomFields map[string]json.RawMessage `json:"custom_fields,omitempty"`
}

func DefaultIncidentProcess() IncidentProcess {
	return IncidentProcess{AssignmentRules: []IncidentAssignmentRule{}, Templates: []IncidentTemplate{},
		Transitions:         map[string][]string{"NEW": {"IN_PROGRESS", "ON_HOLD", "CANCELLED"}, "IN_PROGRESS": {"ON_HOLD", "RESOLVED", "CANCELLED"}, "ON_HOLD": {"IN_PROGRESS", "RESOLVED", "CANCELLED"}, "RESOLVED": {"CLOSED", "IN_PROGRESS"}, "CLOSED": {"IN_PROGRESS"}, "CANCELLED": {}},
		CloseRequiredFields: []string{"resolution_code", "resolution_description"}}
}

func canonicalID(value string, optional bool) bool {
	if value == "" {
		return optional
	}
	id, err := strconv.ParseUint(value, 10, 64)
	return err == nil && id > 0 && strconv.FormatUint(id, 10) == value
}

func validateIncidentProcess(c Configuration) error {
	p := c.IncidentProcess
	if p == nil {
		return nil
	}
	if !canonicalID(p.DefaultGroupID, true) || p.AutoCloseHours < 0 || p.AutoCloseHours > 8760 || p.ReopenHours < 0 || p.ReopenHours > 8760 || len(p.AssignmentRules) > 100 || len(p.Templates) > 100 {
		return fmt.Errorf("%w: incident process limits", ErrInvalid)
	}
	defaults := DefaultIncidentProcess()
	for state, targets := range p.Transitions {
		allowed, ok := defaults.Transitions[state]
		if !ok {
			return fmt.Errorf("%w: unsupported incident state", ErrInvalid)
		}
		seen := map[string]bool{}
		for _, target := range targets {
			found := false
			for _, v := range allowed {
				found = found || v == target
			}
			if !found || seen[target] {
				return fmt.Errorf("%w: unsupported or repeated incident transition", ErrInvalid)
			}
			seen[target] = true
		}
	}
	if p.AutoCloseHours > 0 {
		found := false
		for _, v := range p.Transitions["RESOLVED"] {
			found = found || v == "CLOSED"
		}
		if !found {
			return fmt.Errorf("%w: auto closure requires RESOLVED to CLOSED", ErrInvalid)
		}
	}
	keys := map[string]bool{}
	for _, r := range p.AssignmentRules {
		if !fieldKey.MatchString(r.Key) || keys[r.Key] || !text(strings.TrimSpace(r.Name), 1, 160) || !canonicalID(r.GroupID, false) || !canonicalID(r.ServiceID, true) || r.Priority < 0 || r.Priority > 5 || !text(r.Location, 0, 160) || !text(r.Contains, 0, 200) {
			return fmt.Errorf("%w: assignment rule %q", ErrInvalid, r.Key)
		}
		keys[r.Key] = true
		if r.Category != "" && !activeDictionaryValue(c, "incident.category", r.Category) {
			return fmt.Errorf("%w: assignment category", ErrInvalid)
		}
		if r.CustomField != "" {
			found := false
			for _, f := range c.RecordTypes["incident"].Fields {
				if f.Key == r.CustomField && f.Active {
					found = true
					if _, err := NormalizeValue(f, r.CustomValue); err != nil {
						return fmt.Errorf("%w: assignment condition %s", ErrInvalid, r.CustomField)
					}
				}
			}
			if !found || !json.Valid(r.CustomValue) || len(r.CustomValue) > 4000 {
				return fmt.Errorf("%w: assignment custom field", ErrInvalid)
			}
		}
	}
	keys = map[string]bool{}
	for _, t := range p.Templates {
		if !fieldKey.MatchString(t.Key) || keys[t.Key] || t.Version == 0 || !text(t.Title, 1, 200) || !text(t.Description, 1, 20000) || t.Impact < 1 || t.Impact > 3 || t.Urgency < 1 || t.Urgency > 3 || !canonicalID(t.ServiceID, true) || !text(t.Location, 0, 160) {
			return fmt.Errorf("%w: incident template %q", ErrInvalid, t.Key)
		}
		keys[t.Key] = true
		for key, value := range t.CustomFields {
			found := false
			for _, f := range c.RecordTypes["incident"].Fields {
				if f.Key == key && f.Active {
					found = true
					if _, err := NormalizeValue(f, value); err != nil {
						return fmt.Errorf("%w: template custom field %s", ErrInvalid, key)
					}
				}
			}
			if !found {
				return fmt.Errorf("%w: template custom field %s", ErrInvalid, key)
			}
		}

		if _, err := validateLabels(t.Labels, true); err != nil {
			return err
		}
		if !t.Retired && (!activeDictionaryValue(c, "incident.category", t.Category) || !activeDictionaryValue(c, "incident.subcategory."+t.Category, t.Subcategory)) {
			return fmt.Errorf("%w: incident template category", ErrInvalid)
		}
	}
	allowed := map[string]bool{"title": true, "description": true, "service_id": true, "location": true, "resolution_code": true, "resolution_description": true}
	for _, f := range c.RecordTypes["incident"].Fields {
		if f.Active {
			allowed["custom."+f.Key] = true
		}
	}
	seen := map[string]bool{}
	for _, key := range p.CloseRequiredFields {
		if !allowed[key] || seen[key] {
			return fmt.Errorf("%w: closure field %q", ErrInvalid, key)
		}
		seen[key] = true
	}
	return nil
}

func activeDictionaryValue(c Configuration, key, value string) bool {
	for _, o := range c.Dictionaries[key] {
		if o.Value == value && !o.Retired {
			return true
		}
	}
	return false
}

func validateTemplateVersions(previous, next Configuration) error {
	if previous.IncidentProcess == nil || next.IncidentProcess == nil {
		return nil
	}
	for _, old := range previous.IncidentProcess.Templates {
		for _, candidate := range next.IncidentProcess.Templates {
			if old.Key != candidate.Key {
				continue
			}
			left, right := old, candidate
			left.Retired = false
			right.Retired = false
			if candidate.Version < old.Version || !reflect.DeepEqual(left, right) && candidate.Version <= old.Version {
				return fmt.Errorf("%w: changed template %s requires a new version", ErrInvalid, old.Key)
			}
		}
	}
	return nil
}
