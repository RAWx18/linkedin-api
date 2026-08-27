// SPDX-FileCopyrightText: 2026 Ryan Madhuwala
// SPDX-License-Identifier: MIT

package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/garudexlabs/linkedin-api/internal/cache"
	"github.com/garudexlabs/linkedin-api/internal/domain"
	"github.com/garudexlabs/linkedin-api/internal/linkedin"
	"github.com/garudexlabs/linkedin-api/internal/observability"
	"github.com/garudexlabs/linkedin-api/internal/service"
)

func newServiceTTL(m *mockClient, c service.Cache, ttl time.Duration) *service.ProfileService {
	return service.NewProfileService(service.Deps{
		Client: m, Cache: c, Metrics: observability.NewMetrics(),
		Logger: discardLogger(), CallerSessionTTL: ttl,
	})
}

func isCode(err error, want domain.Code) bool {
	de, ok := domain.AsError(err)
	return ok && de.Code == want
}

func TestCallerSessionDoesNotUseSharedCache(t *testing.T) {
	m := &mockClient{}
	svc := newServiceTTL(m, cache.NewTTL(time.Minute, 10), time.Minute)
	caller := linkedin.NewCallerCredential("li_at_A", "ajax:A")

	res, err := svc.GetProfile(context.Background(), testRef(), caller)
	if err != nil {
		t.Fatalf("caller fetch: %v", err)
	}
	if res.Meta.Cached {
		t.Error("a caller result must never be marked cached")
	}

	// A second caller request for the same profile hits upstream again: the
	// caller path never reads the shared cache.
	if _, err := svc.GetProfile(context.Background(), testRef(), caller); err != nil {
		t.Fatalf("second caller fetch: %v", err)
	}
	if c := atomic.LoadInt32(&m.profileCalls); c != 2 {
		t.Errorf("caller requests must not be served from cache, upstream calls = %d", c)
	}

	// A server request also misses, proving the caller never populated the cache.
	if _, err := svc.GetProfile(context.Background(), testRef(), linkedin.ServerCredential()); err != nil {
		t.Fatalf("server fetch: %v", err)
	}
	if c := atomic.LoadInt32(&m.profileCalls); c != 3 {
		t.Errorf("server request should miss the cache after caller-only traffic, calls = %d", c)
	}

	// The server response is cached, so a second server request is served locally.
	if _, err := svc.GetProfile(context.Background(), testRef(), linkedin.ServerCredential()); err != nil {
		t.Fatalf("server fetch 2: %v", err)
	}
	if c := atomic.LoadInt32(&m.profileCalls); c != 3 {
		t.Errorf("server response should be cached, calls = %d", c)
	}
}

func TestCallerSessionsDoNotCoalesce(t *testing.T) {
	release := make(chan struct{})
	m := &mockClient{profile: func() (json.RawMessage, error) {
		<-release
		return json.RawMessage(`{"elements":[{"firstName":"Ada"}]}`), nil
	}}
	svc := newServiceTTL(m, cache.Noop{}, time.Minute)
	caller := linkedin.NewCallerCredential("li_at_A", "ajax:A")

	const n = 6
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = svc.GetProfile(context.Background(), testRef(), caller)
		}()
	}
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()

	if c := atomic.LoadInt32(&m.profileCalls); c != n {
		t.Errorf("caller requests must not coalesce, upstream calls = %d want %d", c, n)
	}
}

func TestCallerSessionExpiredNoFallbackNoRetry(t *testing.T) {
	m := &mockClient{profile: func() (json.RawMessage, error) {
		return nil, domain.UpstreamAuth(errors.New("401"))
	}}
	svc := newServiceTTL(m, cache.Noop{}, time.Minute)
	caller := linkedin.NewCallerCredential("li_at_A", "ajax:A")

	_, err := svc.GetProfile(context.Background(), testRef(), caller)
	assertCode(t, err, domain.CodeCallerSessionInvalid)
	if !m.usedCaller() {
		t.Error("client must be called with the caller credential, never the server session")
	}
	if c := atomic.LoadInt32(&m.profileCalls); c != 1 {
		t.Errorf("caller auth failure must not retry or fall back, calls = %d", c)
	}

	// The same session is now fast-failed with no upstream traffic at all.
	_, err = svc.GetProfile(context.Background(), testRef(), caller)
	assertCode(t, err, domain.CodeCallerSessionInvalid)
	if c := atomic.LoadInt32(&m.profileCalls); c != 1 {
		t.Errorf("an expired caller session must not be retried upstream, calls = %d", c)
	}
}

func TestCallerNewCredentialsAreFreshContext(t *testing.T) {
	m := &mockClient{profile: func() (json.RawMessage, error) {
		return nil, domain.UpstreamAuth(errors.New("401"))
	}}
	svc := newServiceTTL(m, cache.Noop{}, time.Minute)

	a := linkedin.NewCallerCredential("li_at_A", "ajax:A")
	b := linkedin.NewCallerCredential("li_at_B", "ajax:B")

	if _, err := svc.GetProfile(context.Background(), testRef(), a); !isCode(err, domain.CodeCallerSessionInvalid) {
		t.Fatalf("A: %v", err)
	}
	// A is expired now, so it fast-fails with no new upstream call.
	_, _ = svc.GetProfile(context.Background(), testRef(), a)
	if c := atomic.LoadInt32(&m.profileCalls); c != 1 {
		t.Errorf("A should be fast-failed after expiry, calls = %d", c)
	}
	// B has a different fingerprint, so it is a fresh context and is attempted.
	if _, err := svc.GetProfile(context.Background(), testRef(), b); !isCode(err, domain.CodeCallerSessionInvalid) {
		t.Fatalf("B: %v", err)
	}
	if c := atomic.LoadInt32(&m.profileCalls); c != 2 {
		t.Errorf("new credentials must be a fresh context, calls = %d", c)
	}
}

func TestTwoCallersDoNotShareData(t *testing.T) {
	m := &mockClient{}
	svc := newServiceTTL(m, cache.NewTTL(time.Minute, 10), time.Minute)
	a := linkedin.NewCallerCredential("li_at_A", "ajax:A")
	b := linkedin.NewCallerCredential("li_at_B", "ajax:B")

	if _, err := svc.GetProfile(context.Background(), testRef(), a); err != nil {
		t.Fatalf("A: %v", err)
	}
	if _, err := svc.GetProfile(context.Background(), testRef(), b); err != nil {
		t.Fatalf("B: %v", err)
	}
	if c := atomic.LoadInt32(&m.profileCalls); c != 2 {
		t.Errorf("each caller must fetch independently, calls = %d", c)
	}
}

func TestConcurrentDistinctCallers(t *testing.T) {
	m := &mockClient{}
	svc := newServiceTTL(m, cache.NewTTL(time.Minute, 100), time.Minute)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			cred := linkedin.NewCallerCredential(fmt.Sprintf("li_%d", i), fmt.Sprintf("ajax:%d", i))
			_, _ = svc.GetProfile(context.Background(), testRef(), cred)
		}(i)
	}
	wg.Wait()

	if c := atomic.LoadInt32(&m.profileCalls); c != 20 {
		t.Errorf("distinct concurrent callers each fetch once, calls = %d", c)
	}
}
