package incidents

import (
	"context"
	"fmt"

	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
)

type MajorChildSummary struct {
	ID      uint64 `json:"id,string"`
	Number  string `json:"number"`
	Title   string `json:"title"`
	Version uint64 `json:"version,string"`
	State   string `json:"state"`
}

func (s *Service) MajorDecisionPage(ctx context.Context, a Actor, id, before uint64, limit int) ([]MajorDecision, uint64, error) {
	if !a.Has("major.read") && !a.Has("major.manage") {
		return nil, 0, ErrForbidden
	}
	if limit < 1 || limit > 100 {
		return nil, 0, ErrInvalid
	}
	if _, err := s.Get(ctx, a, id); err != nil {
		return nil, 0, err
	}
	root, _, err := s.majorRoot(ctx, s.repo.db, a.TenantID, id)
	if err != nil {
		return nil, 0, err
	}
	r, err := s.repo.Get(ctx, s.repo.db, a.TenantID, root, false)
	if err != nil {
		return nil, 0, err
	}
	if !readable(a, r) {
		return nil, 0, ErrNotFound
	}
	rows, err := s.repo.db.QueryContext(ctx, `SELECT d.id,d.case_version,d.decision_type,d.reason,d.actor_user_id,u.display_name,d.occurred_at FROM incident_major_decisions d JOIN users u ON u.id=d.actor_user_id WHERE d.tenant_id=? AND d.incident_id=? AND (?=0 OR d.id<?) ORDER BY d.id DESC LIMIT ?`, a.TenantID, root, before, before, limit+1)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := []MajorDecision{}
	for rows.Next() {
		var item MajorDecision
		if err = rows.Scan(&item.ID, &item.Version, &item.Type, &item.Reason, &item.ActorUserID, &item.ActorName, &item.OccurredAt); err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, 0, err
	}
	var next uint64
	if len(items) > limit {
		items = items[:limit]
		next = items[len(items)-1].ID
	}
	return items, next, nil
}

func (s *Service) MajorLinkedChildren(ctx context.Context, a Actor, id, after uint64, limit int) ([]MajorChildSummary, uint64, error) {
	if !a.Has("major.read") && !a.Has("major.manage") {
		return nil, 0, ErrForbidden
	}
	if limit < 1 || limit > 100 {
		return nil, 0, ErrInvalid
	}
	if _, err := s.Get(ctx, a, id); err != nil {
		return nil, 0, err
	}
	access, args := readPredicate(a, "i")
	params := []any{a.TenantID, fmt.Sprint(id), after}
	params = append(params, args...)
	params = append(params, limit+1)
	rows, err := s.repo.db.QueryContext(ctx, `SELECT i.id,i.number,i.title,i.version,i.state FROM work_relations r JOIN incidents i ON i.tenant_id=r.tenant_id AND i.id=CAST(r.source_id AS UNSIGNED) WHERE r.tenant_id=? AND r.source_type='incident' AND r.target_type='incident' AND r.relation_type='PARENT' AND r.target_id=? AND i.id>? AND `+access+` ORDER BY i.id LIMIT ?`, params...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := []MajorChildSummary{}
	for rows.Next() {
		var item MajorChildSummary
		if err = rows.Scan(&item.ID, &item.Number, &item.Title, &item.Version, &item.State); err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, 0, err
	}
	var next uint64
	if len(items) > limit {
		items = items[:limit]
		next = items[len(items)-1].ID
	}
	return items, next, nil
}

func filterMajorLinks(a Actor, links []MajorLink) []MajorLink {
	out := []MajorLink{}
	for _, link := range links {
		if link.Type == "CI" && !a.Permissions["cmdb.read"] && !a.Permissions["cmdb.manage"] {
			continue
		}
		if link.Type == "SERVICE" && !a.Permissions["service_portfolio.read"] && !a.Permissions["service_portfolio.manage"] {
			continue
		}
		out = append(out, link)
	}
	return out
}

// Feed reads authorize the current incident, parent and audience before SQL
// pagination. A stored public update never grants access to private coordination.
func (s *Service) MajorUpdatePage(ctx context.Context, a Actor, id, before uint64, limit int, private bool) ([]MajorUpdate, uint64, error) {
	if limit < 1 || limit > 100 {
		return nil, 0, ErrInvalid
	}
	if _, err := s.Get(ctx, a, id); err != nil {
		return nil, 0, err
	}
	root, major, err := s.majorRoot(ctx, s.repo.db, a.TenantID, id)
	if err != nil {
		return nil, 0, err
	}
	allowedPrivate := false
	if a.Has("major.read") || a.Has("major.manage") {
		r, e := s.repo.Get(ctx, s.repo.db, a.TenantID, root, false)
		if e != nil {
			return nil, 0, e
		}
		allowedPrivate = readable(a, r)
	}
	if private && !allowedPrivate {
		return nil, 0, ErrForbidden
	}
	if !allowedPrivate && !s.canReadMajorPublic(ctx, s.repo.db, a, root, id, major.CommunicationAudience) {
		return nil, 0, ErrNotFound
	}
	if !allowedPrivate && major.Status != "ACTIVE" && major.Status != "RESTORED" {
		return nil, 0, ErrNotFound
	}
	audience := "REQUESTERS"
	if private {
		audience = "OPERATORS"
	}
	items, err := readMajorUpdatePage(ctx, s.repo.db, a.TenantID, root, before, limit, audience)
	if err != nil {
		return nil, 0, err
	}
	var next uint64
	if len(items) > limit {
		items = items[:limit]
		next = items[len(items)-1].ID
	}
	if allowedPrivate {
		if err = refreshMajorDelivery(ctx, s.repo.db, a.TenantID, items); err != nil {
			return nil, 0, err
		}
	}
	if !allowedPrivate {
		for i := range items {
			items[i].ActorName = ""
			items[i].ActorUserID = 0
			items[i].Delivery = ""
		}
	}
	return items, next, nil
}

func readMajorUpdatePage(ctx context.Context, e database.Executor, tenant, root, before uint64, limit int, audience string) ([]MajorUpdate, error) {
	rows, err := e.QueryContext(ctx, `SELECT u.id,u.audience,u.body,u.actor_user_id,p.display_name,u.delivery_state,u.occurred_at FROM incident_major_updates u JOIN users p ON p.id=u.actor_user_id WHERE u.tenant_id=? AND u.incident_id=? AND u.audience=? AND (?=0 OR u.id<?) ORDER BY u.id DESC LIMIT ?`, tenant, root, audience, before, before, limit+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []MajorUpdate{}
	for rows.Next() {
		var item MajorUpdate
		if err = rows.Scan(&item.ID, &item.Audience, &item.Body, &item.ActorUserID, &item.ActorName, &item.Delivery, &item.OccurredAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
