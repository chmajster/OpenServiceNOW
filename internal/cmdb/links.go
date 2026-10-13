package cmdb

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/chmajster/OpenServiceNOW/internal/assets"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
)

type AssetLink struct {
	Asset *assets.Record `json:"asset,omitempty"`
}
type CILink struct {
	CI *Record `json:"ci,omitempty"`
}
type AssetLinkInput struct {
	Version      uint64 `json:"version,string"`
	AssetID      uint64 `json:"asset_id,string"`
	AssetVersion uint64 `json:"asset_version,string"`
}

func (s *Service) AssetLink(ctx context.Context, a Actor, id uint64) (AssetLink, error) {
	if _, err := s.Get(ctx, a, id); err != nil {
		return AssetLink{}, err
	}
	var aid uint64
	err := s.DB.QueryRowContext(ctx, "SELECT asset_id FROM cmdb_ci_assets WHERE tenant_id=? AND ci_id=?", a.TenantID, id).Scan(&aid)
	if errors.Is(err, sql.ErrNoRows) {
		return AssetLink{}, nil
	}
	if err != nil {
		return AssetLink{}, err
	}
	r, err := s.Assets.Get(ctx, a, aid)
	if errors.Is(err, ErrNotFound) {
		return AssetLink{}, nil
	}
	if err != nil {
		return AssetLink{}, err
	}
	return AssetLink{&r}, nil
}
func (s *Service) AssetCI(ctx context.Context, a Actor, id uint64) (CILink, error) {
	if _, err := s.Assets.Get(ctx, a, id); err != nil {
		return CILink{}, err
	}
	var cid uint64
	err := s.DB.QueryRowContext(ctx, "SELECT ci_id FROM cmdb_ci_assets WHERE tenant_id=? AND asset_id=?", a.TenantID, id).Scan(&cid)
	if errors.Is(err, sql.ErrNoRows) {
		return CILink{}, nil
	}
	if err != nil {
		return CILink{}, err
	}
	r, err := s.Get(ctx, a, cid)
	if errors.Is(err, ErrNotFound) {
		return CILink{}, nil
	}
	if err != nil {
		return CILink{}, err
	}
	return CILink{&r}, nil
}
func (s *Service) SetAsset(ctx context.Context, a Actor, id uint64, in AssetLinkInput) (Record, error) {
	var out Record
	if in.Version == 0 || in.AssetVersion == 0 {
		return out, ErrInvalid
	}
	err := s.ActorTx(ctx, a, func(e database.Executor, a Actor) error {
		if !Writable(a) || !assets.Writable(a) {
			return workrecords.ErrForbidden
		}
		ci, err := s.GetInTx(ctx, e, a, id, true)
		if err != nil {
			return err
		}
		if ci.Version != in.Version || ci.Archived {
			return ErrConflict
		}
		var old uint64
		err = e.QueryRowContext(ctx, "SELECT asset_id FROM cmdb_ci_assets WHERE tenant_id=? AND ci_id=?", a.TenantID, id).Scan(&old)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		aid := in.AssetID
		if aid == 0 {
			aid = old
		}
		if aid == 0 {
			return ErrConflict
		}
		r, err := s.Assets.GetInTx(ctx, e, a, aid, true)
		if err != nil {
			return err
		}
		if r.Version != in.AssetVersion || r.Archived {
			return ErrConflict
		}
		if in.AssetID > 0 && old > 0 {
			if old != in.AssetID {
				return fmt.Errorf("%w: najpierw odłącz dotychczasowy Asset", ErrConflict)
			}
			out = ci
			return nil
		}
		if in.AssetID == 0 {
			_, err = e.ExecContext(ctx, "DELETE FROM cmdb_ci_assets WHERE tenant_id=? AND ci_id=? AND asset_id=?", a.TenantID, id, aid)
		} else {
			_, err = e.ExecContext(ctx, "INSERT INTO cmdb_ci_assets(tenant_id,ci_id,asset_id) VALUES(?,?,?)", a.TenantID, id, aid)
		}
		if err != nil {
			return assets.Missing(err)
		}
		if _, err = s.Assets.TouchInTx(ctx, e, a, r, "ci_link"); err != nil {
			return err
		}
		out, err = s.touch(ctx, e, a, ci, "asset_link")
		return err
	})
	return out, err
}
func (s *Service) domain(kind string) (RecordDomain, string, string, error) {
	switch kind {
	case "incidents":
		if d := s.Domains[kind]; d != nil {
			return d, "cmdb_incident_links", "incident_id", nil
		}
	case "problems":
		if d := s.Domains[kind]; d != nil {
			return d, "cmdb_problem_links", "problem_id", nil
		}
	case "requests":
		if d := s.Domains[kind]; d != nil {
			return d, "cmdb_request_links", "request_id", nil
		}
	}
	return nil, "", "", ErrInvalid
}

type RecordCIs struct {
	Record Reference `json:"record"`
	Items  []Record  `json:"items"`
	Next   uint64    `json:"next_before_id,string,omitempty"`
}
type RecordLinkInput struct {
	Version   uint64 `json:"version,string"`
	CIVersion uint64 `json:"ci_version,string"`
}

