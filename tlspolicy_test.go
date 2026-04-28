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

func newTestPolicyHandler(t *testing.T, prober tlsProber) (*TLSPolicyHandler, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	opts, err := redis.ParseURL("redis://" + mr.Addr())
	if err != nil {
		t.Fatalf("parse redis url: %v", err)
	}
	client := redis.NewClient(opts)
	t.Cleanup(func() { _ = client.Close() })
	return &TLSPolicyHandler{
		redis:    client,
		prober:   prober,
		ttlTLS:   7 * 24 * time.Hour,
		ttlNoTLS: 24 * time.Hour,
		logger:   zap.NewNop(),
	}, mr
}

func TestTLSPolicyHandler_EmailInput_ExtractsDomain(t *testing.T) {
	probeCount := 0
	prober := &countingProber{hasTLS: true, count: &probeCount}
	h, _ := newTestPolicyHandler(t, prober)

	// Postfix sends a full address — should probe example.com, not the full address.
	resp := h.Lookup(context.Background(), "user@example.com")
	if resp.Status != "OK" || resp.Data != "encrypt" {
		t.Errorf("expected OK encrypt, got %s %s", resp.Status, resp.Data)
	}

	// Second call with bare domain must hit the same cache entry.
	resp2 := h.Lookup(context.Background(), "example.com")
	if probeCount != 1 {
		t.Errorf("expected cache hit (1 probe total), got %d", probeCount)
	}
	if resp2.Status != "OK" || resp2.Data != "encrypt" {
		t.Errorf("expected OK encrypt from cache, got %s %s", resp2.Status, resp2.Data)
	}
}

func TestTLSPolicyHandler_EmptyDomain_NotFound(t *testing.T) {
	h, _ := newTestPolicyHandler(t, &mockProber{hasTLS: true})
	resp := h.Lookup(context.Background(), "@")
	if resp.Status != "NOTFOUND" {
		t.Errorf("expected NOTFOUND for empty domain, got %s", resp.Status)
	}
}

func TestTLSPolicyHandler_CacheMiss_HasTLS(t *testing.T) {
	h, _ := newTestPolicyHandler(t, &mockProber{hasTLS: true})
	resp := h.Lookup(context.Background(), "example.com")
	if resp.Status != "OK" || resp.Data != "encrypt" {
		t.Errorf("expected OK encrypt, got %s %s", resp.Status, resp.Data)
	}
}

func TestTLSPolicyHandler_CacheMiss_NoTLS(t *testing.T) {
	h, _ := newTestPolicyHandler(t, &mockProber{hasTLS: false})
	resp := h.Lookup(context.Background(), "example.com")
	if resp.Status != "NOTFOUND" {
		t.Errorf("expected NOTFOUND, got %s", resp.Status)
	}
}

func TestTLSPolicyHandler_CacheHit_Encrypt(t *testing.T) {
	probeCount := 0
	prober := &countingProber{hasTLS: true, count: &probeCount}
	h, _ := newTestPolicyHandler(t, prober)
	ctx := context.Background()

	h.Lookup(ctx, "example.com")
	if probeCount != 1 {
		t.Fatalf("expected 1 probe, got %d", probeCount)
	}

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
	h, _ := newTestPolicyHandler(t, prober)
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
	h, _ := newTestPolicyHandler(t, prober)
	ctx := context.Background()

	resp := h.Lookup(ctx, "error.example.com")
	if resp.Status != "NOTFOUND" {
		t.Errorf("expected NOTFOUND on probe error, got %s", resp.Status)
	}

	h.Lookup(ctx, "error.example.com")
	if probeCount != 2 {
		t.Errorf("expected 2 probes (errors not cached), got %d", probeCount)
	}
}

func TestTLSPolicyHandler_CacheTTL_TLS(t *testing.T) {
	h, mr := newTestPolicyHandler(t, &mockProber{hasTLS: true})
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
	h, mr := newTestPolicyHandler(t, &mockProber{hasTLS: false})
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
	h, mr := newTestPolicyHandler(t, &mockProber{hasTLS: true})
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

	h, err := NewTLSPolicyHandler(context.Background(), "redis://127.0.0.1:1?dial_timeout=100ms", prober, cfg, zap.NewNop())
	if err != nil {
		t.Fatalf("expected no error on ping failure, got %v", err)
	}
	if h == nil {
		t.Fatal("expected non-nil handler even when ping fails")
	}
	_ = h.Close()
}

func TestTLSPolicyHandler_CacheSet_RedisError(t *testing.T) {
	mr := miniredis.RunT(t)
	opts, _ := redis.ParseURL("redis://" + mr.Addr())
	client := redis.NewClient(opts)
	h := &TLSPolicyHandler{
		redis:    client,
		ttlTLS:   time.Hour,
		ttlNoTLS: time.Hour,
		logger:   zap.NewNop(),
	}
	mr.Close()
	// Should not panic; error is logged and swallowed.
	h.cacheSet(context.Background(), "userli:tlspolicy:domain:test.com", tlsPolicyCacheEncrypt, time.Hour)
	_ = client.Close()
}
