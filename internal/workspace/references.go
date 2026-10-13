package workspace

import (
	"context"
	"errors"
	"fmt"
	"github.com/chmajster/OpenServiceNOW/internal/catalog"
	"github.com/chmajster/OpenServiceNOW/internal/changes"
	"github.com/chmajster/OpenServiceNOW/internal/cmdb"
	"github.com/chmajster/OpenServiceNOW/internal/incidents"
	"github.com/chmajster/OpenServiceNOW/internal/problems"
	"github.com/chmajster/OpenServiceNOW/internal/requests"
	"github.com/chmajster/OpenServiceNOW/internal/workflow"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
	"strconv"
	"strings"
)

var referenceKinds = []string{"incident", "request", "requested_item", "fulfillment_task", "problem", "problem_task", "change", "change_task", "ci", "catalog_item", "workflow_execution"}
var referenceRoots = map[string]string{"incidents": "incident", "requests": "request", "fulfillment": "fulfillment_task", "problems": "problem", "changes": "change", "cmdb": "ci", "catalog": "catalog_item", "approvals": "workflow_execution"}

func (s *Service) scopes(a workrecords.Principal) (map[string]workrecords.ReferenceScope, error) {
	out := map[string]workrecords.ReferenceScope{}
	add := func(v workrecords.ReferenceScope, err error) error {
		if errors.Is(err, workrecords.ErrForbidden) {
			return nil
		}
		if err != nil {
			return err
		}
		out[v.Kind] = v
		return nil
	}
	for _, fn := range []func() (workrecords.ReferenceScope, error){
		func() (workrecords.ReferenceScope, error) { return incidents.ReferenceScope(incidentActor(a)) },
		func() (workrecords.ReferenceScope, error) { return requests.ReferenceScope(a, false) },
		func() (workrecords.ReferenceScope, error) { return requests.ReferenceScope(a, true) },
		func() (workrecords.ReferenceScope, error) { return requests.RequestedItemReferenceScope(a) },
		func() (workrecords.ReferenceScope, error) { return problems.ReferenceScope(a, false) },
		func() (workrecords.ReferenceScope, error) { return problems.ReferenceScope(a, true) },
		func() (workrecords.ReferenceScope, error) { return changes.ReferenceScope(a, false) },
		func() (workrecords.ReferenceScope, error) { return changes.ReferenceScope(a, true) },
		func() (workrecords.ReferenceScope, error) { return cmdb.ReferenceScope(a) },
		func() (workrecords.ReferenceScope, error) { return catalog.ReferenceScope(a) },
		func() (workrecords.ReferenceScope, error) { return workflow.ReferenceScope(a) },
	} {
		if err := add(fn()); err != nil {
			return nil, err
		}
	}
	return out, nil
}

type MetadataItem struct {
	Key    string                `json:"key"`
	Record workrecords.Reference `json:"record"`
}
type Metadata struct {
	Items []MetadataItem `json:"items"`
}

func parseKeys(keys []string) (map[string][]uint64, error) {
	if len(keys) > 150 {
		return nil, fmt.Errorf("%w: limit wynosi 150 rekordów", ErrInvalid)
	}
	out := map[string][]uint64{}
	seen := map[string]bool{}
	bytes := 0
	for _, key := range keys {
		bytes += len(key)
		parts := strings.Split(key, "/")
		if bytes > 12288 || len(parts) != 3 || parts[0] != "" || referenceRoots[parts[1]] == "" || seen[key] {
			return nil, fmt.Errorf("%w: nieprawidłowy zestaw rekordów", ErrInvalid)
		}
		id, err := strconv.ParseUint(parts[2], 10, 64)
		if err != nil || id == 0 || fmt.Sprint(id) != parts[2] {
			return nil, fmt.Errorf("%w: nieprawidłowy identyfikator", ErrInvalid)
		}
		seen[key] = true
		kind := referenceRoots[parts[1]]
		out[kind] = append(out[kind], id)
	}
	return out, nil
}
func (s *Service) Metadata(ctx context.Context, a workrecords.Principal, keys []string) (Metadata, error) {
	out := Metadata{Items: []MetadataItem{}}
	ids, err := parseKeys(keys)
	if err != nil {
		return out, err
	}
	scopes, err := s.scopes(a)
	if err != nil {
		return out, err
	}
	for _, kind := range referenceKinds {
		values := ids[kind]
		scope, ok := scopes[kind]
		if len(values) == 0 || !ok {
			continue
		}
		rows, err := workrecords.References(ctx, s.DB, scope, workrecords.ReferenceSelection{IDs: values, Limit: 150})
		if err != nil {
			return out, err
		}
		for _, r := range rows {
			out.Items = append(out.Items, MetadataItem{Key: fmt.Sprintf("/%s/%d", scope.Root, r.ID), Record: r})
		}
	}
	return out, nil
}
