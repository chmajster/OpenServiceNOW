package metadata

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/chmajster/OpenServiceNOW/internal/audit"
	"github.com/chmajster/OpenServiceNOW/internal/events"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
)

const ManagePermission = "admin.metadata.manage"

type Service struct {
	DB     *sql.DB
	tx     *database.TxManager
	outbox *events.Repository
}

func NewService(db *sql.DB) *Service {
	return &Service{DB: db, tx: database.NewTxManager(db), outbox: events.NewRepository(db)}
}

func (s *Service) adminTx(ctx context.Context, actor Actor, fn func(database.Executor, Actor) error) error {
	return s.tx.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(exec database.Executor) error {
		principal := workrecords.Principal{Actor: workrecords.Actor{TenantID: actor.TenantID, UserID: actor.UserID, RequestID: actor.RequestID}, IPAddress: actor.IPAddress, Permissions: actor.Permissions}
		current, err := workrecords.LockPrincipal(ctx, exec, principal)
		if err != nil { return err }
		if !current.Has(ManagePermission) { return workrecords.ErrForbidden }
		actor.Permissions = current.Permissions
		return fn(exec, actor)
	})
}

func tenantName(ctx context.Context, exec database.Executor, tenant uint64) (string, error) {
	var name string
	err := exec.QueryRowContext(ctx, "SELECT name FROM tenants WHERE id=?", tenant).Scan(&name)
	return name, err
}

func readVersion(ctx context.Context, exec database.Executor, tenant, version uint64) (Version, error) {
	var out Version
	var configuration []byte
	var published sql.NullTime
	err := exec.QueryRowContext(ctx, "SELECT version,state,created_by,created_at,published_at,configuration FROM metadata_config_versions WHERE tenant_id=? AND version=?", tenant, version).Scan(&out.Version, &out.State, &out.CreatedBy, &out.CreatedAt, &published, &configuration)
	if errors.Is(err, sql.ErrNoRows) { return out, ErrNotFound }
	if err != nil { return out, err }
	if published.Valid { out.PublishedAt = &published.Time }
	if err := json.Unmarshal(configuration, &out.Config); err != nil { return out, err }
	out.Config = WithDefaults(out.Config)
	return out, nil
}

func (s *Service) Head(ctx context.Context, actor Actor) (Head, error) {
	var out Head
	if err:=s.ensureTenantDefaults(ctx,s.DB,actor.TenantID);err!=nil{return out,err}
	var draft, published sql.Null[uint64]
	err := s.DB.QueryRowContext(ctx, "SELECT version,draft_version,published_version FROM metadata_config_heads WHERE tenant_id=?", actor.TenantID).Scan(&out.Version, &draft, &published)
	if errors.Is(err, sql.ErrNoRows) {
		name, nameErr := tenantName(ctx, s.DB, actor.TenantID)
		if nameErr != nil { return out, nameErr }
		config := DefaultConfiguration(name)
		out.Draft, out.Published = config, config
		return out, nil
	}
	if err != nil { return out, err }
	name, err := tenantName(ctx, s.DB, actor.TenantID)
	if err != nil { return out, err }
	defaultConfig := DefaultConfiguration(name)
	if draft.Valid {
		version, err := readVersion(ctx, s.DB, actor.TenantID, draft.V)
		if err != nil { return out, err }
		out.DraftVersion, out.Draft = version.Version, version.Config
	} else { out.Draft = defaultConfig }
	if published.Valid {
		version, err := readVersion(ctx, s.DB, actor.TenantID, published.V)
		if err != nil { return out, err }
		out.PublishedVersion, out.Published = version.Version, version.Config
	} else { out.Published = defaultConfig }
	return out, nil
}

func (s *Service) Published(ctx context.Context, exec database.Executor, tenant uint64) (uint64, Configuration, error) {
	if err := s.ensureTenantDefaults(ctx,exec,tenant); err != nil { return 0,Configuration{},err }
	var version sql.Null[uint64]
	err := exec.QueryRowContext(ctx, "SELECT published_version FROM metadata_config_heads WHERE tenant_id=? FOR SHARE", tenant).Scan(&version)
	if errors.Is(err, sql.ErrNoRows) || err == nil && !version.Valid {
		name, err := tenantName(ctx, exec, tenant)
		if err != nil { return 0, Configuration{}, err }
		return 0, DefaultConfiguration(name), nil
	}
	if err != nil { return 0, Configuration{}, err }
	configuration, err := readVersion(ctx, exec, tenant, version.V)
	return version.V, configuration.Config, err
}

