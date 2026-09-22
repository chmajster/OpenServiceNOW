package migrations

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

type HealthChecker struct {
	DB  *sql.DB
	Dir string
}

func (h HealthChecker) Name() string { return "schema" }

func (h HealthChecker) Check(ctx context.Context) error {
	if h.DB == nil {
		return errors.New("database handle is nil")
	}
	migrations, err := Load(h.Dir)
	if err != nil {
		return err
	}
	if len(migrations) == 0 {
		return errors.New("no migration files found")
	}

	var version uint
	var dirty bool
	err = h.DB.QueryRowContext(ctx, `SELECT version, dirty FROM schema_migrations LIMIT 1`).Scan(&version, &dirty)
	if errors.Is(err, sql.ErrNoRows) {
		return errors.New("database schema has no applied migrations")
	}
	if err != nil {
		return fmt.Errorf("read schema migration state: %w", err)
	}
	if dirty {
		return fmt.Errorf("database schema migration %d is dirty", version)
	}
	latest := migrations[len(migrations)-1].Version
	if version != latest {
		return fmt.Errorf("database schema version %d does not match required version %d", version, latest)
	}
	return nil
}
