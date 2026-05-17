package usmarket

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	redis "github.com/redis/go-redis/v9"
)

var reserveRedisRateLimitSlotScript = redis.NewScript(`
local key = KEYS[1]
local interval = tonumber(ARGV[1])
local base_ttl = tonumber(ARGV[2])
local t = redis.call("TIME")
local now = tonumber(t[1]) * 1000 + math.floor(tonumber(t[2]) / 1000)
local next_at = tonumber(redis.call("GET", key) or "0")
if next_at < now then
  next_at = now
end
local wake_at = next_at
local stored_next = next_at + interval
local ttl = math.max(base_ttl, stored_next - now + base_ttl)
redis.call("SET", key, tostring(stored_next), "PX", ttl)
return wake_at
`)

var applyRedisRateLimitBackoffScript = redis.NewScript(`
local key = KEYS[1]
local cooldown = tonumber(ARGV[1])
local base_ttl = tonumber(ARGV[2])
local t = redis.call("TIME")
local now = tonumber(t[1]) * 1000 + math.floor(tonumber(t[2]) / 1000)
local next_at = tonumber(redis.call("GET", key) or "0")
local penalty_at = now + cooldown
if next_at < penalty_at then
  next_at = penalty_at
end
local ttl = math.max(base_ttl, next_at - now + base_ttl)
redis.call("SET", key, tostring(next_at), "PX", ttl)
return next_at
`)

type redisRequestLimiter struct {
	client   *redis.Client
	key      string
	interval time.Duration
	baseTTL  time.Duration
}

func newRedisRequestLimiter(ctx context.Context, provider string, qps int, cfg DistributedRateLimitConfig) (*redisRequestLimiter, error) {
	if qps <= 0 {
		return nil, nil
	}
	if strings.TrimSpace(cfg.Addr) == "" {
		return nil, fmt.Errorf("redis distributed limiter enabled but addr is empty")
	}
	client := redis.NewClient(&redis.Options{
		Addr:         cfg.Addr,
		Password:     cfg.Password,
		DB:           cfg.DB,
		DialTimeout:  cfg.DialTimeout,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
	})
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, err
	}
	interval := time.Second / time.Duration(qps)
	baseTTL := 2 * time.Second
	if candidate := 4 * interval; candidate > baseTTL {
		baseTTL = candidate
	}
	key := fmt.Sprintf("rate-limit:us-fundamentals:%s:qps:%d", strings.ToLower(strings.TrimSpace(provider)), qps)
	if prefix := strings.TrimSpace(cfg.KeyPrefix); prefix != "" {
		key = prefix + ":" + key
	}
	return &redisRequestLimiter{client: client, key: key, interval: interval, baseTTL: baseTTL}, nil
}

func (l *redisRequestLimiter) Wait(ctx context.Context) error {
	if l == nil {
		return nil
	}
	wakeAt, err := l.runScript(ctx, reserveRedisRateLimitSlotScript, l.interval)
	if err != nil {
		return err
	}
	delay := time.Until(wakeAt)
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (l *redisRequestLimiter) Backoff(ctx context.Context, cooldown time.Duration) error {
	if l == nil || cooldown <= 0 {
		return nil
	}
	_, err := l.runScript(ctx, applyRedisRateLimitBackoffScript, cooldown)
	return err
}

func (l *redisRequestLimiter) Close() error {
	if l == nil || l.client == nil {
		return nil
	}
	return l.client.Close()
}

func (l *redisRequestLimiter) runScript(ctx context.Context, script *redis.Script, delay time.Duration) (time.Time, error) {
	if l == nil {
		return time.Time{}, nil
	}
	delayMS := delay.Milliseconds()
	if delayMS < 0 {
		delayMS = 0
	}
	baseTTLMS := l.baseTTL.Milliseconds()
	if baseTTLMS <= 0 {
		baseTTLMS = 1000
	}
	result, err := script.Run(ctx, l.client, []string{l.key}, delayMS, baseTTLMS).Result()
	if err != nil {
		return time.Time{}, err
	}
	whenMS, err := redisValueToInt64(result)
	if err != nil {
		return time.Time{}, err
	}
	return time.UnixMilli(whenMS), nil
}

func redisValueToInt64(value any) (int64, error) {
	switch typed := value.(type) {
	case int64:
		return typed, nil
	case string:
		return strconv.ParseInt(typed, 10, 64)
	case []byte:
		return strconv.ParseInt(string(typed), 10, 64)
	default:
		return 0, fmt.Errorf("unexpected redis limiter reply type %T", value)
	}
}
