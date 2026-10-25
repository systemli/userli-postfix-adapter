package main

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// defaultLookupCacheTTL is the default TTL for cached successful lookups when
// LOOKUP_CACHE_TTL is not set. Setting LOOKUP_CACHE_TTL=0 disables the cache.
const defaultLookupCacheTTL = 300 * time.Second

// Defaults for SASL brute-force protection.
const (
	defaultSASLFailureDelay  = 2 * time.Second
	defaultSASLMaxFailures   = 10
	defaultSASLFailureWindow = 15 * time.Minute

	// maxSASLFailureDelay keeps the failure delay plus the Userli API
	// timeout (5s) below Postfix' AUTH_TIMEOUT (10s).
	maxSASLFailureDelay = 4 * time.Second
)

// Config is the configuration for the application.
type Config struct {
	// UserliToken is the token for the userli service.
	UserliToken string

	// UserliBaseURL is the base URL for the userli service.
	UserliBaseURL string

	// PostfixRecipientDelimiter is the recipient delimiter used by Postfix.
	PostfixRecipientDelimiter string

	// SocketmapListenAddr is the address to listen for socketmap requests.
	SocketmapListenAddr string

	// PolicyListenAddr is the address to listen for policy requests.
	PolicyListenAddr string

	// MetricsListenAddr is the address to listen for metrics requests.
	MetricsListenAddr string

	// RateLimitMessage is the message returned when rate limit is exceeded.
	RateLimitMessage string

	// RedisURL is the connection URL for Redis (used to persist rate-limit state
	// and cache successful lookup responses).
	RedisURL string

	// LookupCacheTTL is the TTL for cached successful lookup responses.
	// Zero disables caching entirely.
	LookupCacheTTL time.Duration

	// SASLListenAddr is the address to listen for Dovecot SASL auth requests.
	// Supports TCP (e.g. ":10004") or UNIX socket (e.g. "/var/spool/postfix/private/auth").
	// Leave empty to disable the SASL auth server.
	SASLListenAddr string

	// SASLFailureDelay is the delay before answering a failed SASL
	// authentication. Must stay well below Postfix' 10s auth timeout.
	SASLFailureDelay time.Duration

	// SASLMaxFailures is the number of failed SASL authentications per
	// client IP within SASLFailureWindow after which further attempts are
	// rejected without asking the Userli API. Zero disables the limit.
	SASLMaxFailures int

	// SASLFailureWindow is the window in which failed SASL authentications
	// are counted per client IP.
	SASLFailureWindow time.Duration
}

// NewConfig creates a new Config with default values.
func NewConfig() (*Config, error) {
	userliBaseURL := os.Getenv("USERLI_BASE_URL")
	if userliBaseURL == "" {
		userliBaseURL = "http://localhost:8000"
	}

	userliToken := os.Getenv("USERLI_TOKEN")
	if userliToken == "" {
		return nil, fmt.Errorf("USERLI_TOKEN is required")
	}

	postfixRecipientDelimiter := os.Getenv("POSTFIX_RECIPIENT_DELIMITER")

	socketmapListenAddr := os.Getenv("SOCKETMAP_LISTEN_ADDR")
	if socketmapListenAddr == "" {
		socketmapListenAddr = ":10001"
	}

	metricsListenAddr := os.Getenv("METRICS_LISTEN_ADDR")
	if metricsListenAddr == "" {
		metricsListenAddr = ":10002"
	}

	policyListenAddr := os.Getenv("POLICY_LISTEN_ADDR")
	if policyListenAddr == "" {
		policyListenAddr = ":10003"
	}

	rateLimitMessage := os.Getenv("RATE_LIMIT_MESSAGE")
	if rateLimitMessage == "" {
		rateLimitMessage = "Rate limit exceeded, please try again later"
	}

	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		return nil, fmt.Errorf("REDIS_URL is required")
	}

	lookupCacheTTL := defaultLookupCacheTTL
	if raw := os.Getenv("LOOKUP_CACHE_TTL"); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil {
			return nil, fmt.Errorf("LOOKUP_CACHE_TTL: %w", err)
		}
		if parsed < 0 {
			return nil, fmt.Errorf("LOOKUP_CACHE_TTL must be non-negative")
		}
		lookupCacheTTL = parsed
	}

	saslListenAddr := os.Getenv("SASL_LISTEN_ADDR")

	saslFailureDelay := defaultSASLFailureDelay
	if raw := os.Getenv("SASL_FAILURE_DELAY"); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil {
			return nil, fmt.Errorf("SASL_FAILURE_DELAY: %w", err)
		}
		if parsed < 0 || parsed > maxSASLFailureDelay {
			return nil, fmt.Errorf("SASL_FAILURE_DELAY must be between 0 and %s", maxSASLFailureDelay)
		}
		saslFailureDelay = parsed
	}

	saslMaxFailures := defaultSASLMaxFailures
	if raw := os.Getenv("SASL_MAX_FAILURES"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			return nil, fmt.Errorf("SASL_MAX_FAILURES: %w", err)
		}
		if parsed < 0 {
			return nil, fmt.Errorf("SASL_MAX_FAILURES must be non-negative")
		}
		saslMaxFailures = parsed
	}

	saslFailureWindow := defaultSASLFailureWindow
	if raw := os.Getenv("SASL_FAILURE_WINDOW"); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil {
			return nil, fmt.Errorf("SASL_FAILURE_WINDOW: %w", err)
		}
		if parsed < time.Second {
			return nil, fmt.Errorf("SASL_FAILURE_WINDOW must be at least 1s")
		}
		saslFailureWindow = parsed
	}

	return &Config{
		UserliBaseURL:             userliBaseURL,
		UserliToken:               userliToken,
		PostfixRecipientDelimiter: postfixRecipientDelimiter,
		SocketmapListenAddr:       socketmapListenAddr,
		PolicyListenAddr:          policyListenAddr,
		MetricsListenAddr:         metricsListenAddr,
		RateLimitMessage:          rateLimitMessage,
		RedisURL:                  redisURL,
		LookupCacheTTL:            lookupCacheTTL,
		SASLListenAddr:            saslListenAddr,
		SASLFailureDelay:          saslFailureDelay,
		SASLMaxFailures:           saslMaxFailures,
		SASLFailureWindow:         saslFailureWindow,
	}, nil
}
