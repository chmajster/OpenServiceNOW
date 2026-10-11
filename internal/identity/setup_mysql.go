package identity

import (
	"context"
	"database/sql"
	"fmt"

	platformdb "github.com/chmajster/OpenServiceNOW/internal/platform/database"
)

func (r *MySQLRepository) SetupRequired(ctx context.Context) (bool, error) {
	var required bool
	err := r.db.QueryRowContext(ctx, `
		SELECT completed_at IS NULL
		  AND NOT EXISTS (SELECT 1 FROM users)
		  AND NOT EXISTS (SELECT 1 FROM tenants)
		FROM installation_state WHERE id = 1
	`).Scan(&required)
	if err != nil {
		return false, fmt.Errorf("read installation state: %w", err)
	}
	return required, nil
}

func (r *MySQLRepository) LockInstallation(ctx context.Context, exec platformdb.Executor) (bool, error) {
	var completedAt sql.NullTime
	if err := exec.QueryRowContext(ctx,
		"SELECT completed_at FROM installation_state WHERE id = 1 FOR UPDATE",
	).Scan(&completedAt); err != nil {
		return false, fmt.Errorf("lock installation state: %w", err)
	}
	if completedAt.Valid {
		return false, nil
	}
	var hasData bool
	if err := exec.QueryRowContext(ctx,
		"SELECT EXISTS (SELECT 1 FROM users) OR EXISTS (SELECT 1 FROM tenants)",
	).Scan(&hasData); err != nil {
		return false, fmt.Errorf("check existing installation: %w", err)
	}
	return !hasData, nil
}

func (r *MySQLRepository) CompleteInstallation(ctx context.Context, exec platformdb.Executor) error {
	_, err := exec.ExecContext(ctx,
		"UPDATE installation_state SET completed_at = COALESCE(completed_at, UTC_TIMESTAMP(6)) WHERE id = 1",
	)
	if err != nil {
		return fmt.Errorf("complete installation: %w", err)
	}
	return nil
}
