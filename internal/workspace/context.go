package workspace

import (
	"context"
	"fmt"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
)

type ContextSection struct {
	Key        string                  `json:"key"`
	Label      string                  `json:"label"`
	Definition string                  `json:"definition"`
	Items      []workrecords.Reference `json:"items"`
	MoreURL    string                  `json:"more_url"`
	HasMore    bool                    `json:"has_more"`
}
type RecordContext struct {
	Record   workrecords.Reference `json:"record"`
	Sections []ContextSection      `json:"sections"`
}

func (s *Service) Context(ctx context.Context, a workrecords.Principal, root string, id uint64) (RecordContext, error) {
	out := RecordContext{Sections: []ContextSection{}}
	kind := referenceRoots[root]
	if kind == "" || id == 0 {
		return out, ErrInvalid
	}
	scopes, err := s.scopes(a)
	if err != nil {
		return out, err
	}
	source, ok := scopes[kind]
	if !ok {
		return out, workrecords.ErrNotFound
	}
	records, err := workrecords.References(ctx, s.DB, source, workrecords.ReferenceSelection{IDs: []uint64{id}, Limit: 1})
	if err != nil {
		return out, err
	}
	if len(records) == 0 {
		return out, workrecords.ErrNotFound
	}
	out.Record = records[0]
	add := func(key, label, definition, more string, scope workrecords.ReferenceScope) error {
		items, err := workrecords.References(ctx, s.DB, scope, workrecords.ReferenceSelection{Limit: 11})
		if err != nil {
			return err
		}
		section := ContextSection{Key: key, Label: label, Definition: definition, Items: items, MoreURL: more}
		if len(items) > 10 {
			section.Items = items[:10]
			section.HasMore = true
		}
		out.Sections = append(out.Sections, section)
		return nil
	}
	more := fmt.Sprintf("/%s/%d?tab=related", root, id)
	links := map[string]struct{ table, column string }{"incident": {"cmdb_incident_links", "incident_id"}, "request": {"cmdb_request_links", "request_id"}, "problem": {"cmdb_problem_links", "problem_id"}, "change": {"cmdb_change_links", "change_id"}}
	if link, ok := links[kind]; ok {
		if ci, allowed := scopes["ci"]; allowed {
			ci = ci.Restricted("EXISTS(SELECT 1 FROM "+link.table+" l WHERE l.tenant_id=c.tenant_id AND l.ci_id=c.id AND l."+link.column+"=?)", id)
			if err = add("cis", "CI i usługi biznesowe", "Bezpośrednie powiązania CMDB. Każdy element podlega własnej kontroli dostępu.", more, ci); err != nil {
				return out, err
			}
		}
	}
	if kind == "ci" {
		more = fmt.Sprintf("/cmdb/%d?tab=records", id)
		for _, target := range []string{"incident", "request", "problem", "change"} {
			scope, ok := scopes[target]
			if !ok {
				continue
			}
			link := links[target]
			scope = scope.Restricted("EXISTS(SELECT 1 FROM "+link.table+" l WHERE l.tenant_id=? AND l."+link.column+"="+scope.ID+" AND l.ci_id=?)", a.TenantID, id)
			if err = add(target, domainLabel(target), "Sprawy bezpośrednio powiązane z CI, ograniczone uprawnieniami swojej domeny.", more, scope); err != nil {
				return out, err
			}
		}
		if ci, ok := scopes["ci"]; ok {
			ci = ci.Restricted("EXISTS(SELECT 1 FROM cmdb_relations l WHERE l.tenant_id=c.tenant_id AND ((l.source_id=? AND l.target_id=c.id) OR (l.target_id=? AND l.source_id=c.id)))", id, id)
			if err = add("dependencies", "Zależności CI i usług", "Uprawnione końce relacji. Typ i kierunek relacji są dostępne na karcie CMDB.", fmt.Sprintf("/cmdb/%d?tab=relations", id), ci); err != nil {
				return out, err
			}
		}
	}
	if kind == "incident" {
		if scope, ok := scopes["problem"]; ok {
			scope = scope.Restricted("EXISTS(SELECT 1 FROM problem_incident_links l WHERE l.tenant_id=p.tenant_id AND l.problem_id=p.id AND l.incident_id=?)", id)
			if err = add("problems", "Problemy", "Problemy jawnie powiązane z incydentem.", more, scope); err != nil {
				return out, err
			}
		}
	}
	if kind == "problem" {
		if scope, ok := scopes["incident"]; ok {
			scope = scope.Restricted("EXISTS(SELECT 1 FROM problem_incident_links l WHERE l.tenant_id=i.tenant_id AND l.incident_id=i.id AND l.problem_id=?)", id)
			if err = add("incidents", "Incydenty", "Powiązane incydenty. Stan problemu nie zmienia ich stanu.", more, scope); err != nil {
				return out, err
			}
		}
		if scope, ok := scopes["change"]; ok {
			scope = scope.Restricted("EXISTS(SELECT 1 FROM change_problem_links l WHERE l.tenant_id=p.tenant_id AND l.change_id=p.id AND l.problem_id=?)", id)
			if err = add("changes", "Zmiany naprawcze", "Zmiany jawnie powiązane z problemem.", more, scope); err != nil {
				return out, err
			}
		}
	}
	if kind == "change" {
		if scope, ok := scopes["problem"]; ok {
			scope = scope.Restricted("EXISTS(SELECT 1 FROM change_problem_links l WHERE l.tenant_id=p.tenant_id AND l.problem_id=p.id AND l.change_id=?)", id)
			if err = add("problems", "Problemy", "Problemy jawnie powiązane ze zmianą.", more, scope); err != nil {
				return out, err
			}
		}
	}
	if kind == "problem" || kind == "change" {
		if scope, ok := scopes[kind+"_task"]; ok {
			scope = scope.Restricted("p.id=?", id)
			if err = add("tasks", "Zadania", "Zadania tego rekordu, z dostępem sprawdzanym na rekordzie nadrzędnym.", fmt.Sprintf("/%s/%d?tab=tasks", root, id), scope); err != nil {
				return out, err
			}
		}
	}
	if kind == "request" {
		if itemScope, ok := scopes["requested_item"]; ok {
			itemScope = itemScope.Restricted("r.id=?", id)
			if err = add("requested_items", "Zamówione pozycje", "Publiczne nazwy i numery zamówionych pozycji RITM, podlegające dostępowi do wniosku.", fmt.Sprintf("/requests/%d?tab=info", id), itemScope); err != nil {
				return out, err
			}
		}
		if scope, ok := scopes["fulfillment_task"]; ok {
			scope = scope.Restricted("r.id=?", id)
			if err = add("tasks", "Zadania realizacji", "Wyłącznie zadania dostępne w aktywnych grupach operatora albo przez uprawnienie zarządzania.", fmt.Sprintf("/requests/%d?tab=info", id), scope); err != nil {
				return out, err
			}
		}
	}
	if kind == "change" || kind == "request" {
		scope := scopes["workflow_execution"]
		column := "x.change_id"
		if kind == "request" {
			column = "x.request_id"
		}
		scope = scope.Restricted(column+"=?", id)
		if kind == "request" {
			more = fmt.Sprintf("/requests/%d", id)
		} else {
			more = fmt.Sprintf("/changes/%d?tab=approvals", id)
		}
		if err = add("approvals", "Procesy i akceptacje", "Istniejące wykonania workflow z niezależną kontrolą dostępu.", more, scope); err != nil {
			return out, err
		}
	}
	return out, nil
}
func domainLabel(kind string) string {
	return map[string]string{"incident": "Incydenty", "request": "Wnioski", "problem": "Problemy", "change": "Zmiany", "ci": "Elementy konfiguracji", "catalog_item": "Pozycje katalogu zamówień", "fulfillment_task": "Zadania realizacji", "problem_task": "Zadania analityczne", "change_task": "Zadania wykonawcze", "workflow_execution": "Procesy i akceptacje"}[kind]
}
