package config

import (
	"strings"
	"testing"
)

func TestDefaultConfigValid(t *testing.T) {
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.HTTPAddr != ":8080" {
		t.Fatalf("HTTPAddr = %q", cfg.HTTPAddr)
	}
	if cfg.MySQL.Port != 3306 {
		t.Fatalf("MySQL.Port = %d", cfg.MySQL.Port)
	}
}

func TestLoadRejectsMalformedInteger(t *testing.T) {
	t.Setenv("MYSQL_PORT", "not-a-number")
	if _, err := Load(); err == nil {
		t.Fatal("expected invalid integer error")
	}
}

func TestLoadRejectsMalformedDuration(t *testing.T) {
	t.Setenv("SHUTDOWN_TIMEOUT", "later")
	if _, err := Load(); err == nil {
		t.Fatal("expected invalid duration error")
	}
}

func TestValidateRejectsInvalidPool(t *testing.T) {
	cfg := Config{
		HTTPAddr:        ":8080",
		ShutdownTimeout: 1,
		MySQL: MySQLConfig{
			Host: "db", Port: 3306, Database: "x", User: "u",
			MaxOpenConns: 1, MaxIdleConns: 2,
			ConnMaxLifetime: 1, ConnMaxIdleTime: 1, PingTimeout: 1,
		},
		Redis: RedisConfig{Addr: "redis:6379", PingTimeout: 1},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestDSNSupportsSpecialCharacters(t *testing.T) {
	cfg := MySQLConfig{
		Host: "127.0.0.1",
		Port: 3306,
		Database: "open snow",
		User: "user@domain",
		Password: "p@ss:/?#[]",
	}
	dsn := cfg.DSN()
	if dsn == "" {
		t.Fatal("DSN must not be empty")
	}
	if !strings.Contains(dsn, "user@domain:") {
		t.Fatalf("DSN does not contain expected user: %q", dsn)
	}
	if !strings.Contains(dsn, "/open+snow?") {
		t.Fatalf("database name was not escaped: %q", dsn)
	}
}
