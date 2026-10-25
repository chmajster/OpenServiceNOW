package cmdb

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/chmajster/OpenServiceNOW/internal/assets"
	"github.com/chmajster/OpenServiceNOW/internal/attachments"
	"github.com/chmajster/OpenServiceNOW/internal/audit"
	"github.com/chmajster/OpenServiceNOW/internal/jobs"
	"github.com/chmajster/OpenServiceNOW/internal/metadata"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
	"io"
	"strings"
	"time"
)

type Import struct {
	ID uint64 `json:"id,string"`
	ImportConfig
	CreatorID  uint64         `json:"creator_id,string"`
	ExecutorID uint64         `json:"executor_id,string"`
	FileName   string         `json:"file_name"`
	State      string         `json:"state"`
	ErrorCode  string         `json:"error_code"`
	Version    uint64         `json:"version,string"`
	JobID      uint64         `json:"job_id,string,omitempty"`
	JobStatus  string         `json:"job_status"`
	Progress   uint8          `json:"progress"`
	Attempts   uint32         `json:"attempts"`
	CreatedAt  time.Time      `json:"created_at"`
	UpdatedAt  time.Time      `json:"updated_at"`
	Counts     map[string]int `json:"counts"`
	sourceID   uint64
}
type ImportRow struct {
	Number      int             `json:"row_number"`
	Identity    string          `json:"identity_value"`
	Action      string          `json:"action"`
	State       string          `json:"state"`
	RecordID    uint64          `json:"record_id,string,omitempty"`
	Version     uint64          `json:"expected_version,string"`
	RuleVersion uint64          `json:"identity_rule_version,string"`
	Before      json.RawMessage `json:"before"`
	After       json.RawMessage `json:"after"`
	ErrorCode   string          `json:"error_code"`
	Error       string          `json:"error_message"`
}
type ImportPage struct {
	Items []Import `json:"items"`
	Next  uint64   `json:"next_before_id,string,omitempty"`
}
type ImportRows struct {
	Items []ImportRow `json:"items"`
	Next  int         `json:"next_after_row,omitempty"`
}
type importJob struct {
	ImportID uint64 `json:"import_id,string"`
	Phase    string `json:"phase"`
}

func importAllowed(a Actor, target string) bool {
	return a.Has("cmdb.import") && (target == "CI" && Writable(a) || target == "ASSET" && assets.Writable(a))
}

const importColumns = `i.id,i.target,i.identity_key,i.mapping,i.creator_id,i.executor_id,i.file_name,i.state,i.error_code,i.version,COALESCE(i.source_attachment_id,0),COALESCE(i.job_id,0),COALESCE(j.status,''),COALESCE(j.attempt_count,0),i.created_at,i.updated_at`

func scanImport(row interface{ Scan(...any) error }) (Import, error) {
	var i Import
	var mapping []byte
	err := row.Scan(&i.ID, &i.Target, &i.IdentityKey, &mapping, &i.CreatorID, &i.ExecutorID, &i.FileName, &i.State, &i.ErrorCode, &i.Version, &i.sourceID, &i.JobID, &i.JobStatus, &i.Attempts, &i.CreatedAt, &i.UpdatedAt)
	if err != nil {
		return i, assets.Missing(err)
	}
	if err = json.Unmarshal(mapping, &i.ImportConfig); err == nil && i.Mapping != nil {
		// Versioned rows persist the pinned metadata version and allowlist.
	} else {
		err = json.Unmarshal(mapping, &i.Mapping)
	}
	i.Counts = map[string]int{}
	if i.State == "PREVIEW_READY" || i.State == "COMPLETED" || i.State == "PARTIAL" {
		i.Progress = 100
	}
	return i, err
}

