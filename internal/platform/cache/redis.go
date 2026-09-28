package cache

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"time"

	"github.com/chmajster/OpenServiceNOW/internal/platform/config"
	"github.com/redis/go-redis/v9"
)

func OpenRedis(ctx context.Context, cfg config.RedisConfig) (*redis.Client, error) {
	options, err := redisOptions(cfg)
	if err != nil {
		return nil, err
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

func redisOptions(cfg config.RedisConfig) (*redis.Options, error) {
	options := &redis.Options{Addr: cfg.Addr, Password: cfg.Password, DB: cfg.DB}
	if !cfg.TLSEnabled {
		return options, nil
	}

	serverName := cfg.TLSServerName
	if serverName == "" {
		host, _, err := net.SplitHostPort(cfg.Addr)
		if err != nil {
			return nil, fmt.Errorf("derive Redis TLS server name: %w", err)
		}
		serverName = host
	}
	options.TLSConfig = &tls.Config{
		MinVersion:         tls.VersionTLS12,
		ServerName:         serverName,
		InsecureSkipVerify: cfg.TLSInsecureSkipVerify, //nolint:gosec -- explicit operator-controlled compatibility option
	}
	return options, nil
}

func Ping(ctx context.Context, client *redis.Client, timeout time.Duration) error {
	pingCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return client.Ping(pingCtx).Err()
}
