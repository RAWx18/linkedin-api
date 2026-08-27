// SPDX-FileCopyrightText: 2026 Ryan Madhuwala
// SPDX-License-Identifier: MIT

package service

import (
	"sync"
	"time"
)

// callerHealthMaxEntries bounds the health map so a flood of distinct caller
// sessions can never grow it without limit.
const callerHealthMaxEntries = 4096

// callerHealth tracks caller sessions that LinkedIn has rejected, keyed by their
// non-reversible fingerprint. It stores only the fingerprint and an expiry, never
// any credential material, and is bounded and short-lived. A rejected caller
// session is fast-failed for the retention window instead of being retried
// against LinkedIn, and it never affects the server session or any other caller.
type callerHealth struct {
	mu      sync.Mutex
	ttl     time.Duration
	max     int
	entries map[string]time.Time
	now     func() time.Time
}

func newCallerHealth(ttl time.Duration) *callerHealth {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &callerHealth{
		ttl:     ttl,
		max:     callerHealthMaxEntries,
		entries: make(map[string]time.Time),
		now:     time.Now,
	}
}

// unhealthy reports whether the fingerprint is currently marked as rejected.
func (h *callerHealth) unhealthy(fp string) bool {
	if fp == "" {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	exp, ok := h.entries[fp]
	if !ok {
		return false
	}
	if h.now().After(exp) {
		delete(h.entries, fp)
		return false
	}
	return true
}

// markUnhealthy records the fingerprint as rejected for the retention window.
func (h *callerHealth) markUnhealthy(fp string) {
	if fp == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, exists := h.entries[fp]; !exists && len(h.entries) >= h.max {
		h.evictLocked()
	}
	h.entries[fp] = h.now().Add(h.ttl)
}

// clear removes any mark for the fingerprint after a successful retrieval.
func (h *callerHealth) clear(fp string) {
	if fp == "" {
		return
	}
	h.mu.Lock()
	delete(h.entries, fp)
	h.mu.Unlock()
}

// tracked reports how many fingerprints are currently marked, for a bounded gauge.
func (h *callerHealth) tracked() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.entries)
}

// evictLocked drops expired entries, then one arbitrary entry if still full. The
// caller must hold the lock.
func (h *callerHealth) evictLocked() {
	now := h.now()
	for k, exp := range h.entries {
		if now.After(exp) {
			delete(h.entries, k)
		}
	}
	if len(h.entries) >= h.max {
		for k := range h.entries {
			delete(h.entries, k)
			break
		}
	}
}