func (s *Service) ensureTenantDefaults(ctx context.Context,exec database.Executor,tenant uint64) error {
	var exists bool
	if err:=exec.QueryRowContext(ctx,"SELECT EXISTS(SELECT 1 FROM metadata_config_heads WHERE tenant_id=?)",tenant).Scan(&exists);err!=nil{return err}
	if exists{return nil}
	name,err:=tenantName(ctx,exec,tenant);if err!=nil{return err}
	var creator uint64
	if err=exec.QueryRowContext(ctx,"SELECT user_id FROM tenant_memberships WHERE tenant_id=? AND active=TRUE ORDER BY user_id LIMIT 1",tenant).Scan(&creator);err!=nil{return err}
	configuration:=DefaultConfiguration(name)
	raw,err:=json.Marshal(configuration);if err!=nil{return err}
	if _,err=exec.ExecContext(ctx,"INSERT IGNORE INTO metadata_config_versions(tenant_id,version,state,configuration,created_by,published_at) VALUES(?,1,'PUBLISHED',?,?,UTC_TIMESTAMP(6))",tenant,raw,creator);err!=nil{return err}
	_,err=exec.ExecContext(ctx,"INSERT IGNORE INTO metadata_config_heads(tenant_id,version,draft_version,published_version) VALUES(?,1,NULL,1)",tenant)
	return err
}

func (s *Service) Schema(ctx context.Context, tenant uint64, typeKey string) (Schema, error) {
	version, config, err := s.Published(ctx, s.DB, tenant)
	if err != nil { return Schema{}, err }
	return BuildSchema(typeKey,version,config)
}

func (s *Service) ConfigurationAtVersion(ctx context.Context,tenant,version uint64)(Configuration,error){
	if version==0{return Configuration{},ErrInvalid}
	item,err:=readVersion(ctx,s.DB,tenant,version);if err!=nil{return Configuration{},err}
	if item.State!="PUBLISHED"&&item.State!="RETIRED"{return Configuration{},ErrNotFound}
	return item.Config,nil
}

func BuildSchema(typeKey string,version uint64,config Configuration)(Schema,error){
	schema,err:=config.Schema(typeKey,version);if err!=nil{return schema,err};schema=enrichSchema(schema,config);schema.Configuration=config;return schema,nil
}

func enrichSchema(schema Schema, config Configuration) Schema {
	for index := range schema.Fields {
		field := &schema.Fields[index]
		if field.Key == "category" { field.Options = config.Dictionaries["incident.category"] }
		if field.Key == "subcategory" {
			options := []Option{}
			for key, items := range config.Dictionaries {
				if strings.HasPrefix(key, "incident.subcategory.") { options = append(options, items...) }
			}
			field.Options = uniqueOptions(options)
		}
		if field.Key == "state" { field.Options = config.Dictionaries["incident.state"] }
		if field.Key == "resolution_code" { field.Options = config.Dictionaries["incident.resolution_code"] }
		if field.Key == "environment" { field.Options = config.Dictionaries["ci.environment"] }
		if field.Key == "class" {
			field.Options = []Option{{Value:"PHYSICAL_SERVER",Labels:map[string]string{"pl":"Serwer fizyczny","en":"Physical server"}},{Value:"VM",Labels:map[string]string{"pl":"Maszyna wirtualna","en":"Virtual machine"}},{Value:"NETWORK_DEVICE",Labels:map[string]string{"pl":"Urządzenie sieciowe","en":"Network device"}},{Value:"WORKSTATION",Labels:map[string]string{"pl":"Komputer użytkownika","en":"Workstation"}},{Value:"PRINTER",Labels:map[string]string{"pl":"Drukarka","en":"Printer"}},{Value:"STORAGE_DEVICE",Labels:map[string]string{"pl":"Urządzenie pamięci masowej","en":"Storage device"}},{Value:"MOBILE_DEVICE",Labels:map[string]string{"pl":"Urządzenie mobilne","en":"Mobile device"}},{Value:"PERIPHERAL_DEVICE",Labels:map[string]string{"pl":"Urządzenie peryferyjne","en":"Peripheral device"}},{Value:"UPS",Labels:map[string]string{"pl":"Zasilacz awaryjny UPS","en":"Uninterruptible power supply"}},{Value:"APPLICATION",Labels:map[string]string{"pl":"Aplikacja","en":"Application"}},{Value:"SERVICE",Labels:map[string]string{"pl":"Usługa","en":"Service"}}}
		}
		if field.Key == "status" && schema.TypeKey == "ci" { field.Options = config.Dictionaries["ci.status"] }
	}
	return schema
}

