package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// mockProber implements a controllable TLS prober for unit tests.
type mockProber struct {
	hasTLS bool
	err    error
}

func (m *mockProber) Probe(_ context.Context, _ string) (bool, error) {
	return m.hasTLS, m.err
}

// testPolicyHandler is a test-only variant of TLSPolicyHandler with an injectable prober interface.
type testPolicyHandler struct {
	redis    *redis.Client
	prober   interface{ Probe(context.Context, string) (bool, error) }
	ttlTLS   time.Duration
	ttlNoTLS time.Duration
	logger   *zap.Logger
}

func (h *testPolicyHandler) Lookup(ctx context.Context, domain string) *SocketmapResponse {
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
		return &SocketmapResponse{Status: "NOTFOUND"}
	}

	hasTLS, probeErr := h.prober.Probe(ctx, domain)

	if probeErr != nil {
		tlsPolicyProbeTotal.WithLabelValues("error").Inc()
		return &SocketmapResponse{Status: "NOTFOUND"}
	}

	if hasTLS {
		tlsPolicyProbeTotal.WithLabelValues("tls").Inc()
		_ = h.redis.Set(ctx, key, tlsPolicyCacheEncrypt, h.ttlTLS).Err()
		return &SocketmapResponse{Status: "OK", Data: "encrypt"}
	}

	tlsPolicyProbeTotal.WithLabelValues("notls").Inc()
	_ = h.redis.Set(ctx, key, tlsPolicyCacheNoTLS, h.ttlNoTLS).Err()
	return &SocketmapResponse{Status: "NOTFOUND"}
}

func newTestHandler(t *testing.T, prober interface {
	Probe(context.Context, string) (bool, error)
}) (*testPolicyHandler, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return &testPolicyHandler{
		redis:    client,
		prober:   prober,
		ttlTLS:   7 * 24 * time.Hour,
		ttlNoTLS: 24 * time.Hour,
		logger:   zap.NewNop(),
	}, mr
}

func TestTLSPolicyHandler_CacheMiss_HasTLS(t *testing.T) {
	h, _ := newTestHandler(t, &mockProber{hasTLS: true})
	resp := h.Lookup(context.Background(), "example.com")
	if resp.Status != "OK" || resp.Data != "encrypt" {
		t.Errorf("expected OK encrypt, got %s %s", resp.Status, resp.Data)
	}
}

func TestTLSPolicyHandler_CacheMiss_NoTLS(t *testing.T) {
	h, _ := newTestHandler(t, &mockProber{hasTLS: false})
	resp := h.Lookup(context.Background(), "example.com")
	if resp.Status != "NOTFOUND" {
		t.Errorf("expected NOTFOUND, got %s", resp.Status)
	}
}

func TestTLSPolicyHandler_CacheHit_Encrypt(t *testing.T) {
	probeCount := 0
	prober := &countingProber{hasTLS: true, count: &probeCount}
	h, _ := newTestHandler(t, prober)
	ctx := context.Background()

	// First call: probe + cache
	h.Lookup(ctx, "example.com")
	if probeCount != 1 {
		t.Fatalf("expected 1 probe, got %d", probeCount)
	}

	// Second call: cache hit, no probe
	resp := h.Lookup(ctx, "example.com")
	if probeCount != 1 {
		t.Errorf("expected no second probe, got %d", probeCount)
	}
	if resp.Status != "OK" || resp.Data != "encrypt" {
		t.Errorf("expected OK encrypt from cache, got %s %s", resp.Status, resp.Data)
	}
}

func TestTLSPolicyHandler_CacheHit_NoTLS(t *testing.T) {
	probeCount := 0
	prober := &countingProber{hasTLS: false, count: &probeCount}
	h, _ := newTestHandler(t, prober)
	ctx := context.Background()

	h.Lookup(ctx, "notls.example.com")
	if probeCount != 1 {
		t.Fatalf("expected 1 probe, got %d", probeCount)
	}

	resp := h.Lookup(ctx, "notls.example.com")
	if probeCount != 1 {
		t.Errorf("expected no second probe on notls cache hit, got %d", probeCount)
	}
	if resp.Status != "NOTFOUND" {
		t.Errorf("expected NOTFOUND from cache, got %s", resp.Status)
	}
}

