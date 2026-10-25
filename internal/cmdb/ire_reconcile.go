package cmdb

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/chmajster/OpenServiceNOW/internal/audit"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
)

type reconcileContext struct {
	source DataSource
	rule   IdentityRule
	ciID   uint64
	state  string
	why    string
	text   string
	ids    []uint64
}

func (s *Service) canonicalCIID(ctx context.Context, e database.Executor, tenant, id uint64) (uint64, error) {
	seen := map[uint64]bool{}
	for depth := 0; depth < 32; depth++ {
		if seen[id] {
			return 0, ErrConflict
		}
		seen[id] = true
		var canonical uint64
		err := e.QueryRowContext(ctx, `SELECT COALESCE(redirect_ci_id,id) FROM cmdb_cis WHERE tenant_id=? AND id=?`, tenant, id).Scan(&canonical)
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrNotFound
		}
		if err != nil {
			return 0, err
		}
		if canonical == id {
			return id, nil
		}
		id = canonical
	}
	return 0, ErrConflict
}

func normalizeIdentity(key, value, normalization string) (string, error) {
	value = strings.TrimSpace(value)
	switch normalization {
	case "", "TRIM":
		return value, nil
	case "LOWER_TRIM":
		return strings.ToLower(value), nil
	case "UPPER_TRIM":
		return strings.ToUpper(value), nil
	case "UPPER_MAC":
		address, err := net.ParseMAC(value)
		if err != nil || len(address) != 6 {
			return "", ErrInvalid
		}
		return strings.ToUpper(strings.ReplaceAll(address.String(), ":", "")), nil
	default:
		return "", ErrInvalid
	}
}

func observationString(raw json.RawMessage) (string, bool) {
	var value string
	if json.Unmarshal(raw, &value) == nil {
		return value, true
	}
	var number json.Number
	if len(raw) > 0 && raw[0] != '"' && json.Unmarshal(raw, &number) == nil {
		return number.String(), true
	}
	return "", false
}

