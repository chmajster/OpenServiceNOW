package incidents

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/chmajster/OpenServiceNOW/internal/audit"
	"github.com/chmajster/OpenServiceNOW/internal/events"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
)

type majorScanner interface{ Scan(...any) error }

const majorColumns = `i.number,i.title,i.state,i.priority,c.status,c.candidate_reason,c.decision_reason,c.nominated_by,c.nominated_at,c.decided_by,c.decided_at,c.commander_user_id,c.deputy_user_id,c.communications_owner_id,c.bridge_url,c.impact_summary,c.impact_confidence,c.workaround,c.communication_audience,c.update_interval_minutes,c.next_update_at,c.detected_at,c.restored_at,c.version,c.updated_at,CASE WHEN rv.completed_at IS NOT NULL THEN 'COMPLETE' WHEN rv.incident_id IS NOT NULL THEN 'IN_PROGRESS' ELSE 'NOT_STARTED' END,(SELECT COUNT(*) FROM incident_major_actions ac WHERE ac.tenant_id=c.tenant_id AND ac.incident_id=c.incident_id AND ac.status IN('OPEN','IN_PROGRESS')),(SELECT COUNT(*) FROM incident_major_actions ac WHERE ac.tenant_id=c.tenant_id AND ac.incident_id=c.incident_id AND ac.status IN('OPEN','IN_PROGRESS') AND ac.owner_user_id IS NULL),(SELECT MAX(mu.occurred_at) FROM incident_major_updates mu WHERE mu.tenant_id=c.tenant_id AND mu.incident_id=c.incident_id)`
const majorJoins = ` FROM incident_major_cases c JOIN incidents i ON i.tenant_id=c.tenant_id AND i.id=c.incident_id LEFT JOIN incident_major_reviews rv ON rv.tenant_id=c.tenant_id AND rv.incident_id=c.incident_id `

func scanMajor(row majorScanner, incidentID uint64) (MajorCase, error) {
	var item MajorCase
	item.IncidentID = incidentID
	var nominated, decided, commander, deputy, owner sql.Null[uint64]
	var decidedAt, next, detected, restored sql.NullTime
	err := row.Scan(&item.Number, &item.Title, &item.IncidentState, &item.Priority, &item.Status, &item.CandidateReason, &item.DecisionReason, &nominated, &item.NominatedAt, &decided, &decidedAt, &commander, &deputy, &owner, &item.BridgeURL, &item.ImpactSummary, &item.ImpactConfidence, &item.Workaround, &item.CommunicationAudience, &item.UpdateIntervalMinutes, &next, &detected, &restored, &item.Version, &item.UpdatedAt, &item.ReviewStatus, &item.OpenActions, &item.UnassignedActions, &item.LastUpdateAt)
	if errors.Is(err, sql.ErrNoRows) {
		return MajorCase{}, ErrNotFound
	}
	if err != nil {
		return MajorCase{}, err
	}
	if nominated.Valid {
		item.NominatedBy = nominated.V
	}
	if decided.Valid {
		item.DecidedBy = &decided.V
	}
	if decidedAt.Valid {
		item.DecidedAt = &decidedAt.Time
	}
	if commander.Valid {
		item.CommanderUserID = &commander.V
	}
	if deputy.Valid {
		item.DeputyUserID = &deputy.V
	}
	if owner.Valid {
		item.CommunicationsOwnerID = &owner.V
	}
	if next.Valid {
		item.NextUpdateAt = &next.Time
	}
	if detected.Valid {
		item.DetectedAt = &detected.Time
	}
	if restored.Valid {
		item.RestoredAt = &restored.Time
	}
	return item, nil
}

func (s *Service) majorCase(ctx context.Context, exec database.Executor, tenant, incidentID uint64, lock bool) (MajorCase, error) {
	query := "SELECT " + majorColumns + majorJoins + " WHERE c.tenant_id=? AND c.incident_id=?"
	if lock {
		query += " FOR UPDATE"
	}
	return scanMajor(exec.QueryRowContext(ctx, query, tenant, incidentID), incidentID)
}