func updateImportProgress(i *Import) {
	if i.State == "PREVIEW_READY" || i.State == "COMPLETED" || i.State == "PARTIAL" {
		i.Progress = 100
		return
	}
	total := 0
	resolved := 0
	for state, count := range i.Counts {
		total += count
		if state == "APPLIED" || state == "ERROR" || state == "CONFLICT" {
			resolved += count
		}
	}
	if total > 0 {
		i.Progress = uint8(resolved * 100 / total)
	}
}
func (s *Service) importInTx(ctx context.Context, e database.Executor, a Actor, id uint64, lock bool) (Import, error) {
	q := "SELECT " + importColumns + " FROM inventory_imports i LEFT JOIN jobs j ON j.id=i.job_id WHERE i.tenant_id=? AND i.id=?"
	if lock {
		q += " FOR UPDATE"
	}
	i, err := scanImport(e.QueryRowContext(ctx, q, a.TenantID, id))
	if err != nil {
		return i, err
	}
	if !importAllowed(a, i.Target) {
		return i, ErrNotFound
	}
	return i, nil
}
func (s *Service) GetImport(ctx context.Context, a Actor, id uint64) (Import, error) {
	i, err := s.importInTx(ctx, s.DB, a, id, false)
	if err != nil {
		return i, err
	}
	rows, err := s.DB.QueryContext(ctx, "SELECT state,COUNT(*) FROM inventory_import_rows WHERE tenant_id=? AND import_id=? GROUP BY state", a.TenantID, id)
	if err != nil {
		return i, err
	}
	defer rows.Close()
	for rows.Next() {
		var state string
		var n int
		if err = rows.Scan(&state, &n); err != nil {
			return i, err
		}
		i.Counts[state] = n
	}
	if err = rows.Err(); err != nil {
		return i, err
	}
	updateImportProgress(&i)
	return i, nil
}
func (s *Service) ListImports(ctx context.Context, a Actor, before uint64, limit int) (ImportPage, error) {
	out := ImportPage{Items: []Import{}}
	if limit < 1 || limit > 100 {
		return out, ErrInvalid
	}
	ci, as := importAllowed(a, "CI"), importAllowed(a, "ASSET")
	if !ci && !as {
		return out, workrecords.ErrForbidden
	}
	q := "SELECT " + importColumns + " FROM inventory_imports i LEFT JOIN jobs j ON j.id=i.job_id WHERE i.tenant_id=? AND ((i.target='CI' AND ?) OR (i.target='ASSET' AND ?))"
	args := []any{a.TenantID, ci, as}
	if before > 0 {
		q += " AND i.id<?"
		args = append(args, before)
	}
	q += " ORDER BY i.id DESC LIMIT ?"
	args = append(args, limit+1)
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		i, err := scanImport(rows)
		if err != nil {
			return out, err
		}
		out.Items = append(out.Items, i)
	}
	if len(out.Items) > limit {
		out.Items = out.Items[:limit]
		out.Next = out.Items[limit-1].ID
	}
	return out, rows.Err()
}

const importRowColumns = `source_row,identity_value,action,state,COALESCE(record_id,0),expected_version,ire_rule_version,before_value,after_value,error_code,error_message`

func scanImportRow(row interface{ Scan(...any) error }) (ImportRow, error) {
	var r ImportRow
	err := row.Scan(&r.Number, &r.Identity, &r.Action, &r.State, &r.RecordID, &r.Version, &r.RuleVersion, &r.Before, &r.After, &r.ErrorCode, &r.Error)
	return r, err
}
func (s *Service) ImportRows(ctx context.Context, a Actor, id uint64, after, limit int) (ImportRows, error) {
	out := ImportRows{Items: []ImportRow{}}
	if after < 0 || limit < 1 || limit > 100 {
		return out, ErrInvalid
	}
	if _, err := s.importInTx(ctx, s.DB, a, id, false); err != nil {
		return out, err
	}
	rows, err := s.DB.QueryContext(ctx, "SELECT "+importRowColumns+" FROM inventory_import_rows WHERE tenant_id=? AND import_id=? AND source_row>? ORDER BY source_row LIMIT ?", a.TenantID, id, after, limit+1)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		r, err := scanImportRow(rows)
		if err != nil {
			return out, err
		}
		out.Items = append(out.Items, r)
	}
	if len(out.Items) > limit {
		out.Items = out.Items[:limit]
		out.Next = out.Items[limit-1].Number
	}
	return out, rows.Err()
}
func (s *Service) enqueueImport(ctx context.Context, e database.Executor, a Actor, i *Import, phase string) error {
	payload, _ := json.Marshal(importJob{i.ID, phase})
	j, _, err := jobs.NewRepository(nil).EnqueueInTx(ctx, e, jobs.EnqueueInput{TenantID: a.TenantID, Type: "cmdb.import", Payload: payload, CreatedBy: &a.UserID, MaxAttempts: 5, IdempotencyKey: fmt.Sprintf("inventory:%d:%d:%s", i.ID, i.Version, phase)})
	if err != nil {
		return err
	}
	i.JobID = j.ID
	_, err = e.ExecContext(ctx, "UPDATE inventory_imports SET job_id=? WHERE tenant_id=? AND id=?", j.ID, a.TenantID, i.ID)
	return err
}
func importAudit(ctx context.Context, e database.Executor, a Actor, id uint64, action string, data map[string]any) error {
	return audit.NewMySQLRepository().Append(ctx, e, audit.Event{TenantID: &a.TenantID, ActorUserID: &a.UserID, Action: "inventory.import." + action, ObjectType: "inventory_import", ObjectID: fmt.Sprint(id), NewValues: data, RequestID: a.RequestID, IPAddress: a.IPAddress, Source: "inventory"})
}

