package main

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// newRedisClient parses the Redis URL, opens a client, and pings the server.
// A failed ping is logged as a warning but does not abort startup (fail-open).
func newRedisClient(ctx context.Context, url string, logger *zap.Logger) (*redis.Client, error) {
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

	return client, nil
}