func (s *Service) majorTx(ctx context.Context, a Actor, incidentID uint64, permission string, fn func(database.Executor, Record, Actor) error) error {
	return s.tx.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(exec database.Executor) error {
		principal, err := workrecords.LockPrincipal(ctx, exec, workrecords.Principal{Actor: workActor(a), IPAddress: a.IPAddress, Permissions: a.Permissions})
		if err != nil {
			return err
		}
		a.Permissions = principal.Permissions
		if permission != "" {
			allowed := false
			for _, candidate := range strings.Split(permission, "|") {
				if a.Permissions[candidate] {
					allowed = true
					break
				}
			}
			if !allowed {
				return ErrForbidden
			}
		}
		incident, err := s.repo.Get(ctx, exec, a.TenantID, incidentID, true)
		if err != nil {
			return err
		}
		if !readable(a, incident) {
			return ErrNotFound
		}
		return fn(exec, incident, a)
	})
}

func majorRequestHash(incidentID uint64, kind string, value any) ([32]byte, error) {
	raw, err := json.Marshal(struct {
		IncidentID string `json:"incident_id"`
		Kind       string `json:"kind"`
		Value      any    `json:"value"`
	}{strconv.FormatUint(incidentID, 10), kind, value})
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(raw), nil
}

func (s *Service) majorReplay(ctx context.Context, exec database.Executor, a Actor, key, kind string, digest [32]byte) (bool, error) {
	id, err := s.repo.reserveCollaboration(ctx, exec, a, key, "major."+strings.TrimPrefix(kind, "decision."), digest)
	if err != nil {
		return false, err
	}
	return id != 0, nil
}

type MajorDomainEvent struct {
	ResourceVersion uint64    `json:"resource_version,string,omitempty"`
	ActorUserID     uint64    `json:"actor_user_id,string"`
	OccurredAt      time.Time `json:"occurred_at"`
	Visibility      string    `json:"visibility"`
	IncidentID      uint64    `json:"incident_id,string"`
	CaseVersion     uint64    `json:"case_version,string"`
	ResourceID      uint64    `json:"resource_id,string,omitempty"`
	Audience        string    `json:"audience,omitempty"`
}

func (s *Service) recordMajorEvent(ctx context.Context, exec database.Executor, a Actor, incidentID, caseVersion uint64, kind, visibility, reason string, resourceID uint64, audience string, at time.Time) (uint64, error) {
	resourceVersion := uint64(0)
	if kind == "incident.major.action.changed" && resourceID != 0 {
		if err := exec.QueryRowContext(ctx, `SELECT ac.version,mc.version FROM incident_major_actions ac JOIN incident_major_cases mc ON mc.tenant_id=ac.tenant_id AND mc.incident_id=ac.incident_id WHERE ac.tenant_id=? AND ac.id=? AND ac.incident_id=?`, a.TenantID, resourceID, incidentID).Scan(&resourceVersion, &caseVersion); err != nil {
			return 0, err
		}
	}
	payload, err := json.Marshal(MajorDomainEvent{ResourceVersion: resourceVersion, ActorUserID: a.UserID, OccurredAt: at, Visibility: visibility, IncidentID: incidentID, CaseVersion: caseVersion, ResourceID: resourceID, Audience: audience})
	if err != nil {
		return 0, err
	}
	var auditActor *uint64
	if a.UserID != 0 {
		auditActor = &a.UserID
	}
	if err := s.audit.Append(ctx, exec, audit.Event{TenantID: &a.TenantID, ActorUserID: auditActor, Time: at, Action: kind, ObjectType: "incident_major", ObjectID: strconv.FormatUint(incidentID, 10), NewValues: map[string]any{"case_version": strconv.FormatUint(caseVersion, 10), "visibility": visibility, "resource_id": strconv.FormatUint(resourceID, 10), "reason": reason}, RequestID: a.RequestID, Source: a.source(), IPAddress: a.IPAddress}); err != nil {
		return 0, err
	}
	id, _, err := s.outbox.Append(ctx, exec, events.Input{TenantID: a.TenantID, Type: kind, AggregateType: "incident", AggregateID: strconv.FormatUint(incidentID, 10), Payload: payload, IdempotencyKey: fmt.Sprintf("incident-major:%d:%d:%s:%d:%d", incidentID, caseVersion, kind, resourceID, resourceVersion), AvailableAt: at})
	return id, err
}

