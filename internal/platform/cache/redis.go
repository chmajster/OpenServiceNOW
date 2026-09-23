package cache

import (
	"context"
	"crypto/tls"
	"fmt"
	"time"

	"github.com/chmajster/OpenServiceNOW/internal/platform/config"
	"github.com/redis/go-redis/v9"
)

func OpenRedis(ctx context.Context, cfg config.RedisConfig) (*redis.Client, error) {
	options := &redis.Options{Addr: cfg.Addr, Password: cfg.Password, DB: cfg.DB}
	if cfg.TLSEnabled {
		options.TLSConfig = &tls.Config{
			MinVersion:         tls.VersionTLS12,
			ServerName:         cfg.TLSServerName,
			InsecureSkipVerify: cfg.TLSInsecureSkipVerify, //nolint:gosec -- explicit operator-controlled compatibility option
		}
	}
	client := redis.NewClient(options)
	pingCtx, cancel := context.WithTimeout(ctx, cfg.PingTimeout)
	defer cancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("ping redis: %w", err)
	}
	return client, nil
}

func Ping(ctx context.Context, client *redis.Client, timeout time.Duration) error {
	pingCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return client.Ping(pingCtx).Err()
}
