package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var logger *zap.Logger

func init() {
	// Initialize logger with default config
	logLevel := "info"
	if os.Getenv("LOG_LEVEL") != "" {
		logLevel = os.Getenv("LOG_LEVEL")
	}

	level, err := zapcore.ParseLevel(logLevel)
	if err != nil {
		log.Fatal(err)
	}

	logger = zap.New(zapcore.NewCore(
		zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()),
		zapcore.Lock(os.Stdout),
		level,
	))
}

func main() {
	defer func() { _ = logger.Sync() }()

	config, err := NewConfig()
	if err != nil {
		logger.Fatal("Failed to load configuration", zap.Error(err))
	}

	userli := NewUserli(config.UserliToken, config.UserliBaseURL, WithDelimiter(config.PostfixRecipientDelimiter))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	redisClient, err := newRedisClient(ctx, config.RedisURL, logger.Named("redis"))
	if err != nil {
		logger.Fatal("Failed to initialize Redis client", zap.Error(err))
	}
	defer func() { _ = redisClient.Close() }()

	rateLimiter := NewRateLimiter(redisClient, logger.Named("ratelimit"))

	var lookupCache *LookupCache
	if config.LookupCacheTTL > 0 {
		lookupCache = NewLookupCache(redisClient, config.LookupCacheTTL, logger.Named("lookupcache"))
	}

	lookupServer := NewLookupServer(userli, lookupCache, logger.Named("lookup"))
	policyServer := NewPolicyServer(userli, rateLimiter, config.RateLimitMessage, logger.Named("policy"))

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		StartMetricsServer(ctx, config.MetricsListenAddr, userli)
	}()

	wg.Add(1)
	go StartLookupServer(ctx, &wg, config.SocketmapListenAddr, lookupServer)

	wg.Add(1)
	go StartPolicyServer(ctx, &wg, config.PolicyListenAddr, policyServer)

	wg.Wait()
	logger.Info("All servers stopped")
}
