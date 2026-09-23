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

func SchemaReady(ctx context.Context, db *sql.DB, timeout time.Duration) error {
	checkCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var count uint64
	if err := db.QueryRowContext(checkCtx, "SELECT COUNT(*) FROM platform_metadata").Scan(&count); err != nil {
		return fmt.Errorf("foundation schema not ready: %w", err)
	}
	return nil
}
