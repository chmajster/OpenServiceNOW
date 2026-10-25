package cmdb

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/chmajster/OpenServiceNOW/internal/audit"
	"github.com/chmajster/OpenServiceNOW/internal/jobs"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
)

type ireJob struct {
	RunID uint64 `json:"run_id,string"`
	Phase string `json:"phase"`
}

const runColumns = `id,source_id,created_by,executor_id,mode,snapshot_scope,complete_snapshot,state,rule_versions,version,error_code,created_at,updated_at,finished_at`

func scanRun(row interface{ Scan(...any) error }) (ReconciliationRun, error) {
	var run ReconciliationRun
	err := row.Scan(&run.ID, &run.SourceID, &run.CreatedBy, &run.ExecutorID, &run.Mode, &run.SnapshotScope, &run.CompleteSnapshot, &run.State, &run.RuleVersions, &run.Version, &run.ErrorCode, &run.CreatedAt, &run.UpdatedAt, &run.FinishedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return run, ErrNotFound
	}
	if run.Counts == nil {
		run.Counts = map[string]int{}
	}
	return run, err
}

func (s *Service) runInTx(ctx context.Context, e database.Executor, a Actor, id uint64, lock bool) (ReconciliationRun, error) {
	query := "SELECT " + runColumns + " FROM cmdb_ire_runs WHERE tenant_id=? AND id=?"
	if lock {
		query += " FOR UPDATE"
	}
	run, err := scanRun(e.QueryRowContext(ctx, query, a.TenantID, id))
	if err != nil {
		return run, err
	}
	if run.CreatedBy != a.UserID && run.ExecutorID != a.UserID && !canManageIRE(a) {
		return ReconciliationRun{}, ErrNotFound
	}
	return run, nil
}

func (s *Service) enqueueIRE(ctx context.Context, e database.Executor, a Actor, run ReconciliationRun, phase string) error {
	payload, err := json.Marshal(ireJob{RunID: run.ID, Phase: phase})
	if err != nil {
		return err
	}
	_, _, err = jobs.NewRepository(nil).EnqueueInTx(ctx, e, jobs.EnqueueInput{TenantID: a.TenantID, Type: "cmdb.ire", Payload: payload, CreatedBy: &a.UserID, MaxAttempts: 8, IdempotencyKey: fmt.Sprintf("cmdb-ire:%d:%d:%s", run.ID, run.Version, phase)})
	return err
}

func validateRunInput(input ReconciliationRunInput) error {
	if input.SourceID == 0 || len(input.IdempotencyKey) < 8 || len(input.IdempotencyKey) > 128 || input.Mode != "INCREMENTAL" && input.Mode != "FULL" || len(input.Observations) == 0 || len(input.Observations) > 500 || len(input.SnapshotScope) > 255 || input.CompleteSnapshot && input.Mode != "FULL" || input.Mode == "FULL" && strings.TrimSpace(input.SnapshotScope) == "" {
		return ErrInvalid
	}
	if !assetsText(input.IdempotencyKey, 8, 128) {
		return ErrInvalid
	}
	return nil
}

func observationHash(input ReconciliationRunInput) ([32]byte, error) {
	data, err := json.Marshal(struct {
		SourceID         uint64        `json:"source_id"`
		Mode             string        `json:"mode"`
		Scope            string        `json:"snapshot_scope"`
		CompleteSnapshot bool          `json:"complete_snapshot"`
		Observations     []Observation `json:"observations"`
	}{input.SourceID, input.Mode, input.SnapshotScope, input.CompleteSnapshot, input.Observations})
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(data), nil
}