func insertMajorDecision(ctx context.Context, exec database.Executor, a Actor, incidentID, version uint64, kind, reason string, at time.Time) error {
	_, err := exec.ExecContext(ctx, `INSERT INTO incident_major_decisions(tenant_id,incident_id,case_version,decision_type,reason,actor_user_id,occurred_at) VALUES(?,?,?,?,?,?,?)`, a.TenantID, incidentID, version, kind, reason, a.UserID, at)
	return err
}

func (s *Service) NominateMajor(ctx context.Context, a Actor, id uint64, input MajorDecisionInput, key string) (MajorCase, error) {
	input.Reason = strings.TrimSpace(input.Reason)
	if !a.Has("major.nominate") {
		return MajorCase{}, ErrForbidden
	}
	if !requestKeyPattern.MatchString(key) || input.Version == 0 || !validText(input.Reason, 1, 2000) {
		return MajorCase{}, ErrInvalid
	}
	digest, err := majorRequestHash(id, "nominate", input)
	if err != nil {
		return MajorCase{}, err
	}
	var result MajorCase
	err = s.majorTx(ctx, a, id, "incident.major.nominate", func(exec database.Executor, incident Record, a Actor) error {
		replay, err := s.majorReplay(ctx, exec, a, key, "nominate", digest)
		if err != nil {
			return err
		}
		if replay {
			result, err = s.majorCase(ctx, exec, a.TenantID, id, false)
			return err
		}
		if !editable(incident) || incident.State == "RESOLVED" {
			return ErrTransition
		}
		current, err := s.majorCase(ctx, exec, a.TenantID, id, true)
		if errors.Is(err, ErrNotFound) {
			if incident.Version != input.Version {
				return ErrConflict
			}
			now := s.now().UTC().Truncate(time.Microsecond)
			_, err = exec.ExecContext(ctx, `INSERT INTO incident_major_cases(tenant_id,incident_id,status,candidate_reason,nominated_by,nominated_at,created_at,updated_at) VALUES(?,?,'CANDIDATE',?,?, ?,?,?)`, a.TenantID, id, input.Reason, a.UserID, now, now, now)
			if err != nil {
				return err
			}
			current = MajorCase{IncidentID: id, Status: "CANDIDATE", CandidateReason: input.Reason, NominatedBy: a.UserID, NominatedAt: now, Version: 1, UpdatedAt: now}
		} else if err != nil {
			return err
		} else {
			if current.Version != input.Version {
				return ErrConflict
			}
			if current.Status != "REJECTED" && current.Status != "DEMOTED" {
				return ErrConflict
			}
			now := s.now().UTC().Truncate(time.Microsecond)
			_, err = exec.ExecContext(ctx, `UPDATE incident_major_cases SET status='CANDIDATE',candidate_reason=?,decision_reason='',nominated_by=?,nominated_at=?,decided_by=NULL,decided_at=NULL,commander_user_id=NULL,deputy_user_id=NULL,communications_owner_id=NULL,bridge_url='',impact_summary='',workaround='',next_update_at=NULL,detected_at=NULL,restored_at=NULL,version=version+1,updated_at=? WHERE tenant_id=? AND incident_id=? AND version=?`, input.Reason, a.UserID, now, now, a.TenantID, id, input.Version)
			if err != nil {
				return err
			}
			current, err = s.majorCase(ctx, exec, a.TenantID, id, false)
			if err != nil {
				return err
			}
		}
		if err := insertMajorDecision(ctx, exec, a, id, current.Version, "NOMINATED", input.Reason, current.NominatedAt); err != nil {
			return err
		}
		if err := s.repo.completeCollaboration(ctx, exec, a, key, id); err != nil {
			return err
		}
		if _, err := s.recordMajorEvent(ctx, exec, a, id, current.Version, "incident.major.candidate", "INTERNAL", input.Reason, 0, "", current.NominatedAt); err != nil {
			return err
		}
		result = current
		return nil
	})
	return result, err
}

