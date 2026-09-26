package database

import (
	"database/sql"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	migratemysql "github.com/golang-migrate/migrate/v4/database/mysql"
)

func NewMigrator(db *sql.DB, sourceURL string) (*migrate.Migrate, error) {
	if db == nil {
		return nil, fmt.Errorf("migration database must not be nil")
	}
	if sourceURL == "" {
		return nil, fmt.Errorf("migration source URL must not be empty")
	}

	driver, err := migratemysql.WithInstance(db, &migratemysql.Config{})
	if err != nil {
		return nil, fmt.Errorf("create migration database driver: %w", err)
	}
	instance, err := migrate.NewWithDatabaseInstance(sourceURL, "mysql", driver)
	if err != nil {
		return nil, fmt.Errorf("create migrator: %w", err)
	}
	return instance, nil
}