func (s *Service) CreateReconciliationRun(ctx context.Context, a Actor, input ReconciliationRunInput) (ReconciliationRun, error) {
	if !canManageIRE(a) {
		return ReconciliationRun{}, workrecords.ErrForbidden
	}
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	input.SnapshotScope = strings.TrimSpace(input.SnapshotScope)
	if err := validateRunInput(input); err != nil {
		return ReconciliationRun{}, err
	}
	hash, err := observationHash(input)
	if err != nil {
		return ReconciliationRun{}, err
	}
	var out ReconciliationRun
	err = s.ActorTx(ctx, a, func(e database.Executor, current Actor) error {
		if !canManageIRE(current) {
			return workrecords.ErrForbidden
		}
		source, err := s.sourceInTx(ctx, e, current.TenantID, input.SourceID, true)
		if err != nil {
			return err
		}
		if source.State != "ACTIVE" || source.Type == "MANUAL" {
			return ErrConflict
		}
		var existing ReconciliationRun
		var existingHash []byte
		err = e.QueryRowContext(ctx, "SELECT "+runColumns+",payload_hash FROM cmdb_ire_runs WHERE tenant_id=? AND source_id=? AND idempotency_key=? FOR UPDATE", current.TenantID, source.ID, input.IdempotencyKey).Scan(&existing.ID, &existing.SourceID, &existing.CreatedBy, &existing.ExecutorID, &existing.Mode, &existing.SnapshotScope, &existing.CompleteSnapshot, &existing.State, &existing.RuleVersions, &existing.Version, &existing.ErrorCode, &existing.CreatedAt, &existing.UpdatedAt, &existing.FinishedAt, &existingHash)
		if err == nil {
			if !equalBytes(existingHash, hash[:]) {
				return workrecords.ErrOperationConflict
			}
			out = existing
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err = s.ensureDefaultRules(ctx, e, current); err != nil {
			return err
		}
		normalized := make([]Observation, len(input.Observations))
		seen := map[string]bool{}
		ruleVersions := map[string]uint64{}
		batchBytes := 0
		now := time.Now().UTC()
		for index, observation := range input.Observations {
			observation.ScopeKey = strings.TrimSpace(observation.ScopeKey)
			if observation.ScopeKey == "" {
				if input.Mode == "FULL" {
					observation.ScopeKey = input.SnapshotScope
				} else {
					observation.ScopeKey = source.ScopeKey
				}
			}
			if input.Mode == "FULL" && observation.ScopeKey != input.SnapshotScope || !assetsText(strings.TrimSpace(observation.NativeKey), 1, 255) || len(observation.ScopeKey) > 255 || len(observation.Values) > 64 {
				return ErrInvalid
			}
			observation.NativeKey = strings.TrimSpace(observation.NativeKey)
			key := observation.ScopeKey + "\x00" + observation.NativeKey
			if seen[key] {
				return fmt.Errorf("%w: duplikat klucza zewnętrznego w jednej partii", ErrInvalid)
			}
			seen[key] = true
			if observation.ObservedAt.IsZero() {
				observation.ObservedAt = now
			} else if observation.ObservedAt.After(now.Add(5*time.Minute)) || observation.ObservedAt.Before(time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)) {
				return ErrInvalid
			}
			for field, raw := range observation.Values {
				if !fieldKeyPattern.MatchString(field) || len(raw) == 0 || len(raw) > 4096 || !json.Valid(raw) {
					return ErrInvalid
				}
				batchBytes += len(field) + len(raw)
			}
			if batchBytes > 5<<20 {
				return fmt.Errorf("%w: partia obserwacji przekracza limit 5 MiB", ErrInvalid)
			}
			class, _ := observationString(observation.Values["class"])
			if _, valid := ClassDefinition(class); !valid {
				var mappedClass string
				mapErr := e.QueryRowContext(ctx, `SELECT c.class FROM cmdb_ire_external_identities x JOIN cmdb_cis c ON c.tenant_id=x.tenant_id AND c.id=x.ci_id WHERE x.tenant_id=? AND x.source_id=? AND x.scope_key=? AND x.source_native_key=?`, current.TenantID, source.ID, observation.ScopeKey, observation.NativeKey).Scan(&mappedClass)
				if mapErr != nil && !errors.Is(mapErr, sql.ErrNoRows) {
					return mapErr
				}
				class = mappedClass
			}
			if _, valid := ClassDefinition(class); valid {
				rule, ruleErr := s.ruleInTx(ctx, e, current.TenantID, class, 0, false)
				if ruleErr != nil {
					return ruleErr
				}
				ruleVersions[class] = rule.Version
			}
			normalized[index] = observation
		}
		input.Observations = normalized
		versionData, _ := json.Marshal(ruleVersions)
		result, err := e.ExecContext(ctx, `INSERT INTO cmdb_ire_runs(tenant_id,source_id,created_by,executor_id,idempotency_key,payload_hash,mode,snapshot_scope,complete_snapshot,state,rule_versions,version) VALUES(?,?,?,?,?,?,?,?,?,'QUEUED_DRY_RUN',?,1)`, current.TenantID, source.ID, current.UserID, current.UserID, input.IdempotencyKey, hash[:], input.Mode, input.SnapshotScope, input.CompleteSnapshot, versionData)
		if err != nil {
			return duplicateAsConflict(err)
		}
		id, err := result.LastInsertId()
		if err != nil {
			return err
		}
		out = ReconciliationRun{ID: uint64(id), SourceID: source.ID, CreatedBy: current.UserID, ExecutorID: current.UserID, Mode: input.Mode, SnapshotScope: input.SnapshotScope, CompleteSnapshot: input.CompleteSnapshot, State: "QUEUED_DRY_RUN", RuleVersions: versionData, Version: 1, Counts: map[string]int{}}
		for index, observation := range input.Observations {
			payload, marshalErr := json.Marshal(observation.Values)
			if marshalErr != nil {
				return marshalErr
			}
			class, _ := observationString(observation.Values["class"])
			rowRuleVersion := ruleVersions[class]
			if _, err = e.ExecContext(ctx, `INSERT INTO cmdb_ire_observations(tenant_id,run_id,source_row,scope_key,source_native_key,parent_ci_id,observed_at,values_json,result_state,rule_version,candidates,accepted_changes,rejected_changes) VALUES(?,?,?,?,?,?,?,?,'PENDING',?,?,?,?)`, current.TenantID, id, index+1, observation.ScopeKey, observation.NativeKey, nullableID(observation.ParentCIID), observation.ObservedAt.UTC(), payload, rowRuleVersion, json.RawMessage(`[]`), json.RawMessage(`{}`), json.RawMessage(`{}`)); err != nil {
				return err
			}
		}
		if err = s.enqueueIRE(ctx, e, current, out, "dry_run"); err != nil {
			return err
		}
		return audit.NewMySQLRepository().Append(ctx, e, audit.Event{TenantID: &current.TenantID, ActorUserID: &current.UserID, Action: "cmdb.ire.run.created", ObjectType: "cmdb_ire_run", ObjectID: fmt.Sprint(id), NewValues: map[string]any{"source_id": source.ID, "mode": input.Mode, "complete_snapshot": input.CompleteSnapshot, "record_count": len(input.Observations)}, RequestID: current.RequestID, IPAddress: current.IPAddress, Source: "cmdb.ire"})
	})
	if err != nil {
		return out, err
	}
	return s.GetReconciliationRun(ctx, a, out.ID)
}