func (s *Service) CreateImport(ctx context.Context, a Actor, c ImportConfig, name, key string, content io.Reader) (Import, error) {
	var out Import
	if !importAllowed(a, c.Target) {
		return out, workrecords.ErrForbidden
	}
	if c.SourceID > 0 && !canManageIRE(a) {
		return out, workrecords.ErrForbidden
	}
	if c.Target == "CI" {
		c.CustomFields = []string{}
		c.CustomFieldsVersion = 0
		if s.Metadata != nil {
			version, configuration, err := s.Metadata.Published(ctx, s.DB, a.TenantID)
			if err != nil {
				return out, err
			}
			schema, err := metadata.BuildSchema("ci", version, configuration)
			if err != nil {
				return out, err
			}
			c.CustomFieldsVersion = version
			for _, field := range schema.Fields {
				if field.Active && !field.System {
					c.CustomFields = append(c.CustomFields, field.Key)
				}
			}
		}
	} else {
		c.CustomFields = nil
		c.CustomFieldsVersion = 0
	}
	if err := ValidateImportConfig(c); err != nil {
		return out, err
	}
	data, err := io.ReadAll(io.LimitReader(content, MaxCSVBytes+1))
	if err != nil {
		return out, err
	}
	if _, err = ReadCSV(data, c); err != nil {
		return out, err
	}
	if s.Files == nil {
		return out, ErrInvalid
	}
	input := attachments.CreateInput{TenantID: a.TenantID, ObjectType: "cmdb_import", ObjectID: "pending", FileName: name, UploadedBy: &a.UserID, Visibility: "INTERNAL"}
	prepared, err := s.Files.Prepare(ctx, input, bytes.NewReader(data))
	if err != nil {
		return out, err
	}
	used := false
	hashInput, _ := json.Marshal(struct {
		Config ImportConfig
		File   [32]byte
	}{c, sha256.Sum256(data)})
	err = s.ActorTx(ctx, a, func(e database.Executor, a Actor) error {
		if !importAllowed(a, c.Target) {
			return workrecords.ErrForbidden
		}
		if c.SourceID > 0 {
			if !canManageIRE(a) {
				return workrecords.ErrForbidden
			}
		}
		existing, err := workrecords.ReserveOperation(ctx, e, a.Actor, key, "inventory_import", sha256.Sum256(hashInput))
		if err != nil {
			return err
		}
		if existing > 0 {
			out, err = s.importInTx(ctx, e, a, existing, false)
			return err
		}
		if c.SourceID > 0 {
			if _, err := s.csvImportSource(ctx, e, a, c.SourceID); err != nil {
				return err
			}
		}
		mapping, _ := json.Marshal(c)
		result, err := e.ExecContext(ctx, "INSERT INTO inventory_imports(tenant_id,creator_id,executor_id,target,identity_key,mapping,file_name) VALUES(?,?,?,?,?,?,?)", a.TenantID, a.UserID, a.UserID, c.Target, c.IdentityKey, mapping, name)
		if err != nil {
			return err
		}
		id, err := result.LastInsertId()
		if err != nil {
			return err
		}
		out = Import{ID: uint64(id), ImportConfig: c, CreatorID: a.UserID, ExecutorID: a.UserID, Version: 1, State: "QUEUED_PREVIEW"}
		input.ObjectID = fmt.Sprint(id)
		file, err := s.Files.CreatePreparedInTx(ctx, e, input, prepared)
		if err != nil {
			return err
		}
		if _, err = e.ExecContext(ctx, "UPDATE inventory_imports SET source_attachment_id=? WHERE tenant_id=? AND id=?", file.ID, a.TenantID, id); err != nil {
			return err
		}
		if err = s.enqueueImport(ctx, e, a, &out, "preview"); err != nil {
			return err
		}
		if err = importAudit(ctx, e, a, out.ID, "created", map[string]any{"target": c.Target, "identity_key": c.IdentityKey}); err != nil {
			return err
		}
		if err = workrecords.CompleteOperation(ctx, e, a.Actor, key, out.ID); err != nil {
			return err
		}
		used = true
		return nil
	})
	if (!used || err != nil) && !errors.Is(err, database.ErrCommitUncertain) {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = s.Files.Discard(cleanup, prepared)
	}
	if err != nil {
		return out, err
	}
	return s.GetImport(ctx, a, out.ID)
}

