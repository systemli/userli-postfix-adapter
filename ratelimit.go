package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// rateLimitTTL is set slightly above 24h so a quiet sender's key naturally
// evicts but no still-relevant timestamp is dropped early.
const rateLimitTTL = 25 * time.Hour

// rateLimitKeyPrefix is the static Redis key prefix for rate-limit sorted sets.
const rateLimitKeyPrefix = "userli:ratelimit:sender:"

// rateLimitScript implements the sliding-window check atomically.
// ARGV: hourLimit, dayLimit, now (unix nano), hourAgo, dayAgo, member suffix, TTL seconds.
// Returns {allowed (1/0), hourCount, dayCount}. The new entry is only added when allowed.
const rateLimitScript = `
local key = KEYS[1]
local hour_limit = tonumber(ARGV[1])
local day_limit = tonumber(ARGV[2])
local now = ARGV[3]
local hour_ago = ARGV[4]
local day_ago = ARGV[5]
local suffix = ARGV[6]
local ttl = tonumber(ARGV[7])

redis.call("ZREMRANGEBYSCORE", key, "-inf", "(" .. day_ago)

local day_count = tonumber(redis.call("ZCARD", key))
local hour_count = tonumber(redis.call("ZCOUNT", key, "(" .. hour_ago, "+inf"))

if hour_limit > 0 and hour_count >= hour_limit then
    return {0, hour_count, day_count}
end
if day_limit > 0 and day_count >= day_limit then
    return {0, hour_count, day_count}
end

redis.call("ZADD", key, now, now .. ":" .. suffix)
redis.call("EXPIRE", key, ttl)

return {1, hour_count + 1, day_count + 1}
`

// RateLimiter enforces per-sender sliding-window quotas using Redis as backing store.
// State persists across restarts; Redis errors fail open (the message is allowed).
type RateLimiter struct {
	client *redis.Client
	script *redis.Script
	logger *zap.Logger
}

// NewRateLimiter parses the Redis URL, opens a client and pings the server.
// A failed ping is logged as a warning but does not abort startup (fail-open).
func NewRateLimiter(ctx context.Context, url string, logger *zap.Logger) (*RateLimiter, error) {
	opts, err := redis.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("parse redis url: %w", err)
	}

	client := redis.NewClient(opts)

	pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		logger.Warn("Redis ping failed at startup, continuing fail-open",
			zap.String("addr", opts.Addr), zap.Error(err))
	} else {
		logger.Info("Connected to Redis", zap.String("addr", opts.Addr))
	}

	return &RateLimiter{
		client: client,
		script: redis.NewScript(rateLimitScript),
		logger: logger,
	}, nil
}

// CheckAndIncrement runs the sliding-window check atomically in Redis.
// Returns allowed=true with zero counts when quota is nil (no limits configured).
// Redis errors return allowed=true (fail-open) and increment the backend-error counter.
func (rl *RateLimiter) CheckAndIncrement(ctx context.Context, sender string, quota *Quota) (allowed bool, hourCount, dayCount int) {
	if quota == nil {
		return true, 0, 0
	}

	now := time.Now().UnixNano()
	hourAgo := now - int64(time.Hour)
	dayAgo := now - int64(24*time.Hour)

	suffix, err := randomSuffix()
	if err != nil {
		rl.logger.Warn("Failed to generate suffix, allowing message", zap.Error(err))
		rateLimitBackendErrors.WithLabelValues("check").Inc()
		return true, 0, 0
	}

	res, err := rl.script.Run(ctx, rl.client,
		[]string{keyFor(sender)},
		quota.PerHour, quota.PerDay,
		now, hourAgo, dayAgo,
		suffix, int64(rateLimitTTL.Seconds()),
	).Result()
	if err != nil {
		rl.logger.Warn("Rate limit check failed, allowing message", zap.Error(err))
		rateLimitBackendErrors.WithLabelValues("check").Inc()
		return true, 0, 0
	}

	values, ok := res.([]any)
	if !ok || len(values) != 3 {
		rl.logger.Warn("Unexpected script result, allowing message")
		rateLimitBackendErrors.WithLabelValues("check").Inc()
		return true, 0, 0
	}

	allowedRaw, _ := values[0].(int64)
	hourRaw, _ := values[1].(int64)
	dayRaw, _ := values[2].(int64)

	return allowedRaw == 1, int(hourRaw), int(dayRaw)
}

// GetCounts returns the current hour and day counts for a sender without incrementing.
// Redis errors return (0, 0) and increment the backend-error counter.
func (rl *RateLimiter) GetCounts(ctx context.Context, sender string) (hourCount, dayCount int) {
	key := keyFor(sender)
	now := time.Now().UnixNano()
	hourAgo := fmt.Sprintf("(%d", now-int64(time.Hour))
	dayAgo := fmt.Sprintf("(%d", now-int64(24*time.Hour))

	pipe := rl.client.Pipeline()
	pipe.ZRemRangeByScore(ctx, key, "-inf", dayAgo)
	dayCmd := pipe.ZCard(ctx, key)
	hourCmd := pipe.ZCount(ctx, key, hourAgo, "+inf")

	if _, err := pipe.Exec(ctx); err != nil {
		rl.logger.Warn("GetCounts redis error", zap.String("sender", sender), zap.Error(err))
		rateLimitBackendErrors.WithLabelValues("get_counts").Inc()
		return 0, 0
	}

	return int(hourCmd.Val()), int(dayCmd.Val())
}

// Close shuts down the Redis client.
func (rl *RateLimiter) Close() error {
	return rl.client.Close()
}

func keyFor(sender string) string {
	return rateLimitKeyPrefix + sender
}

func randomSuffix() (string, error) {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
