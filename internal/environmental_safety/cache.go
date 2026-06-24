package environmental_safety

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

const defaultCacheTTL = 10 * time.Minute

// Cache holds per-site environmental readings with TTL refresh.
type Cache struct {
	mu      sync.RWMutex
	entries map[string]*cacheEntry
	ttl     time.Duration
	source  Source
	log     *slog.Logger
}

type cacheEntry struct {
	reading   *Reading
	fetchedAt time.Time
}

func newCache(source Source, ttl time.Duration) *Cache {
	if ttl <= 0 {
		ttl = defaultCacheTTL
	}
	return &Cache{
		entries: make(map[string]*cacheEntry),
		ttl:     ttl,
		source:  source,
		log:     slog.With("component", "env_cache"),
	}
}

// Get returns the cached reading for siteID, refreshing if stale.
// Fail-open: returns nil if fetch fails and no cached value exists.
func (c *Cache) Get(siteID string) *Reading {
	c.mu.RLock()
	e, ok := c.entries[siteID]
	c.mu.RUnlock()

	if ok && time.Since(e.fetchedAt) < c.ttl {
		return e.reading
	}

	// 캐시 만료 또는 미존재 — 동기 fetch
	r, err := c.source.Fetch(siteID)
	if err != nil {
		c.log.Warn("env cache fetch failed; using stale/nil", "site_id", siteID, "error", err)
		if ok {
			return e.reading // stale 허용
		}
		return nil
	}

	c.mu.Lock()
	c.entries[siteID] = &cacheEntry{reading: r, fetchedAt: time.Now()}
	c.mu.Unlock()
	return r
}

// StartRefresh launches a background goroutine that refreshes known sites every interval.
func (c *Cache) StartRefresh(ctx context.Context, siteIDs []string, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				c.mu.RLock()
				known := make([]string, 0, len(c.entries))
				for id := range c.entries {
					known = append(known, id)
				}
				c.mu.RUnlock()
				// 미리 알려진 siteIDs + 캐시에 있는 것 모두 갱신
				all := unique(append(siteIDs, known...))
				for _, sid := range all {
					r, err := c.source.Fetch(sid)
					if err != nil {
						c.log.Warn("env refresh failed", "site_id", sid, "error", err)
						continue
					}
					c.mu.Lock()
					c.entries[sid] = &cacheEntry{reading: r, fetchedAt: time.Now()}
					c.mu.Unlock()
				}
			}
		}
	}()
}

func unique(ss []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		if _, ok := seen[s]; !ok {
			seen[s] = struct{}{}
			out = append(out, s)
		}
	}
	return out
}