// Review refreshes unapplied rows only. Already applied rows survive retries.
func (s *Service) ImportAction(ctx context.Context, a Actor, id, version uint64, action string) (Import, error) {
	err := s.ActorTx(ctx, a, func(e database.Executor, a Actor) error {
		i, err := s.importInTx(ctx, e, a, id, true)
		if err != nil {
			return err
		}
		if i.Version != version {
			return ErrConflict
		}
		state := ""
		phase := ""
		switch action {
		case "apply":
			if i.State != "PREVIEW_READY" {
				return ErrConflict
			}
			var invalid int
			if err = e.QueryRowContext(ctx, "SELECT COUNT(*) FROM inventory_import_rows WHERE tenant_id=? AND import_id=? AND state IN('ERROR','CONFLICT')", a.TenantID, id).Scan(&invalid); err != nil {
				return err
			}
			if invalid > 0 {
				return fmt.Errorf("%w: popraw plik; podgląd zawiera błędy", ErrConflict)
			}
			state = "QUEUED_APPLY"
			phase = "apply"
		case "review":
			if i.State != "PREVIEW_READY" && i.State != "PARTIAL" && i.State != "FAILED" {
				return ErrConflict
			}
			state = "QUEUED_PREVIEW"
			phase = "preview"
		case "cancel":
			if i.State == "COMPLETED" || i.State == "CANCELLED" {
				return ErrConflict
			}
			state = "CANCELLED"
		default:
			return ErrInvalid
		}
		i.Version++
		i.ExecutorID = a.UserID
		if _, err = e.ExecContext(ctx, "UPDATE inventory_imports SET state=?,executor_id=?,version=?,error_code='',updated_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND id=?", state, a.UserID, i.Version, a.TenantID, id); err != nil {
			return err
		}
		if phase != "" {
			if err = s.enqueueImport(ctx, e, a, &i, phase); err != nil {
				return err
			}
		}
		return importAudit(ctx, e, a, id, action, map[string]any{"version": fmt.Sprint(i.Version), "executor_id": fmt.Sprint(a.UserID)})
	})
	if err != nil {
		return Import{}, err
	}
	return s.GetImport(ctx, a, id)
}

