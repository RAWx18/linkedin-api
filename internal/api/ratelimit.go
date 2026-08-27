// SPDX-FileCopyrightText: 2026 Ryan Madhuwala
// SPDX-License-Identifier: MIT

package api

import (
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/garudexlabs/linkedin-api/internal/audit"
	"github.com/garudexlabs/linkedin-api/internal/domain"
)

const (
	rateLimiterMaxKeys = 10000
	rateLimiterIdleTTL = 10 * time.Minute
)

type bucket struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// rateLimiter keeps a token bucket per client identifier. The map is
// bounded and idle entries are evicted lazily so memory cannot grow without limit.
type rateLimiter struct {
	mu       sync.Mutex
	buckets  map[string]*bucket
	rps      rate.Limit
	burst    int
	keyFunc  func(*http.Request) string
	decision string
}

func newRateLimiter(rps float64, burst int, keyFunc func(*http.Request) string, decision string) *rateLimiter {
	return &rateLimiter{
		buckets:  make(map[string]*bucket),
		rps:      rate.Limit(rps),
		burst:    burst,
		keyFunc:  keyFunc,
		decision: decision,
	}
}

func (rl *rateLimiter) limiterFor(key string) *rate.Limiter {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	if b, ok := rl.buckets[key]; ok {
		b.lastSeen = time.Now()
		return b.limiter
	}
	if len(rl.buckets) >= rateLimiterMaxKeys {
		rl.evictLocked()
	}
	lim := rate.NewLimiter(rl.rps, rl.burst)
	rl.buckets[key] = &bucket{limiter: lim, lastSeen: time.Now()}
	return lim
}

// evictLocked drops idle entries and, if still full, one arbitrary entry to keep
// the map bounded. The caller must hold the lock.
func (rl *rateLimiter) evictLocked() {
	cutoff := time.Now().Add(-rateLimiterIdleTTL)
	for key, b := range rl.buckets {
		if b.lastSeen.Before(cutoff) {
			delete(rl.buckets, key)
		}
	}
	if len(rl.buckets) >= rateLimiterMaxKeys {
		for key := range rl.buckets {
			delete(rl.buckets, key)
			break
		}
	}
}

func (rl *rateLimiter) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := rl.keyFunc(r)
		if key == "" {
			next.ServeHTTP(w, r)
			return
		}
		if !rl.limiterFor(key).Allow() {
			audit.SetRateDecision(r.Context(), rl.decision)
			writeError(w, r, domain.RateLimited("rate limit exceeded, please slow down", 1))
			return
		}
		next.ServeHTTP(w, r)
	})
}
