package ratelimit

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// tokenBucketScript performs an atomic token-bucket check. Doing the
// read-refill-write in a single Lua script means concurrent requests from
// many gateway instances can share one bucket without a race.
//
// KEYS[1] = bucket key
// ARGV    = rate (tokens/sec), burst (capacity), now (unix seconds, float)
// returns { allowed(0|1), remaining(floor), retry_after_ms }
const tokenBucketScript = `
local key    = KEYS[1]
local rate   = tonumber(ARGV[1])
local burst  = tonumber(ARGV[2])
local now    = tonumber(ARGV[3])

local data   = redis.call('HMGET', key, 'tokens', 'ts')
local tokens = tonumber(data[1])
local ts     = tonumber(data[2])
if tokens == nil then
  tokens = burst
  ts = now
end

local delta = math.max(0, now - ts)
tokens = math.min(burst, tokens + delta * rate)

local allowed = 0
local retry_ms = 0
if tokens >= 1 then
  tokens = tokens - 1
  allowed = 1
else
  retry_ms = math.ceil((1 - tokens) / rate * 1000)
end

redis.call('HSET', key, 'tokens', tokens, 'ts', now)
redis.call('EXPIRE', key, math.ceil(burst / rate) + 1)

return { allowed, math.floor(tokens), retry_ms }
`

// RedisLimiter is a distributed token-bucket limiter backed by Redis.
type RedisLimiter struct {
	client *redis.Client
	script *redis.Script
	now    func() time.Time
}

// NewRedisLimiter connects to Redis at addr and verifies the connection.
func NewRedisLimiter(ctx context.Context, addr string) (*RedisLimiter, error) {
	client := redis.NewClient(&redis.Options{Addr: addr})
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("connect to redis at %s: %w", addr, err)
	}
	return &RedisLimiter{
		client: client,
		script: redis.NewScript(tokenBucketScript),
		now:    time.Now,
	}, nil
}

// Allow runs the token-bucket Lua script atomically for key.
func (l *RedisLimiter) Allow(ctx context.Context, key string, rate float64, burst int) (Result, error) {
	now := float64(l.now().UnixNano()) / float64(time.Second)
	vals, err := l.script.Run(ctx, l.client, []string{"ratelimit:" + key}, rate, burst, now).Int64Slice()
	if err != nil {
		return Result{}, fmt.Errorf("run token-bucket script: %w", err)
	}
	if len(vals) != 3 {
		return Result{}, fmt.Errorf("token-bucket script returned %d values, want 3", len(vals))
	}
	return Result{
		Allowed:    vals[0] == 1,
		Limit:      burst,
		Remaining:  int(vals[1]),
		RetryAfter: time.Duration(vals[2]) * time.Millisecond,
	}, nil
}

// Close releases the Redis connection.
func (l *RedisLimiter) Close() error { return l.client.Close() }
