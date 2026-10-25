package incidents

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/chmajster/OpenServiceNOW/internal/metadata"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
)

type AssignmentPreview struct {
	RuleKey             string  `json:"rule_key"`
	GroupID             *uint64 `json:"group_id,string"`
	GroupName           string  `json:"group_name"`
	PreservesAssignment bool    `json:"preserves_assignment"`
}

func processTransitions(r Record) []string {
	if r.ProcessPolicy.Transitions == nil {
		return transitions(r.State)
	}
	return r.ProcessPolicy.Transitions[r.State]
}

func canRequesterReopen(a Actor, r Record, now time.Time) bool {
	return r.ProcessPolicy.RequesterCanReopen && a.Has("read_own") && (r.ReporterID == a.UserID || r.AffectedUserID == a.UserID) && reopenInWindow(r, now)
}
func reopenInWindow(r Record, now time.Time) bool {
	if r.State != "RESOLVED" && r.State != "CLOSED" {
		return false
	}
	return r.ProcessPolicy.ReopenHours == 0 || r.ResolvedAt != nil && !now.After(r.ResolvedAt.Add(time.Duration(r.ProcessPolicy.ReopenHours)*time.Hour))
}

func validateProcessTransition(a Actor, r Record, input TransitionInput, now time.Time) error {
	found := false
	for _, state := range processTransitions(r) {
		found = found || state == input.State
	}
	if !found {
		return ErrTransition
	}
	if input.State == "IN_PROGRESS" && (r.State == "RESOLVED" || r.State == "CLOSED") && !reopenInWindow(r, now) {
		return fmt.Errorf("%w: reopen window has expired", ErrTransition)
	}
	if input.State == "CLOSED" {
		for _, key := range r.ProcessPolicy.CloseRequiredFields {
			value := ""
			switch key {
			case "title":
				value = r.Title
			case "description":
				value = r.Description
			case "service_id":
				if r.ServiceID != nil {
					value = fmt.Sprint(*r.ServiceID)
				}
			case "location":
				value = r.Location
			case "resolution_code":
				value = r.ResolutionCode
			case "resolution_description":
				value = r.ResolutionDescription
			default:
				if strings.HasPrefix(key, "custom.") {
					raw := r.CustomFields[strings.TrimPrefix(key, "custom.")]
					if len(raw) > 0 && string(raw) != "null" && string(raw) != "\"\"" {
						value = string(raw)
					}
				}
			}
			if strings.TrimSpace(value) == "" {
				return fmt.Errorf("%w: required closure field %s", ErrInvalid, key)
			}
		}
	}
	return nil
}

func matchAssignment(p metadata.IncidentProcess, f Fields, priority int) AssignmentPreview {
	for _, r := range p.AssignmentRules {
		if !r.Active || r.ServiceID != "" && (f.ServiceID == nil || r.ServiceID != fmt.Sprint(*f.ServiceID)) || r.Category != "" && r.Category != f.Category || r.Location != "" && !strings.EqualFold(r.Location, f.Location) || r.Priority > 0 && r.Priority != priority || r.Contains != "" && !strings.Contains(strings.ToLower(f.Title+"\n"+f.Description), strings.ToLower(r.Contains)) {
			continue
		}
		if r.CustomField != "" {
			var a, b any
			if json.Unmarshal(f.CustomFields[r.CustomField], &a) != nil || json.Unmarshal(r.CustomValue, &b) != nil || !reflect.DeepEqual(a, b) {
				continue
			}
		}
		id, err := strconv.ParseUint(r.GroupID, 10, 64)
		if err == nil && id > 0 {
			return AssignmentPreview{RuleKey: r.Key, GroupID: &id}
		}
	}
	if p.DefaultGroupID != "" {
		id, err := strconv.ParseUint(p.DefaultGroupID, 10, 64)
		if err == nil && id > 0 {
			return AssignmentPreview{RuleKey: "default", GroupID: &id}
		}
	}
	return AssignmentPreview{}
}

