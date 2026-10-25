package cmdb

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/chmajster/OpenServiceNOW/internal/activity"
	"github.com/chmajster/OpenServiceNOW/internal/assets"
	"github.com/chmajster/OpenServiceNOW/internal/attachments"
	"github.com/chmajster/OpenServiceNOW/internal/audit"
	"github.com/chmajster/OpenServiceNOW/internal/metadata"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
	"strings"
)

type Service struct {
	DB       *sql.DB
	tx       *database.TxManager
	Assets   *assets.Service
	Files    *attachments.Service
	Domains  map[string]RecordDomain
	Metadata *metadata.Service
}

func NewService(db *sql.DB, files *attachments.Service, domains map[string]RecordDomain) *Service {
	return &Service{DB: db, tx: database.NewTxManager(db), Assets: assets.NewService(db), Files: files, Domains: domains}
}
func (s *Service) WithMetadata(service *metadata.Service) *Service { s.Metadata = service; return s }

func metadataActor(a Actor) metadata.Actor {
	return metadata.Actor{TenantID: a.TenantID, UserID: a.UserID, Permissions: a.Permissions, RequestID: a.RequestID, IPAddress: a.IPAddress}
}

func metadataBaseValues(f Fields) map[string]json.RawMessage {
	values := map[string]json.RawMessage{}
	for key, value := range map[string]any{"identifier": f.Identifier, "name": f.Name, "class": f.Class, "status": f.Status, "owner_id": fmt.Sprint(f.OwnerID), "environment": f.Environment, "location": f.Location} {
		if raw, err := json.Marshal(value); err == nil {
			values[key] = raw
		}
	}
	return values
}

func dictionaryValues(configuration metadata.Configuration, key string, fallback []string) []string {
	options := configuration.Dictionaries[key]
	if len(options) == 0 {
		return fallback
	}
	values := make([]string, 0, len(options))
	for _, option := range options {
		values = append(values, option.Value)
	}
	return values
}

func (s *Service) normalize(ctx context.Context, e database.Executor, a Actor, f Fields) (Fields, error) {
	if s.Metadata == nil {
		return Normalize(f)
	}
	_, configuration, err := s.Metadata.Published(ctx, e, a.TenantID)
	if err != nil {
		return f, err
	}
	return NormalizeWithOptions(f, dictionaryValues(configuration, "ci.status", statuses), dictionaryValues(configuration, "ci.environment", environments))
}

func (s *Service) SchemaForTenant(ctx context.Context, tenant uint64) (Contract, error) {
	contract := Schema()
	if s.Metadata == nil {
		return contract, nil
	}
	version, configuration, err := s.Metadata.Published(ctx, s.DB, tenant)
	if err != nil {
		return Contract{}, err
	}
	contract.CustomFieldsSchema, err = metadata.BuildSchema("ci", version, configuration)
	if err != nil {
		return Contract{}, err
	}
	for _, field := range contract.CustomFieldsSchema.Fields {
		if field.Active && !field.System {
			contract.CIImportFields = append(contract.CIImportFields, "custom_fields."+field.Key)
		}
	}
	contract.Statuses = dictionaryValues(configuration, "ci.status", statuses)
	contract.Environments = dictionaryValues(configuration, "ci.environment", environments)
	for index := range contract.CIFields {
		switch contract.CIFields[index].Key {
		case "status":
			contract.CIFields[index].Options = contract.Statuses
		case "environment":
			contract.CIFields[index].Options = contract.Environments
		}
	}
	return contract, nil
}

func (s *Service) loadCustomFields(ctx context.Context, e database.Executor, a Actor, r *Record) error {
	if s.Metadata == nil {
		return nil
	}
	version, _, err := s.Metadata.Published(ctx, e, a.TenantID)
	if err != nil {
		return err
	}
	r.CustomFieldsVersion = version
	values, err := metadata.ReadValues(ctx, e, a.TenantID, "ci", []uint64{r.ID})
	if err != nil {
		return err
	}
	r.CustomFields = values[r.ID]
	if r.CustomFields == nil {
		r.CustomFields = map[string]json.RawMessage{}
	}
	return nil
}