func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for index := range a {
		if a[index] != b[index] {
			return false
		}
	}
	return true
}

func (s *Service) GetReconciliationRun(ctx context.Context, a Actor, id uint64) (ReconciliationRun, error) {
	if !canManageIRE(a) {
		return ReconciliationRun{}, workrecords.ErrForbidden
	}
	run, err := s.runInTx(ctx, s.DB, a, id, false)
	if err != nil {
		return run, err
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT result_state,COUNT(*) FROM cmdb_ire_observations WHERE tenant_id=? AND run_id=? GROUP BY result_state`, a.TenantID, id)
	if err != nil {
		return run, err
	}
	defer rows.Close()
	for rows.Next() {
		var state string
		var count int
		if err = rows.Scan(&state, &count); err != nil {
			return run, err
		}
		run.Counts[state] = count
	}
	return run, rows.Err()
}

func (s *Service) ListReconciliationRuns(ctx context.Context, a Actor, before uint64, limit int) (IREPage[ReconciliationRun], error) {
	page := IREPage[ReconciliationRun]{Items: []ReconciliationRun{}}
	if !canManageIRE(a) {
		return page, workrecords.ErrForbidden
	}
	if limit < 1 || limit > 100 {
		return page, ErrInvalid
	}
	query := "SELECT " + runColumns + " FROM cmdb_ire_runs WHERE tenant_id=?"
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
		run, err := scanRun(rows)
		if err != nil {
			return page, err
		}
		page.Items = append(page.Items, run)
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

const observationColumns = `id,source_row,scope_key,source_native_key,COALESCE(parent_ci_id,0),observed_at,values_json,result_state,COALESCE(ci_id,0),rule_version,candidates,accepted_changes,rejected_changes,reason_code,reason_message`

func scanObservation(row interface{ Scan(...any) error }) (ReconciliationObservation, error) {
	var observation ReconciliationObservation
	var values, candidates, accepted, rejected []byte
	err := row.Scan(&observation.ID, &observation.Row, &observation.Observation.ScopeKey, &observation.Observation.NativeKey, &observation.Observation.ParentCIID, &observation.Observation.ObservedAt, &values, &observation.State, &observation.CIID, &observation.RuleVersion, &candidates, &accepted, &rejected, &observation.Reason, &observation.Message)
	if err != nil {
		return observation, err
	}
	if err = json.Unmarshal(values, &observation.Observation.Values); err != nil {
		return observation, err
	}
	_ = json.Unmarshal(candidates, &observation.Candidates)
	_ = json.Unmarshal(accepted, &observation.Accepted)
	_ = json.Unmarshal(rejected, &observation.Rejected)
	if observation.Candidates == nil {
		observation.Candidates = []uint64{}
	}
	if observation.Accepted == nil {
		observation.Accepted = map[string]json.RawMessage{}
	}
	if observation.Rejected == nil {
		observation.Rejected = map[string]string{}
	}
	return observation, nil
}

func (s *Service) ReconciliationRows(ctx context.Context, a Actor, id uint64, after uint32, limit int) ([]ReconciliationObservation, uint32, error) {
	if !canManageIRE(a) {
		return nil, 0, workrecords.ErrForbidden
	}
	if limit < 1 || limit > 100 {
		return nil, 0, ErrInvalid
	}
	if _, err := s.runInTx(ctx, s.DB, a, id, false); err != nil {
		return nil, 0, err
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT `+observationColumns+` FROM cmdb_ire_observations WHERE tenant_id=? AND run_id=? AND source_row>? ORDER BY source_row LIMIT ?`, a.TenantID, id, after, limit+1)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []ReconciliationObservation{}
	for rows.Next() {
		row, err := scanObservation(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, row)
	}
	if err = rows.Err(); err != nil {
		return nil, 0, err
	}
	var next uint32
	if len(out) > limit {
		out = out[:limit]
		next = out[limit-1].Row
	}
	return out, next, nil
}