func (s *Service) RecordCIs(ctx context.Context, a Actor, kind string, id, before uint64, limit int) (RecordCIs, error) {
	out := RecordCIs{Items: []Record{}}
	if limit < 1 || limit > 100 {
		return out, ErrInvalid
	}
	d, table, column, err := s.domain(kind)
	if err != nil {
		return out, err
	}
	out.Record, err = d.ReferenceInTx(ctx, s.DB, a, id, false, 0)
	if err != nil {
		return out, err
	}
	if !a.Has("cmdb.read") && !a.Has("cmdb.read_own") {
		return out, workrecords.ErrForbidden
	}
	p, args := Access(a, "c")
	params := []any{a.TenantID, id}
	params = append(params, args...)
	q := "SELECT " + columns + " FROM cmdb_cis c JOIN users u ON u.id=c.owner_id LEFT JOIN `groups` g ON g.tenant_id=c.tenant_id AND g.id=c.support_group_id JOIN " + table + " l ON l.tenant_id=c.tenant_id AND l.ci_id=c.id WHERE l.tenant_id=? AND l." + column + "=? AND " + p
	if before > 0 {
		q += " AND c.id<?"
		params = append(params, before)
	}
	q += " ORDER BY c.id DESC LIMIT ?"
	params = append(params, limit+1)
	rows, err := s.DB.QueryContext(ctx, q, params...)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		r, e := scan(rows)
		if e != nil {
			return out, e
		}
		out.Items = append(out.Items, r)
	}
	if len(out.Items) > limit {
		out.Items = out.Items[:limit]
		out.Next = out.Items[limit-1].ID
	}
	return out, rows.Err()
}
func (s *Service) Records(ctx context.Context, a Actor, kind, q string, ci, before uint64, limit int) (ReferencePage, error) {
	d, _, _, err := s.domain(kind)
	if err != nil {
		return ReferencePage{}, err
	}
	if ci > 0 {
		if _, err = s.Get(ctx, a, ci); err != nil {
			return ReferencePage{}, err
		}
	}
	return d.References(ctx, a, q, ci, before, limit)
}
func (s *Service) ChangeRecordLink(ctx context.Context, a Actor, kind string, id, ciID uint64, in RecordLinkInput, remove bool, keys ...string) (Reference, error) {
	var out Reference
	if in.Version == 0 || in.CIVersion == 0 {
		return out, ErrInvalid
	}
	d, table, column, err := s.domain(kind)
	if err != nil {
		return out, err
	}
	err = s.ActorTx(ctx, a, func(e database.Executor, a Actor) error {
		if !a.Has("cmdb.link") {
			return workrecords.ErrForbidden
		}
		key := ""
		if len(keys) > 0 {
			key = keys[0]
		}
		if key != "" {
			raw, _ := json.Marshal(struct {
				Kind   string
				ID, CI uint64
				Input  RecordLinkInput
				Remove bool
			}{kind, id, ciID, in, remove})
			old, err := workrecords.ReserveOperation(ctx, e, a.Actor, key, "ci_record_link", sha256.Sum256(raw))
			if err != nil {
				return err
			}
			if old > 0 {
				if _, err := s.GetInTx(ctx, e, a, ciID, false); err != nil {
					return err
				}
				out, err = d.ReferenceInTx(ctx, e, a, id, false, 0)
				return err
			}
		}
		ci, err := s.GetInTx(ctx, e, a, ciID, true)
		if err != nil {
			return err
		}
		if ci.Version != in.CIVersion {
			return ErrConflict
		}
		if ci.Archived && !remove {
			return fmt.Errorf("%w: nie można dodawać archiwalnego CI", ErrConflict)
		}
		var exists bool
		if err = e.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM "+table+" WHERE tenant_id=? AND ci_id=? AND "+column+"=?)", a.TenantID, ciID, id).Scan(&exists); err != nil {
			return err
		}
		if exists == !remove {
			out, err = d.ReferenceInTx(ctx, e, a, id, false, 0)
			if err == nil && (!out.CanLink || out.Version != in.Version) {
				return ErrConflict
			}
			if err == nil && key != "" {
				err = workrecords.CompleteOperation(ctx, e, a.Actor, key, id)
			}
			return err
		}
		out, err = d.ReferenceInTx(ctx, e, a, id, true, in.Version)
		if err != nil {
			return err
		}
		if !remove {
			var count int
			if err = e.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table+" WHERE tenant_id=? AND "+column+"=?", a.TenantID, id).Scan(&count); err != nil {
				return err
			}
			if count >= 20 {
				return fmt.Errorf("%w: limit 20 CI na zgłoszenie", ErrInvalid)
			}
			_, err = e.ExecContext(ctx, "INSERT INTO "+table+"(tenant_id,ci_id,"+column+") VALUES(?,?,?)", a.TenantID, ciID, id)
		} else {
			_, err = e.ExecContext(ctx, "DELETE FROM "+table+" WHERE tenant_id=? AND ci_id=? AND "+column+"=?", a.TenantID, ciID, id)
		}
		if err != nil {
			return assets.Missing(err)
		}
		_, err = s.touch(ctx, e, a, ci, "record_links")
		if err == nil && key != "" {
			err = workrecords.CompleteOperation(ctx, e, a.Actor, key, id)
		}
		return err
	})
	return out, err
}
