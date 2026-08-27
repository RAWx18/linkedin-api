// SPDX-FileCopyrightText: 2026 Ryan Madhuwala
// SPDX-License-Identifier: MIT

package upstream

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/garudexlabs/linkedin-api/internal/domain"
	"github.com/garudexlabs/linkedin-api/internal/observability"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func hasCode(err error, code domain.Code) bool {
	de, ok := domain.AsError(err)
	return ok && de.Code == code
}

func TestGuardConcurrencyLimit(t *testing.T) {
	g := New(Config{MaxConcurrency: 1, RateRPS: 1000, RateBurst: 1000, FailureThreshold: 100, Cooldown: time.Minute}, nil, nil)

	release, err := g.Enter(context.Background())
	if err != nil {
		t.Fatalf("first enter: %v", err)
	}
	if _, err := g.Enter(context.Background()); !hasCode(err, domain.CodeUpstreamUnavailable) {
		t.Fatalf("expected concurrency rejection, got %v", err)
	}
	release(nil)
	release2, err := g.Enter(context.Background())
	if err != nil {
		t.Fatalf("after release: %v", err)
	}
	release2(nil)
}

func TestGuardRateLimit(t *testing.T) {
	g := New(Config{MaxConcurrency: 100, RateRPS: 0.001, RateBurst: 1, FailureThreshold: 100, Cooldown: time.Minute}, nil, nil)

	release, err := g.Enter(context.Background())
	if err != nil {
		t.Fatalf("first enter: %v", err)
	}
	release(nil)
	if _, err := g.Enter(context.Background()); !hasCode(err, domain.CodeUpstreamRateLimited) {
		t.Fatalf("expected rate rejection, got %v", err)
	}
}

func TestGuardOpensCircuitAndRejects(t *testing.T) {
	g := New(Config{MaxConcurrency: 100, RateRPS: 1000, RateBurst: 1000, FailureThreshold: 2, Cooldown: time.Minute}, nil, nil)
	transient := domain.UpstreamUnavailable(errors.New("503"))

	for i := 0; i < 2; i++ {
		release, err := g.Enter(context.Background())
		if err != nil {
			t.Fatalf("enter %d: %v", i, err)
		}
		release(transient)
	}

	_, err := g.Enter(context.Background())
	de, ok := domain.AsError(err)
	if !ok || de.Code != domain.CodeUpstreamUnavailable || de.RetryAfter <= 0 {
		t.Fatalf("expected open-circuit rejection with retry-after, got %v", err)
	}
}

func TestGuardSessionHealthMetric(t *testing.T) {
	m := observability.NewMetrics()
	g := New(Config{
		MaxConcurrency: 100, RateRPS: 1000, RateBurst: 1000,
		FailureThreshold: 100, Cooldown: time.Minute,
		SessionThreshold: 1, SessionCooldown: time.Minute,
	}, m, discardLogger())

	if testutil.ToFloat64(m.SessionHealthy) != 1 {
		t.Fatal("session should start healthy")
	}
	release, err := g.Enter(context.Background())
	if err != nil {
		t.Fatalf("enter: %v", err)
	}
	release(domain.UpstreamAuth(errors.New("401")))

	if testutil.ToFloat64(m.SessionHealthy) != 0 {
		t.Error("session should be unhealthy after an auth failure")
	}
	if _, err := g.Enter(context.Background()); !hasCode(err, domain.CodeUpstreamUnavailable) {
		t.Errorf("guard should reject while the session is unhealthy, got %v", err)
	}
}

func TestBreakerHalfOpenRecovers(t *testing.T) {
	b := &breaker{threshold: 3, cooldown: time.Minute}
	now := time.Now()
	transient := domain.UpstreamUnavailable(errors.New("x"))

	b.record(transient, now)
	b.record(transient, now)
	if _, blocked := b.blocked(now); blocked {
		t.Fatal("should not open before the threshold")
	}
	b.record(transient, now)
	if _, blocked := b.blocked(now); !blocked {
		t.Fatal("should open at the threshold")
	}

	later := now.Add(2 * time.Minute)
	if _, blocked := b.blocked(later); blocked {
		t.Fatal("should admit one probe after the cooldown")
	}
	if _, blocked := b.blocked(later); !blocked {
		t.Fatal("only a single probe should be admitted")
	}
	b.record(nil, later)
	if _, blocked := b.blocked(later); blocked {
		t.Fatal("a successful probe should close the breaker")
	}
}

