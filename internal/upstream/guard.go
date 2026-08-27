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
// repeated upstream failures. The breaker is split in two: a general breaker for
// transient failures and rate limits that gates all traffic, and a session
// breaker for authentication and challenge responses on the server session that
// gates only server-session traffic. Caller-supplied sessions carry their own
// credentials, so a dead server session never blocks them, and one caller's
// invalid session never trips the shared breaker.
type Guard struct {
	sem     chan struct{}
	limiter *rate.Limiter
	breaker *breaker
	metrics *observability.Metrics
	now     func() time.Time
}

// New builds a Guard from cfg. The server session starts healthy.
func New(cfg Config, metrics *observability.Metrics, logger *slog.Logger) *Guard {
	if metrics != nil {
		metrics.SessionHealthy.Set(1)
	}
	return &Guard{
		sem:     make(chan struct{}, cfg.MaxConcurrency),
		limiter: rate.NewLimiter(rate.Limit(cfg.RateRPS), cfg.RateBurst),
		breaker: &breaker{
			general: subBreaker{threshold: cfg.FailureThreshold, cooldown: cfg.Cooldown},
			session: subBreaker{threshold: cfg.SessionThreshold, cooldown: cfg.SessionCooldown, grow: true},
			metrics: metrics,
			logger:  logger,
		},
		metrics: metrics,
		now:     time.Now,
	}
}

// Enter reserves capacity for one upstream operation in the given credential
// mode. It returns a domain error and does no upstream work when the breaker is
// open for that mode, the aggregate rate is exceeded, or the concurrency ceiling
// is reached. On success it returns a release function that must be called with
// the operation's final error to free the slot and update the breaker.
func (g *Guard) Enter(_ context.Context, mode domain.CredentialMode) (func(error), error) {
	if retryAfter, open := g.breaker.blocked(g.now(), mode); open {
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
			g.breaker.record(opErr, g.now(), mode)
		})
	}, nil
}

func (g *Guard) reject(reason string) {
	if g.metrics != nil {
		g.metrics.UpstreamRejected.WithLabelValues(reason).Inc()
	}
}

// failCat classifies an operation result for the breaker.
type failCat int

const (
	catNone    failCat = iota // success, not-found, or client error: no upstream fault
	catGeneral                // transient failure or rate limit: an upstream-wide fault
	catAuth                   // authentication or challenge: a session-specific fault
)

func classify(err error) failCat {
	de, ok := domain.AsError(err)
	if !ok {
		return catNone
	}
	switch de.Code {
	case domain.CodeUpstreamAuthFailed:
		return catAuth
	case domain.CodeUpstreamRateLimited, domain.CodeUpstreamUnavailable,
		domain.CodeUpstreamTimeout, domain.CodeUpstreamParseError:
		return catGeneral
	default:
		return catNone
	}
}

func retryAfterOf(err error) int {
	if de, ok := domain.AsError(err); ok {
		return de.RetryAfter
	}
	return 0
}

// subBreaker is a single three-state circuit breaker: it opens after a threshold
// of consecutive failures, blocks for a cooldown, then admits one probe at a
// time. When grow is set the cooldown doubles on each successive trip.
type subBreaker struct {
	threshold int
	cooldown  time.Duration
	grow      bool

	failures  int
	strikes   int
	openUntil time.Time
	probing   bool
	open      bool
}

// blocking reports whether the sub-breaker currently blocks and a Retry-After
// hint, without mutating probe state.
func (s *subBreaker) blocking(now time.Time) (int, bool) {
	if s.openUntil.IsZero() {
		return 0, false
	}
	if now.Before(s.openUntil) {
		return retryAfterSeconds(s.openUntil, now), true
	}
	if s.probing {
		return 1, true
	}
	return 0, false
}

// canProbe reports whether the cooldown has elapsed and no probe is in flight.
func (s *subBreaker) canProbe(now time.Time) bool {
	return !s.openUntil.IsZero() && !now.Before(s.openUntil) && !s.probing
}

func (s *subBreaker) fail(now time.Time, retryAfter int) {
	s.failures++
	if s.failures >= s.threshold {
		s.trip(now, retryAfter)
	}
}

