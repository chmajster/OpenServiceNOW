package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	AppEnv        string
	LogLevel      string
	HTTP          HTTPConfig
	MySQL         MySQLConfig
	Redis         RedisConfig
	MigrationsDir string
}

type HTTPConfig struct {
	Addr              string
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	ShutdownTimeout   time.Duration
	BodyLimitBytes    int64
	HealthTimeout     time.Duration
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
	ConnectTimeout  time.Duration
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
}

type RedisConfig struct {
	Addr           string
	Password       string
	DB             int
	ConnectTimeout time.Duration
	ReadTimeout    time.Duration
	WriteTimeout   time.Duration
}

type lookupFunc func(string) (string, bool)

func Load() (Config, error) {
	return load(os.LookupEnv)
}

func load(lookup lookupFunc) (Config, error) {
	cfg := Config{
		AppEnv:        envString(lookup, "APP_ENV", "development"),
		LogLevel:      envString(lookup, "LOG_LEVEL", "info"),
		MigrationsDir: envString(lookup, "MIGRATIONS_DIR", "migrations"),
	}
	var parseErrs []error

	cfg.HTTP.Addr = envString(lookup, "HTTP_ADDR", ":8080")
	cfg.HTTP.ReadHeaderTimeout = parseDuration(lookup, "HTTP_READ_HEADER_TIMEOUT", 5*time.Second, &parseErrs)
	cfg.HTTP.ReadTimeout = parseDuration(lookup, "HTTP_READ_TIMEOUT", 15*time.Second, &parseErrs)
	cfg.HTTP.WriteTimeout = parseDuration(lookup, "HTTP_WRITE_TIMEOUT", 30*time.Second, &parseErrs)
	cfg.HTTP.IdleTimeout = parseDuration(lookup, "HTTP_IDLE_TIMEOUT", 60*time.Second, &parseErrs)
	cfg.HTTP.ShutdownTimeout = parseDuration(lookup, "HTTP_SHUTDOWN_TIMEOUT", 15*time.Second, &parseErrs)
	cfg.HTTP.BodyLimitBytes = parseInt64(lookup, "HTTP_BODY_LIMIT_BYTES", 1<<20, &parseErrs)
	cfg.HTTP.HealthTimeout = parseDuration(lookup, "HEALTH_TIMEOUT", 2*time.Second, &parseErrs)

	cfg.MySQL.Host = envString(lookup, "MYSQL_HOST", "127.0.0.1")
	cfg.MySQL.Port = parseInt(lookup, "MYSQL_PORT", 3306, &parseErrs)
	cfg.MySQL.Database = envString(lookup, "MYSQL_DATABASE", "openservicenow")
	cfg.MySQL.User = envString(lookup, "MYSQL_USER", "openservicenow")
	cfg.MySQL.Password = envString(lookup, "MYSQL_PASSWORD", "")
	cfg.MySQL.MaxOpenConns = parseInt(lookup, "MYSQL_MAX_OPEN_CONNS", 25, &parseErrs)
	cfg.MySQL.MaxIdleConns = parseInt(lookup, "MYSQL_MAX_IDLE_CONNS", 10, &parseErrs)
	cfg.MySQL.ConnMaxLifetime = parseDuration(lookup, "MYSQL_CONN_MAX_LIFETIME", 5*time.Minute, &parseErrs)
	cfg.MySQL.ConnMaxIdleTime = parseDuration(lookup, "MYSQL_CONN_MAX_IDLE_TIME", 2*time.Minute, &parseErrs)
	cfg.MySQL.ConnectTimeout = parseDuration(lookup, "MYSQL_CONNECT_TIMEOUT", 5*time.Second, &parseErrs)
	cfg.MySQL.ReadTimeout = parseDuration(lookup, "MYSQL_READ_TIMEOUT", 5*time.Second, &parseErrs)
	cfg.MySQL.WriteTimeout = parseDuration(lookup, "MYSQL_WRITE_TIMEOUT", 5*time.Second, &parseErrs)

	cfg.Redis.Addr = envString(lookup, "REDIS_ADDR", "127.0.0.1:6379")
	cfg.Redis.Password = envString(lookup, "REDIS_PASSWORD", "")
	cfg.Redis.DB = parseInt(lookup, "REDIS_DB", 0, &parseErrs)
	cfg.Redis.ConnectTimeout = parseDuration(lookup, "REDIS_CONNECT_TIMEOUT", 3*time.Second, &parseErrs)
	cfg.Redis.ReadTimeout = parseDuration(lookup, "REDIS_READ_TIMEOUT", 3*time.Second, &parseErrs)
	cfg.Redis.WriteTimeout = parseDuration(lookup, "REDIS_WRITE_TIMEOUT", 3*time.Second, &parseErrs)

	parseErrs = append(parseErrs, cfg.Validate())
	if err := errors.Join(parseErrs...); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) Validate() error {
	var errs []error

	if strings.TrimSpace(c.HTTP.Addr) == "" {
		errs = append(errs, errors.New("HTTP_ADDR must not be empty"))
	}
	if c.HTTP.BodyLimitBytes <= 0 {
		errs = append(errs, errors.New("HTTP_BODY_LIMIT_BYTES must be greater than zero"))
	}
	if c.HTTP.ReadHeaderTimeout <= 0 || c.HTTP.ReadTimeout <= 0 || c.HTTP.WriteTimeout <= 0 || c.HTTP.IdleTimeout <= 0 {
		errs = append(errs, errors.New("HTTP timeouts must be greater than zero"))
	}
	if c.HTTP.HealthTimeout <= 0 || c.HTTP.ShutdownTimeout <= 0 {
		errs = append(errs, errors.New("HTTP health and shutdown timeouts must be greater than zero"))
	}
	if strings.TrimSpace(c.MySQL.Host) == "" || strings.TrimSpace(c.MySQL.Database) == "" || strings.TrimSpace(c.MySQL.User) == "" {
		errs = append(errs, errors.New("MYSQL_HOST, MYSQL_DATABASE and MYSQL_USER must not be empty"))
	}
	if c.MySQL.Port < 1 || c.MySQL.Port > 65535 {
		errs = append(errs, errors.New("MYSQL_PORT must be between 1 and 65535"))
	}
	if c.MySQL.MaxOpenConns <= 0 || c.MySQL.MaxIdleConns < 0 || c.MySQL.MaxIdleConns > c.MySQL.MaxOpenConns {
		errs = append(errs, errors.New("invalid MySQL connection pool limits"))
	}
	if c.MySQL.ConnMaxLifetime <= 0 || c.MySQL.ConnMaxIdleTime <= 0 || c.MySQL.ConnectTimeout <= 0 || c.MySQL.ReadTimeout <= 0 || c.MySQL.WriteTimeout <= 0 {
		errs = append(errs, errors.New("MySQL timeouts and connection lifetimes must be greater than zero"))
	}
	if strings.TrimSpace(c.Redis.Addr) == "" {
		errs = append(errs, errors.New("REDIS_ADDR must not be empty"))
	}
	if c.Redis.DB < 0 {
		errs = append(errs, errors.New("REDIS_DB must not be negative"))
	}
	if c.Redis.ConnectTimeout <= 0 || c.Redis.ReadTimeout <= 0 || c.Redis.WriteTimeout <= 0 {
		errs = append(errs, errors.New("Redis timeouts must be greater than zero"))
	}
	if strings.TrimSpace(c.MigrationsDir) == "" {
		errs = append(errs, errors.New("MIGRATIONS_DIR must not be empty"))
	}

	return errors.Join(errs...)
}

func envString(lookup lookupFunc, key, fallback string) string {
	if value, ok := lookup(key); ok {
		return value
	}
	return fallback
}

func parseInt(lookup lookupFunc, key string, fallback int, errs *[]error) int {
	value, ok := lookup(key)
	if !ok || strings.TrimSpace(value) == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		*errs = append(*errs, fmt.Errorf("%s must be an integer: %w", key, err))
		return fallback
	}
	return parsed
}

func parseInt64(lookup lookupFunc, key string, fallback int64, errs *[]error) int64 {
	value, ok := lookup(key)
	if !ok || strings.TrimSpace(value) == "" {
		return fallback
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		*errs = append(*errs, fmt.Errorf("%s must be an integer: %w", key, err))
		return fallback
	}
	return parsed
}

func parseDuration(lookup lookupFunc, key string, fallback time.Duration, errs *[]error) time.Duration {
	value, ok := lookup(key)
	if !ok || strings.TrimSpace(value) == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		*errs = append(*errs, fmt.Errorf("%s must be a duration: %w", key, err))
		return fallback
	}
	return parsed
}

func (c MySQLConfig) Address() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}
