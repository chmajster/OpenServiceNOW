package workspace

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/chmajster/OpenServiceNOW/internal/attachments"
	incidentdomain "github.com/chmajster/OpenServiceNOW/internal/incidents"
	"github.com/chmajster/OpenServiceNOW/internal/jobs"
	"github.com/chmajster/OpenServiceNOW/internal/metadata"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/requests"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
)

type PermissionLoader interface {
	ListPermissions(context.Context, uint64, uint64) ([]string, error)
}

type Service struct {
	db               *sql.DB
	tx               *database.TxManager
	jobs             *jobs.Repository
	files            *attachments.Service
	incidents        *incidentdomain.Service
	requests         *requests.Service
	permissionLoader PermissionLoader
	metadata         *metadata.Service
}

func NewService(db *sql.DB, incidentService *incidentdomain.Service, requestService *requests.Service, files *attachments.Service, permissionLoader PermissionLoader) *Service {
	return &Service{db: db, tx: database.NewTxManager(db), jobs: jobs.NewRepository(db), files: files, incidents: incidentService, requests: requestService, permissionLoader: permissionLoader}
}

func (s *Service) WithMetadata(service *metadata.Service) *Service { s.metadata = service; return s }

func (s *Service) ConfiguredFields(ctx context.Context, actor Actor) ([]FieldDefinition, error) {
	fields := availableFields()
	custom, err := s.customFilterFields(ctx, actor)
	if err != nil {
		return nil, err
	}
	fields = append(fields, custom.definitions...)
	return fields, nil
}

type customFilterConfiguration struct {
	definitions []FieldDefinition
	fields      map[string]metadata.Field
	columns     map[string]metadata.Field
}

func (s *Service) customFilterFields(ctx context.Context, actor Actor) (customFilterConfiguration, error) {
	out := customFilterConfiguration{fields: map[string]metadata.Field{}, columns: map[string]metadata.Field{}}
	if s.metadata == nil || !actor.CanReadIncidents() {
		return out, nil
	}
	schema, err := s.metadata.Schema(ctx, actor.TenantID, "incident")
	if err != nil {
		return out, err
	}
	allowed := map[string]bool{}
	for _, key := range schema.List.Filters {
		allowed[key] = true
	}
	visible := map[string]bool{}
	matchedRole := false
	hasRoleViews := len(schema.List.RoleViews) > 0
	for permission, keys := range schema.List.RoleViews {
		if actor.Has(permission) {
			matchedRole = true
			for _, key := range keys {
				visible[key] = true
			}
		}
	}
	if !matchedRole {
		for _, key := range schema.List.Columns {
			visible[key] = true
		}
	}
	for _, field := range schema.Fields {
		columnVisible := visible[field.Key]
		filterVisible := allowed[field.Key] && (!hasRoleViews || columnVisible)
		if !columnVisible && !filterVisible || !field.Active || field.System || field.VisibleWhen != nil {
			continue
		}
		key := "cf." + field.Key
		label := field.Labels["pl"]
		if label == "" {
			label = field.Labels["en"]
		}
		if label == "" {
			label = field.Key
		}
		operators := []string{}
		if filterVisible && filterableMetadataField(field.Type) {
			operators = metadataFieldOperators(field.Type)
		}
		definition := FieldDefinition{Key: key, Label: "Incydent · " + label, Type: workspaceMetadataType(field.Type), Operators: operators, ObjectType: IncidentType, FilterOnly: !columnVisible, ReferenceType: field.ReferenceType}
		for _, option := range field.Options {
			optionLabel := option.Labels["pl"]
			if optionLabel == "" {
				optionLabel = option.Labels["en"]
			}
			if optionLabel == "" {
				optionLabel = option.Value
			}
			definition.Options = append(definition.Options, FieldOption{Value: option.Value, Label: optionLabel})
		}
		out.definitions = append(out.definitions, definition)
		if len(operators) > 0 {
			out.fields[key] = field
		}
		if columnVisible {
			out.columns[key] = field
		}
	}
	return out, nil
}

func filterableMetadataField(kind string) bool {
	switch kind {
	case metadata.TypeText, metadata.TypeLongText, metadata.TypeChoice, metadata.TypeInteger, metadata.TypeDecimal, metadata.TypeBoolean, metadata.TypeDate, metadata.TypeDateTime, metadata.TypeReference:
		return true
	default:
		return false
	}
}