func (s *Service) sourceCSV(ctx context.Context, a Actor, i Import) ([]byte, error) {
	if s.Files == nil || i.sourceID == 0 {
		return nil, ErrInvalid
	}
	file, reader, err := s.Files.Open(ctx, a.TenantID, i.sourceID)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	if file.TenantID != a.TenantID || file.ObjectType != "cmdb_import" || file.ObjectID != fmt.Sprint(i.ID) || file.UploadedBy == nil || *file.UploadedBy != i.CreatorID || file.Visibility != "INTERNAL" || file.SizeBytes > MaxCSVBytes {
		return nil, ErrInvalid
	}
	data, err := io.ReadAll(io.LimitReader(reader, MaxCSVBytes+1))
	if err != nil {
		return nil, err
	}
	if uint64(len(data)) != file.SizeBytes || sha256.Sum256(data) != file.SHA256 {
		return nil, ErrInvalid
	}
	return data, nil
}
func rowFailure(r *ImportRow, code string, err error) {
	r.State = "ERROR"
	r.Action = "ERROR"
	r.ErrorCode = code
	r.Error = err.Error()
	if len(r.Error) > 500 {
		r.Error = string([]rune(r.Error)[:100])
	}
	if len(r.Before) == 0 {
		r.Before = json.RawMessage(`null`)
	}
	if len(r.After) == 0 {
		r.After = json.RawMessage(`null`)
	}
}
func (s *Service) previewRow(ctx context.Context, e database.Executor, a Actor, i Import, c CSVRow, schema metadata.Schema, source DataSource) (ImportRow, error) {
	r := ImportRow{Number: c.Number, Identity: c.Identity, State: "PENDING", Action: "CREATE", Before: json.RawMessage(`null`), After: json.RawMessage(`null`)}
	if c.Error != "" {
		rowFailure(&r, "INVALID_ROW", errors.New(c.Error))
		return r, nil
	}
	table, column := "cmdb_cis", "identifier"
	if i.Target == "ASSET" {
		table = "assets"
		column = i.IdentityKey
	}
	err := e.QueryRowContext(ctx, "SELECT id FROM "+table+" WHERE tenant_id=? AND "+column+"=?", a.TenantID, c.Identity).Scan(&r.RecordID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return r, err
	}
	var old any
	var archived bool
	var oldState, oldClass string
	if r.RecordID > 0 {
		r.Action = "UPDATE"
		if i.Target == "CI" {
			record, err := s.GetInTx(ctx, e, a, r.RecordID, false)
			if err != nil {
				return r, err
			}
			old = record.Fields
			r.Version = record.Version
			archived = record.Archived
			oldClass = record.Class
		} else {
			record, err := s.Assets.GetInTx(ctx, e, a, r.RecordID, false)
			if err != nil {
				return r, err
			}
			old = record.Fields
			r.Version = record.Version
			archived = record.Archived
			oldState = record.State
		}
		r.Before, _ = json.Marshal(old)
	}
	r.After, err = csvFields(i.ImportConfig, c, old, schema)
	if err != nil {
		rowFailure(&r, "VALIDATION", err)
		return r, nil
	}
	if i.Target == "CI" && i.CustomFieldsVersion > 0 && s.Metadata != nil {
		var record Fields
		if json.Unmarshal(r.After, &record) != nil {
			rowFailure(&r, "VALIDATION", ErrInvalid)
			return r, nil
		}
		mapped := map[string]json.RawMessage{}
		for key := range c.Values {
			if !strings.HasPrefix(key, "custom_fields.") {
				continue
			}
			fieldKey := strings.TrimPrefix(key, "custom_fields.")
			if value, ok := record.CustomFields[fieldKey]; ok {
				mapped[fieldKey] = value
			}
		}
		values, pinnedSchema, validationErr := s.Metadata.PrepareValuesAtVersion(ctx, e, metadataActor(a), "ci", r.RecordID, mapped, metadataBaseValues(record), i.CustomFieldsVersion)
		if validationErr != nil {
			rowFailure(&r, "VALIDATION", validationErr)
			return r, nil
		}
		record.CustomFields = map[string]json.RawMessage{}
		for _, field := range pinnedSchema.Fields {
			if field.Active && !field.System {
				if value, ok := values[field.Key]; ok {
					record.CustomFields[field.Key] = value
				}
			}
		}
		record.CustomFieldsVersion = i.CustomFieldsVersion
		r.After, err = json.Marshal(record)
		if err != nil {
			return r, err
		}
	}
	if i.Target == "CI" {
		var record Fields
		if json.Unmarshal(r.After, &record) != nil {
			rowFailure(&r, "VALIDATION", ErrInvalid)
			return r, nil
		}
		resolved, resolveErr := s.resolveObservation(ctx, e, a, source, Observation{NativeKey: c.Identity, ScopeKey: source.ScopeKey, ObservedAt: time.Now().UTC(), Values: observationValues(record)}, 0)
		if resolveErr != nil {
			return r, resolveErr
		}
		if resolved.state == "CONFLICT" || resolved.state == "MULTIPLE" || resolved.state == "INSUFFICIENT" || resolved.state == "MATCH" && resolved.ciID != r.RecordID || resolved.state == "NEW" && r.RecordID > 0 {
			rowFailure(&r, "IDENTITY_CONFLICT", fmt.Errorf("%s: %s", resolved.why, resolved.text))
			return r, nil
		}
		r.RuleVersion = resolved.rule.Version
	}
	if archived {
		rowFailure(&r, "ARCHIVED", errors.New("Rekord jest zarchiwizowany; przywróć go świadomie przed importem"))
		return r, nil
	}
	var owner uint64
	if i.Target == "CI" {
		var f Fields
		json.Unmarshal(r.After, &f)
		owner = f.OwnerID
		if oldClass != "" && oldClass != f.Class {
			rowFailure(&r, "CLASS_IMMUTABLE", errors.New("Klasa istniejącego CI jest niezmienna"))
			return r, nil
		}
	}
	if i.Target == "ASSET" {
		var f assets.Fields
		json.Unmarshal(r.After, &f)
		owner = f.OwnerID
		if oldState != "" && !assets.Transition(oldState, f.State) {
			rowFailure(&r, "LIFECYCLE", errors.New("Niedozwolone przejście cyklu życia Asset"))
			return r, nil
		}
	}
	if err = workrecords.ValidateActiveUser(ctx, e, a.TenantID, owner); err != nil {
		if errors.Is(err, workrecords.ErrNotFound) {
			rowFailure(&r, "OWNER_INACTIVE", errors.New("Właściciel nie jest aktywnym członkiem organizacji"))
			return r, nil
		}
		return r, err
	}
	if bytes.Equal(r.Before, r.After) {
		r.Action = "NO_CHANGE"
	}
	return r, nil
}
func (s *Service) previewImport(ctx context.Context, a Actor, i Import) error {
	data, err := s.sourceCSV(ctx, a, i)
	if err != nil {
		return err
	}
	rows, err := ReadCSV(data, i.ImportConfig)
	if err != nil {
		return err
	}
	var schema metadata.Schema
	if i.Target == "CI" && i.CustomFieldsVersion > 0 && s.Metadata != nil {
		version, versionErr := s.Metadata.Version(ctx, metadataActor(a), i.CustomFieldsVersion)
		if versionErr != nil {
			return versionErr
		}
		schema, versionErr = metadata.BuildSchema("ci", version.Version, version.Config)
		if versionErr != nil {
			return versionErr
		}
	}
	return s.ActorTx(ctx, a, func(e database.Executor, a Actor) error {
		current, err := s.importInTx(ctx, e, a, i.ID, true)
		if err != nil {
			return err
		}
		if current.JobID != i.JobID || current.State != "QUEUED_PREVIEW" {
			return nil
		}
		var csvSource DataSource
		if current.Target == "CI" {
			csvSource, err = s.csvImportSource(ctx, e, a, current.SourceID)
			if err != nil {
				return err
			}
		}
		for _, c := range rows {
			var state string
			err = e.QueryRowContext(ctx, "SELECT state FROM inventory_import_rows WHERE tenant_id=? AND import_id=? AND source_row=?", a.TenantID, i.ID, c.Number).Scan(&state)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			if state == "APPLIED" {
				continue
			}
			r, err := s.previewRow(ctx, e, a, current, c, schema, csvSource)
			if err != nil {
				return err
			}
			if _, err = e.ExecContext(ctx, `INSERT INTO inventory_import_rows(tenant_id,import_id,source_row,identity_value,action,state,record_id,expected_version,ire_rule_version,before_value,after_value,error_code,error_message) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE identity_value=VALUES(identity_value),action=VALUES(action),state=VALUES(state),record_id=VALUES(record_id),expected_version=VALUES(expected_version),ire_rule_version=VALUES(ire_rule_version),before_value=VALUES(before_value),after_value=VALUES(after_value),error_code=VALUES(error_code),error_message=VALUES(error_message)`, a.TenantID, i.ID, r.Number, r.Identity, r.Action, r.State, nullableID(r.RecordID), r.Version, r.RuleVersion, []byte(r.Before), []byte(r.After), r.ErrorCode, r.Error); err != nil {
				return err
			}
		}
		_, err = e.ExecContext(ctx, "UPDATE inventory_imports SET state='PREVIEW_READY',error_code='',version=version+1,updated_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND id=?", a.TenantID, i.ID)
		return err
	})
}
func nullableID(id uint64) any {
	if id == 0 {
		return nil
	}
	return id
}