func uniqueOptions(options []Option) []Option {
	seen := map[string]bool{}; out := []Option{}
	for _, option := range options { if !seen[option.Value] { seen[option.Value] = true; out = append(out, option) } }
	sort.Slice(out,func(i,j int)bool{return out[i].Value<out[j].Value})
	return out
}

func (s *Service) SaveDraft(ctx context.Context, actor Actor, expected uint64, configuration Configuration) (Head, error) {
	if err := ValidateConfiguration(configuration); err != nil { return Head{}, err }
	var head Head
	err := s.adminTx(ctx, actor, func(exec database.Executor, actor Actor) error {
		var current uint64
		var draft,published sql.Null[uint64]
		err := exec.QueryRowContext(ctx,"SELECT version,draft_version,published_version FROM metadata_config_heads WHERE tenant_id=? FOR UPDATE",actor.TenantID).Scan(&current,&draft,&published)
		if errors.Is(err,sql.ErrNoRows) { current=0 } else if err!=nil{return err}
		if current!=expected { return ErrConflict }
		var next uint64
		if err=exec.QueryRowContext(ctx,"SELECT COALESCE(MAX(version),0)+1 FROM metadata_config_versions WHERE tenant_id=?",actor.TenantID).Scan(&next);err!=nil{return err}
		raw,err:=json.Marshal(configuration);if err!=nil{return err}
		if draft.Valid { if _,err=exec.ExecContext(ctx,"UPDATE metadata_config_versions SET state='RETIRED' WHERE tenant_id=? AND version=? AND state='DRAFT'",actor.TenantID,draft.V);err!=nil{return err} }
		if _,err=exec.ExecContext(ctx,"INSERT INTO metadata_config_versions(tenant_id,version,state,configuration,created_by) VALUES(?,?,'DRAFT',?,?)",actor.TenantID,next,raw,actor.UserID);err!=nil{return err}
		if current==0 {
			_,err=exec.ExecContext(ctx,"INSERT INTO metadata_config_heads(tenant_id,version,draft_version,published_version) VALUES(?,?,?,NULL)",actor.TenantID,1,next)
		} else {
			_,err=exec.ExecContext(ctx,"UPDATE metadata_config_heads SET version=version+1,draft_version=?,updated_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND version=?",next,actor.TenantID,current)
		}
		if err!=nil{return err}
		if err=configurationAudit(ctx,exec,s.outbox,actor,"metadata.draft.saved",next,map[string]any{"version":fmt.Sprint(next)});err!=nil{return err}
		head=Head{Version:current+1,DraftVersion:next,Draft:configuration}
		if published.Valid { head.PublishedVersion=published.V; v,e:=readVersion(ctx,exec,actor.TenantID,published.V);if e!=nil{return e};head.Published=v.Config } else { name,e:=tenantName(ctx,exec,actor.TenantID);if e!=nil{return e};head.Published=DefaultConfiguration(name) }
		return nil
	})
	return head,err
}