func (s *Service) prepareCustomFields(ctx context.Context, e database.Executor, a Actor, id uint64, f Fields, pinnedVersion uint64) (uint64, map[string]json.RawMessage, metadata.Schema, error) {
	if s.Metadata == nil {
		return 0, map[string]json.RawMessage{}, metadata.Schema{}, nil
	}
	var values map[string]json.RawMessage
	var schema metadata.Schema
	var err error
	if pinnedVersion > 0 {
		if f.CustomFieldsVersion != 0 && f.CustomFieldsVersion != pinnedVersion {
			return 0, nil, metadata.Schema{}, ErrConflict
		}
		values, schema, err = s.Metadata.PrepareValuesAtVersion(ctx, e, metadataActor(a), "ci", id, f.CustomFields, metadataBaseValues(f), pinnedVersion)
	} else {
		values, schema, err = s.Metadata.PrepareValues(ctx, e, metadataActor(a), "ci", id, f.CustomFields, metadataBaseValues(f), f.CustomFieldsVersion)
	}
	return schema.Version, values, schema, err
}
func Access(a Actor, alias string) (string, []any) {
	if a.Has("cmdb.read") {
		return "1=1", nil
	}
	if a.Has("cmdb.read_own") {
		return alias + ".owner_id=?", []any{a.UserID}
	}
	return "1=0", nil
}
func Writable(a Actor) bool { return a.Has("cmdb.manage") && a.Has("cmdb.read") }
func (s *Service) ActorTx(ctx context.Context, a Actor, fn func(database.Executor, Actor) error) error {
	return s.tx.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(e database.Executor) error {
		current, err := workrecords.LockPrincipal(ctx, e, a)
		if err != nil {
			return err
		}
		return fn(e, current)
	})
}

const columns = `c.id,c.identifier,c.name,c.class,c.status,c.owner_id,u.display_name,c.environment,c.location,c.attributes,c.archived,c.version,c.created_at,c.updated_at`

