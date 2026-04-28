package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

const tlsPolicyKeyPrefix = "userli:tlspolicy:domain:"
const tlsPolicyCacheEncrypt = "encrypt"
const tlsPolicyCacheNoTLS = "notls"

// TLSPolicyHandler implements the smtp_tls_policy_maps socketmap.
// It checks Redis for a cached result first; on a miss it probes the domain's
// MX servers via SMTP and caches the outcome.
type TLSPolicyHandler struct {
	redis    *redis.Client
	prober   *TLSProber
	ttlTLS   time.Duration
	ttlNoTLS time.Duration
	logger   *zap.Logger
}

// NewTLSPolicyHandler creates a TLSPolicyHandler. It opens a dedicated Redis
// connection (separate pool from the rate-limiter). A failed initial ping is
// logged but does not prevent startup.
func NewTLSPolicyHandler(ctx context.Context, url string, prober *TLSProber, cfg *Config, logger *zap.Logger) (*TLSPolicyHandler, error) {
	opts, err := redis.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("parse redis url: %w", err)
	}
	client := redis.NewClient(opts)

	pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		logger.Warn("Redis ping failed for TLS policy handler, continuing fail-open", zap.Error(err))
	} else {
		logger.Info("TLS policy handler connected to Redis", zap.String("addr", opts.Addr))
	}

	return &TLSPolicyHandler{
		redis:    client,
		prober:   prober,
		ttlTLS:   cfg.TLSPolicyCacheTTLTLS,
		ttlNoTLS: cfg.TLSPolicyCacheTTLNoTLS,
		logger:   logger,
	}, nil
}

// Close shuts down the Redis client.
func (h *TLSPolicyHandler) Close() error {
	return h.redis.Close()
}

// Lookup returns the Postfix TLS policy for domain.
//
// Cache hit → immediate response.
// Cache miss → SMTP probe, then write to cache.
// Redis errors and unreachable hosts return NOTFOUND (fail-open).
// Successful probe with no STARTTLS → NOTFOUND, cached for ttlNoTLS.
// Probe errors (connection failures) → NOTFOUND, not cached.
func (h *TLSPolicyHandler) Lookup(ctx context.Context, domain string) *SocketmapResponse {
	key := tlsPolicyKeyPrefix + domain

	cached, err := h.redis.Get(ctx, key).Result()
	if err == nil {
		if cached == tlsPolicyCacheEncrypt {
			tlsPolicyCacheHits.WithLabelValues("encrypt").Inc()
			return &SocketmapResponse{Status: "OK", Data: "encrypt"}
		}
		tlsPolicyCacheHits.WithLabelValues("notls").Inc()
		return &SocketmapResponse{Status: "NOTFOUND"}
	}
	if !errors.Is(err, redis.Nil) {
		h.logger.Warn("Redis error in TLS policy lookup", zap.String("domain", domain), zap.Error(err))
		return &SocketmapResponse{Status: "NOTFOUND"}
	}

	start := time.Now()
	hasTLS, probeErr := h.prober.Probe(ctx, domain)
	tlsPolicyProbeDuration.Observe(time.Since(start).Seconds())

	if probeErr != nil {
		tlsPolicyProbeTotal.WithLabelValues("error").Inc()
		h.logger.Warn("SMTP probe failed, not caching", zap.String("domain", domain), zap.Error(probeErr))
		return &SocketmapResponse{Status: "NOTFOUND"}
	}

	if hasTLS {
		tlsPolicyProbeTotal.WithLabelValues("tls").Inc()
		h.logger.Info("STARTTLS confirmed", zap.String("domain", domain))
		h.cacheSet(ctx, key, tlsPolicyCacheEncrypt, h.ttlTLS)
		return &SocketmapResponse{Status: "OK", Data: "encrypt"}
	}

	tlsPolicyProbeTotal.WithLabelValues("notls").Inc()
	h.cacheSet(ctx, key, tlsPolicyCacheNoTLS, h.ttlNoTLS)
	return &SocketmapResponse{Status: "NOTFOUND"}
}

func (h *TLSPolicyHandler) cacheSet(ctx context.Context, key, value string, ttl time.Duration) {
	if err := h.redis.Set(ctx, key, value, ttl).Err(); err != nil {
		h.logger.Warn("Failed to write TLS policy cache", zap.String("key", key), zap.Error(err))
	}
}
