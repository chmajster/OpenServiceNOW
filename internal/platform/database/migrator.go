package database

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/golang-migrate/migrate/v4"
	migratedatabase "github.com/golang-migrate/migrate/v4/database"
	migratemysql "github.com/golang-migrate/migrate/v4/database/mysql"
)

func NewMigrator(db *sql.DB, sourceURL string) (*migrate.Migrate, error) {
	if db == nil {
		return nil, fmt.Errorf("migration database must not be nil")
	}
	if sourceURL == "" {
		return nil, fmt.Errorf("migration source URL must not be empty")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire migration connection: %w", err)
	}
	driver, err := migratemysql.WithConnection(ctx, conn, &migratemysql.Config{StatementTimeout: 2 * time.Minute})
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("create migration database driver: %w", err)
	}
	instance, err := migrate.NewWithDatabaseInstance(sourceURL, "mysql", &compatibleMySQLDriver{Driver: driver, db: db, conn: conn})
	if err != nil {
		_ = driver.Close()
		return nil, fmt.Errorf("create migrator: %w", err)
	}
	return instance, nil
}

// Historical migration 114 refers to (tenant_id,id) in observations, although
// migration 110 only declared the globally unique primary key on id. MySQL 8.4
// correctly rejects that FK. Preserve historical SQL, and add its missing
// candidate key under the migration lock before 114 (fresh install) or 139
// (upgrade from a server that accepted the old nonstandard FK).
type compatibleMySQLDriver struct {
	migratedatabase.Driver
	db   *sql.DB
	conn *sql.Conn
}

func (d *compatibleMySQLDriver) Close() error {
	err := d.Driver.Close()
	dbErr := d.db.Close()
	if err != nil {
		return err
	}
	return dbErr
}

func (d *compatibleMySQLDriver) Run(reader io.Reader) error {
	raw, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	statement := string(raw)
	// Migration 134 declared 20,000 utf8mb4 characters as VARCHAR, exceeding
	// MySQL's row limit. Preserve historical source while creating valid text.
	if strings.Contains(statement, "CREATE TABLE incident_major_updates") {
		raw = bytes.Replace(raw, []byte("body VARCHAR(20000)"), []byte("body MEDIUMTEXT"), 1)
	}
	if strings.Contains(statement, "CREATE TABLE incident_major_reviews") {
		raw = bytes.ReplaceAll(raw, []byte("VARCHAR(8000)"), []byte("TEXT"))
	}
	if strings.Contains(statement, "CREATE TABLE cmdb_ire_conflicts") || strings.Contains(statement, "ALTER TABLE incidents") && strings.Contains(statement, "process_policy") {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		var exists int
		err = d.conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.statistics WHERE table_schema=DATABASE() AND table_name='cmdb_ire_observations' AND index_name='cmdb_ire_observation_tenant_id' AND non_unique=0`).Scan(&exists)
		if err != nil {
			return fmt.Errorf("check historical observation identity key: %w", err)
		}
		if exists == 0 {
			if _, err = d.conn.ExecContext(ctx, `ALTER TABLE cmdb_ire_observations ADD UNIQUE KEY cmdb_ire_observation_tenant_id(tenant_id,id)`); err != nil {
				return fmt.Errorf("repair historical observation identity key: %w", err)
			}
		}
	}
	return d.Driver.Run(bytes.NewReader(raw))
}
