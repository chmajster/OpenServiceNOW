package database

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/chmajster/OpenServiceNOW/internal/platform/config"
	_ "github.com/go-sql-driver/mysql"
)

func OpenMySQL(ctx context.Context, cfg config.MySQLConfig) (*sql.DB, error) {
	return openMySQL(ctx, cfg, cfg.DSN())
}

func OpenMySQLForMigrations(ctx context.Context, cfg config.MySQLConfig) (*sql.DB, error) {
	return openMySQL(ctx, cfg, cfg.MigrationDSN())
}

func openMySQL(ctx context.Context, cfg config.MySQLConfig, dsn string) (*sql.DB, error) {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("open mysql: %w", err)
	}
	db.SetMaxOpenConns(cfg.MaxOpenConns)
	db.SetMaxIdleConns(cfg.MaxIdleConns)
	db.SetConnMaxLifetime(cfg.ConnMaxLifetime)
	db.SetConnMaxIdleTime(cfg.ConnMaxIdleTime)

	pingCtx, cancel := context.WithTimeout(ctx, cfg.PingTimeout)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping mysql: %w", err)
	}
	return db, nil
}

func Ping(ctx context.Context, db *sql.DB, timeout time.Duration) error {
	pingCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return db.PingContext(pingCtx)
}

const RequiredSchemaVersion uint64 = 63

func SchemaReady(ctx context.Context, db *sql.DB, timeout time.Duration) error {
	checkCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var version uint64
	var dirty bool
	if err := db.QueryRowContext(checkCtx, "SELECT version, dirty FROM schema_migrations LIMIT 1").Scan(&version, &dirty); err != nil {
		return fmt.Errorf("read schema migration state: %w", err)
	}
	if err := ValidateSchemaVersion(version, dirty); err != nil {
		return err
	}

	var installedAt time.Time
	if err := db.QueryRowContext(
		checkCtx,
		"SELECT installed_at FROM platform_metadata WHERE id = 1",
	).Scan(&installedAt); err != nil {
		return fmt.Errorf("read platform installation metadata: %w", err)
	}
	return nil
}

func ValidateSchemaVersion(version uint64, dirty bool) error {
	if dirty {
		return fmt.Errorf("database schema is dirty at version %d", version)
	}
	if version != RequiredSchemaVersion {
		return fmt.Errorf("database schema version %d does not match required version %d", version, RequiredSchemaVersion)
	}
	return nil
}
