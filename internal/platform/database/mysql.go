package database

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/chmajster/OpenServiceNOW/internal/platform/config"
	mysqlDriver "github.com/go-sql-driver/mysql"
)

func Open(ctx context.Context, cfg config.MySQLConfig) (*sql.DB, error) {
	return open(ctx, cfg, false)
}

func OpenForMigrations(ctx context.Context, cfg config.MySQLConfig) (*sql.DB, error) {
	return open(ctx, cfg, true)
}

func open(ctx context.Context, cfg config.MySQLConfig, multiStatements bool) (*sql.DB, error) {
	driverCfg := mysqlDriver.NewConfig()
	driverCfg.Net = "tcp"
	driverCfg.Addr = cfg.Address()
	driverCfg.User = cfg.User
	driverCfg.Passwd = cfg.Password
	driverCfg.DBName = cfg.Database
	driverCfg.ParseTime = true
	driverCfg.Loc = time.UTC
	driverCfg.Collation = "utf8mb4_0900_ai_ci"
	driverCfg.Timeout = cfg.ConnectTimeout
	driverCfg.ReadTimeout = cfg.ReadTimeout
	driverCfg.WriteTimeout = cfg.WriteTimeout
	driverCfg.MultiStatements = multiStatements
	driverCfg.Params = map[string]string{"time_zone": "'+00:00'"}

	connector, err := mysqlDriver.NewConnector(driverCfg)
	if err != nil {
		return nil, fmt.Errorf("create MySQL connector: %w", err)
	}

	db := sql.OpenDB(connector)
	db.SetMaxOpenConns(cfg.MaxOpenConns)
	db.SetMaxIdleConns(cfg.MaxIdleConns)
	db.SetConnMaxLifetime(cfg.ConnMaxLifetime)
	db.SetConnMaxIdleTime(cfg.ConnMaxIdleTime)

	pingCtx, cancel := context.WithTimeout(ctx, cfg.ConnectTimeout)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping MySQL: %w", err)
	}
	return db, nil
}

type HealthChecker struct {
	DB *sql.DB
}

func (h HealthChecker) Name() string { return "mysql" }

func (h HealthChecker) Check(ctx context.Context) error {
	if h.DB == nil {
		return fmt.Errorf("MySQL database handle is nil")
	}
	return h.DB.PingContext(ctx)
}
