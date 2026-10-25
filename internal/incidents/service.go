package incidents

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/chmajster/OpenServiceNOW/internal/activity"
	"github.com/chmajster/OpenServiceNOW/internal/attachments"
	"github.com/chmajster/OpenServiceNOW/internal/audit"
	"github.com/chmajster/OpenServiceNOW/internal/events"
	"github.com/chmajster/OpenServiceNOW/internal/metadata"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
)

type OutboxAppender interface {
	Append(context.Context, database.Executor, events.Input) (uint64, bool, error)
}
type Service struct {
	repo      *Repository
	tx        *database.TxManager
	allocator *workrecords.Allocator
	work      *workrecords.Service
	audit     *audit.MySQLRepository
	outbox    OutboxAppender
	activity  *activity.Repository
	now       func() time.Time
	files     *attachments.Service
	metadata  *metadata.Service
}

func NewService(repo *Repository, tx *database.TxManager, allocator *workrecords.Allocator, work *workrecords.Service, auditRepo *audit.MySQLRepository, outbox OutboxAppender, activityRepo *activity.Repository) *Service {
	return &Service{repo: repo, tx: tx, allocator: allocator, work: work, audit: auditRepo, outbox: outbox, activity: activityRepo, now: time.Now}
}
func (s *Service) WithAttachments(files *attachments.Service) *Service { s.files = files; return s }
func (s *Service) WithMetadata(service *metadata.Service) *Service     { s.metadata = service; return s }

var requestKeyPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{8,128}$`)

func (s *Service) Create(ctx context.Context, a Actor, input Fields, key string) (Record, bool, error) {
	if !a.Has("create") || (!a.Has("read") && !a.Has("read_own")) {
		return Record{}, false, ErrForbidden
	}
	if !requestKeyPattern.MatchString(key) {
		return Record{}, false, fmt.Errorf("%w: Idempotency-Key wymaga 8–128 znaków ASCII (litery, cyfry, . _ : -)", ErrInvalid)
	}
	if input.AffectedUserID == 0 {
		input.AffectedUserID = a.UserID
	}
	if input.AffectedUserID != a.UserID && !a.Has("read") {
		return Record{}, false, ErrForbidden
	}
	var err error
	if s.metadata == nil {
		input, err = normalizeFields(input)
	} else {
		input, err = normalizeFieldsBase(input)
	}
	if err != nil {
		return Record{}, false, err
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		return Record{}, false, err
	}
	digest := sha256.Sum256(encoded)
	var item Record
	created := false
	err = s.tx.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(exec database.Executor) error {
		current, authErr := currentIncidentActor(ctx, exec, a)
		if authErr != nil {
			return authErr
		}
		a = current
		if !a.Has("create") || (!a.Has("read") && !a.Has("read_own")) || input.AffectedUserID != a.UserID && !a.Has("read") {
			return ErrForbidden
		}
		id, err := s.repo.Reserve(ctx, exec, a, key, digest)
		if err != nil {
			return err
		}
		if id != 0 {
			item, err = s.repo.Get(ctx, exec, a.TenantID, id, false)
			if err != nil {
				return err
			}
			if !readable(a, item) {
				return ErrNotFound
			}
			if err = s.loadCustomFields(ctx, exec, a, &item); err != nil {
				return err
			}
			return nil
		}
		configVersion, config, err := s.tenantMetadata(ctx, exec, a)
		if err != nil {
			return err
		}
		if err = validateTemplate(config, input); err != nil {
			return err
		}
		if err = validateIncidentService(ctx, exec, a, input.ServiceID); err != nil {
			return err
		}
		input, err = normalizeFieldsWith(input, incidentCategories(config), config.PriorityMatrix)
		if err != nil {
			return err
		}
		customFields, schema, err := s.prepareCustomFields(ctx, exec, a, 0, input.CustomFields, baseValueMap(input), input.CustomFieldVersion)
		if err != nil {
			return err
		}
		input.CustomFields = customFields
		if err := s.repo.ValidatePeople(ctx, exec, a, a.UserID, input.AffectedUserID); err != nil {
			return err
		}
		number, err := s.allocator.NextInTx(ctx, exec, a.TenantID, "incident", "INC", 7)
		if err != nil {
			return err
		}
		now := s.now().UTC().Truncate(time.Microsecond)
		priority, _ := CalculatePriorityWith(config.PriorityMatrix, input.Impact, input.Urgency)
		item = Record{ServiceID: input.ServiceID, Location: input.Location, TemplateKey: input.TemplateKey, TemplateVersion: input.TemplateVersion, ProcessVersion: configVersion, ProcessPolicy: *config.IncidentProcess, TenantID: a.TenantID, Number: number, Title: input.Title, Description: input.Description, ReporterID: a.UserID, AffectedUserID: input.AffectedUserID, Category: input.Category, Subcategory: input.Subcategory, Impact: input.Impact, Urgency: input.Urgency, Priority: priority, State: "NEW", Version: 1, CustomFieldVersion: configVersion, CustomFields: customFields, CreatedAt: now, UpdatedAt: now}
		item.ID, err = s.repo.Insert(ctx, exec, item)
		if err != nil {
			return err
		}
		if err = s.saveCustomFields(ctx, exec, a, item.ID, configVersion, customFields, schema); err != nil {
			return err
		}
		proposal := matchAssignment(item.ProcessPolicy, input, priority)
		if err = assignmentGroup(ctx, exec, a.TenantID, &proposal); err != nil {
			return err
		}
		item.AssignmentOrigin = proposal.RuleKey
		if _, err = exec.ExecContext(ctx, `UPDATE incidents SET assignment_origin=? WHERE tenant_id=? AND id=?`, proposal.RuleKey, a.TenantID, item.ID); err != nil {
			return err
		}
		_, err = s.work.SetAssignmentInTx(ctx, exec, workActor(a), "incident", fmt.Sprint(item.ID), 0, nil, proposal.GroupID)
		if err != nil {
			return err
		}
		item, err = s.repo.Get(ctx, exec, a.TenantID, item.ID, false)
		if err != nil {
			return err
		}
		if err = s.loadCustomFields(ctx, exec, a, &item); err != nil {
			return err
		}
		if err := s.repo.CompleteRequest(ctx, exec, a, key, item.ID); err != nil {
			return err
		}
		if err := s.recordChange(ctx, exec, a, "incident.created", nil, item, ""); err != nil {
			return err
		}
		created = true
		return nil
	})
	item.Actions = allowedActions(a, item)
	return item, created, err
}
func workActor(a Actor) workrecords.Actor {
	return workrecords.Actor{TenantID: a.TenantID, UserID: a.UserID, RequestID: a.RequestID}
}
func (s *Service) Get(ctx context.Context, a Actor, id uint64) (Record, error) {
	item, err := s.repo.Get(ctx, s.repo.db, a.TenantID, id, false)
	if err != nil {
		return Record{}, err
	}
	if !readable(a, item) {
		return Record{}, ErrNotFound
	}
	if err = s.loadCustomFields(ctx, s.repo.db, a, &item); err != nil {
		return Record{}, err
	}
	item.Actions = allowedActions(a, item)
	return item, nil
}

type mutation func(database.Executor, *Record, Actor) (string, string, error)

func (s *Service) mutate(ctx context.Context, a Actor, id, version uint64, permission string, fn mutation) (Record, error) {
	if !a.Has(permission) && permission != "reopen" {
		return Record{}, ErrForbidden
	}
	if version == 0 {
		return Record{}, fmt.Errorf("%w: wymagana dodatnia wersja", ErrInvalid)
	}
	var updated Record
	err := s.tx.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(exec database.Executor) error {
		current, authErr := currentIncidentActor(ctx, exec, a)
		if authErr != nil {
			return authErr
		}
		a = current
		if permission != "reopen" && !a.Has(permission) {
			return ErrForbidden
		}
		before, err := s.repo.Get(ctx, exec, a.TenantID, id, true)
		if err != nil {
			return err
		}
		if !readable(a, before) {
			return ErrNotFound
		}
		if err = s.loadCustomFields(ctx, exec, a, &before); err != nil {
			return err
		}
		if before.Version != version {
			return ErrConflict
		}
		updated = before
		if permission == "reopen" && !a.Has("update") && !canRequesterReopen(a, before, s.now()) {
			return ErrForbidden
		}
		eventType, reason, err := fn(exec, &updated, a)
		if err != nil {
			return err
		}
		if reflect.DeepEqual(snapshot(before), snapshot(updated)) {
			return nil
		}
		updated.Version = before.Version + 1
		updated.UpdatedAt = s.now().UTC().Truncate(time.Microsecond)
		if err := s.repo.Save(ctx, exec, updated, before.Version); err != nil {
			return err
		}
		if err := s.recordChange(ctx, exec, a, eventType, &before, updated, reason); err != nil {
			return err
		}
		updated, err = s.repo.Get(ctx, exec, a.TenantID, id, false)
		if err != nil {
			return err
		}
		if err = s.loadCustomFields(ctx, exec, a, &updated); err != nil {
			return err
		}
		return nil
	})
	updated.Actions = allowedActions(a, updated)
	return updated, err
}
func (s *Service) Update(ctx context.Context, a Actor, id uint64, input UpdateInput) (Record, error) {
	var normalized Fields
	var err error
	if s.metadata == nil {
		normalized, err = normalizeFields(input.Fields)
	} else {
		normalized, err = normalizeFieldsBase(input.Fields)
	}
	if err != nil {
		return Record{}, err
	}
	return s.mutate(ctx, a, id, input.Version, "update", func(exec database.Executor, r *Record, a Actor) (string, string, error) {
		if !editable(*r) {
			return "", "", ErrTransition
		}
		customVersion, config, err := s.tenantMetadata(ctx, exec, a)
		if err != nil {
			return "", "", err
		}
		normalized, err = normalizeFieldsWith(normalized, historicalCategories(config, *r), config.PriorityMatrix)
		if err != nil {
			return "", "", err
		}
		if normalized.AffectedUserID != r.AffectedUserID {
			if !a.Has("read") {
				return "", "", ErrForbidden
			}
			if err := s.repo.ValidatePeople(ctx, exec, a, a.UserID, normalized.AffectedUserID); err != nil {
				return "", "", err
			}
		}
		customFields, schema, err := s.prepareCustomFields(ctx, exec, a, id, normalized.CustomFields, baseValueMap(normalized), normalized.CustomFieldVersion)
		if err != nil {
			return "", "", err
		}
		if err = s.saveCustomFields(ctx, exec, a, id, schema.Version, customFields, schema); err != nil {
			return "", "", err
		}
		if normalized.Impact != r.Impact || normalized.Urgency != r.Urgency {
			r.Priority, _ = CalculatePriorityWith(config.PriorityMatrix, normalized.Impact, normalized.Urgency)
			r.PriorityOverride = nil
			r.PriorityReason = ""
		}
		if !reflect.DeepEqual(r.ServiceID, normalized.ServiceID) {
			if err = validateIncidentService(ctx, exec, a, normalized.ServiceID); err != nil {
				return "", "", err
			}
		}
		r.ServiceID = normalized.ServiceID
		r.Location = normalized.Location
		r.Title = normalized.Title
		r.Description = normalized.Description
		r.AffectedUserID = normalized.AffectedUserID
		r.Category = normalized.Category
		r.Subcategory = normalized.Subcategory
		r.Impact = normalized.Impact
		r.Urgency = normalized.Urgency
		r.CustomFields = customFields
		r.CustomFieldVersion = customVersion
		return "incident.updated", "", nil
	})
}
func (s *Service) Assign(ctx context.Context, a Actor, id uint64, input AssignmentInput) (Record, error) {
	return s.mutate(ctx, a, id, input.Version, "assign", func(exec database.Executor, r *Record, a Actor) (string, string, error) {
		if !editable(*r) {
			return "", "", ErrTransition
		}
		if (r.State == "IN_PROGRESS" || r.State == "ON_HOLD" || r.State == "RESOLVED") && input.AssignedTo == nil {
			return "", "", fmt.Errorf("%w: incydent w obsłudze musi mieć przypisanego użytkownika", ErrInvalid)
		}
		if err := s.repo.ValidateGroupMember(ctx, exec, a.TenantID, input.AssignedTo, input.AssignmentGroupID); err != nil {
			return "", "", err
		}
		input.Reason = strings.TrimSpace(input.Reason)
		if !validText(input.Reason, 0, 2000) || r.AssignmentOrigin != "" && !validText(input.Reason, 1, 2000) {
			return "", "", fmt.Errorf("%w: provide an assignment override reason", ErrInvalid)
		}
		change, err := s.work.SetAssignmentInTx(ctx, exec, workActor(a), "incident", fmt.Sprint(id), r.Assignment.Version, input.AssignedTo, input.AssignmentGroupID)
		if err != nil {
			return "", "", assignmentError(err)
		}
		r.Assignment = change.After
		r.AssignmentOrigin = "manual"
		r.AssignmentReason = input.Reason
		return "incident.assigned", input.Reason, nil
	})
}
func assignmentError(err error) error {
	if errors.Is(err, workrecords.ErrNotFound) || errors.Is(err, workrecords.ErrInvalid) {
		return fmt.Errorf("%w: przypisanie wymaga aktywnej grupy i aktywnego członkostwa użytkownika w organizacji", ErrInvalid)
	}
	return err
}
func (s *Service) Transition(ctx context.Context, a Actor, id uint64, input TransitionInput) (Record, error) {
	input.Reason = strings.TrimSpace(input.Reason)
	input.ResolutionDescription = strings.TrimSpace(input.ResolutionDescription)
	permission := transitionPermission(input.State)
	if input.State == "IN_PROGRESS" {
		permission = "reopen"
	}
	return s.mutate(ctx, a, id, input.Version, permission, func(exec database.Executor, r *Record, a Actor) (string, string, error) {
		_, config, err := s.tenantMetadata(ctx, exec, a)
		if err != nil {
			return "", "", err
		}
		if err = validateProcessTransition(a, *r, input, s.now()); err != nil {
			return "", "", err
		}
		if input.State == "CLOSED" {
			var active bool
			if err = exec.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM incident_major_cases WHERE tenant_id=? AND incident_id=? AND status IN('CANDIDATE','ACTIVE'))`, a.TenantID, id).Scan(&active); err != nil {
				return "", "", err
			}
			if active {
				return "", "", fmt.Errorf("%w: restore or demote the major incident before closing", ErrTransition)
			}
		}
		if err := validateTransitionWithReasons(*r, input, incidentOptions(config, "incident.resolution_code"), incidentOptions(config, "incident.hold_reason")); err != nil {
			return "", "", err
		}
		if input.State == "IN_PROGRESS" || input.State == "RESOLVED" {
			if err := s.repo.ValidateGroupMember(ctx, exec, a.TenantID, r.Assignment.AssignedTo, r.Assignment.AssignmentGroupID); err != nil {
				return "", "", err
			}
			if _, err := s.work.SetAssignmentInTx(ctx, exec, workActor(a), "incident", fmt.Sprint(id), r.Assignment.Version, r.Assignment.AssignedTo, r.Assignment.AssignmentGroupID); err != nil {
				return "", "", assignmentError(err)
			}
		}
		if input.State == "IN_PROGRESS" || input.State == "RESOLVED" {
			if _, err = exec.ExecContext(ctx, `UPDATE incidents SET auto_close_retry_at=NULL WHERE tenant_id=? AND id=?`, a.TenantID, id); err != nil {
				return "", "", err
			}
		}
		eventType := "incident.state.changed"
		now := s.now().UTC().Truncate(time.Microsecond)
		if input.State == "IN_PROGRESS" {
			if r.StartedAt == nil {
				r.StartedAt = &now
			}
			if r.State == "RESOLVED" || r.State == "CLOSED" {
				eventType = "incident.reopened"
				r.ResolvedAt = nil
				r.ClosedAt = nil
				r.ResolutionCode = ""
				r.ResolutionDescription = ""
			}
		}
		r.HoldReason = ""
		if input.State == "ON_HOLD" {
			r.HoldReason = input.HoldReason
		}
		if input.State == "RESOLVED" {
			r.ResolvedAt = &now
			r.ResolutionCode = input.ResolutionCode
			r.ResolutionDescription = input.ResolutionDescription
			eventType = "incident.resolved"
		}
		if input.State == "CLOSED" {
			r.ClosedAt = &now
			eventType = "incident.closed"
		}
		if input.State == "CANCELLED" {
			r.CancelledAt = &now
			r.CancellationReason = input.Reason
			eventType = "incident.cancelled"
		}
		r.State = input.State
		return eventType, input.Reason, nil
	})
}
func (s *Service) OverridePriority(ctx context.Context, a Actor, id uint64, input PriorityInput) (Record, error) {
	input.Reason = strings.TrimSpace(input.Reason)
	if !validText(input.Reason, 0, 1000) {
		return Record{}, fmt.Errorf("%w: uzasadnienie priorytetu nie może przekraczać 1000 znaków", ErrInvalid)
	}
	if input.Priority != nil && (*input.Priority < 1 || *input.Priority > 5) {
		return Record{}, fmt.Errorf("%w: priorytet musi mieć wartość 1–5", ErrInvalid)
	}
	return s.mutate(ctx, a, id, input.Version, "priority.override", func(exec database.Executor, r *Record, a Actor) (string, string, error) {
		if !editable(*r) {
			return "", "", ErrTransition
		}
		_, config, err := s.tenantMetadata(ctx, exec, a)
		if err != nil {
			return "", "", err
		}
		calculated, _ := CalculatePriorityWith(config.PriorityMatrix, r.Impact, r.Urgency)
		if input.Priority != nil && *input.Priority != calculated {
			if !validText(input.Reason, 1, 1000) {
				return "", "", fmt.Errorf("%w: podaj uzasadnienie nadpisania (1–1000 znaków)", ErrInvalid)
			}
			r.Priority = *input.Priority
			r.PriorityOverride = input.Priority
			r.PriorityReason = input.Reason
		} else {
			r.Priority = calculated
			r.PriorityOverride = nil
			r.PriorityReason = ""
		}
		return "incident.priority.changed", input.Reason, nil
	})
}
func (s *Service) recordChange(ctx context.Context, exec database.Executor, a Actor, eventType string, before *Record, after Record, reason string) error {
	var previous *Snapshot
	if before != nil {
		value := snapshot(*before)
		previous = &value
	}
	current := snapshot(after)
	event := ChangeEvent{ActorUserID: a.UserID, IncidentVersion: after.Version, OccurredAt: after.UpdatedAt, Before: previous, After: current, Reason: reason}
	nextAudit := struct {
		Snapshot
		Reason string `json:"reason,omitempty"`
	}{current, reason}
	if err := s.audit.Append(ctx, exec, audit.Event{TenantID: &a.TenantID, ActorUserID: &a.UserID, Time: after.UpdatedAt, Action: eventType, ObjectType: "incident", ObjectID: fmt.Sprint(after.ID), OldValues: previous, NewValues: nextAudit, RequestID: a.RequestID, Source: a.source(), IPAddress: a.IPAddress}); err != nil {
		return err
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	_, _, err = s.outbox.Append(ctx, exec, events.Input{TenantID: a.TenantID, Type: eventType, AggregateType: "incident", AggregateID: fmt.Sprint(after.ID), Payload: payload, IdempotencyKey: fmt.Sprintf("incident:%d:%d", after.ID, after.Version), AvailableAt: after.UpdatedAt})
	return err
}
func (s *Service) List(ctx context.Context, a Actor, f Filter) (Page, error) {
	if !a.Has("read") && !a.Has("read_own") {
		return Page{}, ErrForbidden
	}
	var err error
	if s.metadata != nil {
		_, config, e := s.tenantMetadata(ctx, s.repo.db, a)
		if e != nil {
			return Page{}, e
		}
		recordType, ok := config.RecordTypes["incident"]
		if !ok {
			return Page{}, ErrInvalid
		}
		f.SearchFields = append([]string{}, recordType.List.SearchFields...)
		f.SearchableTypes = map[string]string{}
		for _, field := range recordType.Fields {
			f.SearchableTypes[field.Key] = field.Type
		}
		err = validateFilterWith(f, config)
	} else {
		f.SearchFields = []string{"number", "title"}
		err = validateFilter(f)
	}
	if err != nil {
		return Page{}, err
	}
	page, err := s.repo.List(ctx, a, f)
	if err != nil {
		return Page{}, err
	}
	if s.metadata != nil {
		ids := make([]uint64, 0, len(page.Incidents))
		for _, item := range page.Incidents {
			ids = append(ids, item.ID)
		}
		version, _, err := s.metadata.Published(ctx, s.repo.db, a.TenantID)
		if err != nil {
			return Page{}, err
		}
		values, err := metadata.ReadValues(ctx, s.repo.db, a.TenantID, "incident", ids)
		if err != nil {
			return Page{}, err
		}
		for index := range page.Incidents {
			page.Incidents[index].CustomFieldVersion = version
			page.Incidents[index].CustomFields = values[page.Incidents[index].ID]
		}
	}
	return page, nil
}
func validateFilter(f Filter) error {
	return validateFilterWith(f, metadata.DefaultConfiguration(""))
}
func validateFilterWith(f Filter, config metadata.Configuration) error {
	if f.Limit < 1 || f.Limit > 100 || !validText(f.Query, 0, 200) || f.Priority < 0 || f.Priority > 5 {
		return ErrInvalid
	}
	if f.Sort != "newest" && f.Sort != "oldest" && f.Sort != "priority" {
		return ErrInvalid
	}
	if f.View != "all" && f.View != "mine" && f.View != "my_group" && f.View != "unassigned" {
		return ErrInvalid
	}
	if f.State != "" {
		found := false
		for _, o := range config.Dictionaries["incident.state"] {
			if o.Value == f.State {
				found = true
			}
		}
		if !found {
			return ErrInvalid
		}
	}
	if f.Category != "" {
		found := false
		for _, o := range config.Dictionaries["incident.category"] {
			if o.Value == f.Category {
				found = true
			}
		}
		if !found {
			return ErrInvalid
		}
	}
	if !f.From.IsZero() && !f.To.IsZero() && f.From.After(f.To) {
		return ErrInvalid
	}
	if f.Sort == "priority" && f.AfterID > 0 && (f.AfterPriority < 1 || f.AfterPriority > 5) {
		return ErrInvalid
	}
	if f.AfterPriority != 0 && (f.Sort != "priority" || f.AfterID == 0) {
		return ErrInvalid
	}
	return nil
}
func (s *Service) History(ctx context.Context, a Actor, id, before uint64, limit int) (HistoryPage, error) {
	if _, err := s.Get(ctx, a, id); err != nil {
		return HistoryPage{}, err
	}
	page, err := s.activity.ListVisibleBySourceEvent(ctx, a.TenantID, "incident", fmt.Sprint(id), before, limit, notesAllowed(a))
	if err != nil {
		return HistoryPage{}, err
	}
	result := HistoryPage{Entries: []HistoryEntry{}, NextBeforeEventID: page.NextBeforeID}
	for _, entry := range page.Entries {
		result.Entries = append(result.Entries, HistoryEntry{ID: entry.ID, Type: entry.Type, Summary: entry.Summary, ActorUserID: entry.ActorUserID, EventTime: entry.EventTime, Data: entry.Data, ActorName: entry.ActorName, SourceEventID: entry.SourceEventID})
	}
	query := `SELECT EXISTS(SELECT 1 FROM outbox_events WHERE tenant_id=? AND aggregate_type='incident' AND aggregate_id=? AND (status<>'DELIVERED' OR EXISTS(SELECT 1 FROM event_deliveries d WHERE d.event_id=outbox_events.id AND d.consumer_id='activity.v1' AND d.status<>'DELIVERED'))`
	if !notesAllowed(a) {
		query += ` AND (event_type IN ('incident.created','incident.updated','incident.assigned','incident.state.changed','incident.reopened','incident.resolved','incident.closed','incident.cancelled','incident.priority.changed','work.comment.added') OR JSON_UNQUOTE(JSON_EXTRACT(payload,'$.visibility'))='PUBLIC') AND event_type<>'work.work_note.added' AND COALESCE(JSON_UNQUOTE(JSON_EXTRACT(payload,'$.message_type')),'')<>'WORK_NOTE'`
	}
	err = s.repo.db.QueryRowContext(ctx, query+`)`, a.TenantID, fmt.Sprint(id)).Scan(&result.Pending)
	return result, err
}
