package config

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	mysqlDriver "github.com/go-sql-driver/mysql"
)

const (
	SessionCookieName = "opensnow_session"
	CSRFCookieName    = "opensnow_csrf"
)

type Config struct {
	AppEnv            string
	HTTPAddr          string
	ShutdownTimeout   time.Duration
	MySQL             MySQLConfig
	Redis             RedisConfig
	Session           SessionConfig
	Auth              AuthConfig
	TrustedProxyCIDRs []string
}

type MySQLConfig struct {
	Host            string
	Port            int
	Database        string
	User            string
	Password        string
	TLSMode         string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
	PingTimeout     time.Duration
}

type RedisConfig struct {
	Addr                  string
	Password              string
	DB                    int
	PingTimeout           time.Duration
	TLSEnabled            bool
	TLSServerName         string
	TLSInsecureSkipVerify bool
}

type SessionConfig struct {
	TTL               time.Duration
	CookieSecure      bool
	CookieName        string
	CSRFCookieName    string
	CleanupInterval   time.Duration
	CleanupBatch      int
	RevokedRetention  time.Duration
}

type AuthConfig struct {
	LoginIPLimit                int
	LoginIPWindow               time.Duration
	LoginAccountLimit           int
	LoginAccountWindow          time.Duration
	MaxConcurrentPasswordChecks int
}

