// SPDX-FileCopyrightText: 2026 Ryan Madhuwala
// SPDX-License-Identifier: MIT

package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/garudexlabs/linkedin-api/internal/cache"
	"github.com/garudexlabs/linkedin-api/internal/domain"
	"github.com/garudexlabs/linkedin-api/internal/observability"
	"github.com/garudexlabs/linkedin-api/internal/service"
	"github.com/garudexlabs/linkedin-api/internal/urlx"
)

type mockClient struct {
	profileCalls int32
	profile      func() (json.RawMessage, error)
}

func (m *mockClient) FetchProfile(context.Context, string) (json.RawMessage, error) {
	atomic.AddInt32(&m.profileCalls, 1)
	if m.profile != nil {
		return m.profile()
	}
	return json.RawMessage(`{"elements":[{"firstName":"Ada","lastName":"Lovelace"}]}`), nil
}

type rejectGate struct{ err error }

func (g rejectGate) Enter(context.Context) (func(error), error) { return nil, g.err }

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newService(m *mockClient, c service.Cache) *service.ProfileService {
	if c == nil {
		c = cache.Noop{}
	}
	return service.NewProfileService(service.Deps{
		Client: m, Cache: c, Metrics: observability.NewMetrics(), Logger: discardLogger(),
	})
}

func testRef() urlx.ProfileRef {
	return urlx.ProfileRef{PublicID: "ada", CanonicalURL: "https://www.linkedin.com/in/ada"}
}

func assertCode(t *testing.T, err error, want domain.Code) {
	t.Helper()
	de, ok := domain.AsError(err)
	if !ok || de.Code != want {
		t.Fatalf("code = %v, want %s", err, want)
	}
}

func TestGetProfileSuccess(t *testing.T) {
	res, err := newService(&mockClient{}, nil).GetProfile(context.Background(), testRef())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Profile.FullName != "Ada Lovelace" {
		t.Errorf("full name = %q", res.Profile.FullName)
	}
	if res.Profile.PublicIdentifier != "ada" {
		t.Errorf("public id = %q", res.Profile.PublicIdentifier)
	}
	if res.Meta.Cached {
		t.Error("fresh result should not be marked cached")
	}
	if res.Meta.Source != "linkedin" || res.Meta.SchemaVersion != domain.SchemaVersion {
		t.Errorf("meta = %+v", res.Meta)
	}
}

func TestGetProfileCacheHit(t *testing.T) {
	m := &mockClient{}
	svc := newService(m, cache.NewTTL(time.Minute, 10))

	if _, err := svc.GetProfile(context.Background(), testRef()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	res, err := svc.GetProfile(context.Background(), testRef())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Meta.Cached {
		t.Error("second lookup should be served from cache")
	}
	if c := atomic.LoadInt32(&m.profileCalls); c != 1 {
		t.Errorf("upstream should be called once, got %d", c)
	}
}

func TestGetProfileUpstreamError(t *testing.T) {
	m := &mockClient{profile: func() (json.RawMessage, error) {
		return nil, domain.UpstreamAuth(errors.New("401"))
	}}
	_, err := newService(m, nil).GetProfile(context.Background(), testRef())
	assertCode(t, err, domain.CodeUpstreamAuthFailed)
}

func TestGetProfileParseError(t *testing.T) {
	m := &mockClient{profile: func() (json.RawMessage, error) {
		return json.RawMessage(`not json`), nil
	}}
	_, err := newService(m, nil).GetProfile(context.Background(), testRef())
	assertCode(t, err, domain.CodeUpstreamParseError)
}

func TestGetProfileNotFoundIsNegativelyCached(t *testing.T) {
	m := &mockClient{profile: func() (json.RawMessage, error) {
		return json.RawMessage(`{"elements":[]}`), nil
	}}
	svc := service.NewProfileService(service.Deps{
		Client:   m,
		Cache:    cache.Noop{},
		Negative: cache.NewNegative(time.Minute, 10),
		Metrics:  observability.NewMetrics(),
		Logger:   discardLogger(),
	})
	if _, err := svc.GetProfile(context.Background(), testRef()); err == nil {
		t.Fatal("expected not-found error")
	}
	_, err := svc.GetProfile(context.Background(), testRef())
	assertCode(t, err, domain.CodeProfileNotFound)
	if c := atomic.LoadInt32(&m.profileCalls); c != 1 {
		t.Errorf("second lookup should hit the negative cache, upstream calls = %d", c)
	}
}

func TestGetProfileGateRejection(t *testing.T) {
	m := &mockClient{}
	svc := service.NewProfileService(service.Deps{
		Client:  m,
		Cache:   cache.Noop{},
		Gate:    rejectGate{err: domain.UpstreamUnavailable(errors.New("circuit open"))},
		Metrics: observability.NewMetrics(),
		Logger:  discardLogger(),
	})
	_, err := svc.GetProfile(context.Background(), testRef())
	assertCode(t, err, domain.CodeUpstreamUnavailable)
	if c := atomic.LoadInt32(&m.profileCalls); c != 0 {
		t.Errorf("gate rejection must not reach upstream, calls = %d", c)
	}
}

func TestGetProfileCoalescesConcurrentLookups(t *testing.T) {
	release := make(chan struct{})
	m := &mockClient{profile: func() (json.RawMessage, error) {
		<-release
		return json.RawMessage(`{"elements":[{"firstName":"Ada","lastName":"Lovelace"}]}`), nil
	}}
	svc := newService(m, nil)

	const n = 8
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = svc.GetProfile(context.Background(), testRef())
		}()
	}
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()

	if c := atomic.LoadInt32(&m.profileCalls); c != 1 {
		t.Errorf("concurrent identical lookups should coalesce to one upstream call, got %d", c)
	}
}
