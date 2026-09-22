package tenants

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	platformdb "github.com/chmajster/OpenServiceNOW/internal/platform/database"
)

type MySQLRepository struct {
	db *sql.DB
}

func NewMySQLRepository(db *sql.DB) *MySQLRepository {
	return &MySQLRepository{db: db}
}

func (r *MySQLRepository) Get(ctx context.Context, tenantID uint64) (Tenant, error) {
	return scanTenant(r.db.QueryRowContext(ctx, `
		SELECT id, slug, name, status, version, created_at, updated_at
		FROM tenants
		WHERE id = ?
	`, tenantID))
}

func (r *MySQLRepository) GetForUpdate(
	ctx context.Context,
	exec platformdb.Executor,
	tenantID uint64,
) (Tenant, error) {
	return scanTenant(exec.QueryRowContext(ctx, `
		SELECT id, slug, name, status, version, created_at, updated_at
		FROM tenants
		WHERE id = ?
		FOR UPDATE
	`, tenantID))
}

func (r *MySQLRepository) UpdateName(
	ctx context.Context,
	exec platformdb.Executor,
	tenantID, expectedVersion uint64,
	name string,
) error {
	result, err := exec.ExecContext(ctx, `
		UPDATE tenants
		SET name = ?, version = version + 1
		WHERE id = ? AND version = ?
	`, name, tenantID, expectedVersion)
	if err != nil {
		return fmt.Errorf("update tenant name: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read tenant update result: %w", err)
	}
	if affected != 1 {
		return ErrConflict
	}
	return nil
}

type rowScanner interface {
	Scan(...any) error
}

func scanTenant(row rowScanner) (Tenant, error) {
	var tenant Tenant
	err := row.Scan(
		&tenant.ID,
		&tenant.Slug,
		&tenant.Name,
		&tenant.Status,
		&tenant.Version,
		&tenant.CreatedAt,
		&tenant.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Tenant{}, ErrNotFound
	}
	if err != nil {
		return Tenant{}, fmt.Errorf("scan tenant: %w", err)
	}
	return tenant, nil
}