func TestBreakerReopensOnFailedProbe(t *testing.T) {
	b := &breaker{threshold: 1, cooldown: time.Minute}
	now := time.Now()
	transient := domain.UpstreamUnavailable(errors.New("x"))

	b.record(transient, now)
	later := now.Add(2 * time.Minute)
	if _, blocked := b.blocked(later); blocked {
		t.Fatal("should admit a probe")
	}
	b.record(transient, later)
	if _, blocked := b.blocked(later); !blocked {
		t.Fatal("a failed probe should reopen the breaker")
	}
}

func TestBreakerRespectsRetryAfter(t *testing.T) {
	b := &breaker{threshold: 1, cooldown: time.Second}
	now := time.Now()

	b.record(domain.UpstreamRateLimited(120, errors.New("429")), now)
	ra, blocked := b.blocked(now)
	if !blocked || ra < 100 {
		t.Fatalf("cooldown should honor the upstream Retry-After, got ra=%d blocked=%v", ra, blocked)
	}
}

func TestBreakerIgnoresNotFound(t *testing.T) {
	b := &breaker{threshold: 1, cooldown: time.Minute}
	now := time.Now()

	b.record(domain.NotFound("missing"), now)
	if _, blocked := b.blocked(now); blocked {
		t.Fatal("a not-found result must not trip the breaker")
	}
}

func TestBreakerSessionTripsFastWithLongCooldown(t *testing.T) {
	m := observability.NewMetrics()
	b := &breaker{
		threshold: 10, cooldown: time.Second,
		sessionThreshold: 2, sessionCooldown: 5 * time.Minute,
		metrics: m, logger: discardLogger(),
	}
	now := time.Now()
	auth := domain.UpstreamAuth(errors.New("401"))

	b.record(auth, now)
	if _, blocked := b.blocked(now); blocked {
		t.Fatal("should not trip on the first session failure")
	}
	b.record(auth, now)
	ra, blocked := b.blocked(now)
	if !blocked {
		t.Fatal("should trip at the session threshold")
	}
	if ra < 240 {
		t.Errorf("session cooldown should be long, retry-after=%d", ra)
	}
	if testutil.ToFloat64(m.SessionHealthy) != 0 {
		t.Error("session should be marked unhealthy")
	}
	if testutil.ToFloat64(m.CircuitOpen) != 1 {
		t.Error("circuit should be open")
	}
}

func TestBreakerSessionRecoversOnProbe(t *testing.T) {
	m := observability.NewMetrics()
	b := &breaker{
		threshold: 10, cooldown: time.Second,
		sessionThreshold: 1, sessionCooldown: time.Minute,
		metrics: m, logger: discardLogger(),
	}
	now := time.Now()

	b.record(domain.UpstreamAuth(errors.New("401")), now)
	if testutil.ToFloat64(m.SessionHealthy) != 0 {
		t.Fatal("session should be unhealthy after an auth failure")
	}
	later := now.Add(2 * time.Minute)
	if _, blocked := b.blocked(later); blocked {
		t.Fatal("should admit a probe after the cooldown")
	}
	b.record(nil, later)
	if testutil.ToFloat64(m.SessionHealthy) != 1 {
		t.Error("a successful probe should restore session health")
	}
	if _, blocked := b.blocked(later); blocked {
		t.Error("breaker should be closed after recovery")
	}
}

func TestBreakerSessionCooldownGrows(t *testing.T) {
	b := &breaker{
		threshold: 10, cooldown: time.Second,
		sessionThreshold: 1, sessionCooldown: time.Minute,
		logger: discardLogger(),
	}
	now := time.Now()
	auth := domain.UpstreamAuth(errors.New("401"))

	b.record(auth, now)
	first, _ := b.blocked(now)

	later := now.Add(2 * time.Minute)
	b.blocked(later)      // admit the probe
	b.record(auth, later) // failed probe reopens with a longer cooldown
	second, _ := b.blocked(later)

	if second <= first {
		t.Errorf("session cooldown should grow after a failed probe, first=%d second=%d", first, second)
	}
}
