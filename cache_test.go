// SPDX-License-Identifier: MIT

package iec61850

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/otfabric/go-mms"
)

func setupCachedLoopback(t *testing.T, strategy CacheStrategy) (*Client, *callCounter) {
	t.Helper()
	ctx := context.Background()

	cc := &callCounter{}
	srv := setupTestServer(t)

	clientT, serverT := loopbackPair()

	go func() {
		_ = srv.Serve(ctx, serverT)
	}()

	mmsClient, err := mms.NewClient(ctx, clientT, mms.DialOptions{})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	client, err := NewClient(mmsClient, ClientOptions{Cache: strategy})
	if err != nil {
		t.Fatalf("iec61850.NewClient: %v", err)
	}

	client.fetchLDsFn = func(ctx2 context.Context) ([]LogicalDevice, error) {
		cc.ldCalls.Add(1)
		return client.fetchLDs(ctx2)
	}

	client.fetchItemsFn = func(ctx2 context.Context, ld string) ([]string, error) {
		cc.itemCalls.Add(1)
		return client.fetchItems(ctx2, ld)
	}

	t.Cleanup(func() {
		_ = client.Close(ctx)
	})

	return client, cc
}

type callCounter struct {
	ldCalls   atomic.Int64
	itemCalls atomic.Int64
}

func TestCacheNone_AlwaysFetches(t *testing.T) {
	client, cc := setupCachedLoopback(t, CacheNone)
	ctx := context.Background()

	_, _ = client.ListLogicalDevices(ctx)
	_, _ = client.ListLogicalDevices(ctx)

	if cc.ldCalls.Load() != 2 {
		t.Errorf("CacheNone: expected 2 LD fetches, got %d", cc.ldCalls.Load())
	}
}

func TestCacheLazy_CachesAfterFirstFetch(t *testing.T) {
	client, cc := setupCachedLoopback(t, CacheLazy)
	ctx := context.Background()

	_, _ = client.ListLogicalDevices(ctx)
	_, _ = client.ListLogicalDevices(ctx)
	_, _ = client.ListLogicalDevices(ctx)

	if cc.ldCalls.Load() != 1 {
		t.Errorf("CacheLazy: expected 1 LD fetch, got %d", cc.ldCalls.Load())
	}
}

func TestCacheExplicit_DoesNotAutoPopulate(t *testing.T) {
	client, cc := setupCachedLoopback(t, CacheExplicit)
	ctx := context.Background()

	// Without RefreshCache, every call hits the server.
	_, _ = client.ListLogicalDevices(ctx)
	_, _ = client.ListLogicalDevices(ctx)

	if cc.ldCalls.Load() != 2 {
		t.Errorf("CacheExplicit: expected 2 LD fetches (no auto-populate), got %d", cc.ldCalls.Load())
	}
}

func TestCacheExplicit_CachesAfterRefresh(t *testing.T) {
	client, cc := setupCachedLoopback(t, CacheExplicit)
	ctx := context.Background()

	// Populate cache via explicit refresh.
	if err := client.RefreshCache(ctx); err != nil {
		t.Fatalf("RefreshCache: %v", err)
	}
	// RefreshCache uses 1 LD fetch.
	if cc.ldCalls.Load() != 1 {
		t.Errorf("expected 1 LD fetch after refresh, got %d", cc.ldCalls.Load())
	}

	// Subsequent calls should use the cache.
	_, _ = client.ListLogicalDevices(ctx)
	_, _ = client.ListLogicalDevices(ctx)

	if cc.ldCalls.Load() != 1 {
		t.Errorf("expected still 1 LD fetch (cache hit), got %d", cc.ldCalls.Load())
	}

	// Invalidate and verify re-fetch needed.
	client.InvalidateCache()
	_, _ = client.ListLogicalDevices(ctx)

	// After invalidate, CacheExplicit still does not auto-populate.
	if cc.ldCalls.Load() != 2 {
		t.Errorf("expected 2 LD fetches after invalidate, got %d", cc.ldCalls.Load())
	}
}