func validateIncidentService(ctx context.Context, e database.Executor, a Actor, id *uint64) error {
	if id == nil {
		return nil
	}
	if !a.Permissions["service_portfolio.read"] && !a.Permissions["service_portfolio.manage"] {
		return ErrForbidden
	}
	var found bool
	if err := e.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM service_portfolio_items WHERE tenant_id=? AND id=? AND lifecycle='ACTIVE')`, a.TenantID, *id).Scan(&found); err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("%w: service must be active in this organization", ErrInvalid)
	}
	return nil
}

func assignmentGroup(ctx context.Context, e database.Executor, tenant uint64, p *AssignmentPreview) error {
	if p.GroupID == nil {
		return nil
	}
	if err := e.QueryRowContext(ctx, "SELECT name FROM `groups` WHERE tenant_id=? AND id=? AND active=TRUE", tenant, *p.GroupID).Scan(&p.GroupName); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: assignment queue is inactive or missing", ErrInvalid)
		}
		return err
	}
	return nil
}

func (s *Service) PreviewAssignment(ctx context.Context, a Actor, input Fields, id uint64) (AssignmentPreview, error) {
	if !a.Has("assign") && !a.Permissions[metadata.ManagePermission] {
		return AssignmentPreview{}, ErrForbidden
	}
	_, config, err := s.tenantMetadata(ctx, s.repo.db, a)
	if err != nil {
		return AssignmentPreview{}, err
	}
	priority, err := CalculatePriorityWith(config.PriorityMatrix, input.Impact, input.Urgency)
	if err != nil {
		return AssignmentPreview{}, err
	}
	result := matchAssignment(*config.IncidentProcess, input, priority)
	if id > 0 {
		record, err := s.Get(ctx, a, id)
		if err != nil {
			return result, err
		}
		result.PreservesAssignment = record.Assignment.AssignedTo != nil || record.AssignmentOrigin == "manual"
	}
	return result, assignmentGroup(ctx, s.repo.db, a.TenantID, &result)
}

func (s *Service) ReapplyAssignment(ctx context.Context, a Actor, id, version uint64) (Record, error) {
	return s.mutate(ctx, a, id, version, "assign", func(e database.Executor, r *Record, a Actor) (string, string, error) {
		if !editable(*r) {
			return "", "", ErrTransition
		}
		if r.Assignment.AssignedTo != nil || r.AssignmentOrigin == "manual" {
			return "", "", fmt.Errorf("%w: manual or assigned work is preserved", ErrConflict)
		}
		_, config, err := s.tenantMetadata(ctx, e, a)
		if err != nil {
			return "", "", err
		}
		p := matchAssignment(*config.IncidentProcess, fields(*r), r.Priority)
		if err = assignmentGroup(ctx, e, a.TenantID, &p); err != nil {
			return "", "", err
		}
		change, err := s.work.SetAssignmentInTx(ctx, e, workActor(a), "incident", fmt.Sprint(id), r.Assignment.Version, nil, p.GroupID)
		if err != nil {
			return "", "", assignmentError(err)
		}
		r.Assignment = change.After
		r.AssignmentOrigin = p.RuleKey
		r.AssignmentReason = ""
		return "incident.assigned", "assignment rules", nil
	})
}

func validateTemplate(config metadata.Configuration, input Fields) error {
	if input.TemplateKey == "" {
		if input.TemplateVersion != 0 {
			return ErrInvalid
		}
		return nil
	}
	for _, t := range config.IncidentProcess.Templates {
		if t.Key == input.TemplateKey && !t.Retired && t.Version == input.TemplateVersion {
			return nil
		}
	}
	return fmt.Errorf("%w: incident template is retired or has changed", ErrConflict)
}

func historicalCategories(config metadata.Configuration, current Record) []Category {
	categories := incidentCategories(config)
	for ci := range categories {
		if categories[ci].Value != current.Category {
			continue
		}
		categories[ci].Retired = false
		for si := range categories[ci].Subcategories {
			if categories[ci].Subcategories[si].Value == current.Subcategory {
				categories[ci].Subcategories[si].Retired = false
			}
		}
	}
	return categories
}

func currentIncidentActor(ctx context.Context, e database.Executor, a Actor) (Actor, error) {
	p, err := workrecords.LockPrincipal(ctx, e, workrecords.Principal{Actor: workActor(a), IPAddress: a.IPAddress, Permissions: a.Permissions})
	if err != nil {
		return a, err
	}
	a.Permissions = p.Permissions
	return a, nil
}

func (a Actor) source() string {
	if a.Source == "worker" {
		return "worker"
	}
	return "api"
}