func (s *Service) DecideMajor(ctx context.Context, a Actor, id uint64, action string, input MajorDecisionInput, key string) (MajorCase, error) {
	input.Reason = strings.TrimSpace(input.Reason)
	if !a.Has("major.manage") {
		return MajorCase{}, ErrForbidden
	}
	if !requestKeyPattern.MatchString(key) || input.Version == 0 || (action != "promote" && action != "reject" && action != "demote") || !validText(input.Reason, 1, 2000) {
		return MajorCase{}, ErrInvalid
	}
	digest, err := majorRequestHash(id, "decision."+action, input)
	if err != nil {
		return MajorCase{}, err
	}
	var result MajorCase
	err = s.majorTx(ctx, a, id, "incident.major.manage", func(exec database.Executor, incident Record, a Actor) error {
		replay, err := s.majorReplay(ctx, exec, a, key, "decision."+action, digest)
		if err != nil {
			return err
		}
		if replay {
			result, err = s.majorCase(ctx, exec, a.TenantID, id, false)
			return err
		}
		current, err := s.majorCase(ctx, exec, a.TenantID, id, true)
		if err != nil {
			return err
		}
		if current.Version != input.Version {
			return ErrConflict
		}
		if action == "promote" && (!editable(incident) || incident.State == "RESOLVED") {
			return ErrTransition
		}
		if action == "promote" && current.Status != "CANDIDATE" || action == "reject" && current.Status != "CANDIDATE" || action == "demote" && current.Status != "ACTIVE" {
			return ErrTransition
		}
		status, decision, eventType := "", "", ""
		switch action {
		case "promote":
			status, decision, eventType = "ACTIVE", "PROMOTED", "incident.major.promoted"
		case "reject":
			status, decision, eventType = "REJECTED", "REJECTED", "incident.major.rejected"
		case "demote":
			status, decision, eventType = "DEMOTED", "DEMOTED", "incident.major.demoted"
		}
		now := s.now().UTC().Truncate(time.Microsecond)
		var next any
		if status == "ACTIVE" {
			next = now.Add(time.Duration(current.UpdateIntervalMinutes) * time.Minute)
		}
		if _, err := exec.ExecContext(ctx, `UPDATE incident_major_cases SET status=?,decision_reason=?,decided_by=?,decided_at=?,next_update_at=?,version=version+1,updated_at=? WHERE tenant_id=? AND incident_id=? AND version=?`, status, input.Reason, a.UserID, now, next, now, a.TenantID, id, input.Version); err != nil {
			return err
		}
		result, err = s.majorCase(ctx, exec, a.TenantID, id, false)
		if err != nil {
			return err
		}
		if err = insertMajorDecision(ctx, exec, a, id, result.Version, decision, input.Reason, now); err != nil {
			return err
		}
		if err = s.repo.completeCollaboration(ctx, exec, a, key, id); err != nil {
			return err
		}
		_, err = s.recordMajorEvent(ctx, exec, a, id, result.Version, eventType, "INTERNAL", input.Reason, 0, "", now)
		return err
	})
	return result, err
}

func validateBridgeURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" || len(raw) > 2048 {
		return "", ErrInvalid
	}
	return u.String(), nil
}