func (s *Service) resolveObservation(ctx context.Context, e database.Executor, a Actor, source DataSource, observation Observation, version uint64) (reconcileContext, error) {
	result := reconcileContext{source: source, state: "INSUFFICIENT", why: "IDENTITY_DATA_MISSING", text: "Brakuje stabilnych danych do identyfikacji", ids: []uint64{}}
	if source.State != "ACTIVE" {
		return result, ErrConflict
	}
	observation.NativeKey = strings.TrimSpace(observation.NativeKey)
	if observation.ScopeKey == "" {
		observation.ScopeKey = source.ScopeKey
	}
	if !assetsText(observation.NativeKey, 1, 255) || len(observation.ScopeKey) > 255 {
		return result, ErrInvalid
	}
	if observation.ObservedAt.IsZero() {
		observation.ObservedAt = time.Now().UTC()
	}
	if observation.ObservedAt.After(time.Now().UTC().Add(5*time.Minute)) || observation.ObservedAt.Before(time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)) {
		return result, ErrInvalid
	}
	if len(observation.Values) > 64 {
		return result, ErrInvalid
	}
	for key, raw := range observation.Values {
		if !fieldKeyPattern.MatchString(key) || len(raw) == 0 || len(raw) > 4096 || !json.Valid(raw) {
			return result, ErrInvalid
		}
	}
	if observation.ParentCIID > 0 {
		parent, err := s.canonicalCIID(ctx, e, a.TenantID, observation.ParentCIID)
		if err != nil {
			return result, err
		}
		observation.ParentCIID = parent
		parentRecord, err := s.GetInTx(ctx, e, a, parent, false)
		if err != nil {
			return result, err
		}
		if parentRecord.Archived {
			return result, ErrConflict
		}
	}
	var mapped uint64
	var manualMapping bool
	err := e.QueryRowContext(ctx, `SELECT ci_id,manually_resolved FROM cmdb_ire_external_identities WHERE tenant_id=? AND source_id=? AND scope_key=? AND source_native_key=?`, a.TenantID, source.ID, observation.ScopeKey, observation.NativeKey).Scan(&mapped, &manualMapping)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return result, err
	}
	class, _ := observationString(observation.Values["class"])
	if mapped > 0 {
		mapped, err = s.canonicalCIID(ctx, e, a.TenantID, mapped)
		if err != nil {
			return result, err
		}
		current, loadErr := s.GetInTx(ctx, e, a, mapped, false)
		if loadErr != nil {
			return result, loadErr
		}
		if class != "" && class != current.Class {
			return reconcileContext{source: source, ciID: mapped, state: "CONFLICT", why: "SOURCE_CLASS_CHANGED", text: "Tożsamość źródła wskazuje istniejący CI innej klasy", ids: []uint64{mapped}}, nil
		}
		class = current.Class
	}
	if _, ok := ClassDefinition(class); !ok {
		if mapped > 0 {
			return reconcileContext{source: source, ciID: mapped, state: "MATCH", ids: []uint64{mapped}}, nil
		}
		return result, nil
	}
	if version == 0 {
		rule, ruleErr := s.ruleInTx(ctx, e, a.TenantID, class, 0, false)
		if errors.Is(ruleErr, ErrNotFound) {
			if ruleErr = s.ensureDefaultRules(ctx, e, a); ruleErr == nil {
				rule, ruleErr = s.ruleInTx(ctx, e, a.TenantID, class, 0, false)
			}
		}
		if ruleErr != nil {
			return result, ruleErr
		}
		result.rule = rule
	} else {
		rule, ruleErr := s.ruleInTx(ctx, e, a.TenantID, class, version, false)
		if ruleErr != nil {
			return result, ruleErr
		}
		result.rule = rule
	}
	if result.rule.ParentDependent && observation.ParentCIID == 0 {
		result.rule.Class = class
		result.why, result.text = "PARENT_REQUIRED", "Reguła identyfikacji wymaga nadrzędnego CI"
		return result, nil
	}
	if mapped > 0 {
		if result.rule.ParentDependent {
			var relationExists bool
			query := `SELECT EXISTS(SELECT 1 FROM cmdb_relations WHERE tenant_id=? AND source_id=? AND target_id=? AND relation_type=?)`
			args := []any{a.TenantID, mapped, observation.ParentCIID, result.rule.ParentRelationType}
			if result.rule.ParentRole != "SOURCE" {
				args = []any{a.TenantID, observation.ParentCIID, mapped, result.rule.ParentRelationType}
			}
			if err = e.QueryRowContext(ctx, query, args...).Scan(&relationExists); err != nil {
				return result, err
			}
			if !relationExists {
				return reconcileContext{source: source, rule: result.rule, ciID: mapped, state: "CONFLICT", why: "PARENT_RELATION_MISMATCH", text: "Zapisane mapowanie źródła nie ma wymaganej relacji z nadrzędnym CI", ids: []uint64{mapped}}, nil
			}
		}
		if manualMapping {
			result.ciID, result.ids, result.state = mapped, []uint64{mapped}, "MATCH"
			return result, nil
		}
		result.ciID, result.ids, result.state = mapped, []uint64{mapped}, "MATCH"
		result.rule.Class = class
		if result.rule.Version > 0 {
			other, matchErr := s.findRuleCandidates(ctx, e, a.TenantID, class, result.rule, observation)
			if matchErr != nil {
				return result, matchErr
			}
			for _, candidate := range other {
				if candidate != mapped {
					result.state, result.why, result.text = "CONFLICT", "IDENTITY_DISAGREEMENT", "Stabilne mapowanie źródła i reguła CI wskazują różne rekordy"
					result.ids = uniqueIDs(append(result.ids, candidate))
					result.ciID = 0
					return result, nil
				}
			}
		}
		return result, nil
	}
	for _, key := range result.rule.RequiredAttributes {
		if _, ok := observation.Values["attributes."+key]; !ok {
			result.rule.Class = class
			result.why, result.text = "REQUIRED_IDENTITY_ATTRIBUTE", "Brakuje wymaganego atrybutu identyfikacyjnego: "+key
			return result, nil
		}
	}
	ids, err := s.findRuleCandidates(ctx, e, a.TenantID, class, result.rule, observation)
	if err != nil {
		return result, err
	}
	result.rule.Class = class
	result.ids = ids
	switch len(ids) {
	case 1:
		result.state, result.ciID, result.why, result.text = "MATCH", ids[0], "", ""
	case 0:
		if missing := missingNewCIFields(observation.Values, class); len(missing) > 0 {
			result.state, result.why, result.text = "INSUFFICIENT", "CREATE_FIELDS_MISSING", "Brakuje pól wymaganych do utworzenia CI: "+strings.Join(missing, ", ")
		} else {
			result.state, result.why, result.text = "NEW", "", ""
		}
	default:
		result.state, result.why, result.text = "MULTIPLE", "MULTIPLE_CANDIDATES", "Reguła zwróciła więcej niż jeden CI; wymagane jest ręczne rozstrzygnięcie"
	}
	return result, nil
}

