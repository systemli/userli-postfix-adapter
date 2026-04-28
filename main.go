package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

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

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	userli := NewUserli(config.UserliToken, config.UserliBaseURL, WithDelimiter(config.PostfixRecipientDelimiter))

	rateLimiter, err := NewRateLimiter(ctx, config.RedisURL, logger.Named("ratelimit"))
	if err != nil {
		logger.Fatal("Failed to initialize rate limiter", zap.Error(err))
	}
	defer func() { _ = rateLimiter.Close() }()

	prober := NewTLSProber(5*time.Second, config.TLSPolicyEhloHostname, logger.Named("tlsprobe"))
	tlsPolicy, err := NewTLSPolicyHandler(ctx, config.RedisURL, prober, config, logger.Named("tlspolicy"))
	if err != nil {
		logger.Fatal("Failed to initialize TLS policy handler", zap.Error(err))
	}
	defer func() { _ = tlsPolicy.Close() }()

	lookupServer := NewLookupServer(userli, tlsPolicy, logger.Named("lookup"))
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
