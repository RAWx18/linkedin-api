// SPDX-FileCopyrightText: 2026 Ryan Madhuwala
// SPDX-License-Identifier: MIT

package upstream

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/garudexlabs/linkedin-api/internal/domain"
	"github.com/garudexlabs/linkedin-api/internal/observability"
)

// maxSessionStrikes caps how far the session cooldown doubles, so a persistently
// invalid session is probed at most this many doublings apart rather than ever
// more slowly without bound.
const maxSessionStrikes = 4

// Config tunes the upstream guard.
type Config struct {
	MaxConcurrency   int
	RateRPS          float64
	RateBurst        int
	FailureThreshold int
	Cooldown         time.Duration
	SessionThreshold int
	SessionCooldown  time.Duration
}

// Guard is the single choke point in front of LinkedIn. It enforces an aggregate
// request rate and a concurrency ceiling, and trips a circuit breaker after
// repeated upstream failures. Authentication and challenge responses are treated
// as a likely session invalidation: the breaker trips after fewer of them and
// stays open far longer, so a dead or challenged session is not hammered into
// deeper trouble.
type Guard struct {
	sem     chan struct{}
	limiter *rate.Limiter
	breaker *breaker
	metrics *observability.Metrics
	now     func() time.Time
}

// New builds a Guard from cfg. The session starts healthy.
func New(cfg Config, metrics *observability.Metrics, logger *slog.Logger) *Guard {
	if metrics != nil {
		metrics.SessionHealthy.Set(1)
	}
	return &Guard{
		sem:     make(chan struct{}, cfg.MaxConcurrency),
		limiter: rate.NewLimiter(rate.Limit(cfg.RateRPS), cfg.RateBurst),
		breaker: &breaker{
			threshold:        cfg.FailureThreshold,
			cooldown:         cfg.Cooldown,
			sessionThreshold: cfg.SessionThreshold,
			sessionCooldown:  cfg.SessionCooldown,
			metrics:          metrics,
			logger:           logger,
		},
		metrics: metrics,
		now:     time.Now,
	}
}

// Enter reserves capacity for one upstream operation. It returns a domain error
// and does no upstream work when the breaker is open, the aggregate rate is
// exceeded, or the concurrency ceiling is reached. On success it returns a
// release function that must be called with the operation's final error to free
// the slot and update the breaker.
func (g *Guard) Enter(_ context.Context) (func(error), error) {
	if retryAfter, open := g.breaker.blocked(g.now()); open {
		g.reject("circuit_open")
		e := domain.UpstreamUnavailable(errors.New("upstream circuit open"))
		e.RetryAfter = retryAfter
		return nil, e
	}
	if !g.limiter.Allow() {
		g.reject("rate")
		return nil, domain.UpstreamRateLimited(1, errors.New("aggregate upstream rate exceeded"))
	}
	select {
	case g.sem <- struct{}{}:
	default:
		g.reject("concurrency")
		return nil, domain.UpstreamUnavailable(errors.New("upstream concurrency limit reached"))
	}

	var once sync.Once
	return func(opErr error) {
		once.Do(func() {
			<-g.sem
			g.breaker.record(opErr, g.now())
		})
	}, nil
}

func (g *Guard) reject(reason string) {
	if g.metrics != nil {
		g.metrics.UpstreamRejected.WithLabelValues(reason).Inc()
	}
}

// breaker is a three-state circuit breaker. It opens after a threshold of
// consecutive trip-worthy failures, blocks for a cooldown, then lets a single
// probe through. Session failures use a separate, smaller threshold and a
// longer, exponentially growing cooldown.
type breaker struct {
	mu               sync.Mutex
	threshold        int
	cooldown         time.Duration
	sessionThreshold int
	sessionCooldown  time.Duration
	metrics          *observability.Metrics
	logger           *slog.Logger

	failures        int
	sessionFailures int
	sessionStrikes  int
	openUntil       time.Time
	probing         bool
	open            bool
	unhealthy       bool
}

