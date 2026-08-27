// SPDX-FileCopyrightText: 2026 Ryan Madhuwala
// SPDX-License-Identifier: MIT

package linkedin_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/garudexlabs/linkedin-api/internal/domain"
	"github.com/garudexlabs/linkedin-api/internal/linkedin"
)

// captureHeaders returns a client pointed at a server that records the headers of
// the last request. The client uses the fixed server session li_at_value /
// ajax:1234 and User-Agent "test-agent".
func captureHeaders(t *testing.T, got *http.Header) *linkedin.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*got = r.Header.Clone()
		_, _ = w.Write([]byte(`{"elements":[]}`))
	}))
	t.Cleanup(srv.Close)
	return newTestClient(srv.URL, 2*time.Second, 0)
}

func TestRequestUsesConfiguredHeaders(t *testing.T) {
	var h http.Header
	c := captureHeaders(t, &h)

	if _, err := c.FetchProfile(context.Background(), "ada", linkedin.ServerCredential()); err != nil {
		t.Fatalf("fetch: %v", err)
	}

	want := map[string]string{
		"User-Agent":                "test-agent",
		"Accept":                    "application/json",
		"Accept-Language":           "en-US,en;q=0.9",
		"X-RestLi-Protocol-Version": "2.0.0",
		"X-Li-Lang":                 "en_US",
		"Csrf-Token":                "ajax:1234",
	}
	for k, v := range want {
		if got := h.Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
	if cookie := h.Get("Cookie"); !strings.Contains(cookie, "li_at=li_at_value") || !strings.Contains(cookie, "JSESSIONID=") {
		t.Errorf("cookie = %q", cookie)
	}
}

func TestRequestMetadataDeterministic(t *testing.T) {
	var seen []http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Header.Clone())
		_, _ = w.Write([]byte(`{"elements":[]}`))
	}))
	defer srv.Close()
	c := newTestClient(srv.URL, 2*time.Second, 0)

	for i := 0; i < 2; i++ {
		if _, err := c.FetchProfile(context.Background(), "ada", linkedin.ServerCredential()); err != nil {
			t.Fatalf("fetch %d: %v", i, err)
		}
	}
	if len(seen) != 2 {
		t.Fatalf("expected 2 requests, got %d", len(seen))
	}
	for _, k := range []string{"User-Agent", "Accept", "Accept-Language", "X-RestLi-Protocol-Version", "X-Li-Lang", "Csrf-Token", "Cookie"} {
		if seen[0].Get(k) != seen[1].Get(k) {
			t.Errorf("header %s differs between identical requests: %q vs %q", k, seen[0].Get(k), seen[1].Get(k))
		}
	}
}

func TestCallerUserAgentOverride(t *testing.T) {
	var h http.Header
	c := captureHeaders(t, &h)

	cred := linkedin.NewCallerCredential("caller_li", "ajax:caller").WithUserAgent("caller-agent/9.9")
	if _, err := c.FetchProfile(context.Background(), "ada", cred); err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if ua := h.Get("User-Agent"); ua != "caller-agent/9.9" {
		t.Errorf("user-agent = %q, want the caller override", ua)
	}
	if cookie := h.Get("Cookie"); !strings.Contains(cookie, "li_at=caller_li") || strings.Contains(cookie, "li_at_value") {
		t.Errorf("caller cookie = %q", cookie)
	}
}

func TestCallerWithoutUserAgentUsesServerUA(t *testing.T) {
	var h http.Header
	c := captureHeaders(t, &h)

	cred := linkedin.NewCallerCredential("caller_li", "ajax:caller")
	if _, err := c.FetchProfile(context.Background(), "ada", cred); err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if ua := h.Get("User-Agent"); ua != "test-agent" {
		t.Errorf("user-agent = %q, want the server default", ua)
	}
	if cookie := h.Get("Cookie"); !strings.Contains(cookie, "li_at=caller_li") {
		t.Errorf("caller cookie = %q", cookie)
	}
}

func TestForbiddenTreatedAsAuthFailure(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	_, err := newTestClient(srv.URL, 2*time.Second, 3).FetchProfile(context.Background(), "ada", linkedin.ServerCredential())
	assertCode(t, err, domain.CodeUpstreamAuthFailed)
	if n := atomic.LoadInt32(&hits); n != 1 {
		t.Errorf("a 403 must not be retried, hits = %d", n)
	}
}
