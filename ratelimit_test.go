package main

import (
	"testing"
	"time"
)

func TestRateLimiter_CheckAndIncrement_NoLimits(t *testing.T) {
	rl := &RateLimiter{
		counters: make(map[string]*senderCounter),
	}

	quota := &Quota{PerHour: 0, PerDay: 0}
	allowed, hourCount, dayCount := rl.CheckAndIncrement("test@example.org", quota)

	if !allowed {
		t.Error("Expected message to be allowed when no limits are set")
	}
	if hourCount != 1 || dayCount != 1 {
		t.Errorf("Expected counts to be 1, got hour=%d, day=%d", hourCount, dayCount)
	}
}

func TestRateLimiter_CheckAndIncrement_NilQuota(t *testing.T) {
	rl := &RateLimiter{
		counters: make(map[string]*senderCounter),
	}

	allowed, hourCount, dayCount := rl.CheckAndIncrement("test@example.org", nil)

	if !allowed {
		t.Error("Expected message to be allowed when quota is nil")
	}
	if hourCount != 0 || dayCount != 0 {
		t.Errorf("Expected counts to be 0, got hour=%d, day=%d", hourCount, dayCount)
	}
}

func TestRateLimiter_CheckAndIncrement_HourlyLimit(t *testing.T) {
	rl := &RateLimiter{
		counters: make(map[string]*senderCounter),
	}

	quota := &Quota{PerHour: 3, PerDay: 100}
	sender := "test@example.org"

	// First 3 messages should be allowed
	for i := 0; i < 3; i++ {
		allowed, _, _ := rl.CheckAndIncrement(sender, quota)
		if !allowed {
			t.Errorf("Message %d should be allowed", i+1)
		}
	}

	// 4th message should be rejected
	allowed, hourCount, _ := rl.CheckAndIncrement(sender, quota)
	if allowed {
		t.Error("4th message should be rejected due to hourly limit")
	}
	if hourCount != 3 {
		t.Errorf("Expected hourCount to be 3, got %d", hourCount)
	}
}

func TestRateLimiter_CheckAndIncrement_DailyLimit(t *testing.T) {
	rl := &RateLimiter{
		counters: make(map[string]*senderCounter),
	}

	quota := &Quota{PerHour: 100, PerDay: 3}
	sender := "test@example.org"

	// First 3 messages should be allowed
	for i := 0; i < 3; i++ {
		allowed, _, _ := rl.CheckAndIncrement(sender, quota)
		if !allowed {
			t.Errorf("Message %d should be allowed", i+1)
		}
	}

	// 4th message should be rejected
	allowed, _, dayCount := rl.CheckAndIncrement(sender, quota)
	if allowed {
		t.Error("4th message should be rejected due to daily limit")
	}
	if dayCount != 3 {
		t.Errorf("Expected dayCount to be 3, got %d", dayCount)
	}
}

func TestRateLimiter_CheckAndIncrement_MultipleSenders(t *testing.T) {
	rl := &RateLimiter{
		counters: make(map[string]*senderCounter),
	}

	quota := &Quota{PerHour: 2, PerDay: 10}
	sender1 := "user1@example.org"
	sender2 := "user2@example.org"

	// Each sender should have their own quota
	for i := 0; i < 2; i++ {
		allowed1, _, _ := rl.CheckAndIncrement(sender1, quota)
		allowed2, _, _ := rl.CheckAndIncrement(sender2, quota)
		if !allowed1 || !allowed2 {
			t.Errorf("Message %d should be allowed for both senders", i+1)
		}
	}

	// Both should be at limit now
	allowed1, _, _ := rl.CheckAndIncrement(sender1, quota)
	allowed2, _, _ := rl.CheckAndIncrement(sender2, quota)
	if allowed1 || allowed2 {
		t.Error("3rd message should be rejected for both senders")
	}
}

func TestRateLimiter_GetCounts(t *testing.T) {
	rl := &RateLimiter{
		counters: make(map[string]*senderCounter),
	}

	sender := "test@example.org"
	quota := &Quota{PerHour: 100, PerDay: 100}

	// Send 5 messages
	for i := 0; i < 5; i++ {
		rl.CheckAndIncrement(sender, quota)
	}

	hourCount, dayCount := rl.GetCounts(sender)
	if hourCount != 5 || dayCount != 5 {
		t.Errorf("Expected counts to be 5, got hour=%d, day=%d", hourCount, dayCount)
	}
}

func TestRateLimiter_GetCounts_NonexistentSender(t *testing.T) {
	rl := &RateLimiter{
		counters: make(map[string]*senderCounter),
	}

	hourCount, dayCount := rl.GetCounts("nonexistent@example.org")
	if hourCount != 0 || dayCount != 0 {
		t.Errorf("Expected counts to be 0 for nonexistent sender, got hour=%d, day=%d", hourCount, dayCount)
	}
}

func TestRateLimiter_Cleanup(t *testing.T) {
	rl := &RateLimiter{
		counters: make(map[string]*senderCounter),
	}

	sender := "test@example.org"

	// Add an old timestamp manually
	rl.counters[sender] = &senderCounter{
		timestamps: []time.Time{
			time.Now().Add(-25 * time.Hour), // Older than 24 hours
			time.Now().Add(-1 * time.Hour),  // Within 24 hours
		},
	}

	rl.cleanup()

	counter := rl.counters[sender]
	if counter == nil {
		t.Fatal("Counter should still exist")
	}

	if len(counter.timestamps) != 1 {
		t.Errorf("Expected 1 timestamp after cleanup, got %d", len(counter.timestamps))
	}
}

func TestRateLimiter_Cleanup_RemovesEmptySender(t *testing.T) {
	rl := &RateLimiter{
		counters: make(map[string]*senderCounter),
	}

	sender := "test@example.org"

	// Add only old timestamps
	rl.counters[sender] = &senderCounter{
		timestamps: []time.Time{
			time.Now().Add(-25 * time.Hour),
			time.Now().Add(-26 * time.Hour),
		},
	}

	rl.cleanup()

	if _, exists := rl.counters[sender]; exists {
		t.Error("Sender with only old timestamps should be removed")
	}
}

func TestRateLimiter_SenderCount(t *testing.T) {
	rl := &RateLimiter{
		counters: make(map[string]*senderCounter),
	}

	quota := &Quota{PerHour: 100, PerDay: 100}

	rl.CheckAndIncrement("user1@example.org", quota)
	rl.CheckAndIncrement("user2@example.org", quota)
	rl.CheckAndIncrement("user3@example.org", quota)

	count := rl.SenderCount()
	if count != 3 {
		t.Errorf("Expected 3 tracked senders, got %d", count)
	}
}
