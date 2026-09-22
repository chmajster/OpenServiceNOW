package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	cfg, err := load(func(string) (string, bool) { return "", false })
	if err != nil {
		t.Fatalf("load defaults: %v", err)
	}
	if cfg.HTTP.Addr != ":8080" {
		t.Fatalf("unexpected HTTP address %q", cfg.HTTP.Addr)
	}
	if cfg.MySQL.Port != 3306 {
		t.Fatalf("unexpected MySQL port %d", cfg.MySQL.Port)
	}
	if cfg.MySQL.MaxOpenConns != 25 || cfg.MySQL.MaxIdleConns != 10 {
		t.Fatalf("unexpected pool defaults: open=%d idle=%d", cfg.MySQL.MaxOpenConns, cfg.MySQL.MaxIdleConns)
	}
}

func TestLoadOverrides(t *testing.T) {
	values := map[string]string{
		"HTTP_ADDR":                "127.0.0.1:9090",
		"HTTP_SHUTDOWN_TIMEOUT":    "7s",
		"MYSQL_HOST":               "mysql.internal",
		"MYSQL_PORT":               "3307",
		"MYSQL_MAX_OPEN_CONNS":     "40",
		"MYSQL_MAX_IDLE_CONNS":     "15",
		"MYSQL_CONN_MAX_IDLE_TIME": "90s",
		"REDIS_ADDR":               "redis.internal:6380",
		"REDIS_DB":                 "2",
		"HTTP_BODY_LIMIT_BYTES":    "2097152",
		"MYSQL_CONN_MAX_LIFETIME":  "10m",
		"MYSQL_CONNECT_TIMEOUT":    "4s",
		"MYSQL_READ_TIMEOUT":       "6s",
		"MYSQL_WRITE_TIMEOUT":      "8s",
		"REDIS_CONNECT_TIMEOUT":    "2s",
		"REDIS_READ_TIMEOUT":       "4s",
		"REDIS_WRITE_TIMEOUT":      "5s",
		"HEALTH_TIMEOUT":           "1500ms",
	}
	cfg, err := load(func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	})
	if err != nil {
		t.Fatalf("load overrides: %v", err)
	}
	if cfg.HTTP.Addr != "127.0.0.1:9090" || cfg.MySQL.Port != 3307 || cfg.Redis.DB != 2 {
		t.Fatalf("overrides not applied: %+v", cfg)
	}
	if cfg.HTTP.ShutdownTimeout != 7*time.Second || cfg.HTTP.HealthTimeout != 1500*time.Millisecond {
		t.Fatalf("duration overrides not applied")
	}
}

func TestValidateRejectsInvalidPool(t *testing.T) {
	cfg, err := load(func(string) (string, bool) { return "", false })
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	cfg.MySQL.MaxOpenConns = 2
	cfg.MySQL.MaxIdleConns = 3
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "connection pool") {
		t.Fatalf("expected pool validation error, got %v", err)
	}
}

func TestLoadRejectsMalformedValues(t *testing.T) {
	values := map[string]string{"MYSQL_PORT": "not-a-port", "HTTP_READ_TIMEOUT": "soon"}
	_, err := load(func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	})
	if err == nil {
		t.Fatal("expected parsing error")
	}
	if !strings.Contains(err.Error(), "MYSQL_PORT") || !strings.Contains(err.Error(), "HTTP_READ_TIMEOUT") {
		t.Fatalf("unexpected error: %v", err)
	}
}
