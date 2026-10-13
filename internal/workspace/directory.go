package workspace

import (
	"context"
	"database/sql"
	"errors"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
)

type DirectoryReference struct {
	ID               uint64 `json:"id,string"`
	Name             string `json:"name"`
	Kind             string `json:"kind"`
	Active           bool   `json:"active"`
	MembershipActive bool   `json:"membership_active"`
}

func (s *Service) DirectoryReference(ctx context.Context, a workrecords.Principal, kind string, id uint64) (DirectoryReference, error) {
	out := DirectoryReference{Kind: kind}
	if id == 0 {
		return out, ErrInvalid
	}
	query := ""
	switch kind {
	case "users":
		if !a.Has("admin.users.manage") {
			return out, workrecords.ErrForbidden
		}
		query = "SELECT u.id,u.display_name,u.active,tm.active FROM users u JOIN tenant_memberships tm ON tm.user_id=u.id WHERE tm.tenant_id=? AND u.id=?"
	case "groups":
		if !a.Has("admin.rbac.manage") {
			return out, workrecords.ErrForbidden
		}
		query = "SELECT id,name,active,TRUE FROM `groups` WHERE tenant_id=? AND id=?"
	default:
		return out, ErrInvalid
	}
	err := s.DB.QueryRowContext(ctx, query, a.TenantID, id).Scan(&out.ID, &out.Name, &out.Active, &out.MembershipActive)
	if errors.Is(err, sql.ErrNoRows) {
		err = workrecords.ErrNotFound
	}
	return out, err
}