func TestTLSPolicyHandler_ProbeError_NotCached(t *testing.T) {
	probeCount := 0
	prober := &countingProber{err: errors.New("connection refused"), count: &probeCount}
	h, _ := newTestHandler(t, prober)
	ctx := context.Background()

	resp := h.Lookup(ctx, "error.example.com")
	if resp.Status != "NOTFOUND" {
		t.Errorf("expected NOTFOUND on probe error, got %s", resp.Status)
	}

	// Second call: should probe again (error is not cached)
	h.Lookup(ctx, "error.example.com")
	if probeCount != 2 {
		t.Errorf("expected 2 probes (errors not cached), got %d", probeCount)
	}
}

func TestTLSPolicyHandler_CacheTTL_TLS(t *testing.T) {
	h, mr := newTestHandler(t, &mockProber{hasTLS: true})
	h.Lookup(context.Background(), "example.com")

	key := tlsPolicyKeyPrefix + "example.com"
	ttl := mr.TTL(key)
	if ttl <= 0 {
		t.Fatalf("expected positive TTL for encrypt entry, got %v", ttl)
	}
	if ttl > 7*24*time.Hour+time.Minute {
		t.Errorf("TTL %v exceeds expected maximum", ttl)
	}
}

func TestTLSPolicyHandler_CacheTTL_NoTLS(t *testing.T) {
	h, mr := newTestHandler(t, &mockProber{hasTLS: false})
	h.Lookup(context.Background(), "notls.example.com")

	key := tlsPolicyKeyPrefix + "notls.example.com"
	ttl := mr.TTL(key)
	if ttl <= 0 {
		t.Fatalf("expected positive TTL for notls entry, got %v", ttl)
	}
	if ttl > 24*time.Hour+time.Minute {
		t.Errorf("noTLS TTL %v exceeds expected maximum of 24h", ttl)
	}
}

func TestTLSPolicyHandler_RedisDown_FailOpen(t *testing.T) {
	h, mr := newTestHandler(t, &mockProber{hasTLS: true})
	mr.Close()
	resp := h.Lookup(context.Background(), "example.com")
	if resp.Status != "NOTFOUND" {
		t.Errorf("expected NOTFOUND when Redis is down, got %s", resp.Status)
	}
}

func TestNewTLSPolicyHandler_Success(t *testing.T) {
	mr := miniredis.RunT(t)
	prober := NewTLSProber(time.Second, "test.local", zap.NewNop())
	cfg := &Config{TLSPolicyCacheTTLTLS: time.Hour, TLSPolicyCacheTTLNoTLS: time.Hour}

	h, err := NewTLSPolicyHandler(context.Background(), "redis://"+mr.Addr(), prober, cfg, zap.NewNop())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if h == nil {
		t.Fatal("expected non-nil handler")
	}
	if err := h.Close(); err != nil {
		t.Errorf("unexpected error on Close: %v", err)
	}
}

func TestNewTLSPolicyHandler_InvalidURL(t *testing.T) {
	_, err := NewTLSPolicyHandler(context.Background(), "://invalid", nil, &Config{}, zap.NewNop())
	if err == nil {
		t.Error("expected error for invalid Redis URL")
	}
}

func TestNewTLSPolicyHandler_PingFailure(t *testing.T) {
	prober := NewTLSProber(time.Second, "test.local", zap.NewNop())
	cfg := &Config{TLSPolicyCacheTTLTLS: time.Hour, TLSPolicyCacheTTLNoTLS: time.Hour}

	// Nothing listening — ping fails, but handler is still created (fail-open).
	h, err := NewTLSPolicyHandler(context.Background(), "redis://127.0.0.1:1?dial_timeout=100ms", prober, cfg, zap.NewNop())
	if err != nil {
		t.Fatalf("expected no error on ping failure, got %v", err)
	}
	if h == nil {
		t.Fatal("expected non-nil handler even when ping fails")
	}
	_ = h.Close()
}

// countingProber counts how many times Probe is called.
type countingProber struct {
	hasTLS bool
	err    error
	count  *int
}

func (c *countingProber) Probe(_ context.Context, _ string) (bool, error) {
	*c.count++
	return c.hasTLS, c.err
}
