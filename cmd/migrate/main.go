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
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := config.Load()
	if err != nil {
		logger.Error("configuration invalid", "error", err)
		os.Exit(1)
	}
	if len(os.Args) != 2 {
		usage(logger)
	}

	command := os.Args[1]
	switch command {
	case "up", "down", "down-all", "version":
	default:
		usage(logger)
	}

	db, err := platformdb.OpenMySQLForMigrations(context.Background(), cfg.MySQL)
	if err != nil {
		logger.Error("mysql unavailable", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	m, err := platformdb.NewMigrator(db, "file://migrations")
	if err != nil {
		logger.Error("create migrator", "error", err)
		os.Exit(1)
	}
	defer func() { _, _ = m.Close() }()

	if command == "down" || command == "down-all" {
		version, _, versionErr := m.Version()
		if errors.Is(versionErr, migrate.ErrNilVersion) {
			versionErr = nil
		}
		if versionErr != nil {
			logger.Error("read migration version before downgrade", "error", versionErr)
			os.Exit(1)
		}
		for _, table := range portfolioDataTablesForDowngrade(uint(version), command) {
			var hasData bool
			query := "SELECT EXISTS(SELECT 1 FROM " + table + " LIMIT 1)"
			if err := db.QueryRowContext(context.Background(), query).Scan(&hasData); err != nil {
				logger.Error("check portfolio data before downgrade", "table", table, "error", err)
				os.Exit(1)
			}
			if hasData {
				logger.Error("migration downgrade blocked to protect tenant service portfolio data; export and explicitly remove the records before retrying", "table", table)
				os.Exit(1)
			}
		}
	}

	switch command {
	case "up":
		err = m.Up()
	case "down":
		err = m.Steps(-1)
	case "down-all":
		err = m.Down()
	case "version":
		printVersion(logger, m)
		return
	}

	if err != nil && !errors.Is(err, migrate.ErrNoChange) {
		logger.Error("migration failed", "command", command, "error", err)
		os.Exit(1)
	}
	fmt.Printf("migration %s complete\n", command)
}

func portfolioDataTablesForDowngrade(version uint, command string) []string {
	tables := map[uint]string{
		97:  "service_portfolio_org_refs",
		98:  "service_portfolio_items",
		99:  "service_portfolio_relations",
		100: "service_portfolio_component_availability",
		101: "service_portfolio_offers",
		102: "service_portfolio_offer_audience_groups",
	}
	if command == "down" {
		if table, ok := tables[version]; ok {
			return []string{table}
		}
		return nil
	}
	if command != "down-all" || version < 97 {
		return nil
	}
	if version > 102 {
		version = 102
	}
	result := make([]string, 0, version-96)
	for current := uint(97); current <= version; current++ {
		if table, ok := tables[current]; ok {
			result = append(result, table)
		}
	}
	return result
}

func usage(logger *slog.Logger) {
	logger.Error("usage", "command", "migrate up|down|down-all|version")
	os.Exit(2)
}

func printVersion(logger *slog.Logger, m *migrate.Migrate) {
	version, dirty, err := m.Version()
	if errors.Is(err, migrate.ErrNilVersion) {
		fmt.Println("version: none dirty: false")
		return
	}
	if err != nil {
		logger.Error("read migration version", "error", err)
		os.Exit(1)
	}
	fmt.Printf("version: %d dirty: %t\n", version, dirty)
}
