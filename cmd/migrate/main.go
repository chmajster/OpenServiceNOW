package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/chmajster/OpenServiceNOW/internal/platform/config"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
	"github.com/chmajster/OpenServiceNOW/internal/platform/migrations"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: migrate <up|down|status>")
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	db, err := database.OpenForMigrations(ctx, cfg.MySQL)
	if err != nil {
		return err
	}

	runner, err := migrations.New(db, cfg.MigrationsDir)
	if err != nil {
		_ = db.Close()
		return err
	}
	defer runner.Close()

	switch strings.ToLower(args[0]) {
	case "up":
		return runner.Up()
	case "down":
		return runner.Down()
	case "status":
		status, err := runner.Status()
		if err != nil {
			return err
		}
		if status.Empty {
			fmt.Println("schema: empty")
			return nil
		}
		state := "clean"
		if status.Dirty {
			state = "dirty"
		}
		fmt.Printf("schema: version=%d state=%s\n", status.Version, state)
		return nil
	default:
		return fmt.Errorf("unknown migration command %q; expected up, down or status", args[0])
	}
}