func (s *Service) RunAction(ctx context.Context, a Actor, id, version uint64, action string) (ReconciliationRun, error) {
	if !canManageIRE(a) {
		return ReconciliationRun{}, workrecords.ErrForbidden
	}
	err := s.ActorTx(ctx, a, func(e database.Executor, current Actor) error {
		if !canManageIRE(current) {
			return workrecords.ErrForbidden
		}
		run, err := s.runInTx(ctx, e, current, id, true)
		if err != nil {
			return err
		}
		if run.Version != version {
			return ErrConflict
		}
		phase := ""
		switch action {
		case "apply":
			if run.State != "DRY_RUN" && run.State != "PARTIAL" {
				return ErrConflict
			}
			phase = "apply"
			run.State = "QUEUED_APPLY"
		case "cancel":
			if run.State != "QUEUED_DRY_RUN" && run.State != "DRY_RUN" && run.State != "QUEUED_APPLY" {
				return ErrConflict
			}
			run.State = "CANCELLED"
		default:
			return ErrInvalid
		}
		run.Version++
		if _, err = e.ExecContext(ctx, `UPDATE cmdb_ire_runs SET state=?,version=?,executor_id=?,updated_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND id=? AND version=?`, run.State, run.Version, current.UserID, current.TenantID, id, version); err != nil {
			return err
		}
		if phase != "" {
			if err = s.enqueueIRE(ctx, e, current, run, phase); err != nil {
				return err
			}
		}
		return audit.NewMySQLRepository().Append(ctx, e, audit.Event{TenantID: &current.TenantID, ActorUserID: &current.UserID, Action: "cmdb.ire.run." + action, ObjectType: "cmdb_ire_run", ObjectID: fmt.Sprint(id), NewValues: map[string]any{"state": run.State, "version": run.Version}, RequestID: current.RequestID, IPAddress: current.IPAddress, Source: "cmdb.ire"})
	})
	if err != nil {
		return ReconciliationRun{}, err
	}
	return s.GetReconciliationRun(ctx, a, id)
}

