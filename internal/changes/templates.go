package changes

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/chmajster/OpenServiceNOW/internal/catalog"
	"github.com/chmajster/OpenServiceNOW/internal/cmdb"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/workflow"
	"reflect"
	"strconv"
	"time"
)

type TemplateConfig struct {
	Fields             Fields   `json:"fields"`
	CIIDs              []string `json:"ci_ids"`
	MaxRisk            string   `json:"max_risk"`
	MaxDurationSeconds int64    `json:"max_duration_seconds"`
}
type Template struct {
	ID               uint64         `json:"id,string"`
	Version          uint64         `json:"version,string"`
	Name             string         `json:"name"`
	Draft            TemplateConfig `json:"draft"`
	PublishedVersion *uint64        `json:"published_version,string"`
	Withdrawn        bool           `json:"withdrawn"`
	AuthorID         uint64         `json:"author_id,string"`
}
type TemplateInput struct {
	Version uint64         `json:"version,string"`
	Name    string         `json:"name"`
	Config  TemplateConfig `json:"config"`
}

func (s *Service) template(ctx context.Context, e database.Executor, a Actor, id uint64) (Template, error) {
	var t Template
	var raw []byte
	err := e.QueryRowContext(ctx, "SELECT id,version,name,draft,published_version,withdrawn,author_id FROM change_templates WHERE tenant_id=? AND id=?", a.TenantID, id).Scan(&t.ID, &t.Version, &t.Name, &raw, &t.PublishedVersion, &t.Withdrawn, &t.AuthorID)
	if err != nil {
		return t, missing(err)
	}
	err = json.Unmarshal(raw, &t.Draft)
	return t, err
}
func (s *Service) Template(ctx context.Context, a Actor, id uint64) (Template, error) {
	if !a.Has("change.template_author") && !a.Has("change.template_approve") {
		return Template{}, ErrForbidden
	}
	return s.template(ctx, s.db, a, id)
}
func (s *Service) TemplateConfig(ctx context.Context, a Actor, id, version uint64) (TemplateConfig, error) {
	if !a.Has("change.read") && !a.Has("change.read_own") {
		return TemplateConfig{}, ErrForbidden
	}
	return publishedTemplate(ctx, s.db, a.TenantID, id, version)
}
func publishedTemplate(ctx context.Context, e database.Executor, tenant, id, version uint64) (TemplateConfig, error) {
	var c TemplateConfig
	var raw []byte
	err := e.QueryRowContext(ctx, "SELECT v.config FROM change_template_versions v JOIN change_templates t ON t.tenant_id=v.tenant_id AND t.id=v.template_id AND t.withdrawn=FALSE WHERE v.tenant_id=? AND v.template_id=? AND v.version=?", tenant, id, version).Scan(&raw)
	if err != nil {
		return c, missing(err)
	}
	err = json.Unmarshal(raw, &c)
	return c, err
}
func validateTemplateConfig(c TemplateConfig) error {
	if c.Fields.Type != "STANDARD" || c.Fields.TemplateID != nil || c.Fields.TemplateVersion != nil || c.Fields.GroupID == nil || c.Fields.WorkflowID == nil || c.Fields.WorkflowVersion == nil || c.Fields.PlannedStart != nil || c.Fields.PlannedEnd != nil || c.Fields.ReviewResult != "" || c.Fields.ReviewOutcome != "" || c.MaxDurationSeconds < 1 || c.MaxDurationSeconds > 30*86400 || !contains([]string{"HIGH", "MEDIUM", "LOW"}, c.MaxRisk) || len(c.CIIDs) < 1 || len(c.CIIDs) > 20 {
		return ErrInvalid
	}
	f := c.Fields
	f.Type = "NORMAL"
	if _, err := Normalize(f); err != nil {
		return err
	}
	for _, v := range []string{f.Justification, f.Scope, f.ImplementationPlan, f.TestPlan, f.RollbackPlan} {
		if v == "" {
			return ErrInvalid
		}
	}
	seen := map[string]bool{}
	for _, v := range c.CIIDs {
		n, err := strconv.ParseUint(v, 10, 64)
		if err != nil || n == 0 || fmt.Sprint(n) != v || seen[v] {
			return ErrInvalid
		}
		seen[v] = true
	}
	return nil
}
func (s *Service) SaveTemplate(ctx context.Context, a Actor, id uint64, in TemplateInput, key string) (Template, error) {
	var out Template
	if !catalog.Text(in.Name, 1, 160) {
		return out, ErrInvalid
	}
	if err := validateTemplateConfig(in.Config); err != nil {
		return out, err
	}
	raw, _ := json.Marshal(struct {
		ID    uint64
		Input TemplateInput
	}{id, in})
	err := s.actorTx(ctx, a, func(e database.Executor, a Actor) error {
		if !a.Has("change.template_author") {
			return ErrForbidden
		}
		old, err := reserve(ctx, e, a, key, "change_template_save", sha256.Sum256(raw))
		if err != nil {
			return err
		}
		if old > 0 {
			out, err = s.template(ctx, e, a, old)
			return err
		}
		c, err := workflow.Published(ctx, e, a.TenantID, *in.Config.Fields.WorkflowID, *in.Config.Fields.WorkflowVersion)
		if err != nil {
			return err
		}
		if c.Domain != "CHANGE" || c.ChangeType != "STANDARD" || c.GroupID != *in.Config.Fields.GroupID {
			return ErrInvalid
		}
		service := cmdb.NewService(s.db, nil, nil)
		for _, v := range in.Config.CIIDs {
			n, _ := strconv.ParseUint(v, 10, 64)
			if _, err = service.GetInTx(ctx, e, a, n, false); err != nil {
				return err
			}
		}
		config, _ := json.Marshal(in.Config)
		if id == 0 {
			if in.Version != 0 {
				return ErrInvalid
			}
			var count int
			if err = e.QueryRowContext(ctx, "SELECT COUNT(*) FROM change_templates WHERE tenant_id=?", a.TenantID).Scan(&count); err != nil {
				return err
			}
			if count >= 100 {
				return ErrInvalid
			}
			result, err := e.ExecContext(ctx, "INSERT INTO change_templates(tenant_id,name,draft,author_id) VALUES(?,?,?,?)", a.TenantID, in.Name, config, a.UserID)
			if err != nil {
				return err
			}
			n, err := result.LastInsertId()
			if err != nil {
				return err
			}
			id = uint64(n)
		} else {
			before, err := s.template(ctx, e, a, id)
			if err != nil {
				return err
			}
			if in.Version == 0 || before.Version != in.Version {
				return ErrConflict
			}
			if _, err = e.ExecContext(ctx, "UPDATE change_templates SET name=?,draft=?,version=version+1,author_id=? WHERE tenant_id=? AND id=? AND version=?", in.Name, config, a.UserID, a.TenantID, id, in.Version); err != nil {
				return err
			}
		}
		if err = catalog.Audit(ctx, e, a, "change.template.saved", "change_template", id, map[string]any{"name": in.Name}); err != nil {
			return err
		}
		out, err = s.template(ctx, e, a, id)
		if err != nil {
			return err
		}
		return complete(ctx, e, a, key, id)
	})
	return out, err
}
func (s *Service) PublishTemplate(ctx context.Context, a Actor, id, version uint64, withdraw bool, key string) (Template, error) {
	var out Template
	raw, _ := json.Marshal(struct {
		ID, Version uint64
		Withdraw    bool
	}{id, version, withdraw})
	err := s.actorTx(ctx, a, func(e database.Executor, a Actor) error {
		if !a.Has("change.template_approve") {
			return ErrForbidden
		}
		old, err := reserve(ctx, e, a, key, "change_template_publish", sha256.Sum256(raw))
		if err != nil {
			return err
		}
		if old > 0 {
			out, err = s.template(ctx, e, a, old)
			return err
		}
		t, err := s.template(ctx, e, a, id)
		if err != nil {
			return err
		}
		if version == 0 || t.Version != version {
			return ErrConflict
		}
		if !withdraw {
			if t.AuthorID == a.UserID {
				return fmt.Errorf("%w: szablon zatwierdza inna osoba niż autor ostatniej wersji", ErrForbidden)
			}
			if err = validateTemplateConfig(t.Draft); err != nil {
				return err
			}
			c, err := workflow.Published(ctx, e, a.TenantID, *t.Draft.Fields.WorkflowID, *t.Draft.Fields.WorkflowVersion)
			if err != nil {
				return err
			}
			if c.Domain != "CHANGE" || c.ChangeType != "STANDARD" {
				return ErrInvalid
			}
			if _, err = workflow.ActiveExecutor(ctx, e, a.TenantID, c); err != nil {
				return err
			}
			service := cmdb.NewService(s.db, nil, nil)
			for _, v := range t.Draft.CIIDs {
				n, _ := strconv.ParseUint(v, 10, 64)
				r, err := service.GetInTx(ctx, e, a, n, false)
				if err != nil {
					return err
				}
				if r.Archived {
					return ErrInvalid
				}
			}
			config, _ := json.Marshal(t.Draft)
			if _, err = e.ExecContext(ctx, "INSERT INTO change_template_versions(tenant_id,template_id,version,config,approved_by,approved_at) VALUES(?,?,?,?,?,?)", a.TenantID, id, version, config, a.UserID, time.Now().UTC()); err != nil {
				return err
			}
			if _, err = e.ExecContext(ctx, "UPDATE change_templates SET published_version=?,withdrawn=FALSE,version=version+1 WHERE tenant_id=? AND id=?", version, a.TenantID, id); err != nil {
				return err
			}
		} else {
			if _, err = e.ExecContext(ctx, "UPDATE change_templates SET withdrawn=TRUE,version=version+1 WHERE tenant_id=? AND id=?", a.TenantID, id); err != nil {
				return err
			}
		}
		if err = catalog.Audit(ctx, e, a, "change.template.approved", "change_template", id, map[string]any{"version": fmt.Sprint(version), "withdrawn": withdraw}); err != nil {
			return err
		}
		out, err = s.template(ctx, e, a, id)
		if err != nil {
			return err
		}
		return complete(ctx, e, a, key, id)
	})
	return out, err
}
func (s *Service) validateTemplate(ctx context.Context, e database.Executor, a Actor, f Fields, change uint64) error {
	if f.TemplateID == nil || f.TemplateVersion == nil {
		return ErrInvalid
	}
	c, err := publishedTemplate(ctx, e, a.TenantID, *f.TemplateID, *f.TemplateVersion)
	if err != nil {
		return err
	}
	level := map[string]int{"LOW": 1, "MEDIUM": 2, "HIGH": 3}
	expected := c.Fields
	if f.Type != "STANDARD" || level[f.Risk] > level[c.MaxRisk] || !optionalEqual(f.GroupID, expected.GroupID) || !optionalEqual(f.WorkflowID, expected.WorkflowID) || !optionalEqual(f.WorkflowVersion, expected.WorkflowVersion) || f.Scope != expected.Scope || f.Justification != expected.Justification || f.ImplementationPlan != expected.ImplementationPlan || f.TestPlan != expected.TestPlan || f.RollbackPlan != expected.RollbackPlan || f.Impact != expected.Impact {
		return fmt.Errorf("%w: zakres, plany, wpływ, grupa i proces muszą odpowiadać zatwierdzonemu szablonowi", ErrInvalid)
	}
	if f.PlannedStart != nil && f.PlannedEnd.Sub(*f.PlannedStart) > time.Duration(c.MaxDurationSeconds)*time.Second {
		return fmt.Errorf("%w: przekroczono czas szablonu", ErrInvalid)
	}
	if change > 0 {
		cis, err := s.ciSnapshots(ctx, e, a, change)
		if err != nil {
			return err
		}
		ids := []string{}
		for _, ci := range cis {
			ids = append(ids, fmt.Sprint(ci.ID))
		}
		// The approved set is exact, so Standard cannot silently omit an affected CI.
		set := func(v []string) map[string]bool {
			out := map[string]bool{}
			for _, id := range v {
				out[id] = true
			}
			return out
		}
		if !reflect.DeepEqual(set(ids), set(c.CIIDs)) {
			return fmt.Errorf("%w: zestaw CI musi odpowiadać zatwierdzonemu szablonowi", ErrInvalid)
		}
	}
	return nil
}