func Load() (Config, error) {
	appEnv := getenv("APP_ENV", "development")
	mysqlPort, err := envInt("MYSQL_PORT", 3306)
	if err != nil {
		return Config{}, err
	}
	maxOpen, err := envInt("MYSQL_MAX_OPEN_CONNS", 25)
	if err != nil {
		return Config{}, err
	}
	maxIdle, err := envInt("MYSQL_MAX_IDLE_CONNS", 10)
	if err != nil {
		return Config{}, err
	}
	redisDB, err := envInt("REDIS_DB", 0)
	if err != nil {
		return Config{}, err
	}
	shutdownTimeout, err := envDuration("SHUTDOWN_TIMEOUT", 15*time.Second)
	if err != nil {
		return Config{}, err
	}
	connMaxLifetime, err := envDuration("MYSQL_CONN_MAX_LIFETIME", 30*time.Minute)
	if err != nil {
		return Config{}, err
	}
	connMaxIdleTime, err := envDuration("MYSQL_CONN_MAX_IDLE_TIME", 5*time.Minute)
	if err != nil {
		return Config{}, err
	}
	mysqlPingTimeout, err := envDuration("MYSQL_PING_TIMEOUT", 2*time.Second)
	if err != nil {
		return Config{}, err
	}
	redisPingTimeout, err := envDuration("REDIS_PING_TIMEOUT", 2*time.Second)
	if err != nil {
		return Config{}, err
	}
	redisTLSEnabled, err := envBool("REDIS_TLS_ENABLED", false)
	if err != nil {
		return Config{}, err
	}
	redisTLSInsecure, err := envBool("REDIS_TLS_INSECURE_SKIP_VERIFY", false)
	if err != nil {
		return Config{}, err
	}
	sessionTTL, err := envDuration("SESSION_TTL", 12*time.Hour)
	if err != nil {
		return Config{}, err
	}
	cookieSecure, err := envBool("SESSION_COOKIE_SECURE", appEnv != "development" && appEnv != "test")
	if err != nil {
		return Config{}, err
	}
	sessionCleanupInterval, err := envDuration("SESSION_CLEANUP_INTERVAL", 15*time.Minute)
	if err != nil {
		return Config{}, err
	}
	sessionCleanupBatch, err := envInt("SESSION_CLEANUP_BATCH", 500)
	if err != nil {
		return Config{}, err
	}
	sessionRevokedRetention, err := envDuration("SESSION_REVOKED_RETENTION", 24*time.Hour)
	if err != nil {
		return Config{}, err
	}
	loginIPLimit, err := envInt("AUTH_LOGIN_IP_LIMIT", 10)
	if err != nil {
		return Config{}, err
	}
	loginIPWindow, err := envDuration("AUTH_LOGIN_IP_WINDOW", time.Minute)
	if err != nil {
		return Config{}, err
	}
	loginAccountLimit, err := envInt("AUTH_LOGIN_ACCOUNT_LIMIT", 20)
	if err != nil {
		return Config{}, err
	}
	loginAccountWindow, err := envDuration("AUTH_LOGIN_ACCOUNT_WINDOW", 15*time.Minute)
	if err != nil {
		return Config{}, err
	}
	maxConcurrentPasswordChecks, err := envInt("AUTH_MAX_CONCURRENT_PASSWORD_CHECKS", 4)
	if err != nil {
		return Config{}, err
	}

	trustedProxyCIDRs := envCSV("TRUSTED_PROXY_CIDRS")

	cfg := Config{
		AppEnv:          appEnv,
		HTTPAddr:        getenv("HTTP_ADDR", "127.0.0.1:8080"),
		ShutdownTimeout: shutdownTimeout,
		MySQL: MySQLConfig{
			Host:            getenv("MYSQL_HOST", "127.0.0.1"),
			Port:            mysqlPort,
			Database:        getenv("MYSQL_DATABASE", "opensnow"),
			User:            getenv("MYSQL_USER", "opensnow"),
			Password:        os.Getenv("MYSQL_PASSWORD"),
			TLSMode:         getenv("MYSQL_TLS_MODE", "false"),
			MaxOpenConns:    maxOpen,
			MaxIdleConns:    maxIdle,
			ConnMaxLifetime: connMaxLifetime,
			ConnMaxIdleTime: connMaxIdleTime,
			PingTimeout:     mysqlPingTimeout,
		},
		Redis: RedisConfig{
			Addr:                  getenv("REDIS_ADDR", "127.0.0.1:6379"),
			Password:              os.Getenv("REDIS_PASSWORD"),
			DB:                    redisDB,
			PingTimeout:           redisPingTimeout,
			TLSEnabled:            redisTLSEnabled,
			TLSServerName:         strings.TrimSpace(os.Getenv("REDIS_TLS_SERVER_NAME")),
			TLSInsecureSkipVerify: redisTLSInsecure,
		},
		Session: SessionConfig{
			TTL:              sessionTTL,
			CookieSecure:     cookieSecure,
			CookieName:       SessionCookieName,
			CSRFCookieName:   CSRFCookieName,
			CleanupInterval:  sessionCleanupInterval,
			CleanupBatch:     sessionCleanupBatch,
			RevokedRetention: sessionRevokedRetention,
		},
		Auth: AuthConfig{
			LoginIPLimit:                loginIPLimit,
			LoginIPWindow:               loginIPWindow,
			LoginAccountLimit:           loginAccountLimit,
			LoginAccountWindow:          loginAccountWindow,
			MaxConcurrentPasswordChecks: maxConcurrentPasswordChecks,
		},
		TrustedProxyCIDRs: trustedProxyCIDRs,
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) Validate() error {
	var errs []error
	if strings.TrimSpace(c.HTTPAddr) == "" {
		errs = append(errs, errors.New("HTTP_ADDR must not be empty"))
	} else if _, _, err := net.SplitHostPort(c.HTTPAddr); err != nil {
		errs = append(errs, fmt.Errorf("HTTP_ADDR must be host:port: %w", err))
	}
	if strings.TrimSpace(c.MySQL.Host) == "" {
		errs = append(errs, errors.New("MYSQL_HOST must not be empty"))
	}
	if c.MySQL.Port < 1 || c.MySQL.Port > 65535 {
		errs = append(errs, errors.New("MYSQL_PORT must be between 1 and 65535"))
	}
	if strings.TrimSpace(c.MySQL.Database) == "" {
		errs = append(errs, errors.New("MYSQL_DATABASE must not be empty"))
	}
	if strings.TrimSpace(c.MySQL.User) == "" {
		errs = append(errs, errors.New("MYSQL_USER must not be empty"))
	}
	switch c.MySQL.TLSMode {
	case "false", "true", "skip-verify", "preferred":
	default:
		errs = append(errs, errors.New("MYSQL_TLS_MODE must be one of false,true,skip-verify,preferred"))
	}
	if c.MySQL.MaxOpenConns < 1 {
		errs = append(errs, errors.New("MYSQL_MAX_OPEN_CONNS must be at least 1"))
	}
	if c.MySQL.MaxIdleConns < 0 || c.MySQL.MaxIdleConns > c.MySQL.MaxOpenConns {
		errs = append(errs, errors.New("MYSQL_MAX_IDLE_CONNS must be between 0 and MYSQL_MAX_OPEN_CONNS"))
	}
	if c.MySQL.ConnMaxLifetime <= 0 || c.MySQL.ConnMaxIdleTime <= 0 || c.MySQL.PingTimeout <= 0 {
		errs = append(errs, errors.New("MySQL durations must be positive"))
	}
	if strings.TrimSpace(c.Redis.Addr) == "" {
		errs = append(errs, errors.New("REDIS_ADDR must not be empty"))
	} else if _, _, err := net.SplitHostPort(c.Redis.Addr); err != nil {
		errs = append(errs, fmt.Errorf("REDIS_ADDR must be host:port: %w", err))
	}
	if c.Redis.DB < 0 {
		errs = append(errs, errors.New("REDIS_DB must not be negative"))
	}
	if c.Redis.PingTimeout <= 0 {
		errs = append(errs, errors.New("REDIS_PING_TIMEOUT must be positive"))
	}
	if !c.Redis.TLSEnabled && (c.Redis.TLSServerName != "" || c.Redis.TLSInsecureSkipVerify) {
		errs = append(errs, errors.New("Redis TLS options require REDIS_TLS_ENABLED=true"))
	}
	if c.ShutdownTimeout <= 0 {
		errs = append(errs, errors.New("SHUTDOWN_TIMEOUT must be positive"))
	}
	if c.Session.TTL <= 0 || c.Session.TTL > 30*24*time.Hour {
		errs = append(errs, errors.New("SESSION_TTL must be greater than 0 and at most 30 days"))
	}
	if strings.TrimSpace(c.Session.CookieName) == "" || strings.TrimSpace(c.Session.CSRFCookieName) == "" {
		errs = append(errs, errors.New("session cookie names must not be empty"))
	}
	if c.Session.CookieName == c.Session.CSRFCookieName {
		errs = append(errs, errors.New("session and CSRF cookie names must differ"))
	}
	if c.AppEnv != "development" && c.AppEnv != "test" && !c.Session.CookieSecure {
		errs = append(errs, errors.New("SESSION_COOKIE_SECURE must be true outside development and test"))
	}
	if c.Session.CleanupInterval <= 0 || c.Session.RevokedRetention < 0 {
		errs = append(errs, errors.New("session cleanup durations are invalid"))
	}
	if c.Session.CleanupBatch < 1 || c.Session.CleanupBatch > 10000 {
		errs = append(errs, errors.New("SESSION_CLEANUP_BATCH must be between 1 and 10000"))
	}
	if c.Auth.LoginIPLimit < 1 || c.Auth.LoginIPLimit > 10000 ||
		c.Auth.LoginAccountLimit < 1 || c.Auth.LoginAccountLimit > 10000 {
		errs = append(errs, errors.New("authentication rate limits must be between 1 and 10000"))
	}
	if c.Auth.LoginIPWindow < time.Millisecond || c.Auth.LoginIPWindow > 24*time.Hour ||
		c.Auth.LoginAccountWindow < time.Millisecond || c.Auth.LoginAccountWindow > 24*time.Hour {
		errs = append(errs, errors.New("authentication rate limit windows must be between 1ms and 24h"))
	}
	if c.Auth.MaxConcurrentPasswordChecks < 1 || c.Auth.MaxConcurrentPasswordChecks > 16 {
		errs = append(errs, errors.New("AUTH_MAX_CONCURRENT_PASSWORD_CHECKS must be between 1 and 16"))
	}
	for _, cidr := range c.TrustedProxyCIDRs {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			errs = append(errs, fmt.Errorf("invalid TRUSTED_PROXY_CIDRS entry %q: %w", cidr, err))
			continue
		}
		prefix, _ := network.Mask.Size()
		if prefix == 0 {
			errs = append(errs, fmt.Errorf("TRUSTED_PROXY_CIDRS entry %q trusts every address", cidr))
		}
	}
	return errors.Join(errs...)
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer: %w", key, err)
	}
	return n, nil
}

