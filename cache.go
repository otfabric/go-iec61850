package iec61850

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/otfabric/go-mms"
	"golang.org/x/sync/singleflight"
)

// modelCache stores cached browse/discovery results. All access is
// guarded by mu. A nil modelCache means caching is disabled.
type modelCache struct {
	mu       sync.RWMutex
	strategy CacheStrategy

	lds      []LogicalDevice
	ldsValid bool

	// itemsByLD maps LD name → flat list of MMS item IDs. This is
	// the raw material from which LNs, DOs, DAs, and trees are
	// derived.
	itemsByLD map[string][]string

	// parsedByLD maps LD name → parsed Ref slice derived from
	// itemsByLD. Populated lazily on first FindPaths call to avoid
	// re-parsing every MMS item ID on every search.
	parsedByLD map[string][]Ref

	// dsByLD maps LD name → cached dataset definitions.
	dsByLD map[string]map[string]*cachedDS

	// sflight deduplicates concurrent fetches for the same key.
	sflight singleflight.Group
}

type cachedDS struct {
	ds *DataSet
}

func newModelCache(strategy CacheStrategy) *modelCache {
	return &modelCache{
		strategy:   strategy,
		itemsByLD:  make(map[string][]string),
		parsedByLD: make(map[string][]Ref),
		dsByLD:     make(map[string]map[string]*cachedDS),
	}
}

// invalidateAll clears the entire cache.
func (mc *modelCache) invalidateAll() {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	mc.lds = nil
	mc.ldsValid = false
	mc.itemsByLD = make(map[string][]string)
	mc.parsedByLD = make(map[string][]Ref)
	mc.dsByLD = make(map[string]map[string]*cachedDS)
}

// invalidateLD clears cached variable data (and parsed refs) for a
// single logical device. Dataset definitions for the LD are
// preserved — use [invalidateDS] to clear those separately.
func (mc *modelCache) invalidateLD(ld string) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	delete(mc.itemsByLD, ld)
	delete(mc.parsedByLD, ld)
}

// invalidateDS clears cached dataset definitions for an LD.
func (mc *modelCache) invalidateDS(ld string) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	delete(mc.dsByLD, ld)
}

// getDS returns a deep copy of a cached dataset definition if available.
func (mc *modelCache) getDS(ld, dsName string) (*DataSet, bool) {
	mc.mu.RLock()
	defer mc.mu.RUnlock()
	ldCache, ok := mc.dsByLD[ld]
	if !ok {
		return nil, false
	}
	entry, ok := ldCache[dsName]
	if !ok {
		return nil, false
	}
	return copyDataSet(entry.ds), true
}

// setDS stores a deep copy of a dataset definition in the cache.
func (mc *modelCache) setDS(ld, dsName string, ds *DataSet) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	if mc.dsByLD[ld] == nil {
		mc.dsByLD[ld] = make(map[string]*cachedDS)
	}
	mc.dsByLD[ld][dsName] = &cachedDS{ds: copyDataSet(ds)}
}

func copyDataSet(ds *DataSet) *DataSet {
	cp := &DataSet{
		Reference: ds.Reference,
		Deletable: ds.Deletable,
	}
	if len(ds.Members) > 0 {
		cp.Members = make([]DataSetMember, len(ds.Members))
		for i, m := range ds.Members {
			cp.Members[i] = m
			if len(m.Ref.Path) > 0 {
				cp.Members[i].Ref.Path = append([]string(nil), m.Ref.Path...)
			}
		}
	}
	return cp
}

// getLDs returns cached LDs if valid, otherwise (nil, false).
func (mc *modelCache) getLDs() ([]LogicalDevice, bool) {
	mc.mu.RLock()
	defer mc.mu.RUnlock()
	if mc.ldsValid {
		cp := make([]LogicalDevice, len(mc.lds))
		copy(cp, mc.lds)
		return cp, true
	}
	return nil, false
}

// setLDs stores the LD list in the cache, pre-sorted by name so
// callers never pay a per-call sort cost.
func (mc *modelCache) setLDs(lds []LogicalDevice) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	cp := make([]LogicalDevice, len(lds))
	copy(cp, lds)
	sort.Slice(cp, func(i, j int) bool { return cp[i].Name < cp[j].Name })
	mc.lds = cp
	mc.ldsValid = true
}