func (s *Service) Publish(ctx context.Context, actor Actor, expected uint64) (Head, Impact, error) {
	var result Head
	var impact Impact
	err := s.adminTx(ctx,actor,func(exec database.Executor, actor Actor) error {
		var headVersion uint64
		var draft,published sql.Null[uint64]
		if err:=exec.QueryRowContext(ctx,"SELECT version,draft_version,published_version FROM metadata_config_heads WHERE tenant_id=? FOR UPDATE",actor.TenantID).Scan(&headVersion,&draft,&published);err!=nil{return ErrNotFound}
		if headVersion!=expected{return ErrConflict}
		if !draft.Valid{return ErrConflict}
		candidate,err:=readVersion(ctx,exec,actor.TenantID,draft.V);if err!=nil{return err}
		if err=ValidateConfiguration(candidate.Config);err!=nil{return err}
		var prior Configuration
		if published.Valid { version,e:=readVersion(ctx,exec,actor.TenantID,published.V);if e!=nil{return e};prior=version.Config } else { name,e:=tenantName(ctx,exec,actor.TenantID);if e!=nil{return e};prior=DefaultConfiguration(name) }
		impact,err=s.analyzeAndMigrate(ctx,exec,actor.TenantID,prior,candidate.Config,draft.V,false)
		if err!=nil{return err}
		if !impact.CanPublish { return ImpactError{Impact:impact} }
		impact,err=s.analyzeAndMigrate(ctx,exec,actor.TenantID,prior,candidate.Config,draft.V,true)
		if err!=nil{return err}
		if published.Valid && published.V!=draft.V { if _,err=exec.ExecContext(ctx,"UPDATE metadata_config_versions SET state='RETIRED' WHERE tenant_id=? AND version=?",actor.TenantID,published.V);err!=nil{return err} }
		if _,err=exec.ExecContext(ctx,"UPDATE metadata_config_versions SET state='PUBLISHED',published_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND version=?",actor.TenantID,draft.V);err!=nil{return err}
		if _,err=exec.ExecContext(ctx,"UPDATE metadata_config_heads SET version=version+1,published_version=?,updated_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND version=?",draft.V,actor.TenantID,headVersion);err!=nil{return err}
		if err=configurationAudit(ctx,exec,s.outbox,actor,"metadata.published",draft.V,map[string]any{"version":fmt.Sprint(draft.V)});err!=nil{return err}
		result=Head{Version:headVersion+1,DraftVersion:draft.V,PublishedVersion:draft.V,Draft:candidate.Config,Published:candidate.Config}
		return nil
	})
	return result,impact,err
}

func (s *Service) Restore(ctx context.Context, actor Actor, expected, source uint64) (Head,error) {
	version,err:=readVersion(ctx,s.DB,actor.TenantID,source);if err!=nil{return Head{},err}
	return s.SaveDraft(ctx,actor,expected,version.Config)
}

func (s *Service) Versions(ctx context.Context, actor Actor, before uint64, limit int) ([]Version,uint64,error) {
	if limit<1||limit>100{return nil,0,ErrInvalid}
	rows,err:=s.DB.QueryContext(ctx,"SELECT version,state,created_by,created_at,published_at,configuration FROM metadata_config_versions WHERE tenant_id=? AND (?=0 OR version<?) ORDER BY version DESC LIMIT ?",actor.TenantID,before,before,limit+1)
	if err!=nil{return nil,0,err};defer rows.Close()
	items:=[]Version{}
	for rows.Next(){var item Version;var raw []byte;var published sql.NullTime;if err=rows.Scan(&item.Version,&item.State,&item.CreatedBy,&item.CreatedAt,&published,&raw);err!=nil{return nil,0,err};if published.Valid{item.PublishedAt=&published.Time};if err=json.Unmarshal(raw,&item.Config);err!=nil{return nil,0,err};item.Config=WithDefaults(item.Config);items=append(items,item)}
	if err=rows.Err();err!=nil{return nil,0,err};var next uint64;if len(items)>limit{items=items[:limit];next=items[limit-1].Version};return items,next,nil
}

func (s *Service) Version(ctx context.Context, actor Actor, version uint64) (Version,error) { return readVersion(ctx,s.DB,actor.TenantID,version) }

func configurationAudit(ctx context.Context, exec database.Executor, outbox *events.Repository, actor Actor, kind string, version uint64, values any) error {
	if err:=audit.NewMySQLRepository().Append(ctx,exec,audit.Event{TenantID:&actor.TenantID,ActorUserID:&actor.UserID,Action:kind,ObjectType:"metadata_configuration",ObjectID:fmt.Sprint(version),NewValues:values,RequestID:actor.RequestID,IPAddress:actor.IPAddress,Source:"api"});err!=nil{return err}
	payload,err:=json.Marshal(map[string]any{"actor_user_id":fmt.Sprint(actor.UserID),"occurred_at":time.Now().UTC(),"visibility":"INTERNAL","version":fmt.Sprint(version),"action":kind});if err!=nil{return err}
	_,_,err=outbox.Append(ctx,exec,events.Input{TenantID:actor.TenantID,Type:"metadata.configuration.changed",AggregateType:"metadata_configuration",AggregateID:fmt.Sprint(actor.TenantID),Payload:payload,IdempotencyKey:fmt.Sprintf("metadata:%d:%d:%s",actor.TenantID,version,kind)})
	return err
}

type ImpactError struct { Impact Impact }
func (e ImpactError) Error() string { return "published configuration is incompatible with stored values" }
func (e ImpactError) Unwrap() error { return ErrConflict }

