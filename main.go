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
	lookupServer := NewLookupServer(userli)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Create rate limiter for policy server
	rateLimiter := NewRateLimiter()
	policyServer := NewPolicyServer(userli, rateLimiter)

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		StartMetricsServer(ctx, config.MetricsListenAddr, userli, rateLimiter)
	}()

	wg.Add(1)
	go StartLookupServer(ctx, &wg, config.SocketmapListenAddr, lookupServer)

	wg.Add(1)
	go StartPolicyServer(ctx, &wg, config.PolicyListenAddr, policyServer)

	wg.Wait()
	logger.Info("All servers stopped")
}
