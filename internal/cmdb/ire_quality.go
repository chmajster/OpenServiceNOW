package cmdb

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/chmajster/OpenServiceNOW/internal/jobs"
	"github.com/chmajster/OpenServiceNOW/internal/metadata"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
)

const QualityJobType = "cmdb.ire.quality"

type qualityJob struct {
	Requested bool `json:"requested"`
}

type qualityCI struct {
	id          uint64
	class       string
	identifier  string
	name        string
	status      string
	ownerID     uint64
	environment string
	location    string
	attributes  map[string]json.RawMessage
	lastSeen    sql.NullTime
	hasSource   bool
	hasExternal bool
	ownerActive bool
	staleSource bool
}

type qualityRelation struct {
	id       uint64
	sourceID uint64
	targetID uint64
	kind     string
	source   string
	target   string
}

func defaultRequiredQualityFields() map[string][]string {
	out := map[string][]string{}
	for _, class := range Classes {
		fields := []string{"identifier", "name", "status", "owner_id", "environment"}
		for _, attribute := range class.Attributes {
			if attribute.Required {
				fields = append(fields, "attributes."+attribute.Key)
			}
		}
		out[class.Key] = fields
	}
	return out
}

func (s *Service) QualityConfig(ctx context.Context, a Actor) (QualityConfig, error) {
	if !canReadQuality(a) {
		return QualityConfig{}, workrecords.ErrForbidden
	}
	if err := s.ActorTx(ctx, a, func(e database.Executor, current Actor) error {
		if !canReadQuality(current) {
			return workrecords.ErrForbidden
		}
		_, err := s.qualityConfigInTx(ctx, e, current, true)
		return err
	}); err != nil {
		return QualityConfig{}, err
	}
	return s.qualityConfigInTx(ctx, s.DB, a, false)
}

func (s *Service) qualityConfigInTx(ctx context.Context, e database.Executor, a Actor, create bool) (QualityConfig, error) {
	var config QualityConfig
	var raw []byte
	err := e.QueryRowContext(ctx, `SELECT enabled,schedule_hours,stale_days,required_fields,version,next_run_at FROM cmdb_ire_quality_config WHERE tenant_id=?`, a.TenantID).Scan(&config.Enabled, &config.ScheduleHours, &config.StaleDays, &raw, &config.Version, &config.NextRunAt)
	if errors.Is(err, sql.ErrNoRows) && create {
		required, _ := json.Marshal(defaultRequiredQualityFields())
		if _, err = e.ExecContext(ctx, `INSERT IGNORE INTO cmdb_ire_quality_config(tenant_id,enabled,schedule_hours,stale_days,required_fields,version,updated_by,next_run_at) VALUES(?,TRUE,24,90,?,1,?,UTC_TIMESTAMP(6))`, a.TenantID, required, a.UserID); err != nil {
			return config, err
		}
		return s.qualityConfigInTx(ctx, e, a, false)
	}
	if err != nil {
		return config, err
	}
	if err = json.Unmarshal(raw, &config.RequiredFields); err != nil {
		return config, err
	}
	return config, nil
}

func validateRequiredFields(fields map[string][]string) error {
	if len(fields) == 0 || len(fields) > len(Classes) {
		return ErrInvalid
	}
	for classKey, keys := range fields {
		class, ok := ClassDefinition(classKey)
		if !ok || len(keys) == 0 || len(keys) > 32 {
			return ErrInvalid
		}
		seen := map[string]bool{}
		for _, key := range keys {
			if !fieldKeyPattern.MatchString(key) || seen[key] {
				return ErrInvalid
			}
			if strings.HasPrefix(key, "attributes.") {
				attributeKey := strings.TrimPrefix(key, "attributes.")
				found := false
				for _, attribute := range class.Attributes {
					found = found || attribute.Key == attributeKey
				}
				if !found {
					return ErrInvalid
				}
			}
			seen[key] = true
		}
	}
	return nil
}