func scan(row interface{ Scan(...any) error }) (Record, error) {
	var r Record
	var raw []byte
	err := row.Scan(&r.ID, &r.Identifier, &r.Name, &r.Class, &r.Status, &r.OwnerID, &r.OwnerName, &r.Environment, &r.Location, &raw, &r.Archived, &r.Version, &r.CreatedAt, &r.UpdatedAt)
	if err != nil {
		return r, assets.Missing(err)
	}
	err = json.Unmarshal(raw, &r.Attributes)
	return r, err
}
func (s *Service) GetInTx(ctx context.Context, e database.Executor, a Actor, id uint64, lock bool) (Record, error) {
	p, args := Access(a, "c")
	params := []any{a.TenantID, id}
	params = append(params, args...)
	q := "SELECT " + columns + " FROM cmdb_cis c JOIN users u ON u.id=c.owner_id WHERE c.tenant_id=? AND c.id=? AND " + p
	if lock {
		q += " FOR UPDATE"
	}
	record, err := scan(e.QueryRowContext(ctx, q, params...))
	if err != nil {
		return record, err
	}
	if err = s.loadCustomFields(ctx, e, a, &record); err != nil {
		return Record{}, err
	}
	return record, nil
}
func (s *Service) Get(ctx context.Context, a Actor, id uint64) (Record, error) {
	canonical, err := s.canonicalCIID(ctx, s.DB, a.TenantID, id)
	if err != nil {
		return Record{}, err
	}
	return s.GetInTx(ctx, s.DB, a, canonical, false)
}
func (s *Service) List(ctx context.Context, a Actor, f Filter) (Page, error) {
	page := Page{Items: []Record{}}
	allowedStatuses, allowedEnvironments := statuses, environments
	var configuration metadata.Configuration
	var schema metadata.Schema
	if s.Metadata != nil {
		version, loaded, err := s.Metadata.Published(ctx, s.DB, a.TenantID)
		if err != nil {
			return page, err
		}
		configuration = loaded
		allowedStatuses = dictionaryValues(configuration, "ci.status", statuses)
		allowedEnvironments = dictionaryValues(configuration, "ci.environment", environments)
		typeConfig, ok := configuration.RecordTypes["ci"]
		if !ok {
			return page, ErrInvalid
		}
		if f.Sort == "" {
			f.Sort = typeConfig.List.DefaultSort
		}
		f.SearchFields = append([]string{}, typeConfig.List.SearchFields...)
		schema, err = metadata.BuildSchema("ci", version, configuration)
		if err != nil {
			return page, err
		}
		f.SearchableTypes = map[string]string{}
		for _, field := range schema.Fields {
			if !field.System {
				f.SearchableTypes[field.Key] = field.Type
			}
		}
	} else {
		f.SearchFields = []string{"identifier", "name"}
	}
	if f.Sort == "" {
		f.Sort = "newest"
	}
	if f.Limit < 1 || f.Limit > 100 || !assets.Text(f.Query, 0, 160) || !assets.Text(f.Location, 0, 160) || f.Status != "" && !containsValue(allowedStatuses, f.Status) || f.Environment != "" && !containsValue(allowedEnvironments, f.Environment) || f.Archived != "" && f.Archived != "true" && f.Archived != "false" {
		return page, ErrInvalid
	}
	if f.Sort != "newest" && f.Sort != "oldest" {
		return page, ErrInvalid
	}
	if f.Class != "" {
		if _, ok := ClassDefinition(f.Class); !ok {
			return page, ErrInvalid
		}
	}
	if !a.Has("cmdb.read") && !a.Has("cmdb.read_own") {
		return page, workrecords.ErrForbidden
	}
	p, args := Access(a, "c")
	params := []any{a.TenantID}
	params = append(params, args...)
	q := "SELECT " + columns + " FROM cmdb_cis c JOIN users u ON u.id=c.owner_id WHERE c.tenant_id=? AND c.redirect_ci_id IS NULL AND " + p
	if f.Query != "" {
		searches := []string{}
		systemPattern := strings.NewReplacer("=", "==", "%", "=%", "_", "=_").Replace(strings.ToLower(f.Query)) + "%"
		customPattern := strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(f.Query) + "%"
		fieldByKey := map[string]metadata.Field{}
		for _, field := range schema.Fields {
			fieldByKey[field.Key] = field
		}
		for _, key := range f.SearchFields {
			switch key {
			case "identifier", "name", "class", "status", "environment", "location":
				searches = append(searches, "c."+key+" LIKE ? ESCAPE '='")
				params = append(params, systemPattern)
			default:
				field, ok := fieldByKey[key]
				if !ok || field.System {
					return page, ErrInvalid
				}
				clause, values, err := metadata.SearchClause("ci", "c", field, a.TenantID, customPattern)
				if err != nil {
					return page, err
				}
				searches = append(searches, clause)
				params = append(params, values...)
			}
		}
		if len(searches) == 0 {
			return page, ErrInvalid
		}
		q += " AND (" + strings.Join(searches, " OR ") + ")"
	}
	for _, filter := range []struct{ column, value string }{{"class", f.Class}, {"status", f.Status}, {"environment", f.Environment}, {"location", f.Location}} {
		if filter.value != "" {
			q += " AND c." + filter.column + "=?"
			params = append(params, filter.value)
		}
	}
	if f.OwnerID > 0 {
		q += " AND c.owner_id=?"
		params = append(params, f.OwnerID)
	}
	if f.Archived != "" {
		q += " AND c.archived=?"
		params = append(params, f.Archived == "true")
	}
	if f.Before > 0 {
		if f.Sort == "oldest" {
			q += " AND c.id>?"
		} else {
			q += " AND c.id<?"
		}
		params = append(params, f.Before)
	}
	if s.Metadata != nil {
		for _, filter := range f.CustomFields {
			clause, values, err := metadata.QueryClause("ci", "c", filter, a.TenantID)
			if err != nil {
				return page, err
			}
			q += " AND " + clause
			params = append(params, values...)
		}
	}
	if f.Sort == "oldest" {
		q += " ORDER BY c.id ASC"
	} else {
		q += " ORDER BY c.id DESC"
	}
	q += " LIMIT ?"
	params = append(params, f.Limit+1)
	rows, err := s.DB.QueryContext(ctx, q, params...)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		r, e := scan(rows)
		if e != nil {
			return page, e
		}
		page.Items = append(page.Items, r)
	}
	if len(page.Items) > f.Limit {
		page.Items = page.Items[:f.Limit]
		page.Next = page.Items[f.Limit-1].ID
	}
	if err := rows.Err(); err != nil {
		return page, err
	}
	if s.Metadata != nil {
		ids := make([]uint64, 0, len(page.Items))
		for _, item := range page.Items {
			ids = append(ids, item.ID)
		}
		version, _, err := s.Metadata.Published(ctx, s.DB, a.TenantID)
		if err != nil {
			return page, err
		}
		values, err := metadata.ReadValues(ctx, s.DB, a.TenantID, "ci", ids)
		if err != nil {
			return page, err
		}
		for index := range page.Items {
			page.Items[index].CustomFieldsVersion = version
			page.Items[index].CustomFields = values[page.Items[index].ID]
			if page.Items[index].CustomFields == nil {
				page.Items[index].CustomFields = map[string]json.RawMessage{}
			}
		}
	}
	return page, nil
}
func (s *Service) SaveInTx(ctx context.Context, e database.Executor, a Actor, id uint64, in Record) (Record, error) {
	return s.saveInTx(ctx, e, a, id, in, 0)
}

