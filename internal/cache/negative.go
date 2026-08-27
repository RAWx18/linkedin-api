// SPDX-FileCopyrightText: 2026 Ryan Madhuwala
// SPDX-License-Identifier: MIT

package cache

import (
	"sync"
	"time"
)

// NegativeTTL remembers profiles that were recently confirmed missing so that
// repeated lookups of the same non-existent profile do not each hit LinkedIn.
// It is concurrency-safe, size-bounded, and expiring.
type NegativeTTL struct {
	mu      sync.Mutex
	entries map[string]time.Time
	ttl     time.Duration
	max     int
	now     func() time.Time
}

// NewNegative builds a negative cache with the given TTL and entry cap.
func NewNegative(ttl time.Duration, max int) *NegativeTTL {
	return &NegativeTTL{
		entries: make(map[string]time.Time),
		ttl:     ttl,
		max:     max,
		now:     time.Now,
	}
}

// Blocked reports whether the key is currently remembered as missing.
func (n *NegativeTTL) Blocked(key string) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	exp, ok := n.entries[key]
	if !ok {
		return false
	}
	if n.now().After(exp) {
		delete(n.entries, key)
		return false
	}
	return true
}

// Remember records the key as missing for the configured TTL.
func (n *NegativeTTL) Remember(key string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if _, exists := n.entries[key]; !exists && len(n.entries) >= n.max {
		n.evictLocked()
	}
	n.entries[key] = n.now().Add(n.ttl)
}

// evictLocked drops expired entries, then one arbitrary entry if still full. The
// caller must hold the lock.
func (n *NegativeTTL) evictLocked() {
	now := n.now()
	for k, exp := range n.entries {
		if now.After(exp) {
			delete(n.entries, k)
		}
	}
	if len(n.entries) >= n.max {
		for k := range n.entries {
			delete(n.entries, k)
			break
		}
	}
}
