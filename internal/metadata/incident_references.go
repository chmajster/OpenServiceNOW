package metadata

import (
	"context"
	"fmt"

	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
)

func validateIncidentReferences(ctx context.Context, e database.Executor, tenant uint64, c Configuration) error {
	if c.IncidentProcess == nil {
		return nil
	}
	groups := map[string]bool{}
	services := map[string]bool{}
	if c.IncidentProcess.DefaultGroupID != "" {
		groups[c.IncidentProcess.DefaultGroupID] = true
	}
	for _, r := range c.IncidentProcess.AssignmentRules {
		if r.Active {
			groups[r.GroupID] = true
			if r.ServiceID != "" {
				services[r.ServiceID] = true
			}
		}
	}
	for _, t := range c.IncidentProcess.Templates {
		if !t.Retired && t.ServiceID != "" {
			services[t.ServiceID] = true
		}
	}
	for id := range groups {
		var active bool
		if err := e.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM `groups` WHERE tenant_id=? AND id=? AND active=TRUE)", tenant, id).Scan(&active); err != nil {
			return err
		}
		if !active {
			return fmt.Errorf("%w: incident queue must be active in this organization", ErrInvalid)
		}
	}
	for id := range services {
		var active bool
		if err := e.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM service_portfolio_items WHERE tenant_id=? AND id=? AND lifecycle='ACTIVE')`, tenant, id).Scan(&active); err != nil {
			return err
		}
		if !active {
			return fmt.Errorf("%w: incident service must be active in this organization", ErrInvalid)
		}
	}
	return nil
}
