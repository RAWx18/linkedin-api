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

func fullConfig() Config {
	return Config{
		MaxConcurrency: 100, RateRPS: 1000, RateBurst: 1000,
		FailureThreshold: 2, Cooldown: time.Minute,
		SessionThreshold: 2, SessionCooldown: 5 * time.Minute,
	}
}

func TestGuardConcurrencyLimit(t *testing.T) {
	cfg := fullConfig()
	cfg.MaxConcurrency = 1
	g := New(cfg, nil, nil)

	release, err := g.Enter(context.Background(), domain.ModeServer)
	if err != nil {
		t.Fatalf("first enter: %v", err)
	}
	if _, err := g.Enter(context.Background(), domain.ModeServer); !hasCode(err, domain.CodeUpstreamUnavailable) {
		t.Fatalf("expected concurrency rejection, got %v", err)
	}
	release(nil)
	release2, err := g.Enter(context.Background(), domain.ModeServer)
	if err != nil {
		t.Fatalf("after release: %v", err)
	}
	release2(nil)
}

func TestGuardRateLimit(t *testing.T) {
	cfg := fullConfig()
	cfg.RateRPS = 0.001
	cfg.RateBurst = 1
	g := New(cfg, nil, nil)

	release, err := g.Enter(context.Background(), domain.ModeServer)
	if err != nil {
		t.Fatalf("first enter: %v", err)
	}
	release(nil)
	if _, err := g.Enter(context.Background(), domain.ModeServer); !hasCode(err, domain.CodeUpstreamRateLimited) {
		t.Fatalf("expected rate rejection, got %v", err)
	}
}

func TestGeneralCircuitBlocksAllModes(t *testing.T) {
	g := New(fullConfig(), nil, nil)
	transient := domain.UpstreamUnavailable(errors.New("503"))

	for i := 0; i < 2; i++ {
		release, err := g.Enter(context.Background(), domain.ModeServer)
		if err != nil {
			t.Fatalf("enter %d: %v", i, err)
		}
		release(transient)
	}

	if _, err := g.Enter(context.Background(), domain.ModeServer); !hasCode(err, domain.CodeUpstreamUnavailable) {
		t.Errorf("general circuit should block server traffic, got %v", err)
	}
	if _, err := g.Enter(context.Background(), domain.ModeCaller); !hasCode(err, domain.CodeUpstreamUnavailable) {
		t.Errorf("general circuit should also block caller traffic, got %v", err)
	}
}

func TestServerSessionCircuitDoesNotBlockCaller(t *testing.T) {
	m := observability.NewMetrics()
	g := New(fullConfig(), m, discardLogger())
	auth := domain.UpstreamAuth(errors.New("401"))

	for i := 0; i < 2; i++ {
		release, err := g.Enter(context.Background(), domain.ModeServer)
		if err != nil {
			t.Fatalf("enter %d: %v", i, err)
		}
		release(auth)
	}

	if _, err := g.Enter(context.Background(), domain.ModeServer); !hasCode(err, domain.CodeUpstreamUnavailable) {
		t.Errorf("server-session circuit should block server traffic, got %v", err)
	}
	if testutil.ToFloat64(m.SessionHealthy) != 0 {
		t.Error("server session should be unhealthy")
	}
	release, err := g.Enter(context.Background(), domain.ModeCaller)
	if err != nil {
		t.Errorf("a dead server session must not block caller traffic, got %v", err)
	}
	if release != nil {
		release(nil)
	}
}

func TestCallerAuthDoesNotTripSharedBreaker(t *testing.T) {
	m := observability.NewMetrics()
	cfg := fullConfig()
	cfg.SessionThreshold = 1
	g := New(cfg, m, discardLogger())
	auth := domain.UpstreamAuth(errors.New("401"))

	for i := 0; i < 5; i++ {
		release, err := g.Enter(context.Background(), domain.ModeCaller)
		if err != nil {
			t.Fatalf("caller enter %d: %v", i, err)
		}
		release(auth)
	}

	if testutil.ToFloat64(m.SessionHealthy) != 1 {
		t.Error("caller auth failures must not affect server session health")
	}
	if testutil.ToFloat64(m.CircuitOpen) != 0 {
		t.Error("caller auth failures must not open the shared circuit")
	}
	release, err := g.Enter(context.Background(), domain.ModeServer)
	if err != nil {
		t.Errorf("server traffic must still be admitted, got %v", err)
	}
	if release != nil {
		release(nil)
	}
}

func TestGuardSessionHealthMetric(t *testing.T) {
	m := observability.NewMetrics()
	cfg := fullConfig()
	cfg.SessionThreshold = 1
	g := New(cfg, m, discardLogger())

	if testutil.ToFloat64(m.SessionHealthy) != 1 {
		t.Fatal("session should start healthy")
	}
	release, err := g.Enter(context.Background(), domain.ModeServer)
	if err != nil {
		t.Fatalf("enter: %v", err)
	}
	release(domain.UpstreamAuth(errors.New("401")))

	if testutil.ToFloat64(m.SessionHealthy) != 0 {
		t.Error("session should be unhealthy after a server auth failure")
	}
	if _, err := g.Enter(context.Background(), domain.ModeServer); !hasCode(err, domain.CodeUpstreamUnavailable) {
		t.Errorf("guard should reject server traffic while the session is unhealthy, got %v", err)
	}
}

