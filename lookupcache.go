package main

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// lookupCacheKeyPrefix is the static Redis key prefix for cached lookup
// responses. Each key combines the prefix, the socketmap name, and the
// looked-up key (e.g. "userli:lookup:alias:user@example.com").
const lookupCacheKeyPrefix = "userli:lookup:"

// LookupCache caches successful socketmap lookup responses in Redis to reduce
// load on the upstream Userli API. Only "OK" responses are cached so newly
// created entries become visible without delay; "NOTFOUND", "TEMP" and "PERM"
// responses are never written.
//
// All Redis errors fail open: Get returns a miss, Set is a no-op. Callers
// always proceed to (or return from) the upstream handler unaffected.
type LookupCache struct {
	client *redis.Client
	ttl    time.Duration
	logger *zap.Logger
}

// NewLookupCache wraps an existing Redis client with the lookup-cache policy.
// A non-positive ttl is invalid and indicates the caller should not construct
// a cache at all (main.go passes nil instead).
func NewLookupCache(client *redis.Client, ttl time.Duration, logger *zap.Logger) *LookupCache {
	return &LookupCache{
		client: client,
		ttl:    ttl,
		logger: logger,
	}
}

// Get returns the cached response for (mapName, key) or (nil, false) on miss.
// Redis errors are treated as misses so the caller falls through to the API.
func (c *LookupCache) Get(ctx context.Context, mapName, key string) (*SocketmapResponse, bool) {
	if c == nil {
		return nil, false
	}

	val, err := c.client.Get(ctx, c.keyFor(mapName, key)).Result()
	if errors.Is(err, redis.Nil) {
		lookupCacheTotal.WithLabelValues(mapName, "miss").Inc()
		return nil, false
	}
	if err != nil {
		c.logger.Warn("Lookup cache get failed", zap.String("map", mapName), zap.Error(err))
		lookupCacheTotal.WithLabelValues(mapName, "error").Inc()
		return nil, false
	}

	lookupCacheTotal.WithLabelValues(mapName, "hit").Inc()
	return parseCachedResponse(val), true
}

// Set stores the response in Redis when its status is OK. Other statuses are
// intentionally ignored so the caching policy lives in one place.
func (c *LookupCache) Set(ctx context.Context, mapName, key string, response *SocketmapResponse) {
	if c == nil || response == nil || response.Status != "OK" {
		return
	}

	if err := c.client.Set(ctx, c.keyFor(mapName, key), response.String(), c.ttl).Err(); err != nil {
		c.logger.Warn("Lookup cache set failed", zap.String("map", mapName), zap.Error(err))
		lookupCacheTotal.WithLabelValues(mapName, "error").Inc()
	}
}

func (c *LookupCache) keyFor(mapName, key string) string {
	return lookupCacheKeyPrefix + mapName + ":" + key
}

// parseCachedResponse splits the cached "<status>" or "<status> <data>" form
// back into a SocketmapResponse. The cache only stores OK responses, but the
// parser is forgiving in case a stale value with different shape is ever read.
func parseCachedResponse(s string) *SocketmapResponse {
	parts := strings.SplitN(s, " ", 2)
	if len(parts) == 1 {
		return &SocketmapResponse{Status: parts[0]}
	}
	return &SocketmapResponse{Status: parts[0], Data: parts[1]}
}
