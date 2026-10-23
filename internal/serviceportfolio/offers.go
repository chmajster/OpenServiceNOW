package serviceportfolio

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
)

const offerGroupsTable = "\x60groups\x60"

const offerColumns = `o.id,o.offer_key,o.parent_service_id,p.name,COALESCE(o.catalog_item_id,0),
 COALESCE(JSON_UNQUOTE(JSON_EXTRACT(v.definition,'$.title')),''),o.name,o.description,o.availability_conditions,o.support_model,
 o.obligation_summary,COALESCE(o.sla_definition_id,0),o.lifecycle,o.version`

const offerJoins = ` FROM service_portfolio_offers o JOIN service_portfolio_items p ON p.tenant_id=o.tenant_id AND p.id=o.parent_service_id
 LEFT JOIN catalog_items ci ON ci.tenant_id=o.tenant_id AND ci.id=o.catalog_item_id
 LEFT JOIN catalog_item_versions v ON v.tenant_id=ci.tenant_id AND v.item_id=ci.id AND v.version=ci.published_version`

func scanOffer(row interface{ Scan(...any) error }) (Offer, error) {
	var out Offer
	err := row.Scan(&out.ID, &out.OfferKey, &out.ParentServiceID, &out.ParentServiceName, &out.CatalogItemID, &out.CatalogItemTitle, &out.Name, &out.Description, &out.AvailabilityConditions, &out.SupportModel, &out.ObligationSummary, &out.SLADefinitionID, &out.Lifecycle, &out.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return Offer{}, ErrNotFound
	}
	return out, err
}

func (s *Service) GetOffer(ctx context.Context, a Actor, id uint64) (any, error) {
	if CanRead(a) {
		return s.getOfferAdmin(ctx, s.DB, a, id)
	}
	if !a.Has("service_portfolio.offer.read") && !a.Has("catalog.read") {
		return nil, workrecords.ErrForbidden
	}
	return s.getPublicOffer(ctx, s.DB, a, id)
}

func (s *Service) getOfferAdmin(ctx context.Context, exec database.Executor, a Actor, id uint64) (Offer, error) {
	access, args := portfolioAccess(a, "p")
	params := []any{a.TenantID, id}
	params = append(params, args...)
	offer, err := scanOffer(exec.QueryRowContext(ctx, "SELECT "+offerColumns+offerJoins+" WHERE o.tenant_id=? AND o.id=? AND "+access, params...))
	if err != nil {
		return offer, err
	}
	offer.AudienceGroupIDs, err = s.offerGroups(ctx, exec, a.TenantID, id)
	return offer, err
}

func (s *Service) offerGroups(ctx context.Context, exec database.Executor, tenant, id uint64) ([]string, error) {
	rows, err := exec.QueryContext(ctx, "SELECT group_id FROM service_portfolio_offer_audience_groups WHERE tenant_id=? AND offer_id=? ORDER BY group_id", tenant, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id uint64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, strconv.FormatUint(id, 10))
	}
	return ids, rows.Err()
}

func (s *Service) getPublicOffer(ctx context.Context, exec database.Executor, a Actor, id uint64) (PublicOffer, error) {
	var out PublicOffer
	err := exec.QueryRowContext(ctx, `SELECT o.id,o.offer_key,o.name,o.description,p.name,o.availability_conditions,o.support_model,o.obligation_summary,
 COALESCE(o.catalog_item_id,0),COALESCE(o.sla_definition_id,0),o.version
 FROM service_portfolio_offers o JOIN service_portfolio_items p ON p.tenant_id=o.tenant_id AND p.id=o.parent_service_id
 WHERE o.tenant_id=? AND o.id=? AND o.lifecycle='ACTIVE' AND p.lifecycle='ACTIVE'
 AND (NOT EXISTS(SELECT 1 FROM service_portfolio_offer_audience_groups ag WHERE ag.tenant_id=o.tenant_id AND ag.offer_id=o.id)
  OR EXISTS(SELECT 1 FROM service_portfolio_offer_audience_groups ag JOIN group_memberships gm ON gm.tenant_id=ag.tenant_id AND gm.group_id=ag.group_id
    JOIN `+offerGroupsTable+` g ON g.tenant_id=gm.tenant_id AND g.id=gm.group_id AND g.active=TRUE
    JOIN tenant_memberships tm ON tm.tenant_id=gm.tenant_id AND tm.user_id=gm.user_id AND tm.active=TRUE
    JOIN users u ON u.id=gm.user_id AND u.active=TRUE
    WHERE ag.tenant_id=o.tenant_id AND ag.offer_id=o.id AND gm.user_id=?))`, a.TenantID, id, a.UserID).Scan(&out.ID, &out.OfferKey, &out.Name, &out.Description, &out.ServiceName, &out.AvailabilityConditions, &out.SupportModel, &out.ObligationSummary, &out.CatalogItemID, &out.SLADefinitionID, &out.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return PublicOffer{}, ErrNotFound
	}
	return out, err
}

