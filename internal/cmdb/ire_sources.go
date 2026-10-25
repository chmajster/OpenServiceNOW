package cmdb

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/chmajster/OpenServiceNOW/internal/audit"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
)

var sourceKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_.-]{1,79}$`)
var fieldKeyPattern = regexp.MustCompile(`^(identifier|name|class|status|owner_id|environment|location|attributes\.[a-z][a-z0-9_]{0,47})$`)
var customFieldKeyPattern = regexp.MustCompile(`^custom_fields\.[a-z][a-z0-9_]{0,47}$`)

func validPolicyFieldKey(key string) bool {
	return fieldKeyPattern.MatchString(key) || customFieldKeyPattern.MatchString(key)
}

func canManageIRE(a Actor) bool { return a.Has("cmdb.ire.manage") && Writable(a) }

func canReadQuality(a Actor) bool {
	return a.Has("cmdb.quality.read") || a.Has("cmdb.quality.manage") || a.Has("cmdb.ire.manage")
}

const sourceColumns = `id,source_key,name,source_type,COALESCE(owner_id,0),scope_key,COALESCE(connection_ref,''),COALESCE(source_instance_id,''),state,priority,is_system,version,last_successful_read_at,created_at,updated_at`

func scanSource(row interface{ Scan(...any) error }) (DataSource, error) {
	var source DataSource
	err := row.Scan(&source.ID, &source.Key, &source.Name, &source.Type, &source.OwnerID, &source.ScopeKey, &source.ConnectionRef, &source.SourceInstanceID, &source.State, &source.Priority, &source.System, &source.Version, &source.LastSuccessfulRead, &source.CreatedAt, &source.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return source, ErrNotFound
	}
	return source, err
}

func (s *Service) sourceInTx(ctx context.Context, e database.Executor, tenant, id uint64, lock bool) (DataSource, error) {
	query := "SELECT " + sourceColumns + " FROM cmdb_ire_sources WHERE tenant_id=? AND id=?"
	if lock {
		query += " FOR UPDATE"
	}
	return scanSource(e.QueryRowContext(ctx, query, tenant, id))
}

func (s *Service) sourceByKeyInTx(ctx context.Context, e database.Executor, tenant uint64, key string, lock bool) (DataSource, error) {
	query := "SELECT " + sourceColumns + " FROM cmdb_ire_sources WHERE tenant_id=? AND source_key=?"
	if lock {
		query += " FOR UPDATE"
	}
	return scanSource(e.QueryRowContext(ctx, query, tenant, key))
}

func (s *Service) ensureSystemSource(ctx context.Context, e database.Executor, a Actor, key, name, sourceType string) (DataSource, error) {
	source, err := s.sourceByKeyInTx(ctx, e, a.TenantID, key, true)
	if err == nil {
		if source.State != "ACTIVE" {
			return DataSource{}, ErrConflict
		}
		return source, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return source, err
	}
	if _, err = e.ExecContext(ctx, `INSERT IGNORE INTO cmdb_ire_sources(tenant_id,source_key,name,source_type,owner_id,state,priority,is_system) VALUES(?,?,?,?,?,'ACTIVE',100,TRUE)`, a.TenantID, key, name, sourceType, a.UserID); err != nil {
		return DataSource{}, err
	}
	return s.sourceByKeyInTx(ctx, e, a.TenantID, key, true)
}

func validateSourceInput(input DataSourceInput, creating bool) error {
	input.Key = strings.TrimSpace(input.Key)
	input.Name = strings.TrimSpace(input.Name)
	input.ScopeKey = strings.TrimSpace(input.ScopeKey)
	input.ConnectionRef = strings.TrimSpace(input.ConnectionRef)
	input.SourceInstanceID = strings.TrimSpace(input.SourceInstanceID)
	if !sourceKeyPattern.MatchString(input.Key) || !assetsText(input.Name, 1, 160) || len(input.ScopeKey) > 255 || len(input.ConnectionRef) > 200 || len(input.SourceInstanceID) > 255 || input.ConnectionRef != "" && (strings.Contains(input.ConnectionRef, "://") || strings.ContainsAny(input.ConnectionRef, "?#")) {
		return ErrInvalid
	}
	if input.Type != "CSV" && input.Type != "DISCOVERY" && input.Type != "INTEGRATION" {
		return ErrInvalid
	}
	if input.State != "ACTIVE" && input.State != "DISABLED" {
		return ErrInvalid
	}
	if input.Priority == 0 {
		return ErrInvalid
	}
	if creating && input.Version != 0 || !creating && input.Version == 0 {
		return ErrInvalid
	}
	return nil
}

// assetsText keeps validation in this package independent of an import target.
func assetsText(value string, min, max int) bool {
	return len([]rune(value)) >= min && len([]rune(value)) <= max && strings.TrimSpace(value) == value
}

func (s *Service) ListSources(ctx context.Context, a Actor, before uint64, limit int) (IREPage[DataSource], error) {
	page := IREPage[DataSource]{Items: []DataSource{}}
	if !canManageIRE(a) {
		return page, workrecords.ErrForbidden
	}
	if limit < 1 || limit > 100 {
		return page, ErrInvalid
	}
	query := "SELECT " + sourceColumns + " FROM cmdb_ire_sources WHERE tenant_id=?"
	args := []any{a.TenantID}
	if before > 0 {
		query += " AND id<?"
		args = append(args, before)
	}
	query += " ORDER BY id DESC LIMIT ?"
	args = append(args, limit+1)
	rows, err := s.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		source, scanErr := scanSource(rows)
		if scanErr != nil {
			return page, scanErr
		}
		page.Items = append(page.Items, source)
	}
	if err = rows.Err(); err != nil {
		return page, err
	}
	if len(page.Items) > limit {
		page.Items = page.Items[:limit]
		page.Next = page.Items[limit-1].ID
	}
	return page, nil
}

func (s *Service) SaveSource(ctx context.Context, a Actor, id uint64, input DataSourceInput) (DataSource, error) {
	if !canManageIRE(a) {
		return DataSource{}, workrecords.ErrForbidden
	}
	input.Key = strings.TrimSpace(input.Key)
	input.Name = strings.TrimSpace(input.Name)
	input.ScopeKey = strings.TrimSpace(input.ScopeKey)
	input.ConnectionRef = strings.TrimSpace(input.ConnectionRef)
	input.SourceInstanceID = strings.TrimSpace(input.SourceInstanceID)
	input.Type = strings.ToUpper(strings.TrimSpace(input.Type))
	input.State = strings.ToUpper(strings.TrimSpace(input.State))
	if err := validateSourceInput(input, id == 0); err != nil {
		return DataSource{}, err
	}
	var out DataSource
	err := s.ActorTx(ctx, a, func(e database.Executor, current Actor) error {
		if !canManageIRE(current) {
			return workrecords.ErrForbidden
		}
		if input.OwnerID > 0 {
			if err := workrecords.ValidateActiveUser(ctx, e, current.TenantID, input.OwnerID); err != nil {
				return err
			}
		}
		if id == 0 {
			result, err := e.ExecContext(ctx, `INSERT INTO cmdb_ire_sources(tenant_id,source_key,name,source_type,owner_id,scope_key,connection_ref,source_instance_id,state,priority) VALUES(?,?,?,?,?,?,?,?,?,?)`, current.TenantID, input.Key, input.Name, input.Type, nullableID(input.OwnerID), input.ScopeKey, nullableString(input.ConnectionRef), nullableString(input.SourceInstanceID), input.State, input.Priority)
			if err != nil {
				return duplicateAsConflict(err)
			}
			created, err := result.LastInsertId()
			if err != nil {
				return err
			}
			id = uint64(created)
		} else {
			old, err := s.sourceInTx(ctx, e, current.TenantID, id, true)
			if err != nil {
				return err
			}
			if old.System || old.Version != input.Version {
				return ErrConflict
			}
			result, err := e.ExecContext(ctx, `UPDATE cmdb_ire_sources SET source_key=?,name=?,source_type=?,owner_id=?,scope_key=?,connection_ref=?,source_instance_id=?,state=?,priority=?,version=version+1,updated_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND id=? AND version=?`, input.Key, input.Name, input.Type, nullableID(input.OwnerID), input.ScopeKey, nullableString(input.ConnectionRef), nullableString(input.SourceInstanceID), input.State, input.Priority, current.TenantID, id, input.Version)
			if err != nil {
				return duplicateAsConflict(err)
			}
			if changed, _ := result.RowsAffected(); changed != 1 {
				return ErrConflict
			}
		}
		var err error
		out, err = s.sourceInTx(ctx, e, current.TenantID, id, false)
		if err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]any{"source_key": out.Key, "source_type": out.Type, "state": out.State, "version": out.Version})
		return audit.NewMySQLRepository().Append(ctx, e, audit.Event{TenantID: &current.TenantID, ActorUserID: &current.UserID, Action: "cmdb.ire.source.saved", ObjectType: "cmdb_ire_source", ObjectID: fmt.Sprint(id), NewValues: json.RawMessage(payload), RequestID: current.RequestID, IPAddress: current.IPAddress, Source: "cmdb.ire"})
	})
	return out, err
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func duplicateAsConflict(err error) error {
	if err == nil {
		return nil
	}
	if strings.Contains(strings.ToLower(err.Error()), "duplicate") {
		return ErrConflict
	}
	return err
}

func defaultIdentityRule(class string) IdentityRuleInput {
	keys := [][]string{{"identifier"}}
	switch class {
	case "PHYSICAL_SERVER", "NETWORK_DEVICE", "WORKSTATION", "PRINTER", "STORAGE_DEVICE", "MOBILE_DEVICE", "PERIPHERAL_DEVICE", "UPS":
		keys = [][]string{{"attributes.serial_number", "attributes.manufacturer"}, {"attributes.asset_tag"}, {"identifier"}}
	}
	return IdentityRuleInput{Class: class, MatchKeys: keys, RequiredAttributes: []string{}, Normalization: map[string]string{"identifier": "LOWER_TRIM", "attributes.serial_number": "UPPER_TRIM", "attributes.asset_tag": "UPPER_TRIM"}}
}

func (s *Service) ensureDefaultRules(ctx context.Context, e database.Executor, a Actor) error {
	for _, class := range Classes {
		input := defaultIdentityRule(class.Key)
		definition, _ := json.Marshal(input)
		if _, err := e.ExecContext(ctx, `INSERT IGNORE INTO cmdb_ire_identity_rules(tenant_id,class,rule_version,state,definition,created_by,published_at) VALUES(?,?,1,'ACTIVE',?,?,UTC_TIMESTAMP(6))`, a.TenantID, class.Key, definition, a.UserID); err != nil {
			return err
		}
	}
	return nil
}

func validateRuleInput(input IdentityRuleInput) error {
	if _, ok := ClassDefinition(input.Class); !ok || len(input.MatchKeys) == 0 || len(input.MatchKeys) > 8 || len(input.RequiredAttributes) > 32 || len(input.Normalization) > 64 {
		return ErrInvalid
	}
	class, _ := ClassDefinition(input.Class)
	known := map[string]bool{"identifier": true, "name": true, "class": true, "status": true, "owner_id": true, "environment": true, "location": true}
	for _, attribute := range class.Attributes {
		known["attributes."+attribute.Key] = true
	}
	seen := map[string]bool{}
	for _, keySet := range input.MatchKeys {
		if len(keySet) == 0 || len(keySet) > 8 {
			return ErrInvalid
		}
		for _, key := range keySet {
			if !known[key] || seen[key] || key == "name" || key == "status" || key == "owner_id" || key == "environment" || key == "location" || key == "attributes.ip_address" || key == "attributes.management_ip" {
				return ErrInvalid
			}
			seen[key] = true
		}
	}
	required := map[string]bool{}
	for _, attribute := range class.Attributes {
		required[attribute.Key] = true
	}
	seen = map[string]bool{}
	for _, key := range input.RequiredAttributes {
		if !required[key] || seen[key] {
			return ErrInvalid
		}
		seen[key] = true
	}
	for key, normalization := range input.Normalization {
		if !known[key] || (normalization != "TRIM" && normalization != "LOWER_TRIM" && normalization != "UPPER_TRIM" && normalization != "UPPER_MAC") {
			return ErrInvalid
		}
	}
	if input.ParentDependent {
		if input.ParentRole != "SOURCE" && input.ParentRole != "TARGET" || input.ParentRelationType == "" {
			return ErrInvalid
		}
		canBeParent := false
		for _, definition := range relationDefinitions {
			if definition.Key != input.ParentRelationType {
				continue
			}
			for _, pair := range definition.Pairs {
				if input.ParentRole == "SOURCE" && pair.Source == input.Class || input.ParentRole == "TARGET" && pair.Target == input.Class {
					canBeParent = true
				}
			}
		}
		if !canBeParent {
			return ErrInvalid
		}
	} else if input.ParentRelationType != "" || input.ParentRole != "" {
		return ErrInvalid
	}
	return nil
}

func (s *Service) ruleInTx(ctx context.Context, e database.Executor, tenant uint64, class string, version uint64, lock bool) (IdentityRule, error) {
	query := `SELECT definition,rule_version,state,created_at FROM cmdb_ire_identity_rules WHERE tenant_id=? AND class=? AND `
	args := []any{tenant, class}
	if version == 0 {
		query += "state='ACTIVE'"
	} else {
		query += "rule_version=?"
		args = append(args, version)
	}
	if lock {
		query += " FOR UPDATE"
	}
	var raw []byte
	var rule IdentityRule
	err := e.QueryRowContext(ctx, query, args...).Scan(&raw, &rule.Version, &rule.State, &rule.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return rule, ErrNotFound
	}
	if err != nil {
		return rule, err
	}
	if err = json.Unmarshal(raw, &rule); err != nil {
		return rule, err
	}
	rule.Class = class
	return rule, nil
}

func (s *Service) ListIdentityRules(ctx context.Context, a Actor) ([]IdentityRule, error) {
	if !canManageIRE(a) {
		return nil, workrecords.ErrForbidden
	}
	err := s.ActorTx(ctx, a, func(e database.Executor, current Actor) error {
		if !canManageIRE(current) {
			return workrecords.ErrForbidden
		}
		return s.ensureDefaultRules(ctx, e, current)
	})
	if err != nil {
		return nil, err
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT class,rule_version,state,definition,created_at FROM cmdb_ire_identity_rules WHERE tenant_id=? AND state='ACTIVE' ORDER BY class`, a.TenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []IdentityRule{}
	for rows.Next() {
		var rule IdentityRule
		var raw []byte
		if err = rows.Scan(&rule.Class, &rule.Version, &rule.State, &raw, &rule.CreatedAt); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &rule); err != nil {
			return nil, err
		}
		rule.Class = strings.ToUpper(rule.Class)
		out = append(out, rule)
	}
	return out, rows.Err()
}

