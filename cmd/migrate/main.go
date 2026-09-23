package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"

	"github.com/chmajster/OpenServiceNOW/internal/platform/config"
	platformdb "github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/golang-migrate/migrate/v4"
	migratemysql "github.com/golang-migrate/migrate/v4/database/mysql"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := config.Load()
	if err != nil {
		logger.Error("configuration invalid", "error", err)
		os.Exit(1)
	}
	if len(os.Args) != 2 || (os.Args[1] != "up" && os.Args[1] != "down") {
		logger.Error("usage", "command", "migrate up|down")
		os.Exit(2)
	}

	db, err := platformdb.OpenMySQLForMigrations(context.Background(), cfg.MySQL)
	if err != nil {
		logger.Error("mysql unavailable", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	driver, err := migratemysql.WithInstance(db, &migratemysql.Config{})
	if err != nil {
		logger.Error("create migration database driver", "error", err)
		os.Exit(1)
	}

	m, err := migrate.NewWithDatabaseInstance("file://migrations", "mysql", driver)
	if err != nil {
		logger.Error("create migrator", "error", err)
		os.Exit(1)
	}
	defer func() { _, _ = m.Close() }()

	switch os.Args[1] {
	case "up":
		err = m.Up()
	case "down":
		err = m.Steps(-1)
	}
	if err != nil && !errors.Is(err, migrate.ErrNoChange) {
		logger.Error("migration failed", "direction", os.Args[1], "error", err)
		os.Exit(1)
	}
	fmt.Printf("migration %s complete\n", os.Args[1])
}