func (s *Service) SaveInTxAtMetadataVersion(ctx context.Context, e database.Executor, a Actor, id uint64, in Record, version uint64) (Record, error) {
	if version == 0 {
		return in, ErrInvalid
	}
	return s.saveInTx(ctx, e, a, id, in, version)
}

func (s *Service) saveInTx(ctx context.Context, e database.Executor, a Actor, id uint64, in Record, pinnedVersion uint64) (Record, error) {
	if !Writable(a) {
		return in, workrecords.ErrForbidden
	}
	var f Fields
	var err error
	if pinnedVersion > 0 {
		if s.Metadata == nil {
			return in, ErrInvalid
		}
		configuration, configErr := s.Metadata.ConfigurationAtVersion(ctx, a.TenantID, pinnedVersion)
		if configErr != nil {
			return in, configErr
		}
		f, err = NormalizeWithOptions(in.Fields, dictionaryValues(configuration, "ci.status", statuses), dictionaryValues(configuration, "ci.environment", environments))
	} else {
		f, err = s.normalize(ctx, e, a, in.Fields)
	}
	if err != nil {
		return in, err
	}
	in.Fields = f
	if err := workrecords.ValidateActiveUser(ctx, e, a.TenantID, in.OwnerID); err != nil {
		if !errors.Is(err, workrecords.ErrNotFound) {
			return in, err
		}
		return in, fmt.Errorf("%w: opiekun nie jest aktywnym członkiem organizacji", ErrInvalid)
	}
	var before *Record
	if id > 0 {
		old, err := s.GetInTx(ctx, e, a, id, true)
		if err != nil {
			return in, err
		}
		if old.Archived {
			return in, fmt.Errorf("%w: najpierw przywróć CI z archiwum", ErrConflict)
		}
		if in.Version == 0 || old.Version != in.Version {
			return in, ErrConflict
		}
		if old.Class != in.Class {
			return in, fmt.Errorf("%w: klasa istniejącego CI jest niezmienna", ErrConflict)
		}
		before = &old
	}
	customVersion, customFields, customSchema, err := s.prepareCustomFields(ctx, e, a, id, in.Fields, pinnedVersion)
	if err != nil {
		return in, err
	}
	in.CustomFields = customFields
	raw, _ := json.Marshal(in.Attributes)
	args := []any{in.Identifier, in.Name, in.Class, in.Status, in.OwnerID, in.Environment, in.Location, raw}
	if id == 0 {
		args = append([]any{a.TenantID}, args...)
		r, err := e.ExecContext(ctx, "INSERT INTO cmdb_cis(tenant_id,identifier,name,class,status,owner_id,environment,location,attributes) VALUES(?,?,?,?,?,?,?,?,?)", args...)
		if err != nil {
			return in, assets.Missing(err)
		}
		n, err := r.LastInsertId()
		if err != nil {
			return in, err
		}
		id = uint64(n)
	} else {
		args = append(args, a.TenantID, id, in.Version)
		r, err := e.ExecContext(ctx, "UPDATE cmdb_cis SET identifier=?,name=?,class=?,status=?,owner_id=?,environment=?,location=?,attributes=?,version=version+1,updated_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND id=? AND version=?", args...)
		if err != nil {
			return in, assets.Missing(err)
		}
		n, err := r.RowsAffected()
		if err != nil {
			return in, err
		}
		if n != 1 {
			return in, ErrConflict
		}
	}
	if s.Metadata != nil {
		if err := s.Metadata.SaveValues(ctx, e, a.TenantID, "ci", id, customVersion, customSchema, customFields); err != nil {
			return in, err
		}
	}
	out, err := s.GetInTx(ctx, e, a, id, false)
	if err != nil {
		return in, err
	}
	if before != nil {
		if err := s.loadCustomFields(ctx, e, a, before); err != nil {
			return out, err
		}
	}
	return out, assets.Changed(ctx, e, a, "ci", id, out.Version, before, out, []string{"data"})
}
func (s *Service) Save(ctx context.Context, a Actor, id uint64, in Record, key string) (Record, error) {
	var out Record
	err := s.ActorTx(ctx, a, func(e database.Executor, a Actor) error {
		if !Writable(a) {
			return workrecords.ErrForbidden
		}
		if id == 0 {
			f, err := s.normalize(ctx, e, a, in.Fields)
			if err != nil {
				return err
			}
			in.Fields = f
			raw, _ := json.Marshal(in.Fields)
			old, err := workrecords.ReserveOperation(ctx, e, a.Actor, key, "ci_create", sha256.Sum256(raw))
			if err != nil {
				return err
			}
			if old > 0 {
				out, err = s.GetInTx(ctx, e, a, old, false)
				return err
			}
		}
		var err error
		out, err = s.SaveInTx(ctx, e, a, id, in)
		if err != nil {
			return err
		}
		if id == 0 {
			return workrecords.CompleteOperation(ctx, e, a.Actor, key, out.ID)
		}
		return nil
	})
	return out, err
}
func (s *Service) touch(ctx context.Context, e database.Executor, a Actor, r Record, field string) (Record, error) {
	before := r
	result, err := e.ExecContext(ctx, "UPDATE cmdb_cis SET version=version+1,updated_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND id=? AND version=?", a.TenantID, r.ID, r.Version)
	if err != nil {
		return r, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return r, err
	}
	if n != 1 {
		return r, ErrConflict
	}
	r, err = s.GetInTx(ctx, e, a, r.ID, false)
	if err != nil {
		return r, err
	}
	return r, assets.Changed(ctx, e, a, "ci", r.ID, r.Version, before, r, []string{field})
}
func (s *Service) Archive(ctx context.Context, a Actor, id, version uint64, flag bool, reason string) (Record, error) {
	var out Record
	if !assets.Text(strings.TrimSpace(reason), 1, 1000) || version == 0 {
		return out, ErrInvalid
	}
	err := s.ActorTx(ctx, a, func(e database.Executor, a Actor) error {
		if !Writable(a) {
			return workrecords.ErrForbidden
		}
		canonical, canonicalErr := s.canonicalCIID(ctx, e, a.TenantID, id)
		if canonicalErr != nil {
			return canonicalErr
		}
		if canonical != id {
			return ErrConflict
		}
		r, err := s.GetInTx(ctx, e, a, id, true)
		if err != nil {
			return err
		}
		if r.Version != version {
			return ErrConflict
		}
		if r.Archived == flag {
			out = r
			return nil
		}
		if r.Version != version {
			return ErrConflict
		}
		if _, err = e.ExecContext(ctx, "UPDATE cmdb_cis SET archived=? WHERE tenant_id=? AND id=?", flag, a.TenantID, id); err != nil {
			return err
		}
		out, err = s.touch(ctx, e, a, r, "archived")
		if err != nil {
			return err
		}
		return audit.NewMySQLRepository().Append(ctx, e, audit.Event{TenantID: &a.TenantID, ActorUserID: &a.UserID, Action: "ci.archive", ObjectType: "ci", ObjectID: fmt.Sprint(id), NewValues: map[string]any{"archived": flag, "reason": reason}, RequestID: a.RequestID, IPAddress: a.IPAddress, Source: "inventory"})
	})
	return out, err
}

