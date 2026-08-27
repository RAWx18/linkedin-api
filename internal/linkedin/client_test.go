// SPDX-FileCopyrightText: 2026 Ryan Madhuwala
// SPDX-License-Identifier: MIT

package linkedin_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/garudexlabs/linkedin-api/internal/config"
	"github.com/garudexlabs/linkedin-api/internal/domain"
	"github.com/garudexlabs/linkedin-api/internal/linkedin"
	"github.com/garudexlabs/linkedin-api/internal/observability"
)

func newTestClient(baseURL string, timeout time.Duration, retries int) *linkedin.Client {
	cfg := config.LinkedInConfig{
		BaseURL:      baseURL,
		UserAgent:    "test-agent",
		Timeout:      timeout,
		MaxRetries:   retries,
		RetryBackoff: time.Millisecond,
	}
	session := linkedin.NewSession("li_at_value", "ajax:1234")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return linkedin.NewClient(cfg, session, observability.NewMetrics(), logger)
}

func assertCode(t *testing.T, err error, want domain.Code) {
	t.Helper()
	de, ok := domain.AsError(err)
	if !ok {
		t.Fatalf("expected a domain error, got %v", err)
	}
	if de.Code != want {
		t.Errorf("code = %s, want %s", de.Code, want)
	}
}

func TestFetchProfileSuccess(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		if got := r.Header.Get("Csrf-Token"); got != "ajax:1234" {
			t.Errorf("csrf-token header = %q", got)
		}
		if !strings.Contains(r.Header.Get("Cookie"), "li_at=li_at_value") {
			t.Errorf("cookie header = %q", r.Header.Get("Cookie"))
		}
		if !strings.HasSuffix(r.URL.Path, "/dash/profiles") {
			t.Errorf("path = %s", r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("q") != "memberIdentity" || q.Get("memberIdentity") != "ada" {
			t.Errorf("query = %s", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`{"elements":[{"firstName":"Ada"}]}`))
	}))
	defer srv.Close()

	raw, err := newTestClient(srv.URL, 2*time.Second, 2).FetchProfile(context.Background(), "ada", linkedin.ServerCredential())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(string(raw), "Ada") {
		t.Errorf("body = %s", raw)
	}
	if n := atomic.LoadInt32(&hits); n != 1 {
		t.Errorf("hits = %d, want 1", n)
	}
}

func TestFetchAuthFailureNoRetry(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	_, err := newTestClient(srv.URL, 2*time.Second, 3).FetchProfile(context.Background(), "ada", linkedin.ServerCredential())
	assertCode(t, err, domain.CodeUpstreamAuthFailed)
	if n := atomic.LoadInt32(&hits); n != 1 {
		t.Errorf("auth failures must not retry, hits = %d", n)
	}
}

func TestFetchNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	_, err := newTestClient(srv.URL, 2*time.Second, 2).FetchProfile(context.Background(), "ada", linkedin.ServerCredential())
	assertCode(t, err, domain.CodeProfileNotFound)
}

func TestFetchRateLimitedNoRetry(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Header().Set("Retry-After", "42")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	_, err := newTestClient(srv.URL, 2*time.Second, 3).FetchProfile(context.Background(), "ada", linkedin.ServerCredential())
	de, ok := domain.AsError(err)
	if !ok || de.Code != domain.CodeUpstreamRateLimited {
		t.Fatalf("code = %v", err)
	}
	if de.RetryAfter != 42 {
		t.Errorf("retry-after = %d, want 42", de.RetryAfter)
	}
	if n := atomic.LoadInt32(&hits); n != 1 {
		t.Errorf("rate-limit responses must not retry, hits = %d", n)
	}
}

func TestFetchServerErrorRetries(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, err := newTestClient(srv.URL, 2*time.Second, 2).FetchProfile(context.Background(), "ada", linkedin.ServerCredential())
	assertCode(t, err, domain.CodeUpstreamUnavailable)
	if n := atomic.LoadInt32(&hits); n != 3 {
		t.Errorf("expected 3 attempts (1 + 2 retries), got %d", n)
	}
}

func TestFetchTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	_, err := newTestClient(srv.URL, 50*time.Millisecond, 0).FetchProfile(context.Background(), "ada", linkedin.ServerCredential())
	assertCode(t, err, domain.CodeUpstreamTimeout)
}

func TestFetchReturnsRawBody(t *testing.T) {
	// The client returns raw bytes; JSON validity is the parser's concern.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{not-json`))
	}))
	defer srv.Close()

	raw, err := newTestClient(srv.URL, time.Second, 0).FetchProfile(context.Background(), "ada", linkedin.ServerCredential())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(raw) != `{not-json` {
		t.Errorf("body = %s", raw)
	}
}

func TestFetchRejectsOversizedBody(t *testing.T) {
	const tooBig = (10 << 20) + 1
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.CopyN(w, filler{}, tooBig)
	}))
	defer srv.Close()

	_, err := newTestClient(srv.URL, 5*time.Second, 0).FetchProfile(context.Background(), "ada", linkedin.ServerCredential())
	assertCode(t, err, domain.CodeUpstreamParseError)
}

type filler struct{}

func (filler) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'a'
	}
	return len(p), nil
}

func TestFetchRedirectTreatedAsAuthFailure(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		// LinkedIn redirects unauthenticated calls to a login wall.
		http.Redirect(w, r, "/login", http.StatusFound)
	}))
	defer srv.Close()

	_, err := newTestClient(srv.URL, 2*time.Second, 3).FetchProfile(context.Background(), "ada", linkedin.ServerCredential())
	assertCode(t, err, domain.CodeUpstreamAuthFailed)
	if n := atomic.LoadInt32(&hits); n != 1 {
		t.Errorf("redirect must not be followed or retried, hits = %d", n)
	}
}