// getParsedRefs returns a copy of cached parsed Refs for an LD.
func (mc *modelCache) getParsedRefs(ld string) ([]Ref, bool) {
	mc.mu.RLock()
	defer mc.mu.RUnlock()
	refs, ok := mc.parsedByLD[ld]
	if !ok {
		return nil, false
	}
	cp := make([]Ref, len(refs))
	copy(cp, refs)
	return cp, true
}

// setParsedRefs stores a defensive copy of parsed Refs for an LD.
func (mc *modelCache) setParsedRefs(ld string, refs []Ref) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	cp := make([]Ref, len(refs))
	copy(cp, refs)
	mc.parsedByLD[ld] = cp
}

// getItems returns cached MMS items for an LD if available.
func (mc *modelCache) getItems(ld string) ([]string, bool) {
	mc.mu.RLock()
	defer mc.mu.RUnlock()
	items, ok := mc.itemsByLD[ld]
	if !ok {
		return nil, false
	}
	cp := make([]string, len(items))
	copy(cp, items)
	return cp, true
}

// setItems stores the MMS items for an LD.
func (mc *modelCache) setItems(ld string, items []string) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	cp := make([]string, len(items))
	copy(cp, items)
	mc.itemsByLD[ld] = cp
}

// cachedLDs returns LDs from cache or fetches from server, respecting
// the cache strategy. Concurrent callers for the same key are
// coalesced via singleflight to avoid redundant network round trips.
//
// CacheLazy: auto-populates cache on first access.
// CacheExplicit: only returns cached data; if not populated, fetches
// from server but does NOT store in cache (use RefreshCache to
// populate).
func (c *Client) cachedLDs(ctx context.Context) ([]LogicalDevice, error) {
	if c.cache != nil {
		if lds, ok := c.cache.getLDs(); ok {
			return lds, nil
		}
	}

	if c.cache != nil {
		v, err, _ := c.cache.sflight.Do("_lds", func() (interface{}, error) {
			return c.doFetchLDs(ctx)
		})
		if err != nil {
			return nil, err
		}
		lds := v.([]LogicalDevice)
		if c.cache.strategy == CacheLazy {
			c.cache.setLDs(lds)
		}
		return lds, nil
	}

	return c.doFetchLDs(ctx)
}

// cachedItems returns MMS item IDs for an LD from cache or fetches
// from server. Concurrent callers for the same LD are coalesced
// via singleflight to avoid redundant network round trips.
//
// CacheLazy: auto-populates cache on first access.
// CacheExplicit: only returns cached data; if not populated, fetches
// from server but does NOT store in cache.
func (c *Client) cachedItems(ctx context.Context, ld string) ([]string, error) {
	if c.cache != nil {
		if items, ok := c.cache.getItems(ld); ok {
			return items, nil
		}
	}

	if c.cache != nil {
		v, err, _ := c.cache.sflight.Do("items:"+ld, func() (interface{}, error) {
			return c.doFetchItems(ctx, ld)
		})
		if err != nil {
			return nil, err
		}
		items := v.([]string)
		if c.cache.strategy == CacheLazy {
			c.cache.setItems(ld, items)
		}
		return items, nil
	}

	return c.doFetchItems(ctx, ld)
}

func (c *Client) doFetchLDs(ctx context.Context) ([]LogicalDevice, error) {
	if c.fetchLDsFn != nil {
		return c.fetchLDsFn(ctx)
	}
	return c.fetchLDs(ctx)
}

func (c *Client) doFetchItems(ctx context.Context, ld string) ([]string, error) {
	if c.fetchItemsFn != nil {
		return c.fetchItemsFn(ctx, ld)
	}
	return c.fetchItems(ctx, ld)
}

// fetchLDs fetches logical devices from the server (bypasses cache).
// Results are pre-sorted by name. When the client is configured with an
// IED name, the prefix is stripped from each MMS domain name so callers
// always see bare LD instance names.
func (c *Client) fetchLDs(ctx context.Context) ([]LogicalDevice, error) {
	names, err := c.mmsClient.GetNameListAll(ctx, mms.NameListRequest{
		ObjectClass: mms.ObjectClassDomain,
		Scope:       mms.ObjectScopeVMD,
	})
	if err != nil {
		return nil, fmt.Errorf("iec61850: list logical devices: %w", err)
	}

	sort.Strings(names)

	devices := make([]LogicalDevice, len(names))
	for i, name := range names {
		devices[i] = LogicalDevice{Name: c.stripIEDPrefix(name)}
	}
	return devices, nil
}