func uniqueIDs(input []uint64) []uint64 {
	seen := map[uint64]bool{}
	out := make([]uint64, 0, len(input))
	for _, id := range input {
		if id > 0 && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

func missingNewCIFields(values map[string]json.RawMessage, classKey string) []string {
	missing := []string{}
	for _, key := range []string{"identifier", "name", "class", "status", "owner_id", "environment"} {
		if raw, ok := values[key]; !ok || isEmptyJSON(raw) {
			missing = append(missing, key)
		}
	}
	class, _ := ClassDefinition(classKey)
	for _, attribute := range class.Attributes {
		if attribute.Required {
			if raw, ok := values["attributes."+attribute.Key]; !ok || isEmptyJSON(raw) {
				missing = append(missing, "attributes."+attribute.Key)
			}
		}
	}
	return missing
}

func isEmptyJSON(raw json.RawMessage) bool {
	if len(raw) == 0 || string(raw) == "null" {
		return true
	}
	value, ok := observationString(raw)
	return ok && strings.TrimSpace(value) == ""
}

func (s *Service) findRuleCandidates(ctx context.Context, e database.Executor, tenant uint64, class string, rule IdentityRule, observation Observation) ([]uint64, error) {
	if rule.ParentDependent {
		var parentTenant uint64
		if err := e.QueryRowContext(ctx, `SELECT tenant_id FROM cmdb_cis WHERE tenant_id=? AND id=?`, tenant, observation.ParentCIID).Scan(&parentTenant); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, ErrNotFound
			}
			return nil, err
		}
	}
	all := []uint64{}
	for _, keySet := range rule.MatchKeys {
		where := []string{"c.tenant_id=?", "c.class=?", "c.archived=FALSE", "c.redirect_ci_id IS NULL"}
		args := []any{tenant, class}
		complete := true
		for _, key := range keySet {
			raw, ok := observation.Values[key]
			value, valueOK := observationString(raw)
			if !ok || !valueOK || strings.TrimSpace(value) == "" {
				complete = false
				break
			}
			normalization := rule.Normalization[key]
			value, err := normalizeIdentity(key, value, normalization)
			if err != nil {
				return nil, err
			}
			expression := ciIdentityExpression(key)
			if expression == "" {
				return nil, ErrInvalid
			}
			where = append(where, identityExpression(expression, normalization)+"=?")
			if strings.HasPrefix(key, "attributes.") {
				args = append(args, "$."+strings.TrimPrefix(key, "attributes."))
			}
			args = append(args, value)
		}
		if !complete {
			continue
		}
		if rule.ParentDependent {
			if rule.ParentRole == "SOURCE" {
				where = append(where, "EXISTS(SELECT 1 FROM cmdb_relations pr WHERE pr.tenant_id=c.tenant_id AND pr.source_id=c.id AND pr.target_id=? AND pr.relation_type=?)")
			} else {
				where = append(where, "EXISTS(SELECT 1 FROM cmdb_relations pr WHERE pr.tenant_id=c.tenant_id AND pr.target_id=c.id AND pr.source_id=? AND pr.relation_type=?)")
			}
			args = append(args, observation.ParentCIID, rule.ParentRelationType)
		}
		query := `SELECT c.id FROM cmdb_cis c WHERE ` + strings.Join(where, " AND ") + ` ORDER BY c.id LIMIT 51`
		rows, err := e.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id uint64
			if err = rows.Scan(&id); err != nil {
				rows.Close()
				return nil, err
			}
			all = append(all, id)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return uniqueIDs(all), nil
}

func ciIdentityExpression(key string) string {
	switch key {
	case "identifier", "name", "class", "status", "environment", "location":
		return "c." + key
	case "owner_id":
		return "CAST(c.owner_id AS CHAR)"
	default:
		if strings.HasPrefix(key, "attributes.") {
			return "JSON_UNQUOTE(JSON_EXTRACT(c.attributes,?))"
		}
		return ""
	}
}

func identityExpression(expression, normalization string) string {
	switch normalization {
	case "LOWER_TRIM":
		return "LOWER(TRIM(" + expression + "))"
	case "UPPER_TRIM":
		return "UPPER(TRIM(" + expression + "))"
	case "UPPER_MAC":
		return "UPPER(REPLACE(REPLACE(REPLACE(TRIM(" + expression + "),':',''),'-',''),'.',''))"
	default:
		return "TRIM(" + expression + ")"
	}
}

func (s *Service) MatchObservation(ctx context.Context, a Actor, sourceID uint64, observation Observation) (MatchResult, error) {
	if !canManageIRE(a) {
		return MatchResult{}, workrecords.ErrForbidden
	}
	var result MatchResult
	err := s.ActorTx(ctx, a, func(e database.Executor, current Actor) error {
		if !canManageIRE(current) {
			return workrecords.ErrForbidden
		}
		source, err := s.sourceInTx(ctx, e, current.TenantID, sourceID, false)
		if err != nil {
			return err
		}
		resolved, err := s.resolveObservation(ctx, e, current, source, observation, 0)
		if err != nil {
			return err
		}
		result = MatchResult{State: publicMatchState(resolved.state), CIID: resolved.ciID, Candidates: resolved.ids, RuleVersion: resolved.rule.Version, Reason: resolved.why, Message: resolved.text}
		return nil
	})
	return result, err
}

func publicMatchState(state string) string {
	switch state {
	case "MATCH":
		return "SINGLE_MATCH"
	case "NEW":
		return "NEW_CI"
	case "INSUFFICIENT":
		return "INSUFFICIENT_DATA"
	case "MULTIPLE":
		return "MULTIPLE_CANDIDATES"
	default:
		return state
	}
}

func observationValues(record Fields) map[string]json.RawMessage {
	values := map[string]json.RawMessage{}
	for key, value := range map[string]any{"identifier": record.Identifier, "name": record.Name, "class": record.Class, "status": record.Status, "owner_id": fmt.Sprint(record.OwnerID), "environment": record.Environment, "location": record.Location} {
		if raw, err := json.Marshal(value); err == nil {
			values[key] = raw
		}
	}
	for key, raw := range record.Attributes {
		values["attributes."+key] = append(json.RawMessage(nil), raw...)
	}
	return values
}

func fieldValues(record Fields) map[string]json.RawMessage {
	values := observationValues(record)
	for key, value := range record.CustomFields {
		values["custom_fields."+key] = append(json.RawMessage(nil), value...)
	}
	return values
}

func applyObservationValues(record Record, values map[string]json.RawMessage) (Record, error) {
	for key, raw := range values {
		switch key {
		case "identifier", "name", "class", "status", "environment", "location":
			value, ok := observationString(raw)
			if !ok {
				return record, ErrInvalid
			}
			switch key {
			case "identifier":
				record.Identifier = value
			case "name":
				record.Name = value
			case "class":
				record.Class = value
			case "status":
				record.Status = value
			case "environment":
				record.Environment = value
			case "location":
				record.Location = value
			}
		case "owner_id":
			value, ok := observationString(raw)
			if !ok || value == "" {
				return record, ErrInvalid
			}
			var owner uint64
			if _, err := fmt.Sscan(value, &owner); err != nil || owner == 0 || fmt.Sprint(owner) != value {
				return record, ErrInvalid
			}
			record.OwnerID = owner
		default:
			if !strings.HasPrefix(key, "attributes.") {
				return record, ErrInvalid
			}
			attributeKey := strings.TrimPrefix(key, "attributes.")
			class, ok := ClassDefinition(record.Class)
			if !ok {
				return record, ErrInvalid
			}
			found := false
			for _, definition := range class.Attributes {
				if definition.Key == attributeKey {
					found = true
					if err := validateAttribute(definition, raw); err != nil {
						return record, fmt.Errorf("%w: atrybut %s", err, attributeKey)
					}
				}
			}
			if !found {
				return record, ErrInvalid
			}
			if record.Attributes == nil {
				record.Attributes = map[string]json.RawMessage{}
			}
			record.Attributes[attributeKey] = append(json.RawMessage(nil), raw...)
		}
	}
	return record, nil
}

func valuesEqual(a, b json.RawMessage) bool {
	var left, right any
	if json.Unmarshal(a, &left) != nil || json.Unmarshal(b, &right) != nil {
		return string(a) == string(b)
	}
	leftJSON, _ := json.Marshal(left)
	rightJSON, _ := json.Marshal(right)
	return string(leftJSON) == string(rightJSON)
}

func (s *Service) fieldPolicyInTx(ctx context.Context, e database.Executor, tenant uint64, class, field string) (FieldPolicy, error) {
	var policy FieldPolicy
	err := e.QueryRowContext(ctx, `SELECT class,field_key,strategy,COALESCE(authoritative_source_id,0),allow_manual_override,version FROM cmdb_ire_field_policies WHERE tenant_id=? AND class IN(?, '*') AND field_key=? ORDER BY (class='*') ASC LIMIT 1`, tenant, class, field).Scan(&policy.Class, &policy.FieldKey, &policy.Strategy, &policy.AuthoritativeSourceID, &policy.AllowManualOverride, &policy.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return FieldPolicy{Class: class, FieldKey: field, Strategy: "PRIORITY", AllowManualOverride: false}, nil
	}
	return policy, err
}

type currentProvenance struct {
	sourceID   uint64
	priority   uint16
	observed   time.Time
	sourceType string
	runType    string
}

func manualProvenanceHolds(prior currentProvenance) bool {
	return prior.sourceType == "MANUAL" && prior.runType == "MANUAL"
}

func (s *Service) priorProvenance(ctx context.Context, e database.Executor, tenant, ci uint64, field string) (currentProvenance, bool, error) {
	var prior currentProvenance
	err := e.QueryRowContext(ctx, `SELECT p.source_id,p.source_priority,p.observed_at,s.source_type,p.run_type FROM cmdb_ire_field_provenance p JOIN cmdb_ire_sources s ON s.tenant_id=p.tenant_id AND s.id=p.source_id WHERE p.tenant_id=? AND p.ci_id=? AND p.field_key=?`, tenant, ci, field).Scan(&prior.sourceID, &prior.priority, &prior.observed, &prior.sourceType, &prior.runType)
	if errors.Is(err, sql.ErrNoRows) {
		return prior, false, nil
	}
	return prior, err == nil, err
}

func (s *Service) activeOverride(ctx context.Context, e database.Executor, tenant, ci uint64, field string) (string, time.Time, bool, error) {
	var kind, reason string
	var expiry sql.NullTime
	err := e.QueryRowContext(ctx, `SELECT override_type,reason,expires_at FROM cmdb_ire_manual_overrides WHERE tenant_id=? AND ci_id=? AND field_key=?`, tenant, ci, field).Scan(&kind, &reason, &expiry)
	if errors.Is(err, sql.ErrNoRows) {
		return "", time.Time{}, false, nil
	}
	if err != nil {
		return "", time.Time{}, false, err
	}
	if expiry.Valid && !expiry.Time.After(time.Now().UTC()) {
		return "", time.Time{}, false, nil
	}
	return kind, expiry.Time, true, nil
}

func (s *Service) fieldAcceptable(ctx context.Context, e database.Executor, tenant uint64, ci uint64, class, field string, current json.RawMessage, source DataSource, observed time.Time) (bool, string, error) {
	policy, err := s.fieldPolicyInTx(ctx, e, tenant, class, field)
	if err != nil {
		return false, "", err
	}
	if policy.AuthoritativeSourceID > 0 && policy.AuthoritativeSourceID != source.ID {
		return false, "NOT_AUTHORITATIVE_SOURCE", nil
	}
	prior, hasPrior, err := s.priorProvenance(ctx, e, tenant, ci, field)
	if err != nil {
		return false, "", err
	}
	_, _, hasOverride, err := s.activeOverride(ctx, e, tenant, ci, field)
	if err != nil {
		return false, "", err
	}
	manualValue := hasPrior && manualProvenanceHolds(prior)
	if (manualValue || hasOverride) && source.Type != "MANUAL" && !policy.AllowManualOverride {
		return false, "MANUAL_OVERRIDE", nil
	}
	if source.Type == "MANUAL" {
		return true, "", nil
	}
	switch policy.Strategy {
	case "AUTHORITATIVE":
		if policy.AuthoritativeSourceID == 0 {
			return false, "AUTHORITY_NOT_CONFIGURED", nil
		}
	case "FILL_EMPTY":
		if !isEmptyJSON(current) {
			return false, "FIELD_NOT_EMPTY", nil
		}
	case "FRESHEST":
		if hasPrior && !observed.After(prior.observed) {
			return false, "OLDER_OBSERVATION", nil
		}
	case "PRIORITY":
		if hasPrior && prior.sourceID != source.ID {
			if source.Priority > prior.priority || source.Priority == prior.priority && !observed.After(prior.observed) {
				return false, "LOWER_PRIORITY_OR_OLDER", nil
			}
		} else if hasPrior && observed.Before(prior.observed) {
			return false, "OLDER_OBSERVATION", nil
		}
	default:
		return false, "INVALID_POLICY", ErrInvalid
	}
	return true, "", nil
}

func (s *Service) writeProvenance(ctx context.Context, e database.Executor, tenant, ci, source uint64, field, runType string, runID, ruleVersion uint64, priority uint16, observed time.Time, value json.RawMessage) error {
	if old, exists, err := s.priorProvenance(ctx, e, tenant, ci, field); err != nil {
		return err
	} else if exists {
		if _, err = e.ExecContext(ctx, `INSERT INTO cmdb_ire_field_provenance_history(tenant_id,ci_id,field_key,source_id,run_type,run_id,rule_version,source_priority,observed_at,accepted_at,value_hash,archived_at) SELECT tenant_id,ci_id,field_key,source_id,run_type,run_id,rule_version,source_priority,observed_at,accepted_at,value_hash,UTC_TIMESTAMP(6) FROM cmdb_ire_field_provenance WHERE tenant_id=? AND ci_id=? AND field_key=?`, tenant, ci, field); err != nil {
			return err
		}
		_ = old
	}
	hash := sha256.Sum256(value)
	_, err := e.ExecContext(ctx, `INSERT INTO cmdb_ire_field_provenance(tenant_id,ci_id,field_key,source_id,run_type,run_id,rule_version,source_priority,observed_at,accepted_at,value_hash) VALUES(?,?,?,?,?,?,?,?,?,UTC_TIMESTAMP(6),?) ON DUPLICATE KEY UPDATE source_id=VALUES(source_id),run_type=VALUES(run_type),run_id=VALUES(run_id),rule_version=VALUES(rule_version),source_priority=VALUES(source_priority),observed_at=VALUES(observed_at),accepted_at=VALUES(accepted_at),value_hash=VALUES(value_hash)`, tenant, ci, field, source, runType, nullableID(runID), ruleVersion, priority, observed.UTC(), hash[:])
	return err
}

func (s *Service) applyObservationInTx(ctx context.Context, e database.Executor, a Actor, source DataSource, observation Observation, runType string, runID, pinnedRuleVersion uint64, metadataVersion ...uint64) (ReconciliationObservation, error) {
	out := ReconciliationObservation{Observation: observation, State: "REJECTED", Accepted: map[string]json.RawMessage{}, Rejected: map[string]string{}, Candidates: []uint64{}}
	if observation.ScopeKey == "" {
		observation.ScopeKey = source.ScopeKey
	}
	resolved, err := s.resolveObservation(ctx, e, a, source, observation, pinnedRuleVersion)
	if err != nil {
		return out, err
	}
	out.State, out.CIID, out.Candidates, out.RuleVersion, out.Reason, out.Message = resolved.state, resolved.ciID, resolved.ids, resolved.rule.Version, resolved.why, resolved.text
	if resolved.state != "MATCH" && resolved.state != "NEW" {
		return out, nil
	}
	if resolved.rule.ParentDependent && observation.ParentCIID > 0 {
		if _, err = s.GetInTx(ctx, e, a, observation.ParentCIID, true); err != nil {
			out.State, out.Reason, out.Message = "CONFLICT", "PARENT_NOT_ACCESSIBLE", "Nadrzędny CI nie jest dostępny"
			return out, nil
		}
	}
	var current Record
	if resolved.state == "MATCH" {
		current, err = s.GetInTx(ctx, e, a, resolved.ciID, true)
		if err != nil {
			return out, err
		}
		if current.Archived {
			out.State, out.Reason, out.Message = "CONFLICT", "CI_ARCHIVED", "CI jest zarchiwizowany"
			return out, nil
		}
	} else {
		current = Record{}
	}
	updated, err := applyObservationValues(current, observation.Values)
	if err != nil {
		out.State, out.Reason, out.Message = "REJECTED", "INVALID_VALUES", err.Error()
		return out, nil
	}
	if resolved.state == "MATCH" && current.Class != updated.Class {
		out.State, out.Reason, out.Message = "CONFLICT", "CI_CLASS_IMMUTABLE", "Klasa istniejącego CI jest niezmienna"
		return out, nil
	}
	if resolved.state == "NEW" {
		if missing := missingNewCIFields(observation.Values, updated.Class); len(missing) > 0 {
			out.State, out.Reason, out.Message = "INSUFFICIENT", "CREATE_FIELDS_MISSING", "Brakuje pól wymaganych do utworzenia CI: "+strings.Join(missing, ", ")
			return out, nil
		}
	}
	observed := observation.ObservedAt
	if observed.IsZero() {
		observed = time.Now().UTC()
	}
	toWrite := map[string]json.RawMessage{}
	if resolved.state == "NEW" {
		toWrite = observation.Values
	} else {
		beforeValues := fieldValues(current.Fields)
		for key, raw := range observation.Values {
			accepted, reason, checkErr := s.fieldAcceptable(ctx, e, a.TenantID, current.ID, current.Class, key, beforeValues[key], source, observed)
			if checkErr != nil {
				return out, checkErr
			}
			if !accepted {
				out.Rejected[key] = reason
				continue
			}
			toWrite[key] = raw
		}
		updated, err = applyObservationValues(current, toWrite)
		if err != nil {
			out.State, out.Reason, out.Message = "REJECTED", "INVALID_VALUES", err.Error()
			return out, nil
		}
	}
	var saved Record
	if resolved.state == "NEW" {
		updated.Version = 0
		if len(metadataVersion) > 0 && metadataVersion[0] > 0 {
			saved, err = s.SaveInTxAtMetadataVersion(ctx, e, a, 0, updated, metadataVersion[0])
		} else {
			saved, err = s.SaveInTx(ctx, e, a, 0, updated)
		}
		if err != nil {
			if errors.Is(err, ErrDuplicate) || errors.Is(err, ErrConflict) {
				out.State, out.Reason, out.Message = "CONFLICT", "CI_WRITE_CONFLICT", "CI zmienił się równolegle; wykonaj ponowny dry-run"
				return out, nil
			}
			return out, err
		}
		resolved.ciID = saved.ID
		out.Accepted = cloneRawMap(toWrite)
	} else {
		for key, raw := range toWrite {
			if !valuesEqual(fieldValues(current.Fields)[key], raw) {
				out.Accepted[key] = append(json.RawMessage(nil), raw...)
			}
		}
		if len(out.Accepted) > 0 {
			updated.Version = current.Version
			if len(metadataVersion) > 0 && metadataVersion[0] > 0 {
				saved, err = s.SaveInTxAtMetadataVersion(ctx, e, a, current.ID, updated, metadataVersion[0])
			} else {
				saved, err = s.SaveInTx(ctx, e, a, current.ID, updated)
			}
			if err != nil {
				if errors.Is(err, ErrDuplicate) || errors.Is(err, ErrConflict) {
					out.State, out.Reason, out.Message = "CONFLICT", "CI_WRITE_CONFLICT", "CI zmienił się równolegle; wykonaj ponowny dry-run"
					return out, nil
				}
				return out, err
			}
		} else {
			saved = current
		}
	}
	if saved.ID == 0 {
		return out, ErrInvalid
	}
	if err = s.saveExternalIdentity(ctx, e, a.TenantID, source.ID, observation, saved.ID, runType, runID); err != nil {
		return out, err
	}
	if _, err = e.ExecContext(ctx, `UPDATE cmdb_cis SET last_seen_at=IF(last_seen_at IS NULL OR last_seen_at<? , ?,last_seen_at) WHERE tenant_id=? AND id=?`, observed.UTC(), observed.UTC(), a.TenantID, saved.ID); err != nil {
		return out, err
	}
	for key, raw := range toWrite {
		if err = s.writeProvenance(ctx, e, a.TenantID, saved.ID, source.ID, key, runType, runID, resolved.rule.Version, source.Priority, observed, raw); err != nil {
			return out, err
		}
	}
	if resolved.state == "NEW" && resolved.rule.ParentDependent {
		if err = s.createParentRelation(ctx, e, a, saved, observation.ParentCIID, resolved.rule); err != nil {
			return out, err
		}
	}
	if err = s.recheckRepairedCIInTx(ctx, e, a.TenantID, saved.ID); err != nil {
		return out, err
	}
	out.State, out.CIID, out.RuleVersion = "APPLIED", saved.ID, resolved.rule.Version
	return out, nil
}

func cloneRawMap(input map[string]json.RawMessage) map[string]json.RawMessage {
	out := map[string]json.RawMessage{}
	for key, value := range input {
		out[key] = append(json.RawMessage(nil), value...)
	}
	return out
}

func (s *Service) saveExternalIdentity(ctx context.Context, e database.Executor, tenant, sourceID uint64, observation Observation, ciID uint64, runType string, runID uint64) error {
	observed := observation.ObservedAt
	if observed.IsZero() {
		observed = time.Now().UTC()
	}
	var existing uint64
	err := e.QueryRowContext(ctx, `SELECT ci_id FROM cmdb_ire_external_identities WHERE tenant_id=? AND source_id=? AND scope_key=? AND source_native_key=? FOR UPDATE`, tenant, sourceID, observation.ScopeKey, observation.NativeKey).Scan(&existing)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if existing > 0 {
		canonical, err := s.canonicalCIID(ctx, e, tenant, existing)
		if err != nil {
			return err
		}
		if canonical != ciID {
			return ErrConflict
		}
		_, err = e.ExecContext(ctx, `UPDATE cmdb_ire_external_identities SET ci_id=?,last_seen_run_type=IF(last_seen_at<=?, ?,last_seen_run_type),last_seen_run_id=IF(last_seen_at<=?, ?,last_seen_run_id),stale_since=NULL,last_seen_at=GREATEST(last_seen_at,?),updated_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND source_id=? AND scope_key=? AND source_native_key=?`, ciID, observed.UTC(), runType, observed.UTC(), nullableID(runID), observed.UTC(), tenant, sourceID, observation.ScopeKey, observation.NativeKey)
		return err
	}
	_, err = e.ExecContext(ctx, `INSERT INTO cmdb_ire_external_identities(tenant_id,source_id,scope_key,source_native_key,ci_id,first_seen_at,last_seen_at,last_seen_run_type,last_seen_run_id) VALUES(?,?,?,?,?,?,?,?,?)`, tenant, sourceID, observation.ScopeKey, observation.NativeKey, ciID, observed.UTC(), observed.UTC(), runType, nullableID(runID))
	return duplicateAsConflict(err)
}

func (s *Service) createParentRelation(ctx context.Context, e database.Executor, a Actor, child Record, parentID uint64, rule IdentityRule) error {
	parent, err := s.GetInTx(ctx, e, a, parentID, true)
	if err != nil {
		return err
	}
	sourceID, targetID := child.ID, parent.ID
	if rule.ParentRole == "TARGET" {
		sourceID, targetID = parent.ID, child.ID
	}
	if !RelationType(rule.ParentRelationType, func() string {
		if rule.ParentRole == "SOURCE" {
			return child.Class
		}
		return parent.Class
	}(), func() string {
		if rule.ParentRole == "SOURCE" {
			return parent.Class
		}
		return child.Class
	}()) {
		return ErrInvalid
	}
	var exists bool
	if err = e.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM cmdb_relations WHERE tenant_id=? AND source_id=? AND target_id=? AND relation_type=?)`, a.TenantID, sourceID, targetID, rule.ParentRelationType).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return nil
	}
	if _, err = e.ExecContext(ctx, `INSERT INTO cmdb_relations(tenant_id,source_id,target_id,relation_type) VALUES(?,?,?,?)`, a.TenantID, sourceID, targetID, rule.ParentRelationType); err != nil {
		return err
	}
	if _, err = s.touch(ctx, e, a, parent, "relations"); err != nil {
		return err
	}
	if _, err = s.touch(ctx, e, a, child, "relations"); err != nil {
		return err
	}
	return nil
}

func (s *Service) createIREConflict(ctx context.Context, e database.Executor, a Actor, source DataSource, runID, observationID uint64, kind, sourceKey string, candidates []uint64, valueDiff, referenceDiff any) error {
	if len(candidates) == 0 {
		candidates = []uint64{}
	}
	candidateJSON, _ := json.Marshal(candidates)
	values, _ := json.Marshal(valueDiff)
	refs, _ := json.Marshal(referenceDiff)
	_, err := e.ExecContext(ctx, `INSERT INTO cmdb_ire_conflicts(tenant_id,source_id,run_id,observation_id,conflict_type,source_key,candidate_ci_ids,value_diff,reference_diff) VALUES(?,?,?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE updated_at=updated_at`, a.TenantID, source.ID, nullableID(runID), nullableID(observationID), kind, sourceKey, candidateJSON, values, refs)
	if err != nil {
		return err
	}
	return audit.NewMySQLRepository().Append(ctx, e, audit.Event{TenantID: &a.TenantID, ActorUserID: &a.UserID, Action: "cmdb.ire.conflict.created", ObjectType: "cmdb_ire_conflict", ObjectID: sourceKey, NewValues: map[string]any{"type": kind, "candidate_ci_ids": candidates}, RequestID: a.RequestID, IPAddress: a.IPAddress, Source: "cmdb.ire"})
}
