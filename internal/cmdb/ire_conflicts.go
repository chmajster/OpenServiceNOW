package cmdb

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/chmajster/OpenServiceNOW/internal/audit"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
)

const conflictColumns = `id,source_id,COALESCE(run_id,0),COALESCE(observation_id,0),conflict_type,state,source_key,candidate_ci_ids,value_diff,reference_diff,resolution_note,version,created_at,updated_at`

func scanConflict(row interface{ Scan(...any) error }) (IREConflict, error) {
	var conflict IREConflict
	var candidates, valueDiff, referenceDiff []byte
	err := row.Scan(&conflict.ID, &conflict.SourceID, &conflict.RunID, &conflict.ObservationID, &conflict.Type, &conflict.State, &conflict.SourceKey, &candidates, &valueDiff, &referenceDiff, &conflict.ResolutionNote, &conflict.Version, &conflict.CreatedAt, &conflict.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return conflict, ErrNotFound
	}
	if err != nil {
		return conflict, err
	}
	_ = json.Unmarshal(candidates, &conflict.Candidates)
	if conflict.Candidates == nil {
		conflict.Candidates = []uint64{}
	}
	conflict.ValueDiff = append(json.RawMessage(nil), valueDiff...)
	conflict.ReferenceDiff = append(json.RawMessage(nil), referenceDiff...)
	return conflict, nil
}

