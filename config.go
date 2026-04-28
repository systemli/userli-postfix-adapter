package main

import (
	"fmt"
	"os"
	"time"
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

	// RedisURL is the connection URL for Redis (used to persist rate-limit state).
	RedisURL string

	// TLSPolicyEhloHostname is the hostname sent in EHLO during SMTP probes.
	TLSPolicyEhloHostname string

	// TLSPolicyCacheTTLTLS is the Redis TTL for domains confirmed to support STARTTLS.
	TLSPolicyCacheTTLTLS time.Duration

	// TLSPolicyCacheTTLNoTLS is the Redis TTL for domains confirmed to not support STARTTLS.
	TLSPolicyCacheTTLNoTLS time.Duration
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

	tlsPolicyEhloHostname := os.Getenv("TLS_POLICY_EHLO_HOSTNAME")
	if tlsPolicyEhloHostname == "" {
		tlsPolicyEhloHostname = "localhost"
	}

	tlsPolicyCacheTTLTLS := 168 * time.Hour // 7 days
	if v := os.Getenv("TLS_POLICY_CACHE_TTL_TLS"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			tlsPolicyCacheTTLTLS = d
		}
	}

	tlsPolicyCacheTTLNoTLS := 24 * time.Hour // 1 day
	if v := os.Getenv("TLS_POLICY_CACHE_TTL_NOTLS"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			tlsPolicyCacheTTLNoTLS = d
		}
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
		TLSPolicyEhloHostname:     tlsPolicyEhloHostname,
		TLSPolicyCacheTTLTLS:      tlsPolicyCacheTTLTLS,
		TLSPolicyCacheTTLNoTLS:    tlsPolicyCacheTTLNoTLS,
	}, nil
}
