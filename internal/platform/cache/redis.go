package cache

import (
	"context"
	"fmt"

	"github.com/chmajster/OpenServiceNOW/internal/platform/config"
	"github.com/redis/go-redis/v9"
)

func Open(ctx context.Context, cfg config.RedisConfig) (*redis.Client, error) {
	client := redis.NewClient(&redis.Options{
		Addr:         cfg.Addr,
		Password:     cfg.Password,
		DB:           cfg.DB,
		DialTimeout:  cfg.ConnectTimeout,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
	})

	pingCtx, cancel := context.WithTimeout(ctx, cfg.ConnectTimeout)
	defer cancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("ping Redis: %w", err)
	}
	return client, nil
}

type HealthChecker struct {
	Client *redis.Client
}

func (h HealthChecker) Name() string { return "redis" }

func (h HealthChecker) Check(ctx context.Context) error {
	if h.Client == nil {
		return fmt.Errorf("Redis client is nil")
	}
	return h.Client.Ping(ctx).Err()
}
