package ratelimit

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

var fixedWindowScript = redis.NewScript(`
local current = redis.call("INCR", KEYS[1])
if current == 1 then
  redis.call("PEXPIRE", KEYS[1], ARGV[1])
end
if current > tonumber(ARGV[2]) then
  return 0
end
return 1
`)

type Limiter struct {
	client *redis.Client
	prefix string
}

func New(client *redis.Client, prefix string) *Limiter {
	return &Limiter{client: client, prefix: prefix}
}

func (l *Limiter) Allow(ctx context.Context, key string, limit int64, window time.Duration) (bool, error) {
	if key == "" || limit < 1 || window <= 0 {
		return false, fmt.Errorf("invalid rate limit configuration")
	}
	result, err := fixedWindowScript.Run(
		ctx,
		l.client,
		[]string{l.prefix + key},
		window.Milliseconds(),
		limit,
	).Int()
	if err != nil {
		return false, fmt.Errorf("rate limit check: %w", err)
	}
	return result == 1, nil
}