func (s *subBreaker) trip(now time.Time, retryAfter int) {
	cd := s.cooldown
	if s.grow {
		if s.strikes < maxSessionStrikes {
			s.strikes++
		}
		cd = s.cooldown << (s.strikes - 1)
	}
	if ra := time.Duration(retryAfter) * time.Second; ra > cd {
		cd = ra
	}
	s.openUntil = now.Add(cd)
	s.failures = 0
	s.open = true
}

func (s *subBreaker) close() {
	s.openUntil = time.Time{}
	s.failures = 0
	s.strikes = 0
	s.open = false
}

// probeResult resolves an in-flight probe: a matching failure reopens the
// breaker, anything else closes it.
func (s *subBreaker) probeResult(now time.Time, failed bool, retryAfter int) {
	s.probing = false
	if failed {
		s.trip(now, retryAfter)
	} else {
		s.close()
	}
}

// breaker composes the general and session sub-breakers and owns the shared
// circuit and session-health metrics.
type breaker struct {
	mu      sync.Mutex
	general subBreaker
	session subBreaker
	metrics *observability.Metrics
	logger  *slog.Logger
}

// blocked reports whether calls in the given mode are blocked and a Retry-After
// hint. The general breaker gates every mode; the session breaker gates only
// server-session traffic, so a dead server session never blocks caller sessions.
// When nothing blocks it admits at most one probe, preferring the general
// breaker since it gates all traffic.
func (b *breaker) blocked(now time.Time, mode domain.CredentialMode) (int, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if ra, blk := b.general.blocking(now); blk {
		return ra, true
	}
	if mode == domain.ModeServer {
		if ra, blk := b.session.blocking(now); blk {
			return ra, true
		}
	}
	if b.general.canProbe(now) {
		b.general.probing = true
	} else if mode == domain.ModeServer && b.session.canProbe(now) {
		b.session.probing = true
	}
	return 0, false
}

// record feeds an operation result back into the breakers. A general failure or
// rate limit feeds the general breaker for any mode. An authentication failure
// feeds the session breaker only for server-mode requests; a caller-mode
// authentication failure is handled by the caller-session health tracker and is
// deliberately ignored here so it can never trip the shared breaker.
func (b *breaker) record(err error, now time.Time, mode domain.CredentialMode) {
	b.mu.Lock()
	defer b.mu.Unlock()

	prevGeneralOpen := b.general.open
	prevSessionOpen := b.session.open

	cat := classify(err)
	retryAfter := retryAfterOf(err)

	switch {
	case b.general.probing:
		b.general.probeResult(now, cat == catGeneral, retryAfter)
	case cat == catGeneral:
		b.general.fail(now, retryAfter)
	case cat == catNone:
		b.general.failures = 0
	}

	if mode == domain.ModeServer {
		switch {
		case b.session.probing:
			b.session.probeResult(now, cat == catAuth, retryAfter)
		case cat == catAuth:
			b.session.fail(now, retryAfter)
		case cat == catNone:
			b.session.failures = 0
		}
	}

	b.sync(now, prevGeneralOpen, prevSessionOpen)
}

// sync reconciles the shared circuit and session-health metrics and logs after a
// state change.
func (b *breaker) sync(now time.Time, prevGeneralOpen, prevSessionOpen bool) {
	if b.metrics != nil {
		if b.general.open && !prevGeneralOpen {
			b.metrics.CircuitTrips.Inc()
		}
		if b.session.open && !prevSessionOpen {
			b.metrics.CircuitTrips.Inc()
		}
		open := b.general.open || b.session.open
		if open != (prevGeneralOpen || prevSessionOpen) {
			if open {
				b.metrics.CircuitOpen.Set(1)
			} else {
				b.metrics.CircuitOpen.Set(0)
			}
		}
		if b.session.open != prevSessionOpen {
			if b.session.open {
				b.metrics.SessionHealthy.Set(0)
			} else {
				b.metrics.SessionHealthy.Set(1)
			}
		}
	}
	if b.logger != nil && b.session.open != prevSessionOpen {
		if b.session.open {
			b.logger.Warn("linkedin server session appears unhealthy; pausing server-session upstream requests",
				"cooldown_seconds", retryAfterSeconds(b.session.openUntil, now))
		} else {
			b.logger.Info("linkedin server session recovered")
		}
	}
}

func retryAfterSeconds(until, now time.Time) int {
	if d := int(until.Sub(now).Seconds()); d >= 1 {
		return d
	}
	return 1
}