func (s *Service) applyRow(ctx context.Context, e database.Executor, a Actor, i Import, r ImportRow, source DataSource) (uint64, error) {
	if r.RecordID > 0 {
		if i.Target == "CI" {
			old, err := s.GetInTx(ctx, e, a, r.RecordID, true)
			if err != nil {
				return 0, err
			}
			if old.Version != r.Version || old.Archived {
				return 0, ErrConflict
			}
		} else {
			old, err := s.Assets.GetInTx(ctx, e, a, r.RecordID, true)
			if err != nil {
				return 0, err
			}
			if old.Version != r.Version || old.Archived {
				return 0, ErrConflict
			}
		}
	}
	if r.Action == "NO_CHANGE" {
		return r.RecordID, nil
	}
	if i.Target == "CI" {
		var record Record
		if err := json.Unmarshal(r.After, &record.Fields); err != nil {
			return 0, ErrInvalid
		}
		return s.applyCSVCIObservation(ctx, e, a, i, r, record, source)
	}
	var record assets.Record
	if err := json.Unmarshal(r.After, &record.Fields); err != nil {
		return 0, ErrInvalid
	}
	record.Version = r.Version
	out, err := s.Assets.SaveInTx(ctx, e, a, r.RecordID, record)
	return out.ID, err
}

