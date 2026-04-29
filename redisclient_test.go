package main

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"go.uber.org/zap"
)

func TestNewRedisClient_Success(t *testing.T) {
	mr := miniredis.RunT(t)

	client, err := newRedisClient(context.Background(), "redis://"+mr.Addr(), zap.NewNop())
	if err != nil {
		t.Fatalf("newRedisClient returned error: %v", err)
	}
	if client == nil {
		t.Fatal("newRedisClient returned nil client")
	}
	t.Cleanup(func() { _ = client.Close() })

	if err := client.Ping(context.Background()).Err(); err != nil {
		t.Errorf("Expected ping to succeed, got %v", err)
	}
}

func TestNewRedisClient_InvalidURL(t *testing.T) {
	client, err := newRedisClient(context.Background(), "://not-a-url", zap.NewNop())
	if err == nil {
		t.Fatal("Expected error for malformed URL")
	}
	if client != nil {
		t.Errorf("Expected nil client on parse error, got %v", client)
	}
}

// TestNewRedisClient_PingFailureFailsOpen verifies that a Redis server that
// is unreachable at startup does not abort the constructor — the client is
// returned anyway so the rest of the app can boot and other components can
// fail open at runtime.
func TestNewRedisClient_PingFailureFailsOpen(t *testing.T) {
	// Tight dial timeout keeps the test fast despite go-redis pool retries.
	client, err := newRedisClient(context.Background(),
		"redis://127.0.0.1:1?dial_timeout=100ms", zap.NewNop())
	if err != nil {
		t.Fatalf("Expected no error on PING failure, got %v", err)
	}
	if client == nil {
		t.Fatal("Expected non-nil client even when PING fails")
	}
	t.Cleanup(func() { _ = client.Close() })
}