func workspaceMetadataType(kind string) string {
	switch kind {
	case metadata.TypeText, metadata.TypeLongText:
		return "text"
	case metadata.TypeChoice:
		return "enum"
	case metadata.TypeInteger:
		return "integer"
	case metadata.TypeDecimal:
		return "decimal"
	case metadata.TypeBoolean:
		return "boolean"
	case metadata.TypeDate:
		return "date_only"
	case metadata.TypeDateTime:
		return "datetime"
	case metadata.TypeReference:
		return "reference"
	default:
		return "text"
	}
}

func metadataFieldOperators(kind string) []string {
	switch kind {
	case metadata.TypeText, metadata.TypeLongText:
		return []string{"eq", "neq", "contains", "starts_with", "is_empty"}
	case metadata.TypeChoice:
		return []string{"eq", "neq", "in", "is_empty"}
	case metadata.TypeInteger, metadata.TypeDecimal:
		return []string{"eq", "neq", "gt", "gte", "lt", "lte", "is_empty"}
	case metadata.TypeBoolean:
		return []string{"eq", "neq", "is_empty"}
	case metadata.TypeDate, metadata.TypeDateTime:
		return []string{"before", "after", "within_last_days", "is_empty"}
	case metadata.TypeReference:
		return []string{"eq", "neq", "is_empty"}
	default:
		return nil
	}
}

func (s *Service) validateActorQuery(ctx context.Context, actor Actor, q *Query) (customFilterConfiguration, error) {
	custom, err := s.customFilterFields(ctx, actor)
	if err != nil {
		return customFilterConfiguration{}, err
	}
	if err = validateQuery(q, true, custom.definitions); err != nil {
		return customFilterConfiguration{}, err
	}
	return custom, nil
}

