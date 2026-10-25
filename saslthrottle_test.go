package main

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func newTestSASLThrottle(t *testing.T, delay time.Duration, maxFailures int) (*SASLThrottle, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return NewSASLThrottle(client, delay, maxFailures, time.Minute, zap.NewNop()), mr
}

func TestSASLThrottle_BlocksAfterMaxFailures(t *testing.T) {
	throttle, _ := newTestSASLThrottle(t, 0, 3)
	ctx := context.Background()

	for range 2 {
		throttle.RecordFailure(ctx, "192.0.2.1")
	}
	assert.False(t, throttle.Blocked(ctx, "192.0.2.1"))

	throttle.RecordFailure(ctx, "192.0.2.1")
	assert.True(t, throttle.Blocked(ctx, "192.0.2.1"))
	assert.False(t, throttle.Blocked(ctx, "192.0.2.2"), "other clients must not be affected")
}

func TestSASLThrottle_WindowExpires(t *testing.T) {
	throttle, mr := newTestSASLThrottle(t, 0, 1)
	ctx := context.Background()

	throttle.RecordFailure(ctx, "192.0.2.1")
	assert.True(t, throttle.Blocked(ctx, "192.0.2.1"))

	mr.FastForward(time.Minute + time.Second)
	assert.False(t, throttle.Blocked(ctx, "192.0.2.1"))
}

func TestSASLThrottle_GroupsIPv6By64(t *testing.T) {
	throttle, _ := newTestSASLThrottle(t, 0, 1)
	ctx := context.Background()

	throttle.RecordFailure(ctx, "2001:db8:1:2::1")
	assert.True(t, throttle.Blocked(ctx, "2001:db8:1:2::ffff"))
	assert.False(t, throttle.Blocked(ctx, "2001:db8:1:3::1"))
}

func TestSASLThrottle_IgnoresInvalidIP(t *testing.T) {
	throttle, mr := newTestSASLThrottle(t, 0, 1)
	ctx := context.Background()

	throttle.RecordFailure(ctx, "")
	throttle.RecordFailure(ctx, "unknown")
	assert.False(t, throttle.Blocked(ctx, ""))
	assert.Empty(t, mr.Keys())
}

func TestSASLThrottle_DisabledLimit(t *testing.T) {
	throttle, mr := newTestSASLThrottle(t, 0, 0)
	ctx := context.Background()

	throttle.RecordFailure(ctx, "192.0.2.1")
	assert.False(t, throttle.Blocked(ctx, "192.0.2.1"))
	assert.Empty(t, mr.Keys())
}

func TestSASLThrottle_FailsOpenOnRedisError(t *testing.T) {
	throttle, mr := newTestSASLThrottle(t, 0, 1)
	ctx := context.Background()

	throttle.RecordFailure(ctx, "192.0.2.1")
	mr.Close()
	assert.False(t, throttle.Blocked(ctx, "192.0.2.1"))
}

func TestSASLThrottle_Delay(t *testing.T) {
	throttle, _ := newTestSASLThrottle(t, 50*time.Millisecond, 0)

	start := time.Now()
	throttle.Delay(context.Background())
	assert.GreaterOrEqual(t, time.Since(start), 50*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start = time.Now()
	throttle.Delay(ctx)
	assert.Less(t, time.Since(start), 50*time.Millisecond, "delay must stop when ctx is done")
}

func TestSASLThrottle_Nil(t *testing.T) {
	var throttle *SASLThrottle
	ctx := context.Background()

	throttle.RecordFailure(ctx, "192.0.2.1")
	throttle.Delay(ctx)
	assert.False(t, throttle.Blocked(ctx, "192.0.2.1"))
}