func (s *Service) ListIREConflicts(ctx context.Context, a Actor, before uint64, state string, limit int) (IREPage[IREConflict], error) {
	page := IREPage[IREConflict]{Items: []IREConflict{}}
	if !canManageIRE(a) {
		return page, workrecords.ErrForbidden
	}
	if limit < 1 || limit > 100 || state != "" && state != "OPEN" && state != "RESOLVED" && state != "DISMISSED" {
		return page, ErrInvalid
	}
	query := "SELECT " + conflictColumns + " FROM cmdb_ire_conflicts WHERE tenant_id=?"
	args := []any{a.TenantID}
	if state != "" {
		query += " AND state=?"
		args = append(args, state)
	}
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
		item, err := scanConflict(rows)
		if err != nil {
			return page, err
		}
		page.Items = append(page.Items, item)
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

func (s *Service) ResolveIREConflict(ctx context.Context, a Actor, id, version, ciID uint64, note, action string) (IREConflict, error) {
	if !canManageIRE(a) {
		return IREConflict{}, workrecords.ErrForbidden
	}
	note = strings.TrimSpace(note)
	if version == 0 || len(note) < 3 || len(note) > 1000 || action != "resolve" && action != "dismiss" || action == "resolve" && ciID == 0 || action == "dismiss" && ciID != 0 {
		return IREConflict{}, ErrInvalid
	}
	var out IREConflict
	err := s.ActorTx(ctx, a, func(e database.Executor, current Actor) error {
		if !canManageIRE(current) {
			return workrecords.ErrForbidden
		}
		conflict, err := scanConflict(e.QueryRowContext(ctx, "SELECT "+conflictColumns+" FROM cmdb_ire_conflicts WHERE tenant_id=? AND id=? FOR UPDATE", current.TenantID, id))
		if err != nil {
			return err
		}
		if conflict.State != "OPEN" || conflict.Version != version {
			return ErrConflict
		}
		state := "DISMISSED"
		if action == "resolve" {
			state = "RESOLVED"
			if conflict.ObservationID > 0 {
				if err = s.resolveObservationConflict(ctx, e, current, conflict, ciID, note); err != nil {
					return err
				}
			} else if ciID > 0 {
				if _, err = s.GetInTx(ctx, e, current, ciID, false); err != nil {
					return err
				}
			}
		}
		if _, err = e.ExecContext(ctx, `UPDATE cmdb_ire_conflicts SET state=?,resolution_note=?,resolved_by=?,resolved_at=UTC_TIMESTAMP(6),version=version+1,updated_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND id=? AND version=?`, state, note, current.UserID, current.TenantID, id, version); err != nil {
			return err
		}
		out, err = scanConflict(e.QueryRowContext(ctx, "SELECT "+conflictColumns+" FROM cmdb_ire_conflicts WHERE tenant_id=? AND id=?", current.TenantID, id))
		if err != nil {
			return err
		}
		return audit.NewMySQLRepository().Append(ctx, e, audit.Event{TenantID: &current.TenantID, ActorUserID: &current.UserID, Action: "cmdb.ire.conflict." + action, ObjectType: "cmdb_ire_conflict", ObjectID: fmt.Sprint(id), NewValues: map[string]any{"state": state, "ci_id": ciID, "note": note}, RequestID: current.RequestID, IPAddress: current.IPAddress, Source: "cmdb.ire"})
	})
	return out, err
}

func (s *Service) resolveObservationConflict(ctx context.Context, e database.Executor, a Actor, conflict IREConflict, ciID uint64, note string) error {
	var sourceID, runID uint64
	var scope, nativeKey string
	var observed time.Time
	var row uint32
	var valuesJSON []byte
	if err := e.QueryRowContext(ctx, `SELECT o.run_id,r.source_id,o.scope_key,o.source_native_key,o.observed_at,o.source_row,o.values_json FROM cmdb_ire_observations o JOIN cmdb_ire_runs r ON r.tenant_id=o.tenant_id AND r.id=o.run_id WHERE o.tenant_id=? AND o.id=? FOR UPDATE`, a.TenantID, conflict.ObservationID).Scan(&runID, &sourceID, &scope, &nativeKey, &observed, &row, &valuesJSON); err != nil {
		return err
	}
	var values map[string]json.RawMessage
	if err := json.Unmarshal(valuesJSON, &values); err != nil {
		return err
	}
	ciID, err := s.canonicalCIID(ctx, e, a.TenantID, ciID)
	if err != nil {
		return err
	}
	selected, err := s.GetInTx(ctx, e, a, ciID, true)
	if err != nil {
		return err
	}
	if selected.Archived {
		return ErrConflict
	}
	if observedClass, ok := observationString(values["class"]); ok && observedClass != "" && observedClass != selected.Class {
		return ErrConflict
	}
	var existing uint64
	err = e.QueryRowContext(ctx, `SELECT ci_id FROM cmdb_ire_external_identities WHERE tenant_id=? AND source_id=? AND scope_key=? AND source_native_key=? FOR UPDATE`, a.TenantID, sourceID, scope, nativeKey).Scan(&existing)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if existing == 0 {
		if _, err = e.ExecContext(ctx, `INSERT INTO cmdb_ire_external_identities(tenant_id,source_id,scope_key,source_native_key,ci_id,first_seen_at,last_seen_at,last_seen_run_type,last_seen_run_id,manually_resolved,resolved_by,resolution_note) VALUES(?,?,?,?,?,?,?,'IRE_RESOLUTION',?,TRUE,?,?)`, a.TenantID, sourceID, scope, nativeKey, ciID, observed.UTC(), observed.UTC(), runID, a.UserID, note); err != nil {
			return duplicateAsConflict(err)
		}
	} else {
		if _, err = e.ExecContext(ctx, `UPDATE cmdb_ire_external_identities SET ci_id=?,manually_resolved=TRUE,resolved_by=?,resolution_note=?,stale_since=NULL,updated_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND source_id=? AND scope_key=? AND source_native_key=?`, ciID, a.UserID, note, a.TenantID, sourceID, scope, nativeKey); err != nil {
			return err
		}
	}
	ids, _ := json.Marshal([]uint64{ciID})
	if _, err = e.ExecContext(ctx, `UPDATE cmdb_ire_observations SET result_state='MATCH',ci_id=?,candidates=?,reason_code='MANUAL_RESOLUTION',reason_message='Tożsamość źródła została ręcznie przypisana',processed_at=NULL WHERE tenant_id=? AND run_id=? AND source_row=?`, ciID, ids, a.TenantID, runID, row); err != nil {
		return err
	}
	return nil
}
