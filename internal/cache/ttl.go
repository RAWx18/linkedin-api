// SPDX-FileCopyrightText: 2026 Ryan Madhuwala
// SPDX-License-Identifier: MIT

package cache

import (
	"sync"
	"time"

	"github.com/garudexlabs/linkedin-api/internal/domain"
)

type entry struct {
	result    *domain.ProfileResult
	expiresAt time.Time
}

// TTLCache is a concurrency-safe, size-bounded cache keyed by public identifier.
// It reduces upstream load and latency; bounding the entry count keeps memory
// predictable under adversarial or high-cardinality traffic.
type TTLCache struct {
	mu         sync.RWMutex
	entries    map[string]entry
	ttl        time.Duration
	maxEntries int
	now        func() time.Time
}

// NewTTL builds a cache with the given TTL and maximum entry count.
func NewTTL(ttl time.Duration, maxEntries int) *TTLCache {
	return &TTLCache{
		entries:    make(map[string]entry),
		ttl:        ttl,
		maxEntries: maxEntries,
		now:        time.Now,
	}
}

// Get returns a cached result when present and unexpired.
func (c *TTLCache) Get(key string) (*domain.ProfileResult, bool) {
	c.mu.RLock()
	e, ok := c.entries[key]
	c.mu.RUnlock()
	if !ok {
		return nil, false
	}
	if c.now().After(e.expiresAt) {
		c.mu.Lock()
		if cur, ok := c.entries[key]; ok && c.now().After(cur.expiresAt) {
			delete(c.entries, key)
		}
		c.mu.Unlock()
		return nil, false
	}
	return e.result, true
}

// Set stores a result, evicting when the cache is at capacity.
func (c *TTLCache) Set(key string, result *domain.ProfileResult) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.entries[key]; !exists && len(c.entries) >= c.maxEntries {
		c.evictLocked()
	}
	c.entries[key] = entry{result: result, expiresAt: c.now().Add(c.ttl)}
}

// evictLocked drops expired entries first, then the soonest-to-expire entry if
// the cache is still full. The caller must hold the write lock.
func (c *TTLCache) evictLocked() {
	now := c.now()
	var soonestKey string
	var soonest time.Time
	for k, e := range c.entries {
		if now.After(e.expiresAt) {
			delete(c.entries, k)
			continue
		}
		if soonestKey == "" || e.expiresAt.Before(soonest) {
			soonestKey, soonest = k, e.expiresAt
		}
	}
	if len(c.entries) >= c.maxEntries && soonestKey != "" {
		delete(c.entries, soonestKey)
	}
}

// Noop is a cache that stores nothing, used when caching is disabled.
type Noop struct{}

func (Noop) Get(string) (*domain.ProfileResult, bool) { return nil, false }
func (Noop) Set(string, *domain.ProfileResult)        {}
