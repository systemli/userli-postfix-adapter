package main

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

func TestNewRateLimiter_Success(t *testing.T) {
	mr := miniredis.RunT(t)
	url := "redis://" + mr.Addr()

	rl, err := NewRateLimiter(context.Background(), url, zap.NewNop())
	if err != nil {
		t.Fatalf("NewRateLimiter returned error: %v", err)
	}
	if rl == nil {
		t.Fatal("NewRateLimiter returned nil limiter")
	}
	t.Cleanup(func() { _ = rl.Close() })

	allowed, _, _ := rl.CheckAndIncrement(context.Background(), "smoke@example.org", &Quota{PerHour: 1, PerDay: 1})
	if !allowed {
		t.Error("Expected first message to be allowed against a fresh Redis")
	}
}

func TestNewRateLimiter_InvalidURL(t *testing.T) {
	rl, err := NewRateLimiter(context.Background(), "://not-a-url", zap.NewNop())
	if err == nil {
		t.Fatal("Expected error for malformed URL")
	}
	if rl != nil {
		t.Errorf("Expected nil limiter on parse error, got %v", rl)
	}
}

func TestNewRateLimiter_PingFailureFailsOpen(t *testing.T) {
	// Point at an address that nothing is listening on so the PING fails fast.
	// Constructor must still return a usable limiter (fail-open at startup).
	// Tight dial timeout keeps the test fast despite go-redis pool retries.
	url := "redis://127.0.0.1:1?dial_timeout=100ms"

	rl, err := NewRateLimiter(context.Background(), url, zap.NewNop())
	if err != nil {
		t.Fatalf("Expected no error on PING failure, got %v", err)
	}
	if rl == nil {
		t.Fatal("Expected non-nil limiter even when PING fails")
	}
	t.Cleanup(func() { _ = rl.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	allowed, hourCount, dayCount := rl.CheckAndIncrement(ctx, "ping@example.org", &Quota{PerHour: 1, PerDay: 1})
	if !allowed {
		t.Error("Expected fail-open when Redis is unreachable")
	}
	if hourCount != 0 || dayCount != 0 {
		t.Errorf("Expected zero counts on fail-open, got hour=%d, day=%d", hourCount, dayCount)
	}
}

func newTestRateLimiter(t *testing.T) (*RateLimiter, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	rl := &RateLimiter{
		client: client,
		script: redis.NewScript(rateLimitScript),
		logger: zap.NewNop(),
	}
	t.Cleanup(func() { _ = client.Close() })
	return rl, mr
}

func TestRateLimiter_CheckAndIncrement_NoLimits(t *testing.T) {
	rl, _ := newTestRateLimiter(t)
	ctx := context.Background()

	quota := &Quota{PerHour: 0, PerDay: 0}
	allowed, hourCount, dayCount := rl.CheckAndIncrement(ctx, "test@example.org", quota)

	if !allowed {
		t.Error("Expected message to be allowed when no limits are set")
	}
	if hourCount != 1 || dayCount != 1 {
		t.Errorf("Expected counts to be 1, got hour=%d, day=%d", hourCount, dayCount)
	}
}

func TestRateLimiter_CheckAndIncrement_NilQuota(t *testing.T) {
	rl, _ := newTestRateLimiter(t)
	ctx := context.Background()

	allowed, hourCount, dayCount := rl.CheckAndIncrement(ctx, "test@example.org", nil)

	if !allowed {
		t.Error("Expected message to be allowed when quota is nil")
	}
	if hourCount != 0 || dayCount != 0 {
		t.Errorf("Expected counts to be 0, got hour=%d, day=%d", hourCount, dayCount)
	}
}

func TestRateLimiter_CheckAndIncrement_HourlyLimit(t *testing.T) {
	rl, _ := newTestRateLimiter(t)
	ctx := context.Background()

	quota := &Quota{PerHour: 3, PerDay: 100}
	sender := "test@example.org"

	for i := 0; i < 3; i++ {
		allowed, _, _ := rl.CheckAndIncrement(ctx, sender, quota)
		if !allowed {
			t.Errorf("Message %d should be allowed", i+1)
		}
	}

	allowed, hourCount, _ := rl.CheckAndIncrement(ctx, sender, quota)
	if allowed {
		t.Error("4th message should be rejected due to hourly limit")
	}
	if hourCount != 3 {
		t.Errorf("Expected hourCount to be 3, got %d", hourCount)
	}
}

func TestRateLimiter_CheckAndIncrement_DailyLimit(t *testing.T) {
	rl, _ := newTestRateLimiter(t)
	ctx := context.Background()

	quota := &Quota{PerHour: 100, PerDay: 3}
	sender := "test@example.org"

	for i := 0; i < 3; i++ {
		allowed, _, _ := rl.CheckAndIncrement(ctx, sender, quota)
		if !allowed {
			t.Errorf("Message %d should be allowed", i+1)
		}
	}

	allowed, _, dayCount := rl.CheckAndIncrement(ctx, sender, quota)
	if allowed {
		t.Error("4th message should be rejected due to daily limit")
	}
	if dayCount != 3 {
		t.Errorf("Expected dayCount to be 3, got %d", dayCount)
	}
}

func TestRateLimiter_CheckAndIncrement_MultipleSenders(t *testing.T) {
	rl, _ := newTestRateLimiter(t)
	ctx := context.Background()

	quota := &Quota{PerHour: 2, PerDay: 10}
	sender1 := "user1@example.org"
	sender2 := "user2@example.org"

	for i := 0; i < 2; i++ {
		allowed1, _, _ := rl.CheckAndIncrement(ctx, sender1, quota)
		allowed2, _, _ := rl.CheckAndIncrement(ctx, sender2, quota)
		if !allowed1 || !allowed2 {
			t.Errorf("Message %d should be allowed for both senders", i+1)
		}
	}

	allowed1, _, _ := rl.CheckAndIncrement(ctx, sender1, quota)
	allowed2, _, _ := rl.CheckAndIncrement(ctx, sender2, quota)
	if allowed1 || allowed2 {
		t.Error("3rd message should be rejected for both senders")
	}
}

func TestRateLimiter_GetCounts(t *testing.T) {
	rl, _ := newTestRateLimiter(t)
	ctx := context.Background()

	sender := "test@example.org"
	quota := &Quota{PerHour: 100, PerDay: 100}

	for i := 0; i < 5; i++ {
		rl.CheckAndIncrement(ctx, sender, quota)
	}

	hourCount, dayCount := rl.GetCounts(ctx, sender)
	if hourCount != 5 || dayCount != 5 {
		t.Errorf("Expected counts to be 5, got hour=%d, day=%d", hourCount, dayCount)
	}
}

func TestRateLimiter_GetCounts_NonexistentSender(t *testing.T) {
	rl, _ := newTestRateLimiter(t)
	ctx := context.Background()

	hourCount, dayCount := rl.GetCounts(ctx, "nonexistent@example.org")
	if hourCount != 0 || dayCount != 0 {
		t.Errorf("Expected counts to be 0 for nonexistent sender, got hour=%d, day=%d", hourCount, dayCount)
	}
}

// TestRateLimiter_KeyTTL ensures the rate-limit key is set with an EXPIRE
// so quiet senders get cleaned up automatically (replacing the old cleanup goroutine).
func TestRateLimiter_KeyTTL(t *testing.T) {
	rl, mr := newTestRateLimiter(t)
	ctx := context.Background()

	rl.CheckAndIncrement(ctx, "ttl@example.org", &Quota{PerHour: 10, PerDay: 100})

	key := keyFor("ttl@example.org")
	ttl := mr.TTL(key)
	if ttl <= 0 {
		t.Fatalf("Expected positive TTL on rate-limit key, got %v", ttl)
	}
	if ttl > rateLimitTTL+time.Minute {
		t.Errorf("Expected TTL <= %v, got %v", rateLimitTTL, ttl)
	}

	// Advance the mock clock past the TTL — the key must be gone.
	mr.FastForward(rateLimitTTL + time.Minute)
	if mr.Exists(key) {
		t.Error("Expected key to be expired after FastForward past TTL")
	}
}

// TestRateLimiter_RejectedDoesNotIncrement guards the conditional-ZADD invariant
// in the Lua script: a rejected message must not push the counter above the limit.
func TestRateLimiter_RejectedDoesNotIncrement(t *testing.T) {
	rl, _ := newTestRateLimiter(t)
	ctx := context.Background()

	quota := &Quota{PerHour: 2, PerDay: 100}
	sender := "reject@example.org"

	for i := 0; i < 2; i++ {
		rl.CheckAndIncrement(ctx, sender, quota)
	}

	for i := 0; i < 5; i++ {
		allowed, _, _ := rl.CheckAndIncrement(ctx, sender, quota)
		if allowed {
			t.Errorf("Attempt %d above limit should be rejected", i+1)
		}
	}

	hourCount, _ := rl.GetCounts(ctx, sender)
	if hourCount != 2 {
		t.Errorf("Expected hourCount to stay at 2 after rejected attempts, got %d", hourCount)
	}
}

// TestRateLimiter_FailOpen verifies that Redis errors at runtime allow the message.
func TestRateLimiter_FailOpen(t *testing.T) {
	rl, mr := newTestRateLimiter(t)
	ctx := context.Background()

	mr.Close() // simulate Redis going down

	allowed, hourCount, dayCount := rl.CheckAndIncrement(ctx, "fail@example.org", &Quota{PerHour: 1, PerDay: 1})
	if !allowed {
		t.Error("Expected fail-open: message should be allowed when Redis is unreachable")
	}
	if hourCount != 0 || dayCount != 0 {
		t.Errorf("Expected zero counts on fail-open, got hour=%d, day=%d", hourCount, dayCount)
	}
}