func TestCacheLazy_ItemsCached(t *testing.T) {
	client, cc := setupCachedLoopback(t, CacheLazy)
	ctx := context.Background()

	_, _ = client.ListLogicalNodes(ctx, "simpleIOGenericIO")
	_, _ = client.ListLogicalNodes(ctx, "simpleIOGenericIO")
	_, _ = client.ListDataObjects(ctx, "simpleIOGenericIO", "LLN0")

	if cc.itemCalls.Load() != 1 {
		t.Errorf("CacheLazy: expected 1 items fetch, got %d", cc.itemCalls.Load())
	}
}

func TestCacheExplicit_RefreshLDCache(t *testing.T) {
	client, cc := setupCachedLoopback(t, CacheExplicit)
	ctx := context.Background()

	_, _ = client.ListLogicalNodes(ctx, "simpleIOGenericIO")

	if err := client.RefreshLDCache(ctx, "simpleIOGenericIO"); err != nil {
		t.Fatalf("RefreshLDCache: %v", err)
	}

	_, _ = client.ListLogicalNodes(ctx, "simpleIOGenericIO")

	// 1 initial + 1 refresh + 0 (cache hit)
	if cc.itemCalls.Load() != 2 {
		t.Errorf("expected 2 items fetches, got %d", cc.itemCalls.Load())
	}
}

func TestCacheExplicit_RefreshCache(t *testing.T) {
	client, cc := setupCachedLoopback(t, CacheExplicit)
	ctx := context.Background()

	_, _ = client.ListLogicalDevices(ctx)
	_, _ = client.ListLogicalNodes(ctx, "simpleIOGenericIO")

	if err := client.RefreshCache(ctx); err != nil {
		t.Fatalf("RefreshCache: %v", err)
	}

	// After refresh: 1 initial + 1 refresh = 2 LD fetches
	if cc.ldCalls.Load() != 2 {
		t.Errorf("expected 2 LD fetches, got %d", cc.ldCalls.Load())
	}
	// Items: 1 initial + 1 refresh = 2
	if cc.itemCalls.Load() != 2 {
		t.Errorf("expected 2 items fetches, got %d", cc.itemCalls.Load())
	}
}

func TestInvalidateCache_Noop_WhenNone(t *testing.T) {
	client, _ := setupCachedLoopback(t, CacheNone)
	client.InvalidateCache()
	client.InvalidateLDCache("nonexistent")
}

func TestRefreshCache_Noop_WhenNone(t *testing.T) {
	client, _ := setupCachedLoopback(t, CacheNone)
	ctx := context.Background()

	if err := client.RefreshCache(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := client.RefreshLDCache(ctx, "simpleIOGenericIO"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRefreshLDCache_EmptyLD(t *testing.T) {
	client, _ := setupCachedLoopback(t, CacheExplicit)
	ctx := context.Background()

	err := client.RefreshLDCache(ctx, "")
	if err == nil {
		t.Fatal("expected error for empty LD")
	}
}

func TestInvalidateDS_ViaCreateDelete(t *testing.T) {
	client, _ := setupCachedLoopback(t, CacheLazy)
	ctx := context.Background()

	// Populate the DS cache by reading a dataset.
	_, _ = client.GetDataSet(ctx, "simpleIOGenericIO", "LLN0$dsTest")

	// CreateDataSet calls invalidateDS internally.
	_ = client.CreateDataSet(ctx, "simpleIOGenericIO", "LLN0$dsNew", []DataSetMember{
		{Ref: Ref{LD: "simpleIOGenericIO", LN: "LLN0", Path: []string{"Mod", "stVal"}, FC: FCST}},
	})

	// DeleteDataSet calls invalidateDS internally.
	_ = client.DeleteDataSet(ctx, "simpleIOGenericIO", "LLN0$dsNew")
}

func TestInvalidateLDCache(t *testing.T) {
	client, cc := setupCachedLoopback(t, CacheLazy)
	ctx := context.Background()

	_, _ = client.ListLogicalNodes(ctx, "simpleIOGenericIO")
	client.InvalidateLDCache("simpleIOGenericIO")
	_, _ = client.ListLogicalNodes(ctx, "simpleIOGenericIO")

	if cc.itemCalls.Load() != 2 {
		t.Errorf("expected 2 items fetches after LD invalidation, got %d", cc.itemCalls.Load())
	}
}