type HistoryPage struct {
	activity.Page
	Pending bool `json:"pending"`
}

func (s *Service) History(ctx context.Context, a Actor, kind string, id, before uint64, limit int) (HistoryPage, error) {
	if limit < 1 || limit > 100 {
		return HistoryPage{}, ErrInvalid
	}
	if kind == "ci" {
		if _, err := s.Get(ctx, a, id); err != nil {
			return HistoryPage{}, err
		}
	} else if kind == "asset" {
		if _, err := s.Assets.Get(ctx, a, id); err != nil {
			return HistoryPage{}, err
		}
	} else {
		return HistoryPage{}, ErrInvalid
	}
	p, err := activity.NewRepository(s.DB).ListBySourceEvent(ctx, a.TenantID, kind, fmt.Sprint(id), before, limit)
	if err != nil {
		return HistoryPage{}, err
	}
	out := HistoryPage{Page: p}
	err = s.DB.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM outbox_events o WHERE o.tenant_id=? AND o.aggregate_type=? AND o.aggregate_id=? AND (o.status<>'DELIVERED' OR EXISTS(SELECT 1 FROM event_deliveries d WHERE d.event_id=o.id AND d.consumer_id='activity.v1' AND d.status<>'DELIVERED')))", a.TenantID, kind, fmt.Sprint(id)).Scan(&out.Pending)
	return out, err
}