func (s *Service) IREJobHandler(ctx context.Context, job jobs.Job) (json.RawMessage, error) {
	var payload ireJob
	if json.Unmarshal(job.Payload, &payload) != nil || payload.RunID == 0 || job.TenantID == 0 || job.CreatedBy == nil || payload.Phase != "dry_run" && payload.Phase != "apply" {
		return nil, jobs.ErrInvalid
	}
	a := Actor{Actor: workrecords.Actor{TenantID: job.TenantID, UserID: *job.CreatedBy, RequestID: fmt.Sprintf("cmdb-ire-job-%d", job.ID)}}
	var run ReconciliationRun
	err := s.ActorTx(ctx, a, func(e database.Executor, current Actor) error {
		var err error
		run, err = s.runInTx(ctx, e, current, payload.RunID, true)
		if err != nil {
			return err
		}
		if current.UserID != run.ExecutorID || !canManageIRE(current) {
			return workrecords.ErrForbidden
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if payload.Phase == "dry_run" {
		err = s.processRunDryRun(ctx, a, run)
	} else {
		err = s.processRunApply(ctx, a, run)
	}
	if err != nil {
		return nil, err
	}
	return json.RawMessage(`{"processed":true}`), nil
}

func (s *Service) processRunDryRun(ctx context.Context, a Actor, run ReconciliationRun) error {
	for batch := 0; batch < 25; batch++ {
		done := false
		err := s.ActorTx(ctx, a, func(e database.Executor, current Actor) error {
			locked, err := s.runInTx(ctx, e, current, run.ID, true)
			if err != nil {
				return err
			}
			if locked.State != "QUEUED_DRY_RUN" {
				done = true
				return nil
			}
			if !canManageIRE(current) {
				return workrecords.ErrForbidden
			}
			row, err := scanObservation(e.QueryRowContext(ctx, `SELECT `+observationColumns+` FROM cmdb_ire_observations WHERE tenant_id=? AND run_id=? AND result_state='PENDING' ORDER BY source_row LIMIT 1 FOR UPDATE`, a.TenantID, run.ID))
			if errors.Is(err, sql.ErrNoRows) {
				done = true
				locked.Version++
				_, err = e.ExecContext(ctx, `UPDATE cmdb_ire_runs SET state='DRY_RUN',version=?,updated_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND id=?`, locked.Version, a.TenantID, run.ID)
				return err
			}
			if err != nil {
				return err
			}
			source, err := s.sourceInTx(ctx, e, a.TenantID, locked.SourceID, false)
			if err != nil {
				return err
			}
			resolved, err := s.resolveObservation(ctx, e, current, source, row.Observation, row.RuleVersion)
			if err != nil {
				return err
			}
			candidates, _ := json.Marshal(resolved.ids)
			if _, err = e.ExecContext(ctx, `UPDATE cmdb_ire_observations SET result_state=?,ci_id=?,candidates=?,rule_version=?,reason_code=?,reason_message=? WHERE tenant_id=? AND run_id=? AND source_row=?`, resolved.state, nullableID(resolved.ciID), candidates, resolved.rule.Version, resolved.why, resolved.text, a.TenantID, run.ID, row.Row); err != nil {
				return err
			}
			if resolved.state == "CONFLICT" || resolved.state == "MULTIPLE" {
				kind := "IDENTITY"
				if resolved.state == "MULTIPLE" {
					kind = "DUPLICATE"
				}
				return s.createIREConflict(ctx, e, current, source, run.ID, row.ID, kind, row.Observation.ScopeKey+"::"+row.Observation.NativeKey, resolved.ids, map[string]any{"values": row.Observation.Values, "reason": resolved.why}, map[string]any{})
			}
			return nil
		})
		if err != nil {
			return err
		}
		if done {
			return nil
		}
	}
	return s.continueIREJob(ctx, a, run.ID, "dry_run", "QUEUED_DRY_RUN")
}

func (s *Service) processRunApply(ctx context.Context, a Actor, run ReconciliationRun) error {
	for batch := 0; batch < 25; batch++ {
		done := false
		err := s.ActorTx(ctx, a, func(e database.Executor, current Actor) error {
			locked, err := s.runInTx(ctx, e, current, run.ID, true)
			if err != nil {
				return err
			}
			if locked.State != "QUEUED_APPLY" && locked.State != "APPLYING" {
				done = true
				return nil
			}
			if !canManageIRE(current) {
				return workrecords.ErrForbidden
			}
			row, err := scanObservation(e.QueryRowContext(ctx, `SELECT `+observationColumns+` FROM cmdb_ire_observations WHERE tenant_id=? AND run_id=? AND result_state IN('MATCH','NEW') ORDER BY source_row LIMIT 1 FOR UPDATE`, a.TenantID, run.ID))
			if errors.Is(err, sql.ErrNoRows) {
				done = true
				var remaining int
				if err = e.QueryRowContext(ctx, `SELECT COUNT(*) FROM cmdb_ire_observations WHERE tenant_id=? AND run_id=? AND result_state<>'APPLIED'`, a.TenantID, run.ID).Scan(&remaining); err != nil {
					return err
				}
				state := "COMPLETED"
				if remaining > 0 {
					state = "PARTIAL"
				}
				locked.Version++
				if _, err = e.ExecContext(ctx, `UPDATE cmdb_ire_runs SET state=?,version=?,finished_at=UTC_TIMESTAMP(6),updated_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND id=?`, state, locked.Version, a.TenantID, run.ID); err != nil {
					return err
				}
				if state == "COMPLETED" {
					if _, err = e.ExecContext(ctx, `UPDATE cmdb_ire_sources SET last_successful_read_at=UTC_TIMESTAMP(6),updated_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND id=?`, a.TenantID, locked.SourceID); err != nil {
						return err
					}
					if locked.Mode == "FULL" && locked.CompleteSnapshot {
						if _, err = e.ExecContext(ctx, `UPDATE cmdb_ire_external_identities SET stale_since=COALESCE(stale_since,UTC_TIMESTAMP(6)) WHERE tenant_id=? AND source_id=? AND scope_key=? AND (last_seen_run_type<>'IRE_RUN' OR last_seen_run_id<>?)`, a.TenantID, locked.SourceID, locked.SnapshotScope, run.ID); err != nil {
							return err
						}
					}
				}
				return audit.NewMySQLRepository().Append(ctx, e, audit.Event{TenantID: &a.TenantID, ActorUserID: &a.UserID, Action: "cmdb.ire.run.finished", ObjectType: "cmdb_ire_run", ObjectID: fmt.Sprint(run.ID), NewValues: map[string]any{"state": state}, RequestID: a.RequestID, IPAddress: a.IPAddress, Source: "cmdb.ire"})
			}
			if err != nil {
				return err
			}
			source, err := s.sourceInTx(ctx, e, a.TenantID, locked.SourceID, false)
			if err != nil {
				return err
			}
			result, err := s.applyObservationInTx(ctx, e, current, source, row.Observation, "IRE_RUN", run.ID, row.RuleVersion)
			if err != nil {
				return err
			}
			accepted, _ := json.Marshal(result.Accepted)
			rejected, _ := json.Marshal(result.Rejected)
			candidates, _ := json.Marshal(result.Candidates)
			state := result.State
			if state != "APPLIED" {
				if state != "CONFLICT" && state != "MULTIPLE" && state != "INSUFFICIENT" && state != "REJECTED" {
					state = "REJECTED"
				}
			}
			if _, err = e.ExecContext(ctx, `UPDATE cmdb_ire_observations SET result_state=?,ci_id=?,candidates=?,rule_version=?,accepted_changes=?,rejected_changes=?,reason_code=?,reason_message=?,processed_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND run_id=? AND source_row=?`, state, nullableID(result.CIID), candidates, result.RuleVersion, accepted, rejected, result.Reason, result.Message, a.TenantID, run.ID, row.Row); err != nil {
				return err
			}
			if state == "CONFLICT" || state == "MULTIPLE" {
				kind := "IDENTITY"
				if state == "MULTIPLE" {
					kind = "DUPLICATE"
				}
				if err = s.createIREConflict(ctx, e, current, source, run.ID, row.ID, kind, row.Observation.ScopeKey+"::"+row.Observation.NativeKey, result.Candidates, map[string]any{"values": row.Observation.Values, "reason": result.Reason}, map[string]any{}); err != nil {
					return err
				}
			}
			if locked.State == "QUEUED_APPLY" {
				locked.Version++
				if _, err = e.ExecContext(ctx, `UPDATE cmdb_ire_runs SET state='APPLYING',version=?,updated_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND id=?`, locked.Version, a.TenantID, run.ID); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
		if done {
			return nil
		}
	}
	return s.continueIREJob(ctx, a, run.ID, "apply", "APPLYING")
}

func (s *Service) continueIREJob(ctx context.Context, a Actor, id uint64, phase, expectedState string) error {
	return s.ActorTx(ctx, a, func(e database.Executor, current Actor) error {
		run, err := s.runInTx(ctx, e, current, id, true)
		if err != nil {
			return err
		}
		if run.State != expectedState {
			return nil
		}
		run.Version++
		if _, err = e.ExecContext(ctx, `UPDATE cmdb_ire_runs SET version=?,updated_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND id=?`, run.Version, a.TenantID, id); err != nil {
			return err
		}
		return s.enqueueIRE(ctx, e, current, run, phase)
	})
}
