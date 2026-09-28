package database

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

func WithAdvisoryLock(
	ctx context.Context,
	db *sql.DB,
	name string,
	fn func(context.Context) error,
) (bool, error) {
	if name == "" {
		return false, fmt.Errorf("advisory lock name must not be empty")
	}

	conn, err := db.Conn(ctx)
	if err != nil {
		return false, fmt.Errorf("reserve advisory lock connection: %w", err)
	}
	defer conn.Close()

	var acquired sql.NullInt64
	if err := conn.QueryRowContext(ctx, "SELECT GET_LOCK(?, 0)", name).Scan(&acquired); err != nil {
		return false, fmt.Errorf("acquire advisory lock %q: %w", name, err)
	}
	if !acquired.Valid {
		return false, fmt.Errorf("acquire advisory lock %q returned NULL", name)
	}
	if acquired.Int64 == 0 {
		return false, nil
	}

	defer func() {
		releaseCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		var released sql.NullInt64
		_ = conn.QueryRowContext(releaseCtx, "SELECT RELEASE_LOCK(?)", name).Scan(&released)
	}()

	if err := fn(ctx); err != nil {
		return true, err
	}
	return true, nil
}