func (s *Service) SaveQualityConfig(ctx context.Context, a Actor, input QualityConfig) (QualityConfig, error) {
	if !(a.Has("cmdb.quality.manage") || a.Has("cmdb.ire.manage")) || !Writable(a) {
		return QualityConfig{}, workrecords.ErrForbidden
	}
	if input.Version == 0 || input.ScheduleHours < 1 || input.ScheduleHours > 720 || input.StaleDays < 1 || input.StaleDays > 3650 || validateRequiredFields(input.RequiredFields) != nil {
		return QualityConfig{}, ErrInvalid
	}
	err := s.ActorTx(ctx, a, func(e database.Executor, current Actor) error {
		if !(current.Has("cmdb.quality.manage") || current.Has("cmdb.ire.manage")) || !Writable(current) {
			return workrecords.ErrForbidden
		}
		config, err := s.qualityConfigInTx(ctx, e, current, true)
		if err != nil {
			return err
		}
		if config.Version != input.Version {
			return ErrConflict
		}
		required, _ := json.Marshal(input.RequiredFields)
		if _, err = e.ExecContext(ctx, `UPDATE cmdb_ire_quality_config SET enabled=?,schedule_hours=?,stale_days=?,required_fields=?,version=version+1,updated_by=?,updated_at=UTC_TIMESTAMP(6),next_run_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND version=?`, input.Enabled, input.ScheduleHours, input.StaleDays, required, current.UserID, current.TenantID, input.Version); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return QualityConfig{}, err
	}
	return s.QualityConfig(ctx, a)
}

func (s *Service) ScheduleQuality(ctx context.Context, now time.Time) (int, error) {
	count := 0
	err := s.tx.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(e database.Executor) error {
		rows, err := e.QueryContext(ctx, `SELECT tenant_id,updated_by,schedule_hours FROM cmdb_ire_quality_config WHERE enabled=TRUE AND next_run_at<=? ORDER BY next_run_at,tenant_id LIMIT 50 FOR UPDATE SKIP LOCKED`, now.UTC())
		if err != nil {
			return err
		}
		type dueConfig struct {
			tenant, user uint64
			hours        uint16
		}
		due := []dueConfig{}
		for rows.Next() {
			var item dueConfig
			if err = rows.Scan(&item.tenant, &item.user, &item.hours); err != nil {
				rows.Close()
				return err
			}
			due = append(due, item)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, item := range due {
			payload, _ := json.Marshal(qualityJob{})
			_, _, err = jobs.NewRepository(nil).EnqueueInTx(ctx, e, jobs.EnqueueInput{TenantID: item.tenant, Type: QualityJobType, Payload: payload, CreatedBy: &item.user, MaxAttempts: 5, IdempotencyKey: fmt.Sprintf("cmdb-quality:%d:%d", item.tenant, now.UTC().Unix()/int64(time.Duration(item.hours)*time.Hour/time.Second))})
			if err != nil {
				return err
			}
			next := now.UTC().Add(time.Duration(item.hours) * time.Hour)
			if _, err = e.ExecContext(ctx, `UPDATE cmdb_ire_quality_config SET next_run_at=? WHERE tenant_id=? AND enabled=TRUE`, next, item.tenant); err != nil {
				return err
			}
			count++
		}
		return nil
	})
	return count, err
}

func (s *Service) RunQualityNow(ctx context.Context, a Actor, key string) (uint64, error) {
	if !(a.Has("cmdb.quality.manage") || a.Has("cmdb.ire.manage")) || !Writable(a) {
		return 0, workrecords.ErrForbidden
	}
	if !assetsText(strings.TrimSpace(key), 8, 128) {
		return 0, ErrInvalid
	}
	var jobID uint64
	err := s.ActorTx(ctx, a, func(e database.Executor, current Actor) error {
		if !(current.Has("cmdb.quality.manage") || current.Has("cmdb.ire.manage")) || !Writable(current) {
			return workrecords.ErrForbidden
		}
		if _, err := s.qualityConfigInTx(ctx, e, current, true); err != nil {
			return err
		}
		payload, _ := json.Marshal(qualityJob{Requested: true})
		job, _, err := jobs.NewRepository(nil).EnqueueInTx(ctx, e, jobs.EnqueueInput{TenantID: current.TenantID, Type: QualityJobType, Payload: payload, CreatedBy: &current.UserID, MaxAttempts: 5, IdempotencyKey: "cmdb-quality:manual:" + key})
		if err != nil {
			return err
		}
		jobID = job.ID
		return nil
	})
	return jobID, err
}

func (s *Service) QualityJobHandler(ctx context.Context, job jobs.Job) (json.RawMessage, error) {
	var payload qualityJob
	if job.TenantID == 0 || job.CreatedBy == nil || json.Unmarshal(job.Payload, &payload) != nil {
		return nil, jobs.ErrInvalid
	}
	a := Actor{Actor: workrecords.Actor{TenantID: job.TenantID, UserID: *job.CreatedBy, RequestID: fmt.Sprintf("cmdb-quality-job-%d", job.ID)}}
	err := s.ActorTx(ctx, a, func(e database.Executor, current Actor) error {
		if !(current.Has("cmdb.quality.manage") || current.Has("cmdb.ire.manage")) {
			return workrecords.ErrForbidden
		}
		config, err := s.qualityConfigInTx(ctx, e, current, true)
		if err != nil {
			return err
		}
		return s.evaluateQualityInTx(ctx, e, current, config)
	})
	if err != nil {
		return nil, err
	}
	return json.RawMessage(`{"evaluated":true}`), nil
}

type qualityCounts struct {
	denominator uint64
	valid       uint64
	openKeys    map[uint64]bool
	openOther   uint64
}

func (s *Service) evaluateQualityInTx(ctx context.Context, e database.Executor, a Actor, config QualityConfig) error {
	if err := s.ensureDefaultRules(ctx, e, a); err != nil {
		return err
	}
	cutoff := time.Now().UTC().Add(-time.Duration(config.StaleDays) * 24 * time.Hour)
	rows, err := e.QueryContext(ctx, `SELECT c.id,c.class,c.identifier,c.name,c.status,c.owner_id,c.environment,c.location,c.attributes,c.last_seen_at,EXISTS(SELECT 1 FROM cmdb_ire_external_identities x WHERE x.tenant_id=c.tenant_id AND x.ci_id=c.id) AS has_external,(EXISTS(SELECT 1 FROM cmdb_ire_external_identities x WHERE x.tenant_id=c.tenant_id AND x.ci_id=c.id) OR EXISTS(SELECT 1 FROM cmdb_ire_field_provenance p WHERE p.tenant_id=c.tenant_id AND p.ci_id=c.id)) AS has_source,EXISTS(SELECT 1 FROM tenant_memberships m JOIN users u ON u.id=m.user_id WHERE m.tenant_id=c.tenant_id AND m.user_id=c.owner_id AND m.active=TRUE AND u.active=TRUE) AS owner_active,EXISTS(SELECT 1 FROM cmdb_ire_external_identities x JOIN cmdb_ire_sources ds ON ds.tenant_id=x.tenant_id AND ds.id=x.source_id AND ds.state='ACTIVE' WHERE x.tenant_id=c.tenant_id AND x.ci_id=c.id AND x.stale_since<?) AND NOT EXISTS(SELECT 1 FROM cmdb_ire_external_identities x JOIN cmdb_ire_sources ds ON ds.tenant_id=x.tenant_id AND ds.id=x.source_id AND ds.state='ACTIVE' WHERE x.tenant_id=c.tenant_id AND x.ci_id=c.id AND (x.stale_since IS NULL OR x.stale_since>=?)) AS stale_source FROM cmdb_cis c WHERE c.tenant_id=? AND c.archived=FALSE AND c.redirect_ci_id IS NULL ORDER BY c.id`, cutoff, cutoff, a.TenantID)
	if err != nil {
		return err
	}
	ciRows := []qualityCI{}
	for rows.Next() {
		var item qualityCI
		var raw []byte
		if err = rows.Scan(&item.id, &item.class, &item.identifier, &item.name, &item.status, &item.ownerID, &item.environment, &item.location, &raw, &item.lastSeen, &item.hasExternal, &item.hasSource, &item.ownerActive, &item.staleSource); err != nil {
			rows.Close()
			return err
		}
		if err = json.Unmarshal(raw, &item.attributes); err != nil {
			rows.Close()
			return err
		}
		ciRows = append(ciRows, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	findings := []QualityFinding{}
	counts := map[string]*qualityCounts{}
	for _, key := range []string{"required_fields", "duplicate_identity", "stale_ci", "invalid_reference", "invalid_relation", "owner_missing", "source_missing"} {
		counts[key] = &qualityCounts{openKeys: map[uint64]bool{}}
	}
	classRules := map[string]IdentityRule{}
	for _, class := range Classes {
		rule, ruleErr := s.ruleInTx(ctx, e, a.TenantID, class.Key, 0, false)
		if ruleErr != nil && !errors.Is(ruleErr, ErrNotFound) {
			return ruleErr
		}
		if ruleErr == nil {
			classRules[class.Key] = rule
		}
	}
	identityGroups := map[string]map[uint64]bool{}
	for _, ci := range ciRows {
		counts["required_fields"].denominator++
		counts["duplicate_identity"].denominator++
		counts["owner_missing"].denominator++
		counts["source_missing"].denominator++
		required := config.RequiredFields[ci.class]
		if len(required) == 0 {
			required = defaultRequiredQualityFields()[ci.class]
		}
		var missing []string
		for _, field := range required {
			value, ok := qualityFieldValue(ci, field)
			if !ok || isEmptyJSON(value) {
				missing = append(missing, field)
			}
		}
		if len(missing) > 0 {
			for _, field := range missing {
				findings = append(findings, qualityFinding("required_fields", "ERROR", ci.id, field, map[string]any{"reason": "required_value_missing", "field": field}))
			}
			counts["required_fields"].openKeys[ci.id] = true
		} else {
			counts["required_fields"].valid++
		}
		if !ci.ownerActive {
			findings = append(findings, qualityFinding("owner_missing", "ERROR", ci.id, "owner_id", map[string]any{"reason": "owner_not_active_in_tenant"}))
		} else {
			counts["owner_missing"].valid++
		}
		if !ci.hasSource {
			findings = append(findings, qualityFinding("source_missing", "WARNING", ci.id, "source", map[string]any{"reason": "no_external_source_mapping"}))
		} else {
			counts["source_missing"].valid++
		}
		if ci.hasExternal {
			counts["stale_ci"].denominator++
			if ci.staleSource {
				findings = append(findings, qualityFinding("stale_ci", "WARNING", ci.id, "last_seen_at", map[string]any{"reason": "absent_from_complete_snapshot", "cutoff": cutoff}))
			} else {
				counts["stale_ci"].valid++
			}
		}
		if rule, ok := classRules[ci.class]; ok && !rule.ParentDependent {
			values := qualityCIValues(ci)
			for index, keySet := range rule.MatchKeys {
				signatureParts := []string{}
				complete := true
				for _, field := range keySet {
					raw, found := values[field]
					value, valid := observationString(raw)
					if !found || !valid || strings.TrimSpace(value) == "" {
						complete = false
						break
					}
					value, normErr := normalizeIdentity(field, value, rule.Normalization[field])
					if normErr != nil {
						complete = false
						break
					}
					signatureParts = append(signatureParts, field+"="+value)
				}
				if !complete {
					continue
				}
				signature := ci.class + ":" + fmt.Sprint(index) + ":" + strings.Join(signatureParts, "\x00")
				if identityGroups[signature] == nil {
					identityGroups[signature] = map[uint64]bool{}
				}
				identityGroups[signature][ci.id] = true
			}
		}
	}
	for _, group := range identityGroups {
		if len(group) < 2 {
			continue
		}
		ids := make([]uint64, 0, len(group))
		for id := range group {
			ids = append(ids, id)
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		for _, id := range ids {
			counts["duplicate_identity"].openKeys[id] = true
			findings = append(findings, qualityFinding("duplicate_identity", "ERROR", id, "identity", map[string]any{"candidate_ci_ids": ids}))
		}
	}
	for _, count := range counts {
		count.valid = uint64(len(ciRows))
	}
	// Restore exact per-rule numerator after setting the common CI denominator.
	for _, ci := range ciRows {
		if !ci.ownerActive {
			counts["owner_missing"].valid--
		}
		if !ci.hasSource {
			counts["source_missing"].valid--
		}
		if counts["required_fields"].openKeys[ci.id] {
			counts["required_fields"].valid--
		}
		if counts["duplicate_identity"].openKeys[ci.id] {
			counts["duplicate_identity"].valid--
		}
	}
	counts["stale_ci"].valid = counts["stale_ci"].denominator
	for _, ci := range ciRows {
		if ci.hasExternal && ci.staleSource {
			counts["stale_ci"].valid--
		}
	}
	if err = s.evaluateCustomReferencesInTx(ctx, e, a, counts["invalid_reference"], &findings); err != nil {
		return err
	}
	counts["invalid_reference"].valid = counts["invalid_reference"].denominator - counts["invalid_reference"].openOther
	var relationTotal uint64
	if err = e.QueryRowContext(ctx, `SELECT COUNT(*) FROM cmdb_relations r JOIN cmdb_cis s ON s.tenant_id=r.tenant_id AND s.id=r.source_id JOIN cmdb_cis t ON t.tenant_id=r.tenant_id AND t.id=r.target_id WHERE r.tenant_id=?`, a.TenantID).Scan(&relationTotal); err != nil {
		return err
	}
	counts["invalid_relation"].denominator = relationTotal
	relationRows, err := e.QueryContext(ctx, `SELECT r.id,r.source_id,r.target_id,r.relation_type,s.class,t.class FROM cmdb_relations r JOIN cmdb_cis s ON s.tenant_id=r.tenant_id AND s.id=r.source_id JOIN cmdb_cis t ON t.tenant_id=r.tenant_id AND t.id=r.target_id WHERE r.tenant_id=? ORDER BY r.id`, a.TenantID)
	if err != nil {
		return err
	}
	invalidRelations := uint64(0)
	for relationRows.Next() {
		var relation qualityRelation
		if err = relationRows.Scan(&relation.id, &relation.sourceID, &relation.targetID, &relation.kind, &relation.source, &relation.target); err != nil {
			relationRows.Close()
			return err
		}
		if !RelationType(relation.kind, relation.source, relation.target) {
			invalidRelations++
			findings = append(findings, qualityFinding("invalid_relation", "ERROR", relation.sourceID, fmt.Sprintf("relation:%d", relation.id), map[string]any{"relation_id": relation.id, "target_ci_id": relation.targetID, "relation_type": relation.kind, "source_class": relation.source, "target_class": relation.target}))
		}
	}
	if err = relationRows.Err(); err != nil {
		relationRows.Close()
		return err
	}
	relationRows.Close()
	counts["invalid_relation"].openOther = invalidRelations
	if relationTotal >= invalidRelations {
		counts["invalid_relation"].valid = relationTotal - invalidRelations
	}
	if _, err = e.ExecContext(ctx, `UPDATE cmdb_ire_quality_findings SET state='RESOLVED',resolved_at=UTC_TIMESTAMP(6),checked_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND state='OPEN'`, a.TenantID); err != nil {
		return err
	}
	for _, finding := range findings {
		details, _ := json.Marshal(finding.Details)
		if _, err = e.ExecContext(ctx, `INSERT INTO cmdb_ire_quality_findings(tenant_id,rule_key,severity,ci_id,field_key,details,state) VALUES(?,?,?,?,?,?,'OPEN') ON DUPLICATE KEY UPDATE severity=VALUES(severity),details=VALUES(details),state='OPEN',checked_at=UTC_TIMESTAMP(6),resolved_at=NULL`, a.TenantID, finding.RuleKey, finding.Severity, finding.CIID, finding.FieldKey, details); err != nil {
			return err
		}
	}
	for key, count := range counts {
		var open uint64
		if key == "invalid_relation" || key == "invalid_reference" {
			open = count.openOther
		} else {
			for id := range count.openKeys {
				_ = id
				open++
			}
		}
		if _, err = e.ExecContext(ctx, `INSERT INTO cmdb_ire_quality_metrics(tenant_id,rule_key,valid_count,denominator,open_count,checked_at) VALUES(?,?,?,?,?,UTC_TIMESTAMP(6)) ON DUPLICATE KEY UPDATE valid_count=VALUES(valid_count),denominator=VALUES(denominator),open_count=VALUES(open_count),checked_at=VALUES(checked_at)`, a.TenantID, key, count.valid, count.denominator, open); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) evaluateCustomReferencesInTx(ctx context.Context, e database.Executor, a Actor, count *qualityCounts, findings *[]QualityFinding) error {
	if s.Metadata == nil {
		return nil
	}
	version, configuration, err := s.Metadata.Published(ctx, e, a.TenantID)
	if err != nil {
		return err
	}
	schema, err := configuration.Schema("ci", version)
	if err != nil {
		return err
	}
	for _, field := range schema.Fields {
		if !field.Active || field.System || field.Type != metadata.TypeReference {
			continue
		}
		exists, ok := qualityReferenceExistsSQL(field.ReferenceType)
		if !ok {
			return ErrInvalid
		}
		base := ` FROM metadata_record_values mv WHERE mv.tenant_id=? AND mv.record_type='ci' AND mv.field_key=? AND mv.field_type='REFERENCE' AND mv.value_reference_id IS NOT NULL AND EXISTS(SELECT 1 FROM cmdb_cis ci WHERE ci.tenant_id=mv.tenant_id AND ci.id=mv.record_id AND ci.archived=FALSE AND ci.redirect_ci_id IS NULL)`
		var total uint64
		if err = e.QueryRowContext(ctx, `SELECT COUNT(*)`+base, a.TenantID, field.Key).Scan(&total); err != nil {
			return err
		}
		count.denominator += total
		rows, queryErr := e.QueryContext(ctx, `SELECT mv.record_id,mv.value_reference_id`+base+` AND NOT `+exists+` ORDER BY mv.record_id`, a.TenantID, field.Key)
		if queryErr != nil {
			return queryErr
		}
		for rows.Next() {
			var ciID, targetID uint64
			if err = rows.Scan(&ciID, &targetID); err != nil {
				rows.Close()
				return err
			}
			count.openOther++
			*findings = append(*findings, qualityFinding("invalid_reference", "ERROR", ciID, field.Key, map[string]any{"reason": "referenced_record_unavailable", "reference_type": field.ReferenceType, "referenced_record_id": targetID}))
		}
		if err = rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
	}
	return nil
}

func qualityReferenceExistsSQL(target string) (string, bool) {
	switch target {
	case "user":
		return `EXISTS(SELECT 1 FROM tenant_memberships m JOIN users u ON u.id=m.user_id WHERE m.tenant_id=mv.tenant_id AND m.user_id=mv.value_reference_id AND m.active=TRUE AND u.active=TRUE)`, true
	case "group":
		return "EXISTS(SELECT 1 FROM `groups` g WHERE g.tenant_id=mv.tenant_id AND g.id=mv.value_reference_id AND g.active=TRUE)", true
	case "incident":
		return `EXISTS(SELECT 1 FROM incidents i WHERE i.tenant_id=mv.tenant_id AND i.id=mv.value_reference_id)`, true
	case "request":
		return `EXISTS(SELECT 1 FROM service_requests r WHERE r.tenant_id=mv.tenant_id AND r.id=mv.value_reference_id)`, true
	case "ci":
		return `EXISTS(SELECT 1 FROM cmdb_cis c WHERE c.tenant_id=mv.tenant_id AND c.id=mv.value_reference_id)`, true
	case "asset":
		return `EXISTS(SELECT 1 FROM assets a WHERE a.tenant_id=mv.tenant_id AND a.id=mv.value_reference_id)`, true
	default:
		return "", false
	}
}

func qualityFinding(rule, severity string, ci uint64, field string, details any) QualityFinding {
	raw, _ := json.Marshal(details)
	return QualityFinding{RuleKey: rule, Severity: severity, CIID: ci, FieldKey: field, Details: raw}
}

func qualityFieldValue(ci qualityCI, field string) (json.RawMessage, bool) {
	switch field {
	case "class":
		return json.RawMessage(mustJSON(ci.class)), true
	case "identifier":
		return json.RawMessage(mustJSON(ci.identifier)), true
	case "name":
		return json.RawMessage(mustJSON(ci.name)), true
	case "status":
		return json.RawMessage(mustJSON(ci.status)), true
	case "owner_id":
		return json.RawMessage(mustJSON(fmt.Sprint(ci.ownerID))), true
	case "environment":
		return json.RawMessage(mustJSON(ci.environment)), true
	case "location":
		return json.RawMessage(mustJSON(ci.location)), true
	default:
		if strings.HasPrefix(field, "attributes.") {
			value, ok := ci.attributes[strings.TrimPrefix(field, "attributes.")]
			return value, ok
		}
		return nil, false
	}
}

func mustJSON(value string) string {
	raw, _ := json.Marshal(value)
	return string(raw)
}

func qualityCIValues(ci qualityCI) map[string]json.RawMessage {
	values := map[string]json.RawMessage{}
	for _, field := range []string{"identifier", "name", "class", "status", "owner_id", "environment", "location"} {
		value, ok := qualityFieldValue(ci, field)
		if ok {
			values[field] = value
		}
	}
	for key, value := range ci.attributes {
		values["attributes."+key] = value
	}
	return values
}

func (s *Service) QualitySummary(ctx context.Context, a Actor) ([]QualitySummary, error) {
	if !canReadQuality(a) {
		return nil, workrecords.ErrForbidden
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT rule_key,valid_count,denominator,open_count FROM cmdb_ire_quality_metrics WHERE tenant_id=? ORDER BY rule_key`, a.TenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []QualitySummary{}
	for rows.Next() {
		var item QualitySummary
		if err = rows.Scan(&item.RuleKey, &item.Valid, &item.Denominator, &item.Open); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Service) QualityFindings(ctx context.Context, a Actor, before uint64, class, rule, status string, ownerID uint64, limit int) (IREPage[QualityFinding], error) {
	page := IREPage[QualityFinding]{Items: []QualityFinding{}}
	if !canReadQuality(a) || !a.Has("cmdb.read") && !a.Has("cmdb.read_own") {
		return page, workrecords.ErrForbidden
	}
	if limit < 1 || limit > 100 || status != "" && status != "OPEN" && status != "RESOLVED" || class != "" && func() bool { _, ok := ClassDefinition(class); return !ok }() || ownerID > 0 && !a.Has("cmdb.read") {
		return page, ErrInvalid
	}
	access, scopeArgs := Access(a, "c")
	query := `SELECT f.id,c.class,f.rule_key,f.severity,f.ci_id,f.field_key,f.details,f.state,f.detected_at,f.checked_at FROM cmdb_ire_quality_findings f JOIN cmdb_cis c ON c.tenant_id=f.tenant_id AND c.id=f.ci_id WHERE f.tenant_id=? AND ` + access
	args := []any{a.TenantID}
	args = append(args, scopeArgs...)
	if status != "" {
		query += " AND f.state=?"
		args = append(args, status)
	}
	if class != "" {
		query += " AND c.class=?"
		args = append(args, class)
	}
	if rule != "" {
		query += " AND f.rule_key=?"
		args = append(args, rule)
	}
	if ownerID > 0 {
		query += " AND c.owner_id=?"
		args = append(args, ownerID)
	}
	if before > 0 {
		query += " AND f.id<?"
		args = append(args, before)
	}
	query += " ORDER BY f.id DESC LIMIT ?"
	args = append(args, limit+1)
	rows, err := s.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		var finding QualityFinding
		if err = rows.Scan(&finding.ID, &finding.CIClass, &finding.RuleKey, &finding.Severity, &finding.CIID, &finding.FieldKey, &finding.Details, &finding.State, &finding.DetectedAt, &finding.CheckedAt); err != nil {
			return page, err
		}
		page.Items = append(page.Items, finding)
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

func (s *Service) recheckRepairedCIInTx(ctx context.Context, e database.Executor, tenant, ciID uint64) error {
	rows, err := e.QueryContext(ctx, `SELECT rule_key,field_key FROM cmdb_ire_quality_findings WHERE tenant_id=? AND ci_id=? AND state='OPEN' AND rule_key IN('required_fields','owner_missing','source_missing','stale_ci')`, tenant, ciID)
	if err != nil {
		return err
	}
	type candidate struct{ rule, field string }
	candidates := []candidate{}
	for rows.Next() {
		var item candidate
		if err = rows.Scan(&item.rule, &item.field); err != nil {
			rows.Close()
			return err
		}
		candidates = append(candidates, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if len(candidates) == 0 {
		return nil
	}
	var record qualityCI
	var attributes []byte
	if err = e.QueryRowContext(ctx, `SELECT id,class,identifier,name,status,owner_id,environment,location,attributes,last_seen_at FROM cmdb_cis WHERE tenant_id=? AND id=?`, tenant, ciID).Scan(&record.id, &record.class, &record.identifier, &record.name, &record.status, &record.ownerID, &record.environment, &record.location, &attributes, &record.lastSeen); err != nil {
		return err
	}
	if err = json.Unmarshal(attributes, &record.attributes); err != nil {
		return err
	}
	values := qualityCIValues(record)
	var ownerActive, hasSource, stale bool
	var staleDays uint16
	if err = e.QueryRowContext(ctx, `SELECT stale_days FROM cmdb_ire_quality_config WHERE tenant_id=?`, tenant).Scan(&staleDays); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if staleDays == 0 {
		staleDays = 90
	}
	cutoff := time.Now().UTC().Add(-time.Duration(staleDays) * 24 * time.Hour)
	if err = e.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM tenant_memberships m JOIN users u ON u.id=m.user_id WHERE m.tenant_id=? AND m.user_id=? AND m.active=TRUE AND u.active=TRUE), (EXISTS(SELECT 1 FROM cmdb_ire_external_identities x WHERE x.tenant_id=? AND x.ci_id=?) OR EXISTS(SELECT 1 FROM cmdb_ire_field_provenance p WHERE p.tenant_id=? AND p.ci_id=?)), EXISTS(SELECT 1 FROM cmdb_ire_external_identities x JOIN cmdb_ire_sources ds ON ds.tenant_id=x.tenant_id AND ds.id=x.source_id AND ds.state='ACTIVE' WHERE x.tenant_id=? AND x.ci_id=? AND x.stale_since<?) AND NOT EXISTS(SELECT 1 FROM cmdb_ire_external_identities x JOIN cmdb_ire_sources ds ON ds.tenant_id=x.tenant_id AND ds.id=x.source_id AND ds.state='ACTIVE' WHERE x.tenant_id=? AND x.ci_id=? AND (x.stale_since IS NULL OR x.stale_since>=?))`, tenant, record.ownerID, tenant, ciID, tenant, ciID, tenant, ciID, cutoff, tenant, ciID, cutoff).Scan(&ownerActive, &hasSource, &stale); err != nil {
		return err
	}
	for _, item := range candidates {
		resolved := false
		switch item.rule {
		case "required_fields":
			value, ok := values[item.field]
			resolved = ok && !isEmptyJSON(value)
		case "owner_missing":
			resolved = ownerActive
		case "source_missing":
			resolved = hasSource
		case "stale_ci":
			resolved = !stale
		}
		if resolved {
			if _, err = e.ExecContext(ctx, `UPDATE cmdb_ire_quality_findings SET state='RESOLVED',resolved_at=UTC_TIMESTAMP(6),checked_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND ci_id=? AND rule_key=? AND field_key=? AND state='OPEN'`, tenant, ciID, item.rule, item.field); err != nil {
				return err
			}
		}
	}
	for _, rule := range []string{"required_fields", "owner_missing", "source_missing", "stale_ci"} {
		if _, err = e.ExecContext(ctx, `UPDATE cmdb_ire_quality_metrics m SET m.open_count=(SELECT COUNT(DISTINCT f.ci_id) FROM cmdb_ire_quality_findings f WHERE f.tenant_id=m.tenant_id AND f.rule_key=m.rule_key AND f.state='OPEN'),m.valid_count=m.denominator-(SELECT COUNT(DISTINCT f.ci_id) FROM cmdb_ire_quality_findings f WHERE f.tenant_id=m.tenant_id AND f.rule_key=m.rule_key AND f.state='OPEN'),m.checked_at=UTC_TIMESTAMP(6) WHERE m.tenant_id=? AND m.rule_key=?`, tenant, rule); err != nil {
			return err
		}
	}
	return nil
}
