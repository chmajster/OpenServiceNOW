package workrecords

import (
	"context"
	"database/sql"
	"errors"
	"github.com/chmajster/OpenServiceNOW/internal/identity"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
)

// Principal is the authenticated organization context, never supplied by JSON.
type Principal struct {
	Actor
	IPAddress   string
	Permissions map[string]bool
}

var ErrForbidden = errors.New("brak uprawnień")

func (a Principal) Has(key string) bool { return a.Permissions[key] }

// LockPrincipal serializes writes with membership/RBAC administration and reads
// current permissions inside the same transaction as the business operation.
func LockPrincipal(ctx context.Context, e database.Executor, a Principal) (Principal, error) {
	if a.TenantID == 0 || a.UserID == 0 {
		return a, ErrForbidden
	}
	var status string
	if err := e.QueryRowContext(ctx, "SELECT status FROM tenants WHERE id=? FOR UPDATE", a.TenantID).Scan(&status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return a, ErrForbidden
		}
		return a, err
	}
	if status != "ACTIVE" {
		return a, ErrForbidden
	}
	if err := ValidateActiveUser(ctx, e, a.TenantID, a.UserID); err != nil {
		if errors.Is(err, ErrNotFound) {
			return a, ErrForbidden
		}
		return a, err
	}
	keys, err := identity.NewMySQLRepository(nil).ListPermissionsInTx(ctx, e, a.TenantID, a.UserID)
	if err != nil {
		return a, err
	}
	a.Permissions = map[string]bool{}
	for _, key := range keys {
		a.Permissions[key] = true
	}
	return a, nil
}