func (s *Service) Offers(ctx context.Context, a Actor, before uint64, limit int, queryText string) (any, uint64, error) {
	if limit < 1 || limit > 100 || len(queryText) > 160 {
		return nil, 0, ErrInvalid
	}
	queryText = strings.TrimSpace(queryText)
	if CanRead(a) {
		access, args := portfolioAccess(a, "p")
		params := []any{a.TenantID}
		params = append(params, args...)
		query := "SELECT " + offerColumns + offerJoins + " WHERE o.tenant_id=? AND " + access
		if queryText != "" {
			query += " AND (o.name LIKE ? OR o.offer_key LIKE ? OR p.name LIKE ?)"
			pattern := "%" + queryText + "%"
			params = append(params, pattern, pattern, pattern)
		}
		if before > 0 {
			query += " AND o.id<?"
			params = append(params, before)
		}
		query += " ORDER BY o.id DESC LIMIT ?"
		params = append(params, limit+1)
		rows, err := s.DB.QueryContext(ctx, query, params...)
		if err != nil {
			return nil, 0, err
		}
		defer rows.Close()
		items := []Offer{}
		for rows.Next() {
			item, err := scanOffer(rows)
			if err != nil {
				return nil, 0, err
			}
			item.AudienceGroupIDs, err = s.offerGroups(ctx, s.DB, a.TenantID, item.ID)
			if err != nil {
				return nil, 0, err
			}
			items = append(items, item)
		}
		if err := rows.Err(); err != nil {
			return nil, 0, err
		}
		var next uint64
		if len(items) > limit {
			items = items[:limit]
			next = items[len(items)-1].ID
		}
		return OfferPage{Items: items, Next: next}, next, nil
	}
	if !a.Has("service_portfolio.offer.read") && !a.Has("catalog.read") {
		return nil, 0, workrecords.ErrForbidden
	}
	query := `SELECT o.id,o.offer_key,o.name,o.description,p.name,o.availability_conditions,o.support_model,o.obligation_summary,COALESCE(o.catalog_item_id,0),COALESCE(o.sla_definition_id,0),o.version
 FROM service_portfolio_offers o JOIN service_portfolio_items p ON p.tenant_id=o.tenant_id AND p.id=o.parent_service_id
 WHERE o.tenant_id=? AND o.lifecycle='ACTIVE' AND p.lifecycle='ACTIVE'
 AND (NOT EXISTS(SELECT 1 FROM service_portfolio_offer_audience_groups ag WHERE ag.tenant_id=o.tenant_id AND ag.offer_id=o.id)
  OR EXISTS(SELECT 1 FROM service_portfolio_offer_audience_groups ag JOIN group_memberships gm ON gm.tenant_id=ag.tenant_id AND gm.group_id=ag.group_id
    JOIN ` + offerGroupsTable + ` g ON g.tenant_id=gm.tenant_id AND g.id=gm.group_id AND g.active=TRUE
    JOIN tenant_memberships tm ON tm.tenant_id=gm.tenant_id AND tm.user_id=gm.user_id AND tm.active=TRUE
    JOIN users u ON u.id=gm.user_id AND u.active=TRUE WHERE ag.tenant_id=o.tenant_id AND ag.offer_id=o.id AND gm.user_id=?))`
	params := []any{a.TenantID, a.UserID}
	if queryText != "" {
		query += " AND (o.name LIKE ? OR o.offer_key LIKE ? OR p.name LIKE ?)"
		pattern := "%" + queryText + "%"
		params = append(params, pattern, pattern, pattern)
	}
	if before > 0 {
		query += " AND o.id<?"
		params = append(params, before)
	}
	query += " ORDER BY o.id DESC LIMIT ?"
	params = append(params, limit+1)
	rows, err := s.DB.QueryContext(ctx, query, params...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := []PublicOffer{}
	for rows.Next() {
		var item PublicOffer
		if err := rows.Scan(&item.ID, &item.OfferKey, &item.Name, &item.Description, &item.ServiceName, &item.AvailabilityConditions, &item.SupportModel, &item.ObligationSummary, &item.CatalogItemID, &item.SLADefinitionID, &item.Version); err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	var next uint64
	if len(items) > limit {
		items = items[:limit]
		next = items[len(items)-1].ID
	}
	return PublicOfferPage{Items: items, Next: next}, next, nil
}

func (s *Service) SaveOffer(ctx context.Context, a Actor, id uint64, in OfferInput, idempotencyKey string) (Offer, error) {
	var out Offer
	if !CanManage(a) {
		return out, workrecords.ErrForbidden
	}
	var err error
	in, err = normalizeOffer(in)
	if err != nil {
		return out, err
	}
	var requestHash [32]byte
	if id == 0 {
		if len(idempotencyKey) < 8 || len(idempotencyKey) > 128 {
			return out, ErrInvalid
		}
		raw, marshalErr := json.Marshal(in)
		if marshalErr != nil {
			return out, marshalErr
		}
		requestHash = sha256.Sum256(raw)
	}
	err = s.actorTx(ctx, a, func(exec database.Executor, current Actor) error {
		if id == 0 {
			previous, reserveErr := workrecords.ReserveOperation(ctx, exec, current.Actor, idempotencyKey, "service_offer_create", requestHash)
			if reserveErr != nil {
				return reserveErr
			}
			if previous > 0 {
				out, err = s.getOfferAdmin(ctx, exec, current, previous)
				return err
			}
		}
		parent, err := s.getInTx(ctx, exec, current, in.ParentServiceID, true)
		if err != nil {
			return err
		}
		if parent.Type != "BUSINESS_SERVICE" || parent.Lifecycle == "RETIRED" {
			return fmt.Errorf("%w: oferta musi należeć do biznesowej usługi", ErrInvalid)
		}
		var before *Offer
		lifecycle := "DRAFT"
		if id > 0 {
			old, err := s.getOfferAdmin(ctx, exec, current, id)
			if err != nil {
				return err
			}
			if in.Version == 0 || old.Version != in.Version || old.OfferKey != in.OfferKey {
				return ErrConflict
			}
			before = &old
			lifecycle = old.Lifecycle
			if lifecycle == "WITHDRAWN" {
				return ErrConflict
			}
		}
		if lifecycle == "ACTIVE" {
			if err := s.validateOfferPublication(ctx, exec, current, in, id); err != nil {
				return err
			}
		}
		if err := s.validateOfferReferences(ctx, exec, current, in, id); err != nil {
			return err
		}
		if id == 0 {
			res, err := exec.ExecContext(ctx, `INSERT INTO service_portfolio_offers(tenant_id,offer_key,parent_service_id,catalog_item_id,name,description,availability_conditions,support_model,obligation_summary,sla_definition_id,created_by)
 VALUES(?,?,?,?,?,?,?,?,?,?,?)`, current.TenantID, in.OfferKey, in.ParentServiceID, nullableID(in.CatalogItemID), in.Name, in.Description, in.AvailabilityConditions, in.SupportModel, in.ObligationSummary, nullableID(in.SLADefinitionID), current.UserID)
			if err != nil {
				return missing(err)
			}
			n, err := res.LastInsertId()
			if err != nil {
				return err
			}
			id = uint64(n)
		} else {
			res, err := exec.ExecContext(ctx, `UPDATE service_portfolio_offers SET parent_service_id=?,catalog_item_id=?,name=?,description=?,availability_conditions=?,support_model=?,obligation_summary=?,sla_definition_id=?,version=version+1,updated_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND id=? AND version=?`, in.ParentServiceID, nullableID(in.CatalogItemID), in.Name, in.Description, in.AvailabilityConditions, in.SupportModel, in.ObligationSummary, nullableID(in.SLADefinitionID), current.TenantID, id, in.Version)
			if err != nil {
				return missing(err)
			}
			n, err := res.RowsAffected()
			if err != nil {
				return err
			}
			if n != 1 {
				return ErrConflict
			}
		}
		if _, err = exec.ExecContext(ctx, "DELETE FROM service_portfolio_offer_audience_groups WHERE tenant_id=? AND offer_id=?", current.TenantID, id); err != nil {
			return err
		}
		for _, raw := range in.AudienceGroupIDs {
			groupID, parseErr := strconv.ParseUint(raw, 10, 64)
			if parseErr != nil {
				return ErrInvalid
			}
			if _, err = exec.ExecContext(ctx, "INSERT INTO service_portfolio_offer_audience_groups(tenant_id,offer_id,group_id) VALUES(?,?,?)", current.TenantID, id, groupID); err != nil {
				return missing(err)
			}
		}
		out, err = s.getOfferAdmin(ctx, exec, current, id)
		if err != nil {
			return err
		}
		if err := s.changed(ctx, exec, current, "service_portfolio.offer.changed", "service_portfolio_offer", id, out.Version, before, out, []string{"data"}); err != nil {
			return err
		}
		if before == nil {
			return workrecords.CompleteOperation(ctx, exec, current.Actor, idempotencyKey, out.ID)
		}
		return nil
	})
	return out, err
}

func (s *Service) validateOfferReferences(ctx context.Context, exec database.Executor, a Actor, in OfferInput, id uint64) error {
	for _, raw := range in.AudienceGroupIDs {
		groupID, err := strconv.ParseUint(raw, 10, 64)
		if err != nil || groupID == 0 {
			return ErrInvalid
		}
		var found bool
		err = exec.QueryRowContext(ctx, "SELECT active FROM `groups` WHERE tenant_id=? AND id=?", a.TenantID, groupID).Scan(&found)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: grupa odbiorców nie należy do tej organizacji", ErrInvalid)
		}
		if err != nil {
			return err
		}
	}
	if in.CatalogItemID > 0 {
		var found bool
		err := exec.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM catalog_items i JOIN catalog_item_versions v ON v.tenant_id=i.tenant_id AND v.item_id=i.id AND v.version=i.published_version WHERE i.tenant_id=? AND i.id=?)`, a.TenantID, in.CatalogItemID).Scan(&found)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("%w: oferta musi wskazywać opublikowaną pozycję katalogu", ErrInvalid)
		}
		var mapped bool
		err = exec.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM service_portfolio_offers WHERE tenant_id=? AND catalog_item_id=? AND id<>?)", a.TenantID, in.CatalogItemID, id).Scan(&mapped)
		if err != nil {
			return err
		}
		if mapped {
			return fmt.Errorf("%w: pozycja katalogu jest już powiązana z inną ofertą", ErrConflict)
		}
	}
	if in.SLADefinitionID > 0 {
		var found bool
		err := exec.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM sla_definitions WHERE tenant_id=? AND id=? AND kind='policy')", a.TenantID, in.SLADefinitionID).Scan(&found)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("%w: wskazana konfiguracja SLA nie istnieje w tej organizacji", ErrInvalid)
		}
	}
	return nil
}

func (s *Service) validateOfferPublication(ctx context.Context, exec database.Executor, a Actor, in OfferInput, id uint64) error {
	parent, err := s.getInTx(ctx, exec, a, in.ParentServiceID, false)
	if err != nil {
		return err
	}
	if parent.Lifecycle != "ACTIVE" {
		return fmt.Errorf("%w: najpierw aktywuj usługę nadrzędną", ErrInvalid)
	}
	if in.AvailabilityConditions == "" || in.ObligationSummary == "" {
		return fmt.Errorf("%w: publikacja oferty wymaga warunków dostępności i zobowiązań usługowych", ErrInvalid)
	}
	for _, raw := range in.AudienceGroupIDs {
		var active bool
		groupID, _ := strconv.ParseUint(raw, 10, 64)
		if err = exec.QueryRowContext(ctx, "SELECT active FROM `groups` WHERE tenant_id=? AND id=?", a.TenantID, groupID).Scan(&active); err != nil || !active {
			return fmt.Errorf("%w: wszystkie grupy odbiorców muszą być aktywne", ErrInvalid)
		}
	}
	return nil
}

func (s *Service) TransitionOffer(ctx context.Context, a Actor, id, version uint64, action string) (Offer, error) {
	var out Offer
	if !CanManage(a) || version == 0 {
		return out, workrecords.ErrForbidden
	}
	err := s.actorTx(ctx, a, func(exec database.Executor, current Actor) error {
		before, err := s.getOfferAdmin(ctx, exec, current, id)
		if err != nil {
			return err
		}
		if before.Version != version {
			return ErrConflict
		}
		parent, err := s.getInTx(ctx, exec, current, before.ParentServiceID, true)
		if err != nil {
			return err
		}
		var next string
		switch action {
		case "publish":
			if before.Lifecycle != "DRAFT" {
				return ErrConflict
			}
			input := offerInputFrom(before)
			if err = s.validateOfferReferences(ctx, exec, current, input, id); err != nil {
				return err
			}
			if err = s.validateOfferPublication(ctx, exec, current, input, id); err != nil {
				return err
			}
			next = "ACTIVE"
		case "withdraw":
			if before.Lifecycle != "ACTIVE" {
				return ErrConflict
			}
			next = "WITHDRAWN"
		default:
			return ErrInvalid
		}
		if parent.Lifecycle != "ACTIVE" && next == "ACTIVE" {
			return fmt.Errorf("%w: usługa nadrzędna nie jest aktywna", ErrInvalid)
		}
		res, err := exec.ExecContext(ctx, "UPDATE service_portfolio_offers SET lifecycle=?,version=version+1,updated_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND id=? AND version=?", next, current.TenantID, id, version)
		if err != nil {
			return err
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if affected != 1 {
			return ErrConflict
		}
		out, err = s.getOfferAdmin(ctx, exec, current, id)
		if err != nil {
			return err
		}
		return s.changed(ctx, exec, current, "service_portfolio.offer.changed", "service_portfolio_offer", id, out.Version, before, out, []string{"lifecycle"})
	})
	return out, err
}

func offerInputFrom(o Offer) OfferInput {
	return OfferInput{Version: o.Version, OfferKey: o.OfferKey, ParentServiceID: o.ParentServiceID, CatalogItemID: o.CatalogItemID, Name: o.Name, Description: o.Description, AvailabilityConditions: o.AvailabilityConditions, SupportModel: o.SupportModel, ObligationSummary: o.ObligationSummary, SLADefinitionID: o.SLADefinitionID, AudienceGroupIDs: o.AudienceGroupIDs}
}

func (s *Service) withdrawOffersInTx(ctx context.Context, exec database.Executor, a Actor, parentID uint64) error {
	rows, err := exec.QueryContext(ctx, "SELECT id FROM service_portfolio_offers WHERE tenant_id=? AND parent_service_id=? AND lifecycle='ACTIVE' ORDER BY id FOR UPDATE", a.TenantID, parentID)
	if err != nil {
		return err
	}
	ids := []uint64{}
	for rows.Next() {
		var id uint64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range ids {
		before, err := s.getOfferAdmin(ctx, exec, a, id)
		if err != nil {
			return err
		}
		result, err := exec.ExecContext(ctx, "UPDATE service_portfolio_offers SET lifecycle='WITHDRAWN',version=version+1,updated_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND id=? AND version=?", a.TenantID, id, before.Version)
		if err != nil {
			return err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if affected != 1 {
			return ErrConflict
		}
		after, err := s.getOfferAdmin(ctx, exec, a, id)
		if err != nil {
			return err
		}
		if err = s.changed(ctx, exec, a, "service_portfolio.offer.changed", "service_portfolio_offer", id, after.Version, before, after, []string{"lifecycle", "parent_retired"}); err != nil {
			return err
		}
	}
	return nil
}

func parsePublicOfferID(value string) (uint64, error) {
	id, err := strconv.ParseUint(value, 10, 64)
	if err != nil || id == 0 || strconv.FormatUint(id, 10) != value {
		return 0, ErrInvalid
	}
	return id, nil
}
