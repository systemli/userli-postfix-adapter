package main

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

func newTestLookupCache(t *testing.T) (*LookupCache, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return NewLookupCache(client, 300*time.Second, zap.NewNop()), mr
}

func TestLookupCache_GetMiss(t *testing.T) {
	cache, _ := newTestLookupCache(t)

	resp, ok := cache.Get(context.Background(), "alias", "missing@example.com")
	if ok {
		t.Errorf("Expected miss, got hit with response %v", resp)
	}
	if resp != nil {
		t.Errorf("Expected nil response on miss, got %v", resp)
	}
}

func TestLookupCache_SetThenGetOK(t *testing.T) {
	cache, _ := newTestLookupCache(t)
	ctx := context.Background()

	stored := &SocketmapResponse{Status: "OK", Data: "user1@example.com,user2@example.com"}
	cache.Set(ctx, "alias", "alias@example.com", stored)

	got, ok := cache.Get(ctx, "alias", "alias@example.com")
	if !ok {
		t.Fatal("Expected hit, got miss")
	}
	if got.Status != "OK" || got.Data != "user1@example.com,user2@example.com" {
		t.Errorf("Cached response mismatch: got %v", got)
	}
}

func TestLookupCache_SetThenGetOKBoolean(t *testing.T) {
	// "OK 1" responses (from domain/mailbox handlers) must round-trip
	// through the cache without losing the data field.
	cache, _ := newTestLookupCache(t)
	ctx := context.Background()

	stored := &SocketmapResponse{Status: "OK", Data: "1"}
	cache.Set(ctx, "domain", "example.com", stored)

	got, ok := cache.Get(ctx, "domain", "example.com")
	if !ok {
		t.Fatal("Expected hit, got miss")
	}
	if got.Status != "OK" || got.Data != "1" {
		t.Errorf("Cached response mismatch: got %v", got)
	}
}

func TestLookupCache_SetSkipsNonOK(t *testing.T) {
	cache, mr := newTestLookupCache(t)
	ctx := context.Background()

	for _, status := range []string{"NOTFOUND", "TEMP", "PERM"} {
		key := "any@example.com"
		cache.Set(ctx, "alias", key, &SocketmapResponse{Status: status, Data: "msg"})
		if mr.Exists(lookupCacheKeyPrefix + "alias:" + key) {
			t.Errorf("Cache must not store status %q", status)
		}
	}
}

func TestLookupCache_SetIgnoresNilResponse(t *testing.T) {
	cache, mr := newTestLookupCache(t)
	cache.Set(context.Background(), "alias", "x@example.com", nil)
	if mr.Exists(lookupCacheKeyPrefix + "alias:x@example.com") {
		t.Error("Cache must not store nil responses")
	}
}

func TestLookupCache_TTLExpires(t *testing.T) {
	cache, mr := newTestLookupCache(t)
	ctx := context.Background()

	cache.Set(ctx, "alias", "ttl@example.com", &SocketmapResponse{Status: "OK", Data: "x"})
	if _, ok := cache.Get(ctx, "alias", "ttl@example.com"); !ok {
		t.Fatal("Expected hit immediately after Set")
	}

	mr.FastForward(301 * time.Second)

	if _, ok := cache.Get(ctx, "alias", "ttl@example.com"); ok {
		t.Error("Expected miss after TTL expiry")
	}
}

func TestLookupCache_KeysIsolatedByMapName(t *testing.T) {
	cache, _ := newTestLookupCache(t)
	ctx := context.Background()

	cache.Set(ctx, "alias", "x", &SocketmapResponse{Status: "OK", Data: "alias-data"})
	cache.Set(ctx, "domain", "x", &SocketmapResponse{Status: "OK", Data: "1"})

	a, ok := cache.Get(ctx, "alias", "x")
	if !ok || a.Data != "alias-data" {
		t.Errorf("Expected alias data, got %v ok=%v", a, ok)
	}
	d, ok := cache.Get(ctx, "domain", "x")
	if !ok || d.Data != "1" {
		t.Errorf("Expected domain data, got %v ok=%v", d, ok)
	}
}

// TestLookupCache_FailOpen verifies Redis errors at runtime do not propagate:
// Get returns a miss and Set is a silent no-op, so handlers fall through to
// the upstream API.
func TestLookupCache_FailOpen(t *testing.T) {
	cache, mr := newTestLookupCache(t)
	mr.Close()

	if _, ok := cache.Get(context.Background(), "alias", "x@example.com"); ok {
		t.Error("Expected miss on Redis error")
	}

	cache.Set(context.Background(), "alias", "x@example.com", &SocketmapResponse{Status: "OK", Data: "v"})
}

// TestLookupCache_NilReceiver covers the "cache disabled" path — main.go passes
// nil when LOOKUP_CACHE_TTL=0, so Get/Set must be safe on a nil receiver.
func TestLookupCache_NilReceiver(t *testing.T) {
	var cache *LookupCache

	if _, ok := cache.Get(context.Background(), "alias", "x"); ok {
		t.Error("Nil cache must return miss")
	}

	cache.Set(context.Background(), "alias", "x", &SocketmapResponse{Status: "OK", Data: "v"})
}
