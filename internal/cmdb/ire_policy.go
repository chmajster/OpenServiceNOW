package cmdb

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/chmajster/OpenServiceNOW/internal/audit"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
)

func validatePolicy(policy FieldPolicy) error {
	if policy.Class != "*" {
		if _, ok := ClassDefinition(policy.Class); !ok {
			return ErrInvalid
		}
	}
	if !validPolicyFieldKey(policy.FieldKey) || policy.Strategy != "AUTHORITATIVE" && policy.Strategy != "PRIORITY" && policy.Strategy != "FRESHEST" && policy.Strategy != "FILL_EMPTY" {
		return ErrInvalid
	}
	if policy.Strategy == "AUTHORITATIVE" && policy.AuthoritativeSourceID == 0 || policy.Strategy != "AUTHORITATIVE" && policy.AuthoritativeSourceID != 0 {
		return ErrInvalid
	}
	if policy.FieldKey == "class" {
		return ErrInvalid
	}
	if strings.HasPrefix(policy.FieldKey, "attributes.") && policy.Class != "*" {
		class, _ := ClassDefinition(policy.Class)
		key := strings.TrimPrefix(policy.FieldKey, "attributes.")
		found := false
		for _, attribute := range class.Attributes {
			found = found || attribute.Key == key
		}
		if !found {
			return ErrInvalid
		}
	}
	return nil
}

func (s *Service) ListFieldPolicies(ctx context.Context, a Actor) ([]FieldPolicy, error) {
	if !canManageIRE(a) {
		return nil, workrecords.ErrForbidden
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT class,field_key,strategy,COALESCE(authoritative_source_id,0),allow_manual_override,version FROM cmdb_ire_field_policies WHERE tenant_id=? ORDER BY class,field_key`, a.TenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []FieldPolicy{}
	for rows.Next() {
		var policy FieldPolicy
		if err = rows.Scan(&policy.Class, &policy.FieldKey, &policy.Strategy, &policy.AuthoritativeSourceID, &policy.AllowManualOverride, &policy.Version); err != nil {
			return nil, err
		}
		out = append(out, policy)
	}
	return out, rows.Err()
}

func (s *Service) SaveFieldPolicy(ctx context.Context, a Actor, policy FieldPolicy) (FieldPolicy, error) {
	if !canManageIRE(a) {
		return FieldPolicy{}, workrecords.ErrForbidden
	}
	policy.Class = strings.ToUpper(strings.TrimSpace(policy.Class))
	policy.FieldKey = strings.TrimSpace(policy.FieldKey)
	policy.Strategy = strings.ToUpper(strings.TrimSpace(policy.Strategy))
	if policy.Class == "" || policy.Strategy == "" {
		return FieldPolicy{}, ErrInvalid
	}
	if err := validatePolicy(policy); err != nil {
		return FieldPolicy{}, err
	}
	err := s.ActorTx(ctx, a, func(e database.Executor, current Actor) error {
		if !canManageIRE(current) {
			return workrecords.ErrForbidden
		}
		if strings.HasPrefix(policy.FieldKey, "custom_fields.") {
			if s.Metadata == nil {
				return ErrInvalid
			}
			version, configuration, schemaErr := s.Metadata.Published(ctx, e, current.TenantID)
			if schemaErr != nil {
				return schemaErr
			}
			schema, schemaErr := configuration.Schema("ci", version)
			if schemaErr != nil {
				return schemaErr
			}
			fieldName, found := strings.CutPrefix(policy.FieldKey, "custom_fields.")
			valid := false
			for _, field := range schema.Fields {
				valid = valid || found && field.Key == fieldName && field.Active && !field.System
			}
			if !valid {
				return ErrInvalid
			}
		}
		if policy.AuthoritativeSourceID > 0 {
			source, err := s.sourceInTx(ctx, e, current.TenantID, policy.AuthoritativeSourceID, false)
			if err != nil || source.State != "ACTIVE" {
				if err != nil {
					return err
				}
				return ErrConflict
			}
		}
		var existing uint64
		queryErr := e.QueryRowContext(ctx, `SELECT version FROM cmdb_ire_field_policies WHERE tenant_id=? AND class=? AND field_key=? FOR UPDATE`, current.TenantID, policy.Class, policy.FieldKey).Scan(&existing)
		if errors.Is(queryErr, sql.ErrNoRows) {
			if policy.Version != 0 {
				return ErrConflict
			}
			_, queryErr = e.ExecContext(ctx, `INSERT INTO cmdb_ire_field_policies(tenant_id,class,field_key,strategy,authoritative_source_id,allow_manual_override,version,updated_by) VALUES(?,?,?,?,?,?,1,?)`, current.TenantID, policy.Class, policy.FieldKey, policy.Strategy, nullableID(policy.AuthoritativeSourceID), policy.AllowManualOverride, current.UserID)
			policy.Version = 1
		} else if queryErr == nil {
			if policy.Version != existing {
				return ErrConflict
			}
			policy.Version++
			_, queryErr = e.ExecContext(ctx, `UPDATE cmdb_ire_field_policies SET strategy=?,authoritative_source_id=?,allow_manual_override=?,version=?,updated_by=?,updated_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND class=? AND field_key=? AND version=?`, policy.Strategy, nullableID(policy.AuthoritativeSourceID), policy.AllowManualOverride, policy.Version, current.UserID, current.TenantID, policy.Class, policy.FieldKey, existing)
		}
		if queryErr != nil {
			return duplicateAsConflict(queryErr)
		}
		return audit.NewMySQLRepository().Append(ctx, e, audit.Event{TenantID: &current.TenantID, ActorUserID: &current.UserID, Action: "cmdb.ire.field_policy.saved", ObjectType: "cmdb_ire_field_policy", ObjectID: policy.Class + ":" + policy.FieldKey, NewValues: map[string]any{"strategy": policy.Strategy, "authoritative_source_id": policy.AuthoritativeSourceID, "allow_manual_override": policy.AllowManualOverride, "version": policy.Version}, RequestID: current.RequestID, IPAddress: current.IPAddress, Source: "cmdb.ire"})
	})
	return policy, err
}
