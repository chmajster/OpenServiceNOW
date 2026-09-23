package cache

import (
	"crypto/tls"
	"testing"
	"time"

	"github.com/chmajster/OpenServiceNOW/internal/platform/config"
)

func TestRedisOptionsWithoutTLS(t *testing.T) {
	options, err := redisOptions(config.RedisConfig{
		Addr: "127.0.0.1:6379", DB: 2, PingTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("redisOptions() error = %v", err)
	}
	if options.TLSConfig != nil {
		t.Fatal("TLSConfig must be nil when TLS is disabled")
	}
	if options.DB != 2 {
		t.Fatalf("DB = %d", options.DB)
	}
}

func TestRedisOptionsDerivesTLSServerName(t *testing.T) {
	options, err := redisOptions(config.RedisConfig{
		Addr: "redis.internal.example:6380",
		TLSEnabled: true,
		PingTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("redisOptions() error = %v", err)
	}
	if options.TLSConfig == nil {
		t.Fatal("TLSConfig is nil")
	}
	if options.TLSConfig.ServerName != "redis.internal.example" {
		t.Fatalf("ServerName = %q", options.TLSConfig.ServerName)
	}
	if options.TLSConfig.MinVersion != tls.VersionTLS12 {
		t.Fatalf("MinVersion = %d", options.TLSConfig.MinVersion)
	}
}

func TestRedisOptionsUsesExplicitTLSServerName(t *testing.T) {
	options, err := redisOptions(config.RedisConfig{
		Addr: "10.0.0.10:6380",
		TLSEnabled: true,
		TLSServerName: "redis.internal.example",
		PingTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("redisOptions() error = %v", err)
	}
	if options.TLSConfig == nil {
		t.Fatal("TLSConfig is nil")
	}
	if options.TLSConfig.ServerName != "redis.internal.example" {
		t.Fatalf("TLS ServerName = %q", options.TLSConfig.ServerName)
	}
}