// Each row's domain command and receipt share a transaction. A retry skips the
// committed receipt. Work is bounded to 25 rows per job, with durable continuation.
func (s *Service) applyImport(ctx context.Context, a Actor, i Import) error {
	for n := 0; n < 25; n++ {
		done := false
		err := s.ActorTx(ctx, a, func(e database.Executor, a Actor) error {
			current, err := s.importInTx(ctx, e, a, i.ID, true)
			if err != nil {
				return err
			}
			if current.JobID != i.JobID || (current.State != "QUEUED_APPLY" && current.State != "APPLYING") {
				done = true
				return nil
			}
			var csvSource DataSource
			if current.Target == "CI" {
				csvSource, err = s.csvImportSource(ctx, e, a, current.SourceID)
				if err != nil {
					return err
				}
			}
			r, err := scanImportRow(e.QueryRowContext(ctx, "SELECT "+importRowColumns+" FROM inventory_import_rows WHERE tenant_id=? AND import_id=? AND state='PENDING' ORDER BY source_row LIMIT 1 FOR UPDATE", a.TenantID, i.ID))
			if errors.Is(err, sql.ErrNoRows) {
				done = true
				var failed int
				if err = e.QueryRowContext(ctx, "SELECT COUNT(*) FROM inventory_import_rows WHERE tenant_id=? AND import_id=? AND state IN('ERROR','CONFLICT')", a.TenantID, i.ID).Scan(&failed); err != nil {
					return err
				}
				state := "COMPLETED"
				if failed > 0 {
					state = "PARTIAL"
				}
				_, err = e.ExecContext(ctx, "UPDATE inventory_imports SET state=?,version=version+1,updated_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND id=?", state, a.TenantID, i.ID)
				if err != nil {
					return err
				}
				return importAudit(ctx, e, a, i.ID, "finished", map[string]any{"state": state})
			}
			if err != nil {
				return err
			}
			// Expected command failures roll back to a savepoint before recording a result.
			if _, err = e.ExecContext(ctx, "SAVEPOINT import_row"); err != nil {
				return err
			}
			recordID, err := s.applyRow(ctx, e, a, current, r, csvSource)
			state, code, message := "APPLIED", "", ""
			if err != nil {
				if !errors.Is(err, ErrInvalid) && !errors.Is(err, ErrConflict) && !errors.Is(err, ErrDuplicate) && !errors.Is(err, ErrNotFound) {
					return err
				}
				if _, rollbackErr := e.ExecContext(ctx, "ROLLBACK TO SAVEPOINT import_row"); rollbackErr != nil {
					return rollbackErr
				}
				state = "CONFLICT"
				code = "RECORD_CONFLICT"
				message = "Rekord zmienił się lub identyfikator jest zajęty; odśwież podgląd. " + err.Error()
				if len(message) > 500 {
					message = "Nie można zastosować zmiany; odśwież podgląd"
				}
			}
			if _, err = e.ExecContext(ctx, "UPDATE inventory_import_rows SET state=?,record_id=COALESCE(?,record_id),error_code=?,error_message=? WHERE tenant_id=? AND import_id=? AND source_row=?", state, nullableID(recordID), code, message, a.TenantID, i.ID, r.Number); err != nil {
				return err
			}
			_, err = e.ExecContext(ctx, "UPDATE inventory_imports SET state='APPLYING',updated_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND id=?", a.TenantID, i.ID)
			return err
		})
		if err != nil {
			return err
		}
		if done {
			return nil
		}
	}
	return s.ActorTx(ctx, a, func(e database.Executor, a Actor) error {
		current, err := s.importInTx(ctx, e, a, i.ID, true)
		if err != nil {
			return err
		}
		if current.JobID != i.JobID || current.State != "APPLYING" {
			return nil
		}
		current.Version++
		if _, err = e.ExecContext(ctx, "UPDATE inventory_imports SET version=? WHERE tenant_id=? AND id=?", current.Version, a.TenantID, i.ID); err != nil {
			return err
		}
		return s.enqueueImport(ctx, e, a, &current, "apply")
	})
}
func (s *Service) ImportHandler(ctx context.Context, j jobs.Job) (json.RawMessage, error) {
	var payload importJob
	if json.Unmarshal(j.Payload, &payload) != nil || j.TenantID == 0 || j.CreatedBy == nil || payload.ImportID == 0 || (payload.Phase != "preview" && payload.Phase != "apply") {
		return nil, errors.New("invalid inventory job context")
	}
	a := Actor{Actor: workrecords.Actor{TenantID: j.TenantID, UserID: *j.CreatedBy, RequestID: fmt.Sprintf("inventory-job-%d", j.ID)}}
	var i Import
	err := s.ActorTx(ctx, a, func(e database.Executor, a Actor) error {
		var err error
		i, err = s.importInTx(ctx, e, a, payload.ImportID, true)
		if err != nil {
			return err
		}
		if i.ExecutorID != a.UserID {
			return workrecords.ErrForbidden
		}
		return nil
	})
	if err != nil {
		return s.importJobError(ctx, j, payload.ImportID, "EXECUTOR_ACCESS", err)
	}
	if i.JobID != j.ID || i.State == "CANCELLED" || i.State == "COMPLETED" || i.State == "PARTIAL" || i.State == "PREVIEW_READY" {
		return json.RawMessage(`{"superseded":true}`), nil
	}
	if payload.Phase == "preview" {
		err = s.previewImport(ctx, a, i)
	} else {
		err = s.applyImport(ctx, a, i)
	}
	if err != nil {
		return s.importJobError(ctx, j, i.ID, "EXECUTION", err)
	}
	return json.RawMessage(`{"processed":true}`), nil
}
func (s *Service) importJobError(ctx context.Context, j jobs.Job, id uint64, code string, cause error) (json.RawMessage, error) {
	terminal := j.AttemptCount >= j.MaxAttempts || errors.Is(cause, workrecords.ErrForbidden) || errors.Is(cause, ErrNotFound) || errors.Is(cause, ErrInvalid)
	stateUpdate := ""
	if terminal {
		stateUpdate = ",state='FAILED',version=version+1"
	}
	_, err := s.DB.ExecContext(ctx, "UPDATE inventory_imports SET error_code=?"+stateUpdate+",updated_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND id=? AND job_id=? AND state IN('QUEUED_PREVIEW','QUEUED_APPLY','APPLYING')", code, j.TenantID, id, j.ID)
	if err != nil {
		return nil, fmt.Errorf("inventory job status update failed")
	}
	return nil, fmt.Errorf("inventory import %s failed", strings.ToLower(code))
}
