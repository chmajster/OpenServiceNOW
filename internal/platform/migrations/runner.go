package migrations

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	migrate "github.com/golang-migrate/migrate/v4"
	migratemysql "github.com/golang-migrate/migrate/v4/database/mysql"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

type Migration struct {
	Version  uint
	Name     string
	UpPath   string
	DownPath string
}

type Runner struct {
	migrate *migrate.Migrate
}

type Status struct {
	Version uint
	Dirty   bool
	Empty   bool
}

func New(db *sql.DB, dir string) (*Runner, error) {
	if db == nil {
		return nil, errors.New("migration database handle is nil")
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("resolve migrations directory: %w", err)
	}
	if _, err := Load(absDir); err != nil {
		return nil, err
	}

	driver, err := migratemysql.WithInstance(db, &migratemysql.Config{MigrationsTable: "schema_migrations"})
	if err != nil {
		return nil, fmt.Errorf("create MySQL migration driver: %w", err)
	}
	sourceURL := (&url.URL{Scheme: "file", Path: filepath.ToSlash(absDir)}).String()
	migration, err := migrate.NewWithDatabaseInstance(sourceURL, "mysql", driver)
	if err != nil {
		return nil, fmt.Errorf("create migration runner: %w", err)
	}
	return &Runner{migrate: migration}, nil
}

func (r *Runner) Up() error {
	if err := r.migrate.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migrate up: %w", err)
	}
	return nil
}

func (r *Runner) Down() error {
	if err := r.migrate.Steps(-1); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migrate down one version: %w", err)
	}
	return nil
}

func (r *Runner) Status() (Status, error) {
	version, dirty, err := r.migrate.Version()
	if errors.Is(err, migrate.ErrNilVersion) {
		return Status{Empty: true}, nil
	}
	if err != nil {
		return Status{}, fmt.Errorf("read migration status: %w", err)
	}
	return Status{Version: version, Dirty: dirty}, nil
}

func (r *Runner) Close() error {
	sourceErr, databaseErr := r.migrate.Close()
	return errors.Join(sourceErr, databaseErr)
}

func Load(dir string) ([]Migration, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read migrations directory: %w", err)
	}

	type pair struct {
		version uint
		name    string
		up      string
		down    string
	}
	pairs := make(map[uint]*pair)

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		var direction string
		switch {
		case strings.HasSuffix(name, ".up.sql"):
			direction = "up"
		case strings.HasSuffix(name, ".down.sql"):
			direction = "down"
		default:
			continue
		}

		base := strings.TrimSuffix(strings.TrimSuffix(name, ".up.sql"), ".down.sql")
		parts := strings.SplitN(base, "_", 2)
		if len(parts) != 2 || parts[1] == "" {
			return nil, fmt.Errorf("invalid migration filename %q", name)
		}
		parsed, err := strconv.ParseUint(parts[0], 10, 64)
		if err != nil || parsed == 0 {
			return nil, fmt.Errorf("invalid migration version in %q", name)
		}
		version := uint(parsed)
		item := pairs[version]
		if item == nil {
			item = &pair{version: version, name: parts[1]}
			pairs[version] = item
		}
		if item.name != parts[1] {
			return nil, fmt.Errorf("migration %d has mismatched names", version)
		}
		path := filepath.Join(dir, name)
		if direction == "up" {
			if item.up != "" {
				return nil, fmt.Errorf("duplicate up migration for version %d", version)
			}
			item.up = path
		} else {
			if item.down != "" {
				return nil, fmt.Errorf("duplicate down migration for version %d", version)
			}
			item.down = path
		}
	}

	versions := make([]uint, 0, len(pairs))
	for version := range pairs {
		versions = append(versions, version)
	}
	sort.Slice(versions, func(i, j int) bool { return versions[i] < versions[j] })

	migrations := make([]Migration, 0, len(versions))
	for _, version := range versions {
		item := pairs[version]
		if item.up == "" || item.down == "" {
			return nil, fmt.Errorf("migration %d must contain both up and down files", version)
		}
		migrations = append(migrations, Migration{
			Version:  item.version,
			Name:     item.name,
			UpPath:   item.up,
			DownPath: item.down,
		})
	}
	return migrations, nil
}
