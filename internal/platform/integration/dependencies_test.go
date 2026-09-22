package integration

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/chmajster/OpenServiceNOW/internal/platform/cache"
	"github.com/chmajster/OpenServiceNOW/internal/platform/config"
	"github.com/chmajster/OpenServiceNOW/internal/platform/database"
)

func TestDependencies(t *testing.T) {
	if os.Getenv("RUN_INTEGRATION_TESTS") != "1" {
		t.Skip("set RUN_INTEGRATION_TESTS=1 to run dependency integration tests")
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	db, err := database.Open(ctx, cfg.MySQL)
	if err != nil {
		t.Fatalf("open MySQL: %v", err)
	}
	defer db.Close()

	redisClient, err := cache.Open(ctx, cfg.Redis)
	if err != nil {
		t.Fatalf("open Redis: %v", err)
	}
	defer redisClient.Close()
}