// blocked reports whether calls are currently blocked and a Retry-After hint in
// seconds. Once the cooldown elapses it admits exactly one probe at a time.
func (b *breaker) blocked(now time.Time) (int, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.openUntil.IsZero() {
		return 0, false
	}
	if now.Before(b.openUntil) {
		return retryAfterSeconds(b.openUntil, now), true
	}
	if b.probing {
		return 1, true
	}
	b.probing = true
	return 0, false
}

// record feeds an operation result back into the breaker.
func (b *breaker) record(err error, now time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.probing {
		b.probing = false
		if tripworthy(err) {
			b.trip(err, now, b.isSession(err))
		} else {
			b.reset()
		}
		return
	}
	if !tripworthy(err) {
		b.failures = 0
		b.sessionFailures = 0
		return
	}
	if b.isSession(err) {
		b.sessionFailures++
		if b.sessionFailures >= b.sessionThreshold {
			b.trip(err, now, true)
		}
		return
	}
	b.failures++
	if b.failures >= b.threshold {
		b.trip(err, now, false)
	}
}

// isSession reports whether an error is an authentication or challenge response
// and session handling is configured.
func (b *breaker) isSession(err error) bool {
	if b.sessionThreshold < 1 {
		return false
	}
	de, ok := domain.AsError(err)
	return ok && de.Code == domain.CodeUpstreamAuthFailed
}

// trip opens the breaker. The caller holds the lock. A session failure extends
// the cooldown exponentially and marks the session unhealthy; a rate-limit
// error's Retry-After extends whichever cooldown so LinkedIn's own backoff wins.
func (b *breaker) trip(err error, now time.Time, session bool) {
	cooldown := b.cooldown
	if session {
		if b.sessionStrikes < maxSessionStrikes {
			b.sessionStrikes++
		}
		cooldown = b.sessionCooldown << (b.sessionStrikes - 1)
	}
	if de, ok := domain.AsError(err); ok && de.RetryAfter > 0 {
		if ra := time.Duration(de.RetryAfter) * time.Second; ra > cooldown {
			cooldown = ra
		}
	}
	b.openUntil = now.Add(cooldown)
	b.failures = 0
	b.sessionFailures = 0
	if !b.open {
		b.open = true
		if b.metrics != nil {
			b.metrics.CircuitOpen.Set(1)
			b.metrics.CircuitTrips.Inc()
		}
	}
	if session && !b.unhealthy {
		b.unhealthy = true
		if b.metrics != nil {
			b.metrics.SessionHealthy.Set(0)
		}
		if b.logger != nil {
			b.logger.Warn("linkedin session appears unhealthy; pausing upstream requests", "cooldown", cooldown.String())
		}
	}
}

// reset closes the breaker and restores session health. The caller holds the lock.
func (b *breaker) reset() {
	b.openUntil = time.Time{}
	b.failures = 0
	b.sessionFailures = 0
	b.sessionStrikes = 0
	if b.open {
		b.open = false
		if b.metrics != nil {
			b.metrics.CircuitOpen.Set(0)
		}
	}
	if b.unhealthy {
		b.unhealthy = false
		if b.metrics != nil {
			b.metrics.SessionHealthy.Set(1)
		}
		if b.logger != nil {
			b.logger.Info("linkedin session recovered")
		}
	}
}

func retryAfterSeconds(until, now time.Time) int {
	if d := int(until.Sub(now).Seconds()); d >= 1 {
		return d
	}
	return 1
}

// tripworthy reports whether an error indicates an upstream problem that should
// count toward opening the breaker. Client-side and not-found errors do not.
func tripworthy(err error) bool {
	de, ok := domain.AsError(err)
	if !ok {
		return false
	}
	switch de.Code {
	case domain.CodeUpstreamAuthFailed,
		domain.CodeUpstreamRateLimited,
		domain.CodeUpstreamUnavailable,
		domain.CodeUpstreamTimeout,
		domain.CodeUpstreamParseError:
		return true
	default:
		return false
	}
}
