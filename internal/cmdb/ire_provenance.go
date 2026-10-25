package cmdb

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/chmajster/OpenServiceNOW/internal/audit"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
)

func (s *Service) Provenance(ctx context.Context, a Actor, ciID uint64) (ProvenancePage, error) {
	if !a.Has("cmdb.read") && !a.Has("cmdb.read_own") {
		return ProvenancePage{}, workrecords.ErrForbidden
	}
	canonical, err := s.canonicalCIID(ctx, s.DB, a.TenantID, ciID)
	if err != nil {
		return ProvenancePage{}, err
	}
	ciID = canonical
	record, err := s.Get(ctx, a, ciID)
	if err != nil {
		return ProvenancePage{}, err
	}
	ciID = record.ID
	var last sql.NullTime
	if err := s.DB.QueryRowContext(ctx, `SELECT last_seen_at FROM cmdb_cis WHERE tenant_id=? AND id=?`, a.TenantID, ciID).Scan(&last); err != nil {
		return ProvenancePage{}, err
	}
	page := ProvenancePage{CIID: ciID, Version: record.Version, Items: []ProvenanceEntry{}}
	if last.Valid {
		page.LastSeenAt = &last.Time
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT p.field_key,p.source_id,d.source_key,d.name,p.run_type,COALESCE(p.run_id,0),p.rule_version,p.observed_at,p.accepted_at,COALESCE(o.override_type,''),COALESCE(o.reason,''),o.expires_at FROM cmdb_ire_field_provenance p JOIN cmdb_ire_sources d ON d.tenant_id=p.tenant_id AND d.id=p.source_id LEFT JOIN cmdb_ire_manual_overrides o ON o.tenant_id=p.tenant_id AND o.ci_id=p.ci_id AND o.field_key=p.field_key AND (o.expires_at IS NULL OR o.expires_at>UTC_TIMESTAMP(6)) WHERE p.tenant_id=? AND p.ci_id=? ORDER BY p.field_key LIMIT 101`, a.TenantID, ciID)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		var entry ProvenanceEntry
		var expires sql.NullTime
		if err = rows.Scan(&entry.FieldKey, &entry.SourceID, &entry.SourceKey, &entry.SourceName, &entry.RunType, &entry.RunID, &entry.RuleVersion, &entry.ObservedAt, &entry.AcceptedAt, &entry.OverrideType, &entry.OverrideReason, &expires); err != nil {
			return page, err
		}
		if expires.Valid {
			entry.OverrideUntil = &expires.Time
		}
		entry.OverrideAllowed = fieldKeyPattern.MatchString(entry.FieldKey) || customFieldKeyPattern.MatchString(entry.FieldKey)
		page.Items = append(page.Items, entry)
	}
	if err = rows.Err(); err != nil {
		return page, err
	}
	if len(page.Items) > 100 {
		return ProvenancePage{}, ErrInvalid
	}
	return page, nil
}

func (s *Service) SetManualOverride(ctx context.Context, a Actor, ciID, version uint64, input ManualOverrideInput) (ProvenancePage, error) {
	if !Writable(a) {
		return ProvenancePage{}, workrecords.ErrForbidden
	}
	if version == 0 {
		return ProvenancePage{}, ErrInvalid
	}
	input.FieldKey = strings.TrimSpace(input.FieldKey)
	input.Type = strings.ToUpper(strings.TrimSpace(input.Type))
	input.Reason = strings.TrimSpace(input.Reason)
	if !validPolicyFieldKey(input.FieldKey) || len(input.Reason) < 3 || len(input.Reason) > 500 || input.Type != "PERMANENT" && input.Type != "TEMPORARY" || input.Type == "TEMPORARY" && (input.ExpiresAt.Before(time.Now().UTC().Add(time.Hour)) || input.ExpiresAt.After(time.Now().UTC().Add(365*24*time.Hour))) || input.Type == "PERMANENT" && !input.ExpiresAt.IsZero() {
		return ProvenancePage{}, ErrInvalid
	}
	err := s.ActorTx(ctx, a, func(e database.Executor, current Actor) error {
		if !Writable(current) {
			return workrecords.ErrForbidden
		}
		record, err := s.GetInTx(ctx, e, current, ciID, true)
		if err != nil {
			return err
		}
		if record.Version != version || record.Archived {
			return ErrConflict
		}
		value, exists := fieldValues(record.Fields)[input.FieldKey]
		if !exists {
			return ErrInvalid
		}
		source, err := s.ensureSystemSource(ctx, e, current, "manual", "Manual CMDB changes", "MANUAL")
		if err != nil {
			return err
		}
		var expires any
		if input.Type == "TEMPORARY" {
			expires = input.ExpiresAt.UTC()
		}
		if _, err = e.ExecContext(ctx, `INSERT INTO cmdb_ire_manual_overrides(tenant_id,ci_id,field_key,override_type,reason,created_by,expires_at,version) VALUES(?,?,?,?,?,?,?,1) ON DUPLICATE KEY UPDATE override_type=VALUES(override_type),reason=VALUES(reason),created_by=VALUES(created_by),created_at=UTC_TIMESTAMP(6),expires_at=VALUES(expires_at),version=version+1`, current.TenantID, ciID, input.FieldKey, input.Type, input.Reason, current.UserID, expires); err != nil {
			return err
		}
		if err = s.writeProvenance(ctx, e, current.TenantID, ciID, source.ID, input.FieldKey, "MANUAL_OVERRIDE", 0, 0, source.Priority, time.Now().UTC(), value); err != nil {
			return err
		}
		if _, err = s.touch(ctx, e, current, record, "manual_override"); err != nil {
			return err
		}
		return audit.NewMySQLRepository().Append(ctx, e, audit.Event{TenantID: &current.TenantID, ActorUserID: &current.UserID, Action: "cmdb.ire.manual_override.set", ObjectType: "ci", ObjectID: fmt.Sprint(ciID), NewValues: map[string]any{"field": input.FieldKey, "type": input.Type, "reason": input.Reason, "expires_at": input.ExpiresAt}, RequestID: current.RequestID, IPAddress: current.IPAddress, Source: "cmdb.ire"})
	})
	if err != nil {
		return ProvenancePage{}, err
	}
	return s.Provenance(ctx, a, ciID)
}

func (s *Service) ClearManualOverride(ctx context.Context, a Actor, ciID, version uint64, fieldKey, reason string) (ProvenancePage, error) {
	if !Writable(a) {
		return ProvenancePage{}, workrecords.ErrForbidden
	}
	if version == 0 {
		return ProvenancePage{}, ErrInvalid
	}
	fieldKey, reason = strings.TrimSpace(fieldKey), strings.TrimSpace(reason)
	if !validPolicyFieldKey(fieldKey) || len(reason) < 3 || len(reason) > 500 {
		return ProvenancePage{}, ErrInvalid
	}
	err := s.ActorTx(ctx, a, func(e database.Executor, current Actor) error {
		if !Writable(current) {
			return workrecords.ErrForbidden
		}
		record, err := s.GetInTx(ctx, e, current, ciID, true)
		if err != nil {
			return err
		}
		if record.Version != version || record.Archived {
			return ErrConflict
		}
		result, err := e.ExecContext(ctx, `DELETE FROM cmdb_ire_manual_overrides WHERE tenant_id=? AND ci_id=? AND field_key=?`, current.TenantID, ciID, fieldKey)
		if err != nil {
			return err
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if changed != 1 {
			return ErrNotFound
		}
		if _, err = e.ExecContext(ctx, `UPDATE cmdb_ire_field_provenance SET run_type='MANUAL_OVERRIDE_CLEARED',run_id=NULL,accepted_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND ci_id=? AND field_key=? AND run_type IN('MANUAL','MANUAL_OVERRIDE')`, current.TenantID, ciID, fieldKey); err != nil {
			return err
		}
		if _, err = s.touch(ctx, e, current, record, "manual_override"); err != nil {
			return err
		}
		return audit.NewMySQLRepository().Append(ctx, e, audit.Event{TenantID: &current.TenantID, ActorUserID: &current.UserID, Action: "cmdb.ire.manual_override.cleared", ObjectType: "ci", ObjectID: fmt.Sprint(ciID), NewValues: map[string]any{"field": fieldKey, "reason": reason}, RequestID: current.RequestID, IPAddress: current.IPAddress, Source: "cmdb.ire"})
	})
	if err != nil {
		return ProvenancePage{}, err
	}
	return s.Provenance(ctx, a, ciID)
}

func (s *Service) RecordManualChanges(ctx context.Context, e database.Executor, a Actor, before *Record, after Record, reason string) error {
	source, err := s.ensureSystemSource(ctx, e, a, "manual", "Manual CMDB changes", "MANUAL")
	if err != nil {
		return err
	}
	old := map[string]json.RawMessage{}
	if before != nil {
		old = fieldValues(before.Fields)
	}
	newValues := fieldValues(after.Fields)
	for field := range old {
		if _, exists := newValues[field]; !exists {
			newValues[field] = json.RawMessage(`null`)
		}
	}
	for field, value := range newValues {
		if previous, exists := old[field]; exists && valuesEqual(previous, value) {
			continue
		}
		if err = s.writeProvenance(ctx, e, a.TenantID, after.ID, source.ID, field, "MANUAL", 0, 0, source.Priority, time.Now().UTC(), value); err != nil {
			return err
		}
		if reason != "" {
			if _, err = e.ExecContext(ctx, `INSERT INTO cmdb_ire_manual_overrides(tenant_id,ci_id,field_key,override_type,reason,created_by,expires_at,version) VALUES(?,?,?,'PERMANENT',?,?,NULL,1) ON DUPLICATE KEY UPDATE override_type='PERMANENT',reason=VALUES(reason),created_by=VALUES(created_by),expires_at=NULL,version=version+1`, a.TenantID, after.ID, field, reason, a.UserID); err != nil {
				return err
			}
		}
	}
	return s.recheckRepairedCIInTx(ctx, e, a.TenantID, after.ID)
}