func (s *Service) UpdateMajorCoordination(ctx context.Context, a Actor, id uint64, input MajorCoordinationInput, key string) (MajorCase, error) {
	input.BridgeURL = strings.TrimSpace(input.BridgeURL)
	input.ImpactSummary = strings.TrimSpace(input.ImpactSummary)
	input.Workaround = strings.TrimSpace(input.Workaround)
	if !requestKeyPattern.MatchString(key) || input.Version == 0 || input.UpdateIntervalMinutes < 5 || input.UpdateIntervalMinutes > 1440 || input.ImpactConfidence != "ESTIMATED" && input.ImpactConfidence != "CONFIRMED" || input.CommunicationAudience != "REPORTERS_AND_AFFECTED" && input.CommunicationAudience != "WATCHERS" && input.CommunicationAudience != "ALL_LINKED" || len(input.TeamGroupIDs) > 20 || len(input.ServiceIDs) > 100 || len(input.CIIDs) > 100 || len([]rune(input.ImpactSummary)) > 4000 || len([]rune(input.Workaround)) > 4000 {
		return MajorCase{}, ErrInvalid
	}
	bridge, err := validateBridgeURL(input.BridgeURL)
	if err != nil {
		return MajorCase{}, err
	}
	digest, err := majorRequestHash(id, "coordination", input)
	if err != nil {
		return MajorCase{}, err
	}
	var result MajorCase
	err = s.majorTx(ctx, a, id, "incident.major.manage", func(exec database.Executor, incident Record, a Actor) error {
		replay, err := s.majorReplay(ctx, exec, a, key, "coordination", digest)
		if err != nil {
			return err
		}
		if replay {
			result, err = s.majorCase(ctx, exec, a.TenantID, id, false)
			return err
		}
		current, err := s.majorCase(ctx, exec, a.TenantID, id, true)
		if err != nil {
			return err
		}
		if current.Version != input.Version {
			return ErrConflict
		}
		if current.Status != "ACTIVE" {
			return ErrTransition
		}
		if err := validateMajorPeople(ctx, exec, a.TenantID, input.CommanderUserID, input.DeputyUserID, input.CommunicationsOwnerID); err != nil {
			return err
		}
		for _, person := range []*uint64{input.CommanderUserID, input.DeputyUserID, input.CommunicationsOwnerID} {
			if err := s.validateActionOwner(ctx, exec, a.TenantID, id, person, true); err != nil {
				return err
			}
		}
		if err := validateMajorGroups(ctx, exec, a.TenantID, input.TeamGroupIDs); err != nil {
			return err
		}
		if len(input.ServiceIDs) > 0 && !a.Permissions["service_portfolio.read"] && !a.Permissions["service_portfolio.manage"] {
			return ErrForbidden
		}
		if len(input.CIIDs) > 0 && !a.Permissions["cmdb.read"] && !a.Permissions["cmdb.manage"] {
			return ErrForbidden
		}
		if err := validateMajorTargets(ctx, exec, a.TenantID, input.ServiceIDs, input.CIIDs); err != nil {
			return err
		}
		if input.DetectedAt != nil && input.DetectedAt.After(s.now().Add(time.Minute)) {
			return ErrInvalid
		}
		now := s.now().UTC().Truncate(time.Microsecond)
		var detected any
		if input.DetectedAt != nil {
			detected = input.DetectedAt.UTC().Truncate(time.Microsecond)
		} else if current.DetectedAt != nil {
			detected = *current.DetectedAt
		}
		var next any
		if input.NextUpdateAt != nil {
			if input.NextUpdateAt.Before(now) || input.NextUpdateAt.After(now.Add(7*24*time.Hour)) {
				return ErrInvalid
			}
			next = input.NextUpdateAt.UTC().Truncate(time.Microsecond)
		} else {
			next = now.Add(time.Duration(input.UpdateIntervalMinutes) * time.Minute)
		}
		if _, err = exec.ExecContext(ctx, `UPDATE incident_major_cases SET commander_user_id=?,deputy_user_id=?,communications_owner_id=?,bridge_url=?,impact_summary=?,impact_confidence=?,workaround=?,communication_audience=?,update_interval_minutes=?,next_update_at=?,detected_at=?,version=version+1,updated_at=? WHERE tenant_id=? AND incident_id=? AND version=?`, input.CommanderUserID, input.DeputyUserID, input.CommunicationsOwnerID, bridge, input.ImpactSummary, input.ImpactConfidence, input.Workaround, input.CommunicationAudience, input.UpdateIntervalMinutes, next, detected, now, a.TenantID, id, input.Version); err != nil {
			return err
		}
		if _, err = exec.ExecContext(ctx, `DELETE FROM incident_major_teams WHERE tenant_id=? AND incident_id=?`, a.TenantID, id); err != nil {
			return err
		}
		for _, groupID := range uniqueIDs(input.TeamGroupIDs) {
			if _, err = exec.ExecContext(ctx, `INSERT INTO incident_major_teams(tenant_id,incident_id,group_id,added_by,created_at) VALUES(?,?,?,?,?)`, a.TenantID, id, groupID, a.UserID, now); err != nil {
				return err
			}
		}
		deleteTypes := []string{}
		if a.Permissions["service_portfolio.read"] || a.Permissions["service_portfolio.manage"] {
			deleteTypes = append(deleteTypes, "SERVICE")
		}
		if a.Permissions["cmdb.read"] || a.Permissions["cmdb.manage"] {
			deleteTypes = append(deleteTypes, "CI")
		}
		for _, kind := range deleteTypes {
			if _, err = exec.ExecContext(ctx, `DELETE FROM incident_major_links WHERE tenant_id=? AND incident_id=? AND target_type=?`, a.TenantID, id, kind); err != nil {
				return err
			}
		}

		for _, serviceID := range uniqueIDs(input.ServiceIDs) {
			if _, err = exec.ExecContext(ctx, `INSERT INTO incident_major_links(tenant_id,incident_id,target_type,target_id,added_by,created_at) VALUES(?,?,'SERVICE',?,?,?)`, a.TenantID, id, serviceID, a.UserID, now); err != nil {
				return err
			}
		}
		for _, ciID := range uniqueIDs(input.CIIDs) {
			if _, err = exec.ExecContext(ctx, `INSERT INTO incident_major_links(tenant_id,incident_id,target_type,target_id,added_by,created_at) VALUES(?,?,'CI',?,?,?)`, a.TenantID, id, ciID, a.UserID, now); err != nil {
				return err
			}
		}
		result, err = s.majorCase(ctx, exec, a.TenantID, id, false)
		if err != nil {
			return err
		}
		if err = loadMajorAssociations(ctx, exec, a.TenantID, &result); err != nil {
			return err
		}
		result.Links = filterMajorLinks(a, result.Links)
		if err = s.repo.completeCollaboration(ctx, exec, a, key, id); err != nil {
			return err
		}
		_, err = s.recordMajorEvent(ctx, exec, a, id, result.Version, "incident.major.coordination.changed", "INTERNAL", "", 0, "", now)
		return err
	})
	return result, err
}

