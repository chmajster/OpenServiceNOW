package incidents

import (
	"context"
	"fmt"

	"github.com/chmajster/OpenServiceNOW/internal/identity"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
)

func (s *Service) validateActionOwner(ctx context.Context, e database.Executor, tenant, incident uint64, owner *uint64, coordinationRole bool) error {
	if owner == nil {
		return nil
	}
	if err := validateMajorPeople(ctx, e, tenant, owner); err != nil {
		return err
	}
	keys, err := identity.NewMySQLRepository(nil).ListPermissionsInTx(ctx, e, tenant, *owner)
	if err != nil {
		return err
	}
	a := Actor{TenantID: tenant, UserID: *owner, Permissions: map[string]bool{}}
	for _, key := range keys {
		a.Permissions[key] = true
	}
	r, err := s.repo.Get(ctx, e, tenant, incident, false)
	if err != nil {
		return err
	}
	if !readable(a, r) || coordinationRole && !a.Has("major.read") && !a.Has("major.manage") {
		return fmt.Errorf("%w: selected owner needs current access to the incident and assigned coordination role", ErrInvalid)
	}
	return nil
}
