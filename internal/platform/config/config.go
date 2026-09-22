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

type Config struct {
	AppEnv          string
	HTTPAddr        string
	ShutdownTimeout time.Duration
	MySQL           MySQLConfig
	Redis           RedisConfig
	Session         SessionConfig
}

type MySQLConfig struct {
	Host            string
	Port            int
	Database        string
	User            string
	Password        string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
	PingTimeout     time.Duration
}

type RedisConfig struct {
	Addr        string
	Password    string
	DB          int
	PingTimeout time.Duration
}

type SessionConfig struct {
	TTL            time.Duration
	CookieSecure   bool
	CookieName     string
	CSRFCookieName string
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
	sessionTTL, err := envDuration("SESSION_TTL", 12*time.Hour)
	if err != nil {
		return Config{}, err
	}
	cookieSecure, err := envBool("SESSION_COOKIE_SECURE", appEnv != "development" && appEnv != "test")
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		AppEnv:          appEnv,
		HTTPAddr:        getenv("HTTP_ADDR", ":8080"),
		ShutdownTimeout: shutdownTimeout,
		MySQL: MySQLConfig{
			Host:            getenv("MYSQL_HOST", "127.0.0.1"),
			Port:            mysqlPort,
			Database:        getenv("MYSQL_DATABASE", "opensnow"),
			User:            getenv("MYSQL_USER", "opensnow"),
			Password:        os.Getenv("MYSQL_PASSWORD"),
			MaxOpenConns:    maxOpen,
			MaxIdleConns:    maxIdle,
			ConnMaxLifetime: connMaxLifetime,
			ConnMaxIdleTime: connMaxIdleTime,
			PingTimeout:     mysqlPingTimeout,
		},
		Redis: RedisConfig{
			Addr:        getenv("REDIS_ADDR", "127.0.0.1:6379"),
			Password:    os.Getenv("REDIS_PASSWORD"),
			DB:          redisDB,
			PingTimeout: redisPingTimeout,
		},
		Session: SessionConfig{
			TTL:            sessionTTL,
			CookieSecure:   cookieSecure,
			CookieName:     getenv("SESSION_COOKIE_NAME", "opensnow_session"),
			CSRFCookieName: getenv("CSRF_COOKIE_NAME", "opensnow_csrf"),
		},
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
	if c.ShutdownTimeout <= 0 {
		errs = append(errs, errors.New("SHUTDOWN_TIMEOUT must be positive"))
	}
	if c.Session.TTL <= 0 {
		errs = append(errs, errors.New("SESSION_TTL must be positive"))
	}
	if strings.TrimSpace(c.Session.CookieName) == "" || strings.TrimSpace(c.Session.CSRFCookieName) == "" {
		errs = append(errs, errors.New("session cookie names must not be empty"))
	}
	if c.Session.CookieName == c.Session.CSRFCookieName {
		errs = append(errs, errors.New("session and CSRF cookie names must differ"))
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
	cfg.Params = map[string]string{"charset": "utf8mb4"}
	return cfg.FormatDSN()
}
