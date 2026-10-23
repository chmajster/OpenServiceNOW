package assets

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/workrecords"
)

type CatalogKind string

const (
	CatalogManufacturer CatalogKind = "MANUFACTURER"
	CatalogModel        CatalogKind = "MODEL"
	CatalogLocation     CatalogKind = "LOCATION"
)

type CatalogValue struct {
	ID           uint64      `json:"id,string"`
	Kind         CatalogKind `json:"kind"`
	Value        string      `json:"value"`
	Manufacturer string      `json:"manufacturer,omitempty"`
}

type CatalogPage struct {
	Items        []CatalogValue `json:"items"`
	NextBeforeID uint64         `json:"next_before_id,string,omitempty"`
}

type CatalogFilter struct {
	Kind         CatalogKind
	Query        string
	Manufacturer string
	Before       uint64
	Limit        int
}

func CatalogKindFromPath(value string) (CatalogKind, bool) {
	switch value {
	case "manufacturers":
		return CatalogManufacturer, true
	case "models":
		return CatalogModel, true
	case "locations":
		return CatalogLocation, true
	default:
		return "", false
	}
}

func (s *Service) ListCatalog(ctx context.Context, a Actor, f CatalogFilter) (CatalogPage, error) {
	page := CatalogPage{Items: []CatalogValue{}}
	if f.Limit < 1 || f.Limit > 100 || !validCatalogKind(f.Kind) || !Text(f.Query, 0, 160) || !Text(f.Manufacturer, 0, 160) || f.Kind != CatalogModel && f.Manufacturer != "" {
		return page, ErrInvalid
	}
	if !a.Has("assets.read") {
		return page, workrecords.ErrForbidden
	}
	query := `SELECT id,kind,value,manufacturer FROM asset_catalog_values
		WHERE tenant_id=? AND kind=? AND active=TRUE AND LOCATE(?,value)>0
			AND (kind<>'MODEL' OR ?='' OR manufacturer='' OR manufacturer=?)`
	args := []any{a.TenantID, f.Kind, f.Query, f.Manufacturer, f.Manufacturer}
	if f.Before > 0 {
		query += " AND id<?"
		args = append(args, f.Before)
	}
	query += " ORDER BY id DESC LIMIT ?"
	args = append(args, f.Limit+1)
	rows, err := s.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return page, fmt.Errorf("list asset catalog: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var item CatalogValue
		if err := rows.Scan(&item.ID, &item.Kind, &item.Value, &item.Manufacturer); err != nil {
			return page, err
		}
		page.Items = append(page.Items, item)
	}
	if err := rows.Err(); err != nil {
		return page, err
	}
	if len(page.Items) > f.Limit {
		page.Items = page.Items[:f.Limit]
		page.NextBeforeID = page.Items[f.Limit-1].ID
	}
	return page, nil
}

func (s *Service) CreateCatalogValue(ctx context.Context, a Actor, kind CatalogKind, value, manufacturer string) (CatalogValue, error) {
	var err error
	value, manufacturer, err = normalizeCatalogInput(kind, value, manufacturer)
	if err != nil {
		return CatalogValue{}, err
	}
	var created CatalogValue
	err = s.tx.WithTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted}, func(e database.Executor) error {
		current, err := workrecords.LockPrincipal(ctx, e, a)
		if err != nil {
			return err
		}
		if !Writable(current) {
			return workrecords.ErrForbidden
		}
		if kind == CatalogModel && manufacturer != "" {
			var exists bool
			err := e.QueryRowContext(ctx, `SELECT EXISTS(
				SELECT 1 FROM asset_catalog_values
				WHERE tenant_id=? AND kind='MANUFACTURER' AND value=? AND active=TRUE
			)`, current.TenantID, manufacturer).Scan(&exists)
			if err != nil {
				return err
			}
			if !exists {
				return fmt.Errorf("%w: wybierz producenta z listy przed dodaniem modelu", ErrInvalid)
			}
		}
		result, err := e.ExecContext(ctx, `INSERT INTO asset_catalog_values(tenant_id,kind,value,manufacturer)
			VALUES(?,?,?,?) ON DUPLICATE KEY UPDATE id=LAST_INSERT_ID(id)`, current.TenantID, kind, value, manufacturer)
		if err != nil {
			return err
		}
		id, err := result.LastInsertId()
		if err != nil {
			return err
		}
		if id == 0 {
			return fmt.Errorf("catalog value was not assigned an id")
		}
		if err := e.QueryRowContext(ctx, `SELECT id,kind,value,manufacturer
			FROM asset_catalog_values WHERE tenant_id=? AND id=?`, current.TenantID, id).Scan(
			&created.ID, &created.Kind, &created.Value, &created.Manufacturer,
		); err != nil {
			return err
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if changed == 1 {
			return Changed(ctx, e, current, "asset_catalog_value", created.ID, 1, nil, created, []string{"kind", "value", "manufacturer"})
		}
		return nil
	})
	return created, err
}

func validCatalogKind(kind CatalogKind) bool {
	return kind == CatalogManufacturer || kind == CatalogModel || kind == CatalogLocation
}

func normalizeCatalogInput(kind CatalogKind, value, manufacturer string) (string, string, error) {
	value = strings.TrimSpace(value)
	manufacturer = strings.TrimSpace(manufacturer)
	if !validCatalogKind(kind) || !Text(value, 1, 160) || !Text(manufacturer, 0, 160) || kind != CatalogModel && manufacturer != "" {
		return "", "", ErrInvalid
	}
	return value, manufacturer, nil
}

func ensureCatalogValue(ctx context.Context, e database.Executor, tenantID uint64, kind CatalogKind, value, manufacturer string) error {
	if value == "" {
		return nil
	}
	_, err := e.ExecContext(ctx, `INSERT INTO asset_catalog_values(tenant_id,kind,value,manufacturer)
		VALUES(?,?,?,?) ON DUPLICATE KEY UPDATE id=LAST_INSERT_ID(id)`, tenantID, kind, value, manufacturer)
	return err
}