func envBool(key string, fallback bool) (bool, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return false, fmt.Errorf("%s must be a boolean: %w", key, err)
	}
	return b, nil
}

func envCSV(key string) []string {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		value := strings.TrimSpace(part)
		if value != "" {
			result = append(result, value)
		}
	}
	return result
}

func envDuration(key string, fallback time.Duration) (time.Duration, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("%s must be a duration: %w", key, err)
	}
	return d, nil
}

func (c MySQLConfig) DSN() string {
	return c.dsn(false)
}

func (c MySQLConfig) MigrationDSN() string {
	return c.dsn(true)
}

func (c MySQLConfig) dsn(multiStatements bool) string {
	cfg := mysqlDriver.NewConfig()
	cfg.User = c.User
	cfg.Passwd = c.Password
	cfg.Net = "tcp"
	cfg.Addr = net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
	cfg.DBName = c.Database
	cfg.ParseTime = true
	cfg.Loc = time.UTC
	cfg.Collation = "utf8mb4_0900_ai_ci"
	cfg.Timeout = 5 * time.Second
	cfg.ReadTimeout = 10 * time.Second
	cfg.WriteTimeout = 10 * time.Second
	cfg.MultiStatements = multiStatements
	cfg.TLSConfig = c.TLSMode
	cfg.Params = map[string]string{
		"charset":   "utf8mb4",
		"time_zone": "'+00:00'",
	}
	return cfg.FormatDSN()
}
