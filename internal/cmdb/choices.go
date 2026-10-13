package cmdb

import (
	"context"
	"fmt"
	"github.com/chmajster/OpenServiceNOW/internal/assets"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
)

func (h *Handler) groups(ctx context.Context, a Actor, q string, before uint64, limit int) (any, error) {
	if !a.Has("cmdb.read") && !a.Has("cmdb.read_own") {
		return nil, workrecords.ErrForbidden
	}
	if !assets.Text(q, 0, 160) {
		return nil, ErrInvalid
	}
	sql := "SELECT g.id,g.name FROM `groups` g WHERE g.tenant_id=? AND g.active=TRUE"
	args := []any{a.TenantID}
	if !a.Has("cmdb.read") {
		sql += " AND EXISTS(SELECT 1 FROM group_memberships gm WHERE gm.tenant_id=g.tenant_id AND gm.group_id=g.id AND gm.user_id=?)"
		args = append(args, a.UserID)
	}
	if q != "" {
		sql += " AND LOCATE(?,g.name)>0"
		args = append(args, q)
	}
	if before > 0 {
		sql += " AND g.id<?"
		args = append(args, before)
	}
	sql += " ORDER BY g.id DESC LIMIT ?"
	args = append(args, limit+1)
	rows, err := h.service.DB.QueryContext(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type Choice struct {
		ID   uint64 `json:"id,string"`
		Name string `json:"name"`
	}
	items := []Choice{}
	for rows.Next() {
		var c Choice
		if err = rows.Scan(&c.ID, &c.Name); err != nil {
			return nil, err
		}
		items = append(items, c)
	}
	next := ""
	if len(items) > limit {
		items = items[:limit]
		next = fmt.Sprint(items[limit-1].ID)
	}
	page := map[string]any{"items": items}
	if next != "" {
		page["next_before_id"] = next
	}
	return page, rows.Err()
}