func generalBreaker(threshold int, cooldown time.Duration) *breaker {
	return &breaker{general: subBreaker{threshold: threshold, cooldown: cooldown}}
}

func TestBreakerGeneralHalfOpenRecovers(t *testing.T) {
	b := generalBreaker(3, time.Minute)
	now := time.Now()
	transient := domain.UpstreamUnavailable(errors.New("x"))

	b.record(transient, now, domain.ModeServer)
	b.record(transient, now, domain.ModeServer)
	if _, blocked := b.blocked(now, domain.ModeServer); blocked {
		t.Fatal("should not open before the threshold")
	}
	b.record(transient, now, domain.ModeServer)
	if _, blocked := b.blocked(now, domain.ModeServer); !blocked {
		t.Fatal("should open at the threshold")
	}

	later := now.Add(2 * time.Minute)
	if _, blocked := b.blocked(later, domain.ModeServer); blocked {
		t.Fatal("should admit one probe after the cooldown")
	}
	if _, blocked := b.blocked(later, domain.ModeServer); !blocked {
		t.Fatal("only a single probe should be admitted")
	}
	b.record(nil, later, domain.ModeServer)
	if _, blocked := b.blocked(later, domain.ModeServer); blocked {
		t.Fatal("a successful probe should close the breaker")
	}
}

func TestBreakerReopensOnFailedProbe(t *testing.T) {
	b := generalBreaker(1, time.Minute)
	now := time.Now()
	transient := domain.UpstreamUnavailable(errors.New("x"))

	b.record(transient, now, domain.ModeServer)
	later := now.Add(2 * time.Minute)
	if _, blocked := b.blocked(later, domain.ModeServer); blocked {
		t.Fatal("should admit a probe")
	}
	b.record(transient, later, domain.ModeServer)
	if _, blocked := b.blocked(later, domain.ModeServer); !blocked {
		t.Fatal("a failed probe should reopen the breaker")
	}
}

func TestBreakerRespectsRetryAfter(t *testing.T) {
	b := generalBreaker(1, time.Second)
	now := time.Now()

	b.record(domain.UpstreamRateLimited(120, errors.New("429")), now, domain.ModeServer)
	ra, blocked := b.blocked(now, domain.ModeServer)
	if !blocked || ra < 100 {
		t.Fatalf("cooldown should honor the upstream Retry-After, got ra=%d blocked=%v", ra, blocked)
	}
}

func TestBreakerIgnoresNotFound(t *testing.T) {
	b := generalBreaker(1, time.Minute)
	now := time.Now()

	b.record(domain.NotFound("missing"), now, domain.ModeServer)
	if _, blocked := b.blocked(now, domain.ModeServer); blocked {
		t.Fatal("a not-found result must not trip the breaker")
	}
}

func TestBreakerSessionTripsFastWithLongCooldown(t *testing.T) {
	m := observability.NewMetrics()
	b := &breaker{
		general: subBreaker{threshold: 10, cooldown: time.Second},
		session: subBreaker{threshold: 2, cooldown: 5 * time.Minute, grow: true},
		metrics: m, logger: discardLogger(),
	}
	now := time.Now()
	auth := domain.UpstreamAuth(errors.New("401"))

	b.record(auth, now, domain.ModeServer)
	if _, blocked := b.blocked(now, domain.ModeServer); blocked {
		t.Fatal("should not trip on the first session failure")
	}
	b.record(auth, now, domain.ModeServer)
	ra, blocked := b.blocked(now, domain.ModeServer)
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
		general: subBreaker{threshold: 10, cooldown: time.Second},
		session: subBreaker{threshold: 1, cooldown: time.Minute, grow: true},
		metrics: m, logger: discardLogger(),
	}
	now := time.Now()

	b.record(domain.UpstreamAuth(errors.New("401")), now, domain.ModeServer)
	if testutil.ToFloat64(m.SessionHealthy) != 0 {
		t.Fatal("session should be unhealthy after an auth failure")
	}
	later := now.Add(2 * time.Minute)
	if _, blocked := b.blocked(later, domain.ModeServer); blocked {
		t.Fatal("should admit a probe after the cooldown")
	}
	b.record(nil, later, domain.ModeServer)
	if testutil.ToFloat64(m.SessionHealthy) != 1 {
		t.Error("a successful probe should restore session health")
	}
	if _, blocked := b.blocked(later, domain.ModeServer); blocked {
		t.Error("breaker should be closed after recovery")
	}
}

func TestBreakerSessionCooldownGrows(t *testing.T) {
	b := &breaker{
		general: subBreaker{threshold: 10, cooldown: time.Second},
		session: subBreaker{threshold: 1, cooldown: time.Minute, grow: true},
		logger:  discardLogger(),
	}
	now := time.Now()
	auth := domain.UpstreamAuth(errors.New("401"))

	b.record(auth, now, domain.ModeServer)
	first, _ := b.blocked(now, domain.ModeServer)

	later := now.Add(2 * time.Minute)
	b.blocked(later, domain.ModeServer)      // admit the probe
	b.record(auth, later, domain.ModeServer) // failed probe reopens with a longer cooldown
	second, _ := b.blocked(later, domain.ModeServer)

	if second <= first {
		t.Errorf("session cooldown should grow after a failed probe, first=%d second=%d", first, second)
	}
}