func (s *Service) attachCustomFields(ctx context.Context, actor Actor, items []WorkItem, columns map[string]metadata.Field) error {
	if len(columns) == 0 || len(items) == 0 {
		return nil
	}
	ids := make([]uint64, 0, len(items))
	for _, item := range items {
		if item.ObjectType == IncidentType {
			id, err := strconv.ParseUint(item.ObjectID, 10, 64)
			if err != nil {
				return ErrInvalid
			}
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	values, err := metadata.ReadValues(ctx, s.db, actor.TenantID, "incident", ids)
	if err != nil {
		return err
	}
	for index := range items {
		if items[index].ObjectType != IncidentType {
			continue
		}
		id, _ := strconv.ParseUint(items[index].ObjectID, 10, 64)
		for key := range columns {
			fieldKey := strings.TrimPrefix(key, "cf.")
			if value, ok := values[id][fieldKey]; ok {
				if items[index].CustomFields == nil {
					items[index].CustomFields = map[string]json.RawMessage{}
				}
				items[index].CustomFields[fieldKey] = value
			}
		}
	}
	return nil
}

func (s *Service) References(ctx context.Context, actor Actor, kind, query string, after uint64, limit int) (ReferencePage, error) {
	if !actor.CanUse() {
		return ReferencePage{}, ErrForbidden
	}
	if len([]rune(query)) > 100 || limit < 1 || limit > 50 {
		return ReferencePage{}, ErrInvalid
	}
	pattern := likePattern(query)
	page := ReferencePage{Items: []ReferenceOption{}}
	var rows *sql.Rows
	var err error
	switch kind {
	case "users":
		manager := actor.Has("admin.users.manage") || actor.Has("incident.read") || actor.Has("request.manage")
		sqlText := `SELECT u.id,u.display_name FROM tenant_memberships tm JOIN users u ON u.id=tm.user_id WHERE tm.tenant_id=? AND tm.active=TRUE AND u.active=TRUE AND u.id>? AND (?='' OR u.display_name LIKE ? ESCAPE '!')`
		args := []any{actor.TenantID, after, query, pattern}
		if !manager {
			sqlText += ` AND (u.id=? OR EXISTS(SELECT 1 FROM group_memberships mine JOIN ` + "`groups`" + ` active_group ON active_group.tenant_id=mine.tenant_id AND active_group.id=mine.group_id AND active_group.active=TRUE JOIN group_memberships theirs ON theirs.tenant_id=mine.tenant_id AND theirs.group_id=mine.group_id WHERE mine.tenant_id=tm.tenant_id AND mine.user_id=? AND theirs.user_id=u.id))`
			args = append(args, actor.UserID, actor.UserID)
		}
		rows, err = s.db.QueryContext(ctx, sqlText+` ORDER BY u.id LIMIT ?`, append(args, limit+1)...)
	case "groups":
		manager := actor.Has("admin.users.manage") || actor.Has("incident.assign") || actor.Has("incident.read") || actor.Has("request.manage")
		sqlText := `SELECT g.id,g.name FROM ` + "`groups`" + ` g WHERE g.tenant_id=? AND g.active=TRUE AND g.id>? AND (?='' OR g.name LIKE ? ESCAPE '!')`
		args := []any{actor.TenantID, after, query, pattern}
		if !manager {
			sqlText += ` AND EXISTS(SELECT 1 FROM group_memberships gm WHERE gm.tenant_id=g.tenant_id AND gm.group_id=g.id AND gm.user_id=?)`
			args = append(args, actor.UserID)
		}
		rows, err = s.db.QueryContext(ctx, sqlText+` ORDER BY g.id LIMIT ?`, append(args, limit+1)...)
	default:
		return ReferencePage{}, ErrInvalid
	}
	if err != nil {
		return ReferencePage{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var id uint64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return ReferencePage{}, err
		}
		page.Items = append(page.Items, ReferenceOption{ID: strconv.FormatUint(id, 10), Name: name})
	}
	if err := rows.Err(); err != nil {
		return ReferencePage{}, err
	}
	if len(page.Items) > limit {
		page.Items = page.Items[:limit]
		page.NextAfterID, _ = strconv.ParseUint(page.Items[len(page.Items)-1].ID, 10, 64)
	}
	return page, nil
}

func (s *Service) Get(ctx context.Context, actor Actor, kind, rawID string) (WorkItem, error) {
	id, err := validateID(rawID)
	if err != nil {
		return WorkItem{}, err
	}
	switch kind {
	case IncidentType:
		if s.incidents == nil || !actor.CanReadIncidents() {
			return WorkItem{}, ErrNotFound
		}
		item, err := s.incidents.Get(ctx, incidentActor(actor), id)
		if err != nil {
			return WorkItem{}, mapDomainError(err)
		}
		return incidentItem(item), nil
	case ActionType:
		if s.incidents == nil || !actor.CanReadIncidents() {
			return WorkItem{}, ErrNotFound
		}
		item, err := s.incidents.GetMajorAction(ctx, incidentActor(actor), id)
		if err != nil {
			return WorkItem{}, mapDomainError(err)
		}
		var assignmentVersion uint64
		if err = s.db.QueryRowContext(ctx, `SELECT version FROM work_assignments WHERE tenant_id=? AND object_type='incident_action' AND object_id=?`, actor.TenantID, rawID).Scan(&assignmentVersion); err != nil {
			return WorkItem{}, err
		}
		work := WorkItem{WorkReference: WorkReference{TenantID: actor.TenantID, ObjectType: ActionType, ObjectID: rawID}, Number: item.Number, Title: item.Description, State: item.Status, StateCategory: stateCategory(item.Status, ActionType), Category: "COORDINATION", AssignedTo: item.OwnerUserID, AssignedName: item.OwnerName, Version: item.Version, AssignmentVersion: assignmentVersion, UpdatedAt: item.UpdatedAt, DueAt: &item.DueAt, RecordPath: "/workspace/major?view=actions&action_id=" + rawID}
		if err = s.db.QueryRowContext(ctx, `SELECT created_at,action_kind FROM incident_major_actions WHERE tenant_id=? AND id=?`, actor.TenantID, id).Scan(&work.CreatedAt, &work.Category); err != nil {
			return WorkItem{}, err
		}
		work.Capabilities = capabilities(actor, work)
		return work, nil
	case TaskType:
		if s.requests == nil || !actor.CanReadTasks() {
			return WorkItem{}, ErrNotFound
		}
		item, err := s.requests.Task(ctx, requestActor(actor), id)
		if err != nil {
			return WorkItem{}, mapDomainError(err)
		}
		work := taskItem(actor, item)
		if err := s.db.QueryRowContext(ctx, `SELECT r.created_at FROM fulfillment_tasks t JOIN service_requests r ON r.tenant_id=t.tenant_id AND r.id=t.request_id WHERE t.tenant_id=? AND t.id=?`, actor.TenantID, id).Scan(&work.CreatedAt); err != nil {
			return WorkItem{}, mapDomainError(err)
		}
		return work, nil
	default:
		return WorkItem{}, ErrNotFound
	}
}

func (s *Service) Action(ctx context.Context, actor Actor, input ActionInput) (ActionPage, error) {
	if !actor.CanUse() {
		return ActionPage{}, ErrForbidden
	}
	if len(input.Items) < 1 || len(input.Items) > 50 || input.Action != "assign" && input.Action != "transition" {
		return ActionPage{}, ErrInvalid
	}
	if input.Action == "assign" && input.AssignedTo == nil {
		return ActionPage{}, ErrInvalid
	}
	if input.Action == "transition" && (input.State == "" || len([]rune(input.Reason)) > 10000) {
		return ActionPage{}, ErrInvalid
	}
	seen := map[string]bool{}
	out := ActionPage{Results: make([]ActionResult, 0, len(input.Items))}
	for _, target := range input.Items {
		result := ActionResult{WorkReference: target.WorkReference}
		result.TenantID = actor.TenantID
		id, err := validateID(target.ObjectID)
		key := target.WorkReference.Key()
		if err != nil || target.Version == 0 || target.TenantID != 0 && target.TenantID != actor.TenantID || seen[key] {
			result.Status = "INVALID"
			result.Message = "Nieprawidłowy lub powtórzony rekord."
			out.Results = append(out.Results, result)
			continue
		}
		seen[key] = true
		if target.ObjectType == IncidentType {
			if s.incidents == nil {
				result.Status = "FORBIDDEN"
				result.Message = "Brak dostępu do tego typu pracy."
				out.Results = append(out.Results, result)
				continue
			}
			if input.Action == "assign" {
				_, err = s.incidents.Assign(ctx, incidentActor(actor), id, incidentdomain.AssignmentInput{Version: target.Version, AssignedTo: input.AssignedTo, AssignmentGroupID: target.AssignmentGroupID, Reason: input.Reason})
			} else {
				current, readErr := s.incidents.Get(ctx, incidentActor(actor), id)
				if readErr != nil {
					err = readErr
				} else {
					_, err = s.incidents.Transition(ctx, incidentActor(actor), id, incidentdomain.TransitionInput{Version: target.Version, State: input.State, Reason: incidentTransitionReason(input, current.State), HoldReason: input.HoldReason, ResolutionCode: input.ResolutionCode, ResolutionDescription: input.ResolutionDescription})
				}
			}
		} else if target.ObjectType == ActionType {
			if s.incidents == nil {
				err = ErrNotFound
			} else if input.Action == "assign" {
				_, err = s.incidents.AssignMajorAction(ctx, incidentActor(actor), id, target.Version, input.AssignedTo)
			} else {
				_, err = s.incidents.ChangeMajorAction(ctx, incidentActor(actor), id, incidentdomain.MajorActionChangeInput{Version: target.Version, Status: input.State})
			}
		} else if target.ObjectType == TaskType {
			if s.requests == nil {
				result.Status = "FORBIDDEN"
				result.Message = "Brak dostępu do tego typu pracy."
				out.Results = append(out.Results, result)
				continue
			}
			change := requests.Change{Version: target.Version, State: input.State, Reason: input.Reason, AssignedTo: input.AssignedTo}
			_, err = s.requests.ChangeTask(ctx, requestActor(actor), id, change, input.Action == "assign")
		} else {
			err = ErrNotFound
		}
		if err != nil {
			result.Status, result.Message = actionError(err)
		} else {
			result.Status = "SUCCEEDED"
			if item, getErr := s.Get(ctx, actor, target.ObjectType, target.ObjectID); getErr == nil {
				result.Item = &item
			}
		}
		out.Results = append(out.Results, result)
	}
	return out, nil
}

func (s *Service) CanDownloadExport(ctx context.Context, actor Actor, jobID uint64) (uint64, error) {
	if !actor.CanUse() {
		return 0, ErrNotFound
	}
	var requester uint64
	var attachmentID sql.Null[uint64]
	var rawDefinition []byte
	err := s.db.QueryRowContext(ctx, `SELECT requester_id,attachment_id,definition FROM workspace_exports WHERE tenant_id=? AND job_id=?`, actor.TenantID, jobID).Scan(&requester, &attachmentID, &rawDefinition)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, err
	}
	if requester != actor.UserID || !attachmentID.Valid {
		return 0, ErrNotFound
	}
	var definition Query
	if json.Unmarshal(rawDefinition, &definition) != nil {
		return 0, ErrNotFound
	}
	if _, err = s.validateActorQuery(ctx, actor, &definition); err != nil {
		return 0, ErrNotFound
	}
	job, err := s.jobs.Get(ctx, actor.TenantID, jobID)
	if err != nil || job.Status != jobs.StatusCompleted {
		return 0, ErrNotFound
	}
	if err = s.ensureExportRowsReadable(ctx, actor, jobID); err != nil {
		return 0, err
	}
	return attachmentID.V, nil
}

func (s *Service) ensureExportRowsReadable(ctx context.Context, actor Actor, jobID uint64) error {
	var total uint64
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM workspace_export_items WHERE tenant_id=? AND job_id=?`, actor.TenantID, jobID).Scan(&total); err != nil {
		return err
	}
	if total == 0 {
		return nil
	}
	var visible uint64
	if actor.CanReadIncidents() {
		query := `SELECT COUNT(*) FROM workspace_export_items e JOIN incidents i ON e.tenant_id=i.tenant_id AND e.object_type='incident' AND e.object_id=CAST(i.id AS CHAR) WHERE e.tenant_id=? AND e.job_id=?`
		args := []any{actor.TenantID, jobID}
		if !actor.Has("incident.read") {
			query += ` AND (i.reporter_id=? OR i.affected_user_id=?)`
			args = append(args, actor.UserID, actor.UserID)
		}
		var count uint64
		if err := s.db.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
			return err
		}
		visible += count
	}
	if actor.CanReadTasks() {
		query := `SELECT COUNT(*) FROM workspace_export_items e JOIN fulfillment_tasks t ON e.tenant_id=t.tenant_id AND e.object_type='fulfillment_task' AND e.object_id=CAST(t.id AS CHAR) JOIN work_assignments wa ON wa.tenant_id=t.tenant_id AND wa.object_type='fulfillment_task' AND wa.object_id=CAST(t.id AS CHAR) WHERE e.tenant_id=? AND e.job_id=?`
		args := []any{actor.TenantID, jobID}
		if !actor.Has("request.manage") {
			query += ` AND EXISTS(SELECT 1 FROM group_memberships gm JOIN ` + "`groups`" + ` g ON g.tenant_id=gm.tenant_id AND g.id=gm.group_id AND g.active=TRUE WHERE gm.tenant_id=t.tenant_id AND gm.group_id=wa.assignment_group_id AND gm.user_id=?)`
			args = append(args, actor.UserID)
		}
		var count uint64
		if err := s.db.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
			return err
		}
		visible += count
	}
	if actor.CanReadIncidents() {
		query := `SELECT COUNT(*) FROM workspace_export_items e JOIN incident_major_actions ac ON ac.tenant_id=e.tenant_id AND e.object_type='incident_action' AND e.object_id=CAST(ac.id AS CHAR) JOIN incidents i ON i.tenant_id=ac.tenant_id AND i.id=ac.incident_id WHERE e.tenant_id=? AND e.job_id=?`
		args := []any{actor.TenantID, jobID}
		if !actor.Has("incident.read") {
			query += ` AND (i.reporter_id=? OR i.affected_user_id=?)`
			args = append(args, actor.UserID, actor.UserID)
		}
		if !actor.Has("incident.major.read") && !actor.Has("incident.major.manage") {
			query += ` AND ac.owner_user_id=?`
			args = append(args, actor.UserID)
		}
		var count uint64
		if err := s.db.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
			return err
		}
		visible += count
	}

	if visible != total {
		return ErrNotFound
	}
	return nil
}

func incidentActor(actor Actor) incidentdomain.Actor {
	permissions := map[string]bool{}
	for key, value := range actor.Permissions {
		permissions[key] = value
	}
	return incidentdomain.Actor{TenantID: actor.TenantID, UserID: actor.UserID, Permissions: permissions, RequestID: actor.RequestID, IPAddress: actor.IPAddress}
}

func requestActor(actor Actor) requests.Actor {
	return requests.Actor{Actor: workrecords.Actor{TenantID: actor.TenantID, UserID: actor.UserID, RequestID: actor.RequestID}, IPAddress: actor.IPAddress, Permissions: actor.Permissions}
}

func incidentItem(item incidentdomain.Record) WorkItem {
	return WorkItem{WorkReference: WorkReference{TenantID: item.TenantID, ObjectType: IncidentType, ObjectID: strconv.FormatUint(item.ID, 10)}, Number: item.Number, Title: item.Title, State: item.State, StateCategory: stateCategory(item.State, IncidentType), Category: item.Category, Priority: &item.Priority, AssignedTo: item.Assignment.AssignedTo, AssignedName: item.AssignedName, AssignmentGroupID: item.Assignment.AssignmentGroupID, GroupName: item.GroupName, Version: item.Version, AssignmentVersion: item.Assignment.Version, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt, RecordPath: "/incidents/" + strconv.FormatUint(item.ID, 10), Capabilities: incidentCapabilities(item)}
}

func incidentCapabilities(item incidentdomain.Record) []string {
	out := []string{"open"}
	if item.Actions.Edit {
		out = append(out, "edit")
	}
	if item.Actions.Assign {
		out = append(out, "assign")
	}
	if item.Actions.OverridePriority {
		out = append(out, "override_priority")
	}
	for _, state := range item.Actions.Transitions {
		out = append(out, "transition:"+state)
	}
	return out
}

func taskItem(actor Actor, item requests.Task) WorkItem {
	var priority *int
	out := WorkItem{WorkReference: WorkReference{TenantID: item.Assignment.TenantID, ObjectType: TaskType, ObjectID: strconv.FormatUint(item.ID, 10)}, Number: item.Number, Title: item.Title, State: item.State, StateCategory: stateCategory(item.State, TaskType), Category: "FULFILLMENT", Priority: priority, AssignedTo: item.Assignment.AssignedTo, AssignedName: item.AssignedName, AssignmentGroupID: item.Assignment.AssignmentGroupID, GroupName: item.GroupName, Version: item.Version, AssignmentVersion: item.Assignment.Version, UpdatedAt: item.UpdatedAt, RecordPath: "/fulfillment/" + strconv.FormatUint(item.ID, 10), Capabilities: []string{"open"}}
	// The task API performs the same ownership and workflow checks on every action.
	if item.Assignment.AssignedTo != nil && (*item.Assignment.AssignedTo == actor.UserID || actor.Has("request.manage")) {
		for _, state := range item.Actions {
			out.Capabilities = append(out.Capabilities, "transition:"+state)
		}
	}
	if item.State != "COMPLETED" && item.State != "CANCELLED" && (actor.Has("request.fulfill") || actor.Has("request.manage")) {
		out.Capabilities = append(out.Capabilities, "assign")
	}
	return out
}

func actionError(err error) (string, string) {
	switch {
	case errors.Is(err, ErrInvalid), errors.Is(err, incidentdomain.ErrInvalid), errors.Is(err, requests.ErrInvalid):
		return "INVALID", err.Error()
	case errors.Is(err, ErrForbidden), errors.Is(err, incidentdomain.ErrForbidden), errors.Is(err, requests.ErrForbidden), errors.Is(err, workrecords.ErrForbidden):
		return "FORBIDDEN", "Brak uprawnień do tej operacji."
	case errors.Is(err, incidentdomain.ErrNotFound), errors.Is(err, requests.ErrNotFound), errors.Is(err, workrecords.ErrNotFound):
		return "NOT_FOUND", "Rekord nie jest już dostępny."
	case errors.Is(err, ErrConflict), errors.Is(err, incidentdomain.ErrConflict), errors.Is(err, requests.ErrConflict), errors.Is(err, workrecords.ErrConflict):
		return "CONFLICT", "Rekord zmienił się. Odśwież dane przed ponowieniem."
	case errors.Is(err, incidentdomain.ErrTransition), errors.Is(err, requests.ErrTransition):
		return "INVALID_TRANSITION", err.Error()
	default:
		return "FAILED", "Operacja nie powiodła się."
	}
}

func mapDomainError(err error) error {
	if errors.Is(err, incidentdomain.ErrForbidden) || errors.Is(err, requests.ErrForbidden) || errors.Is(err, workrecords.ErrForbidden) {
		return ErrNotFound
	}
	if errors.Is(err, incidentdomain.ErrNotFound) || errors.Is(err, requests.ErrNotFound) || errors.Is(err, workrecords.ErrNotFound) {
		return ErrNotFound
	}
	return fmt.Errorf("workspace domain operation: %w", err)
}

func incidentTransitionReason(input ActionInput, current string) string {
	if input.State == "CANCELLED" || input.State == "IN_PROGRESS" && (current == "RESOLVED" || current == "CLOSED") {
		return input.Reason
	}
	return ""
}