// fetchItems fetches MMS item IDs for an LD from the server (bypasses
// cache). The ld parameter is the bare LD instance name; when the client
// is configured with an IED name, the MMS domain used in the request is
// iedName+ld.
func (c *Client) fetchItems(ctx context.Context, ld string) ([]string, error) {
	items, err := c.mmsClient.GetNameListAll(ctx, mms.NameListRequest{
		ObjectClass: mms.ObjectClassNamedVariable,
		Scope:       mms.ObjectScopeDomain,
		DomainID:    c.ldDomain(ld),
	})
	if err != nil {
		return nil, fmt.Errorf("iec61850: list variables in %q: %w", ld, err)
	}
	return items, nil
}

// RefreshCache re-fetches all cached model data from the server.
// This is a no-op when caching is disabled ([CacheNone]).
//
// Use this after server model changes to ensure the client sees
// the latest structure. The cache is rebuilt atomically: on failure,
// the previous cache state is preserved.
func (c *Client) RefreshCache(ctx context.Context) error {
	if c.cache == nil {
		return nil
	}
	if err := c.checkOpen(); err != nil {
		return err
	}

	// Build a fresh snapshot before swapping into the cache, so a
	// mid-refresh failure does not leave partially rebuilt data.
	lds, err := c.doFetchLDs(ctx)
	if err != nil {
		return err
	}

	itemsByLD := make(map[string][]string, len(lds))
	for _, ld := range lds {
		items, err := c.doFetchItems(ctx, ld.Name)
		if err != nil {
			return fmt.Errorf("iec61850: refresh cache for %q: %w", ld.Name, err)
		}
		itemsByLD[ld.Name] = items
	}

	// Swap atomically — clear parsedByLD alongside itemsByLD so
	// stale parsed refs cannot survive a full refresh.
	c.cache.mu.Lock()
	c.cache.lds = make([]LogicalDevice, len(lds))
	copy(c.cache.lds, lds)
	c.cache.ldsValid = true
	c.cache.itemsByLD = itemsByLD
	c.cache.parsedByLD = make(map[string][]Ref)
	c.cache.dsByLD = make(map[string]map[string]*cachedDS)
	c.cache.mu.Unlock()

	c.logger.Debug("iec61850: cache refreshed", "devices", len(lds))
	return nil
}

// RefreshLDCache re-fetches cached variable data for a single
// logical device. This is a no-op when caching is disabled
// ([CacheNone]).
//
// Only the per-LD variable list is refreshed; the global logical
// device list is not modified. If the LD was added or removed on
// the server, call [Client.RefreshCache] instead to refresh both.
//
// Both the raw item list and any derived parsed-ref cache for the
// LD are replaced atomically. Dataset definitions are preserved —
// call [Client.InvalidateLDCache] to clear those as well.
func (c *Client) RefreshLDCache(ctx context.Context, ld string) error {
	if c.cache == nil {
		return nil
	}
	if err := c.checkOpen(); err != nil {
		return err
	}
	if ld == "" {
		return &ReferenceError{Input: ld, Reason: "empty logical device name"}
	}

	items, err := c.doFetchItems(ctx, ld)
	if err != nil {
		return err
	}
	// invalidateLD clears both itemsByLD and parsedByLD for the LD,
	// ensuring stale parsed refs cannot survive the refresh.
	c.cache.invalidateLD(ld)
	c.cache.setItems(ld, items)

	c.logger.Debug("iec61850: LD cache refreshed", "ld", ld)
	return nil
}

// InvalidateCache clears all cached model data without re-fetching.
// Subsequent browse calls will fetch fresh data from the server.
// This is a no-op when caching is disabled ([CacheNone]).
func (c *Client) InvalidateCache() {
	if c.cache == nil {
		return
	}
	c.cache.invalidateAll()
	c.logger.Debug("iec61850: cache invalidated")
}

// InvalidateLDCache clears cached variable data and dataset
// definitions for a single logical device. This is a no-op when
// caching is disabled ([CacheNone]).
func (c *Client) InvalidateLDCache(ld string) {
	if c.cache == nil {
		return
	}
	c.cache.invalidateLD(ld)
	c.cache.invalidateDS(ld)
	c.logger.Debug("iec61850: LD cache invalidated", "ld", ld)
}
