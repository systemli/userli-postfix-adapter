package main

import (
	"context"
	"net"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// saslThrottleKeyPrefix is the static Redis key prefix for per-client
// SASL failure counters.
const saslThrottleKeyPrefix = "userli:sasl:failures:"

// saslFailureScript increments the failure counter and starts the window
// on the first failure, so the counter resets once the window has passed
// without new failures being able to extend it.
const saslFailureScript = `
local count = redis.call("INCR", KEYS[1])
if count == 1 then
    redis.call("EXPIRE", KEYS[1], tonumber(ARGV[1]))
end
return count
`

// SASLThrottle slows down and limits failed SASL authentications per
// client IP, similar to Dovecot's auth_failure_delay and auth penalty.
//
// Throttling is keyed on the client IP only, never on the username:
// a per-user lockout would let anyone lock a victim out of their account.
// Redis errors fail open (the attempt is allowed).
type SASLThrottle struct {
	client *redis.Client
	script *redis.Script
	logger *zap.Logger

	// failureDelay is applied before every FAIL caused by bad credentials
	// or throttling.
	failureDelay time.Duration
	// maxFailures is the number of failed attempts per client IP within
	// window after which attempts are rejected without asking the API.
	// Zero disables the limit.
	maxFailures int
	window      time.Duration
}

// NewSASLThrottle wraps an existing Redis client. Lifecycle of the client
// is owned by the caller.
func NewSASLThrottle(client *redis.Client, failureDelay time.Duration, maxFailures int, window time.Duration, logger *zap.Logger) *SASLThrottle {
	return &SASLThrottle{
		client:       client,
		script:       redis.NewScript(saslFailureScript),
		logger:       logger,
		failureDelay: failureDelay,
		maxFailures:  maxFailures,
		window:       window,
	}
}

// Blocked reports whether the client IP exceeded the failure limit.
func (t *SASLThrottle) Blocked(ctx context.Context, clientIP string) bool {
	if t == nil || t.maxFailures <= 0 {
		return false
	}
	key := saslThrottleKey(clientIP)
	if key == "" {
		return false
	}

	count, err := t.client.Get(ctx, key).Int()
	if err == redis.Nil {
		return false
	}
	if err != nil {
		t.logger.Warn("SASL throttle redis error", zap.Error(err))
		return false
	}

	return count >= t.maxFailures
}

// RecordFailure counts a failed authentication for the client IP.
func (t *SASLThrottle) RecordFailure(ctx context.Context, clientIP string) {
	if t == nil || t.maxFailures <= 0 {
		return
	}
	key := saslThrottleKey(clientIP)
	if key == "" {
		return
	}

	if err := t.script.Run(ctx, t.client, []string{key}, int(t.window.Seconds())).Err(); err != nil {
		t.logger.Warn("SASL throttle redis error", zap.Error(err))
	}
}

// Delay waits for the configured failure delay or until ctx is done.
func (t *SASLThrottle) Delay(ctx context.Context) {
	if t == nil || t.failureDelay <= 0 {
		return
	}

	timer := time.NewTimer(t.failureDelay)
	defer timer.Stop()

	select {
	case <-timer.C:
	case <-ctx.Done():
	}
}

// saslThrottleKey returns the Redis key for a client IP. IPv6 addresses
// are grouped by /64, since a single host typically controls the whole
// prefix. Returns "" if the address cannot be parsed.
func saslThrottleKey(clientIP string) string {
	ip := net.ParseIP(clientIP)
	if ip == nil {
		return ""
	}
	if ip4 := ip.To4(); ip4 != nil {
		return saslThrottleKeyPrefix + ip4.String()
	}
	prefix := ip.Mask(net.CIDRMask(64, 128))
	return saslThrottleKeyPrefix + prefix.String() + "/64"
}