func (s *Service) SaveIdentityRule(ctx context.Context, a Actor, input IdentityRuleInput) (IdentityRule, error) {
	if !canManageIRE(a) {
		return IdentityRule{}, workrecords.ErrForbidden
	}
	input.Class = strings.ToUpper(strings.TrimSpace(input.Class))
	if err := validateRuleInput(input); err != nil {
		return IdentityRule{}, err
	}
	var out IdentityRule
	err := s.ActorTx(ctx, a, func(e database.Executor, current Actor) error {
		if !canManageIRE(current) {
			return workrecords.ErrForbidden
		}
		if err := s.ensureDefaultRules(ctx, e, current); err != nil {
			return err
		}
		active, err := s.ruleInTx(ctx, e, current.TenantID, input.Class, 0, true)
		if err != nil {
			return err
		}
		if input.ExpectedVersion != active.Version {
			return ErrConflict
		}
		definition := IdentityRule{Class: input.Class, MatchKeys: input.MatchKeys, RequiredAttributes: input.RequiredAttributes, Normalization: input.Normalization, ParentDependent: input.ParentDependent, ParentRelationType: input.ParentRelationType, ParentRole: input.ParentRole}
		raw, _ := json.Marshal(definition)
		if _, err = e.ExecContext(ctx, `UPDATE cmdb_ire_identity_rules SET state='RETIRED' WHERE tenant_id=? AND class=? AND state='ACTIVE'`, current.TenantID, input.Class); err != nil {
			return err
		}
		if _, err = e.ExecContext(ctx, `INSERT INTO cmdb_ire_identity_rules(tenant_id,class,rule_version,state,definition,created_by,published_at) VALUES(?,?,?,'ACTIVE',?,?,UTC_TIMESTAMP(6))`, current.TenantID, input.Class, active.Version+1, raw, current.UserID); err != nil {
			return err
		}
		out, err = s.ruleInTx(ctx, e, current.TenantID, input.Class, active.Version+1, false)
		if err != nil {
			return err
		}
		return audit.NewMySQLRepository().Append(ctx, e, audit.Event{TenantID: &current.TenantID, ActorUserID: &current.UserID, Action: "cmdb.ire.identity_rule.published", ObjectType: "cmdb_ire_identity_rule", ObjectID: input.Class, NewValues: map[string]any{"version": out.Version, "class": out.Class}, RequestID: current.RequestID, IPAddress: current.IPAddress, Source: "cmdb.ire"})
	})
	return out, err
}
