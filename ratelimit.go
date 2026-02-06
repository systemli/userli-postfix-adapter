package main

import (
	"context"
	"sync"
	"time"
)

// RateLimiter tracks sending rates per sender using a sliding window approach.
// It stores timestamps of sent messages and counts them within time windows.
type RateLimiter struct {
	mu       sync.RWMutex
	counters map[string]*senderCounter
}

// senderCounter tracks message timestamps for a single sender
type senderCounter struct {
	timestamps []time.Time
	mu         sync.Mutex
}

// NewRateLimiter creates a new RateLimiter instance
func NewRateLimiter(ctx context.Context) *RateLimiter {
	rl := &RateLimiter{
		counters: make(map[string]*senderCounter),
	}

	// Start background cleanup goroutine
	go rl.cleanupLoop(ctx)

	return rl
}

// CheckAndIncrement checks if the sender is within quota limits and increments the counter if allowed.
// Returns true if the message should be allowed, false if rate limited.
// If quota limits are 0, they are treated as unlimited.
func (rl *RateLimiter) CheckAndIncrement(sender string, quota *Quota) (allowed bool, hourCount, dayCount int) {
	if quota == nil {
		return true, 0, 0
	}

	// Get or create counter for this sender
	rl.mu.Lock()
	counter, exists := rl.counters[sender]
	if !exists {
		counter = &senderCounter{
			timestamps: make([]time.Time, 0),
		}
		rl.counters[sender] = counter
	}
	rl.mu.Unlock()

	counter.mu.Lock()
	defer counter.mu.Unlock()

	now := time.Now()
	hourAgo := now.Add(-time.Hour)
	dayAgo := now.Add(-24 * time.Hour)

	// Clean old timestamps and count current usage
	validTimestamps := make([]time.Time, 0, len(counter.timestamps))
	hourCount = 0
	dayCount = 0

	for _, ts := range counter.timestamps {
		if ts.After(dayAgo) {
			validTimestamps = append(validTimestamps, ts)
			dayCount++
			if ts.After(hourAgo) {
				hourCount++
			}
		}
	}

	counter.timestamps = validTimestamps

	// Check limits (0 means unlimited)
	if quota.PerHour > 0 && hourCount >= quota.PerHour {
		return false, hourCount, dayCount
	}
	if quota.PerDay > 0 && dayCount >= quota.PerDay {
		return false, hourCount, dayCount
	}

	// Add new timestamp
	counter.timestamps = append(counter.timestamps, now)
	hourCount++
	dayCount++

	return true, hourCount, dayCount
}

// GetCounts returns the current hour and day counts for a sender without incrementing
func (rl *RateLimiter) GetCounts(sender string) (hourCount, dayCount int) {
	rl.mu.RLock()
	counter, exists := rl.counters[sender]
	rl.mu.RUnlock()

	if !exists {
		return 0, 0
	}

	counter.mu.Lock()
	defer counter.mu.Unlock()

	now := time.Now()
	hourAgo := now.Add(-time.Hour)
	dayAgo := now.Add(-24 * time.Hour)

	for _, ts := range counter.timestamps {
		if ts.After(dayAgo) {
			dayCount++
			if ts.After(hourAgo) {
				hourCount++
			}
		}
	}

	return hourCount, dayCount
}

// cleanupLoop periodically removes old entries to prevent memory leaks
func (rl *RateLimiter) cleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			rl.cleanup()
		}
	}
}

// cleanup removes entries older than 24 hours
func (rl *RateLimiter) cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	dayAgo := time.Now().Add(-24 * time.Hour)

	// Collect senders to delete after iteration
	var toDelete []string

	for sender, counter := range rl.counters {
		counter.mu.Lock()

		// Remove old timestamps
		validTimestamps := make([]time.Time, 0, len(counter.timestamps))
		for _, ts := range counter.timestamps {
			if ts.After(dayAgo) {
				validTimestamps = append(validTimestamps, ts)
			}
		}
		counter.timestamps = validTimestamps

		// Mark sender for deletion if no recent activity
		if len(counter.timestamps) == 0 {
			toDelete = append(toDelete, sender)
		}

		counter.mu.Unlock()
	}

	// Delete after iteration to avoid modifying map during range
	for _, sender := range toDelete {
		delete(rl.counters, sender)
	}
}

// SenderCount returns the number of tracked senders (for metrics)
func (rl *RateLimiter) SenderCount() int {
	rl.mu.RLock()
	defer rl.mu.RUnlock()
	return len(rl.counters)
}