func validateMajorPeople(ctx context.Context, e database.Executor, tenant uint64, users ...*uint64) error {
	seen := map[uint64]bool{}
	for _, user := range users {
		if user == nil {
			continue
		}
		if *user == 0 {
			return ErrInvalid
		}
		if seen[*user] {
			continue
		}
		seen[*user] = true
		if err := workrecords.ValidateActiveUser(ctx, e, tenant, *user); err != nil {
			return ErrInvalid
		}
	}
	return nil
}
func uniqueIDs(values []uint64) []uint64 {
	seen := map[uint64]bool{}
	out := make([]uint64, 0, len(values))
	for _, id := range values {
		if id > 0 && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}
func validateMajorGroups(ctx context.Context, e database.Executor, tenant uint64, groups []uint64) error {
	for _, id := range uniqueIDs(groups) {
		var active bool
		if err := e.QueryRowContext(ctx, "SELECT active FROM \x60groups\x60 WHERE tenant_id=? AND id=?", tenant, id).Scan(&active); err != nil {
			return ErrInvalid
		}
		if !active {
			return ErrInvalid
		}
	}
	return nil
}
func validateMajorTargets(ctx context.Context, e database.Executor, tenant uint64, services, cis []uint64) error {
	for _, id := range uniqueIDs(services) {
		var active bool
		if err := e.QueryRowContext(ctx, `SELECT lifecycle='ACTIVE' FROM service_portfolio_items WHERE tenant_id=? AND id=?`, tenant, id).Scan(&active); err != nil {
			return ErrInvalid
		}
		if !active {
			return ErrInvalid
		}
	}
	for _, id := range uniqueIDs(cis) {
		var exists bool
		if err := e.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM cmdb_cis WHERE tenant_id=? AND id=? AND status<>'RETIRED')`, tenant, id).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return ErrInvalid
		}
	}
	return nil
}

func loadMajorAssociations(ctx context.Context, e database.Executor, tenant uint64, item *MajorCase) error {
	item.People = map[string]string{}
	people, peopleErr := e.QueryContext(ctx, `SELECT u.id,u.display_name FROM users u JOIN tenant_memberships tm ON tm.user_id=u.id AND tm.tenant_id=? WHERE u.id IN(?,?,?)`, tenant, item.CommanderUserID, item.DeputyUserID, item.CommunicationsOwnerID)
	if peopleErr != nil {
		return peopleErr
	}
	for people.Next() {
		var id uint64
		var name string
		if peopleErr = people.Scan(&id, &name); peopleErr != nil {
			people.Close()
			return peopleErr
		}
		item.People[strconv.FormatUint(id, 10)] = name
	}
	peopleErr = people.Err()
	people.Close()
	if peopleErr != nil {
		return peopleErr
	}
	item.Teams = []MajorTeam{}
	rows, err := e.QueryContext(ctx, "SELECT g.id,g.name FROM incident_major_teams mt JOIN \x60groups\x60 g ON g.tenant_id=mt.tenant_id AND g.id=mt.group_id WHERE mt.tenant_id=? AND mt.incident_id=? ORDER BY g.name,g.id", tenant, item.IncidentID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var team MajorTeam
		if err = rows.Scan(&team.GroupID, &team.GroupName); err != nil {
			rows.Close()
			return err
		}
		item.Teams = append(item.Teams, team)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	item.Links = []MajorLink{}
	rows, err = e.QueryContext(ctx, `SELECT l.target_type,l.target_id,CASE WHEN l.target_type='SERVICE' THEN p.name ELSE ci.name END FROM incident_major_links l LEFT JOIN service_portfolio_items p ON l.target_type='SERVICE' AND p.tenant_id=l.tenant_id AND p.id=l.target_id LEFT JOIN cmdb_cis ci ON l.target_type='CI' AND ci.tenant_id=l.tenant_id AND ci.id=l.target_id WHERE l.tenant_id=? AND l.incident_id=? ORDER BY l.target_type,l.target_id`, tenant, item.IncidentID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var link MajorLink
		if err = rows.Scan(&link.Type, &link.ID, &link.Name); err != nil {
			return err
		}
		item.Links = append(item.Links, link)
	}
	return rows.Err()
}

func (s *Service) ListMajor(ctx context.Context, a Actor, view string, after uint64, limit int) ([]MajorCase, uint64, error) {
	if !a.Has("major.read") && !a.Has("major.manage") {
		return nil, 0, ErrForbidden
	}
	if limit < 1 || limit > 100 || view != "" && view != "active" && view != "candidates" {
		return nil, 0, ErrInvalid
	}
	access, args := readPredicate(a, "i")
	query := "SELECT c.incident_id," + majorColumns + majorJoins + " WHERE c.tenant_id=? AND " + access + " AND c.incident_id<? AND c.status IN('CANDIDATE','ACTIVE','RESTORED')"
	params := []any{a.TenantID}
	params = append(params, args...)
	params = append(params, after)
	if after == 0 {
		query = strings.Replace(query, "AND c.incident_id<? AND", "AND c.incident_id>0 AND", 1)
		params = params[:len(params)-1]
	}
	if view == "active" {
		query += " AND c.status IN('ACTIVE','RESTORED')"
	}
	if view == "candidates" {
		query += " AND c.status='CANDIDATE'"
	}
	query += " ORDER BY c.incident_id DESC LIMIT ?"
	params = append(params, limit+1)
	rows, err := s.repo.db.QueryContext(ctx, query, params...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := []MajorCase{}
	for rows.Next() {
		var id uint64
		var item MajorCase
		var nominated, decided, commander, deputy, owner sql.Null[uint64]
		var decidedAt, next, detected, restored sql.NullTime
		err = rows.Scan(&id, &item.Number, &item.Title, &item.IncidentState, &item.Priority, &item.Status, &item.CandidateReason, &item.DecisionReason, &nominated, &item.NominatedAt, &decided, &decidedAt, &commander, &deputy, &owner, &item.BridgeURL, &item.ImpactSummary, &item.ImpactConfidence, &item.Workaround, &item.CommunicationAudience, &item.UpdateIntervalMinutes, &next, &detected, &restored, &item.Version, &item.UpdatedAt, &item.ReviewStatus, &item.OpenActions, &item.UnassignedActions, &item.LastUpdateAt)
		if err != nil {
			return nil, 0, err
		}
		item.IncidentID = id
		if nominated.Valid {
			item.NominatedBy = nominated.V
		}
		if decided.Valid {
			item.DecidedBy = &decided.V
		}
		if decidedAt.Valid {
			item.DecidedAt = &decidedAt.Time
		}
		if commander.Valid {
			item.CommanderUserID = &commander.V
		}
		if deputy.Valid {
			item.DeputyUserID = &deputy.V
		}
		if owner.Valid {
			item.CommunicationsOwnerID = &owner.V
		}
		if next.Valid {
			item.NextUpdateAt = &next.Time
		}
		if detected.Valid {
			item.DetectedAt = &detected.Time
		}
		if restored.Valid {
			item.RestoredAt = &restored.Time
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, 0, err
	}
	var nextID uint64
	if len(items) > limit {
		items = items[:limit]
		nextID = items[len(items)-1].IncidentID
	}
	return items, nextID, nil
}

func (s *Service) ListMyMajorActions(ctx context.Context, a Actor, after uint64, limit int) ([]MajorAction, uint64, error) {
	if a.TenantID == 0 || a.UserID == 0 || limit < 1 || limit > 100 {
		return nil, 0, ErrInvalid
	}
	if !a.Has("read") && !a.Has("read_own") && !a.Has("major.read") && !a.Has("major.manage") {
		return nil, 0, ErrForbidden
	}
	access, args := readPredicate(a, "i")
	query := `SELECT ac.id,ac.incident_id,ac.number,ac.description,ac.owner_user_id,COALESCE(u.display_name,''),ac.due_at,ac.status,ac.version,ac.updated_at FROM incident_major_actions ac JOIN incidents i ON i.tenant_id=ac.tenant_id AND i.id=ac.incident_id LEFT JOIN users u ON u.id=ac.owner_user_id WHERE ac.tenant_id=? AND ac.owner_user_id=? AND ac.id>? AND ` + access + ` ORDER BY ac.id LIMIT ?`
	params := []any{a.TenantID, a.UserID, after}
	params = append(params, args...)
	params = append(params, limit+1)
	rows, err := s.repo.db.QueryContext(ctx, query, params...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := []MajorAction{}
	for rows.Next() {
		var x MajorAction
		var owner sql.Null[uint64]
		if err = rows.Scan(&x.ID, &x.IncidentID, &x.Number, &x.Description, &owner, &x.OwnerName, &x.DueAt, &x.Status, &x.Version, &x.UpdatedAt); err != nil {
			return nil, 0, err
		}
		if owner.Valid {
			x.OwnerUserID = &owner.V
		}
		items = append(items, x)
	}
	if err = rows.Err(); err != nil {
		return nil, 0, err
	}
	var next uint64
	if len(items) > limit {
		items = items[:limit]
		next = items[len(items)-1].ID
	}
	return items, next, nil
}

func (s *Service) ChangeMajorAction(ctx context.Context, a Actor, id uint64, input MajorActionChangeInput) (MajorAction, error) {
	if id == 0 || input.Version == 0 || input.Status != "OPEN" && input.Status != "IN_PROGRESS" && input.Status != "DONE" && input.Status != "CANCELLED" {
		return MajorAction{}, ErrInvalid
	}
	var result MajorAction
	err := s.tx.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(exec database.Executor) error {
		var authErr error
		a, authErr = currentIncidentActor(ctx, exec, a)
		if authErr != nil {
			return authErr
		}
		var incidentID uint64
		var owner sql.Null[uint64]
		var status string
		var version uint64
		if err := exec.QueryRowContext(ctx, `SELECT incident_id,owner_user_id,status,version FROM incident_major_actions WHERE tenant_id=? AND id=? FOR UPDATE`, a.TenantID, id).Scan(&incidentID, &owner, &status, &version); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		incident, err := s.repo.Get(ctx, exec, a.TenantID, incidentID, false)
		if err != nil {
			return err
		}
		if !readable(a, incident) {
			return ErrNotFound
		}
		if !a.Has("major.manage") && (!owner.Valid || owner.V != a.UserID) {
			return ErrForbidden
		}
		if version != input.Version {
			return ErrConflict
		}
		if status == "DONE" || status == "CANCELLED" {
			return ErrTransition
		}
		now := s.now().UTC().Truncate(time.Microsecond)
		res, err := exec.ExecContext(ctx, `UPDATE incident_major_actions SET status=?,version=version+1,updated_at=? WHERE tenant_id=? AND id=? AND version=?`, input.Status, now, a.TenantID, id, input.Version)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return ErrConflict
		}
		if _, err = s.recordMajorEvent(ctx, exec, a, incidentID, version+1, "incident.major.action.changed", "INTERNAL", input.Status, id, "", now); err != nil {
			return err
		}
		return exec.QueryRowContext(ctx, `SELECT ac.id,ac.incident_id,ac.number,ac.description,ac.owner_user_id,COALESCE(u.display_name,''),ac.due_at,ac.status,ac.version,ac.updated_at FROM incident_major_actions ac JOIN incidents i ON i.tenant_id=ac.tenant_id AND i.id=ac.incident_id LEFT JOIN users u ON u.id=ac.owner_user_id WHERE ac.tenant_id=? AND ac.id=?`, a.TenantID, id).Scan(&result.ID, &result.IncidentID, &result.Number, &result.Description, &result.OwnerUserID, &result.OwnerName, &result.DueAt, &result.Status, &result.Version, &result.UpdatedAt)
	})
	return result, err
}
