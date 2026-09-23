package config

import (
	"testing"

	mysqlDriver "github.com/go-sql-driver/mysql"
)

func TestDefaultConfigValid(t *testing.T) {
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.HTTPAddr != "127.0.0.1:8080" {
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
	parsed, err := mysqlDriver.ParseDSN(cfg.DSN())
	if err != nil {
		t.Fatalf("ParseDSN() error = %v", err)
	}
	if parsed.User != cfg.User || parsed.Passwd != cfg.Password || parsed.DBName != cfg.Database {
		t.Fatalf("DSN round trip mismatch: user=%q db=%q", parsed.User, parsed.DBName)
	}
}

func TestSessionCookieNamesAreStableProtocolConstants(t *testing.T) {
	t.Setenv("SESSION_COOKIE_NAME", "unexpected-session-name")
	t.Setenv("CSRF_COOKIE_NAME", "unexpected-csrf-name")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Session.CookieName != SessionCookieName {
		t.Fatalf("session cookie name = %q", cfg.Session.CookieName)
	}
	if cfg.Session.CSRFCookieName != CSRFCookieName {
		t.Fatalf("CSRF cookie name = %q", cfg.Session.CSRFCookieName)
	}
}

func TestLoadRejectsInvalidTrustedProxyCIDR(t *testing.T) {
	t.Setenv("TRUSTED_PROXY_CIDRS", "10.0.0.0/8,not-a-cidr")
	if _, err := Load(); err == nil {
		t.Fatal("expected trusted proxy validation error")
	}
}

func TestMySQLDSNSeparatesRuntimeAndMigrationCapabilities(t *testing.T) {
	cfg := MySQLConfig{
		Host: "127.0.0.1",
		Port: 3306,
		Database: "opensnow",
		User: "opensnow",
		Password: "secret",
	}

	runtimeDSN, err := mysqlDriver.ParseDSN(cfg.DSN())
	if err != nil {
		t.Fatalf("ParseDSN(runtime) error = %v", err)
	}
	if runtimeDSN.MultiStatements {
		t.Fatal("runtime DSN must keep multiStatements disabled")
	}

	migrationDSN, err := mysqlDriver.ParseDSN(cfg.MigrationDSN())
	if err != nil {
		t.Fatalf("ParseDSN(migration) error = %v", err)
	}
	if !migrationDSN.MultiStatements {
		t.Fatal("migration DSN must enable multiStatements for golang-migrate WithInstance")
	}
}

func TestLoadRejectsSubMillisecondAuthenticationWindow(t *testing.T) {
	t.Setenv("AUTH_LOGIN_IP_WINDOW", "1ns")
	if _, err := Load(); err == nil {
		t.Fatal("expected authentication rate-limit window validation error")
	}
}

func TestMySQLTLSModeRoundTrip(t *testing.T) {
	cfg := MySQLConfig{
		Host: "db.example.test",
		Port: 3306,
		Database: "opensnow",
		User: "opensnow",
		Password: "secret",
		TLSMode: "true",
	}
	parsed, err := mysqlDriver.ParseDSN(cfg.DSN())
	if err != nil {
		t.Fatalf("ParseDSN() error = %v", err)
	}
	if parsed.TLSConfig != "true" {
		t.Fatalf("TLSConfig = %q", parsed.TLSConfig)
	}
}

func TestLoadRejectsUnknownMySQLTLSMode(t *testing.T) {
	t.Setenv("MYSQL_TLS_MODE", "maybe")
	if _, err := Load(); err == nil {
		t.Fatal("expected MySQL TLS mode validation error")
	}
}

func TestLoadRejectsRedisTLSOptionsWhenTLSDisabled(t *testing.T) {
	t.Setenv("REDIS_TLS_ENABLED", "false")
	t.Setenv("REDIS_TLS_SERVER_NAME", "redis.example.test")
	if _, err := Load(); err == nil {
		t.Fatal("expected Redis TLS configuration validation error")
	}
}
