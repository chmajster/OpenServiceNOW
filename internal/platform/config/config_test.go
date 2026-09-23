package config

import (
	"testing"

	mysqlDriver "github.com/go-sql-driver/mysql"
)

func TestDefaultConfigValid(t *testing.T) {
	clearConfigEnv(t)
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
	clearConfigEnv(t)
	t.Setenv("MYSQL_PORT", "not-a-number")
	if _, err := Load(); err == nil {
		t.Fatal("expected invalid integer error")
	}
}

func TestLoadRejectsMalformedDuration(t *testing.T) {
	clearConfigEnv(t)
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
	clearConfigEnv(t)
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
	clearConfigEnv(t)
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
	clearConfigEnv(t)
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
	clearConfigEnv(t)
	t.Setenv("MYSQL_TLS_MODE", "maybe")
	if _, err := Load(); err == nil {
		t.Fatal("expected MySQL TLS mode validation error")
	}
}

func TestLoadRejectsRedisTLSOptionsWhenTLSDisabled(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("REDIS_TLS_ENABLED", "false")
	t.Setenv("REDIS_TLS_SERVER_NAME", "redis.example.test")
	if _, err := Load(); err == nil {
		t.Fatal("expected Redis TLS configuration validation error")
	}
}

func TestLoadRejectsMalformedHTTPAddress(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("HTTP_ADDR", "not-an-address")
	if _, err := Load(); err == nil {
		t.Fatal("expected HTTP_ADDR validation error")
	}
}

func TestLoadRequiresSecureCookiesOutsideDevAndTest(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("SESSION_COOKIE_SECURE", "false")
	if _, err := Load(); err == nil {
		t.Fatal("expected secure cookie validation error")
	}
}

func clearConfigEnv(t *testing.T) {
	t.Helper()
	keys := []string{
		"APP_ENV",
		"HTTP_ADDR",
		"SHUTDOWN_TIMEOUT",
		"TRUSTED_PROXY_CIDRS",
		"MYSQL_HOST",
		"MYSQL_PORT",
		"MYSQL_DATABASE",
		"MYSQL_USER",
		"MYSQL_PASSWORD",
		"MYSQL_TLS_MODE",
		"MYSQL_MAX_OPEN_CONNS",
		"MYSQL_MAX_IDLE_CONNS",
		"MYSQL_CONN_MAX_LIFETIME",
		"MYSQL_CONN_MAX_IDLE_TIME",
		"MYSQL_PING_TIMEOUT",
		"REDIS_ADDR",
		"REDIS_PASSWORD",
		"REDIS_DB",
		"REDIS_PING_TIMEOUT",
		"REDIS_TLS_ENABLED",
		"REDIS_TLS_SERVER_NAME",
		"REDIS_TLS_INSECURE_SKIP_VERIFY",
		"SESSION_TTL",
		"SESSION_COOKIE_SECURE",
		"SESSION_CLEANUP_INTERVAL",
		"SESSION_CLEANUP_BATCH",
		"SESSION_REVOKED_RETENTION",
		"AUTH_LOGIN_IP_LIMIT",
		"AUTH_LOGIN_IP_WINDOW",
		"AUTH_LOGIN_ACCOUNT_LIMIT",
		"AUTH_LOGIN_ACCOUNT_WINDOW",
		"AUTH_MAX_CONCURRENT_PASSWORD_CHECKS",
	}
	for _, key := range keys {
		t.Setenv(key, "")
	}
}

func TestMySQLDSNForcesUTCSessionTimezone(t *testing.T) {
	cfg := MySQLConfig{
		Host: "127.0.0.1",
		Port: 3306,
		Database: "opensnow",
		User: "opensnow",
		Password: "secret",
	}
	parsed, err := mysqlDriver.ParseDSN(cfg.DSN())
	if err != nil {
		t.Fatalf("ParseDSN() error = %v", err)
	}
	if got := parsed.Params["time_zone"]; got != "'+00:00'" {
		t.Fatalf("time_zone = %q", got)
	}
	if parsed.Loc.String() != "UTC" {
		t.Fatalf("Loc = %q", parsed.Loc.String())
	}
}

func TestLoadRejectsTrustAllProxyCIDR(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("TRUSTED_PROXY_CIDRS", "0.0.0.0/0")
	if _, err := Load(); err == nil {
		t.Fatal("expected trust-all proxy CIDR validation error")
	}
}

func TestLoadRejectsInvalidPasswordVerifierConcurrency(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("AUTH_MAX_CONCURRENT_PASSWORD_CHECKS", "0")
	if _, err := Load(); err == nil {
		t.Fatal("expected password verifier concurrency validation error")
	}
}

func TestLoadRejectsExcessiveSessionTTL(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("SESSION_TTL", "721h")
	if _, err := Load(); err == nil {
		t.Fatal("expected session TTL upper-bound validation error")
	}
}

func TestLoadRejectsExcessiveAuthenticationLimit(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("AUTH_LOGIN_IP_LIMIT", "10001")
	if _, err := Load(); err == nil {
		t.Fatal("expected authentication rate-limit upper-bound validation error")
	}
}

func TestLoadRejectsExcessiveAuthenticationWindow(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("AUTH_LOGIN_ACCOUNT_WINDOW", "25h")
	if _, err := Load(); err == nil {
		t.Fatal("expected authentication window upper-bound validation error")
	}
}

func TestLoadRejectsExcessivePasswordVerifierConcurrency(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("AUTH_MAX_CONCURRENT_PASSWORD_CHECKS", "17")
	if _, err := Load(); err == nil {
		t.Fatal("expected password verifier concurrency validation error")
	}
}
