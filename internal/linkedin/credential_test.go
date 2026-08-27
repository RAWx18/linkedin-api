// SPDX-FileCopyrightText: 2026 Ryan Madhuwala
// SPDX-License-Identifier: MIT

package linkedin_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/garudexlabs/linkedin-api/internal/domain"
	"github.com/garudexlabs/linkedin-api/internal/linkedin"
)

// captureCookies returns a server that records the auth headers of the last
// request and the client pointed at it. The client is built with the fixed
// server session li_at_value / ajax:1234.
func captureCookies(t *testing.T, cookie, csrf *string) *linkedin.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*cookie = r.Header.Get("Cookie")
		*csrf = r.Header.Get("Csrf-Token")
		_, _ = w.Write([]byte(`{"elements":[]}`))
	}))
	t.Cleanup(srv.Close)
	return newTestClient(srv.URL, 2*time.Second, 0)
}

func TestServerCredentialUsesServerSession(t *testing.T) {
	var cookie, csrf string
	c := captureCookies(t, &cookie, &csrf)

	if _, err := c.FetchProfile(context.Background(), "ada", linkedin.ServerCredential()); err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if !strings.Contains(cookie, "li_at=li_at_value") {
		t.Errorf("server cookie = %q", cookie)
	}
	if csrf != "ajax:1234" {
		t.Errorf("server csrf = %q", csrf)
	}
}

func TestCallerCredentialOverridesServerSession(t *testing.T) {
	var cookie, csrf string
	c := captureCookies(t, &cookie, &csrf)

	cred := linkedin.NewCallerCredential("caller_li_at", "ajax:caller")
	if _, err := c.FetchProfile(context.Background(), "ada", cred); err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if !strings.Contains(cookie, "li_at=caller_li_at") {
		t.Errorf("caller cookie = %q", cookie)
	}
	if strings.Contains(cookie, "li_at_value") {
		t.Error("server credential leaked into a caller request")
	}
	if csrf != "ajax:caller" {
		t.Errorf("caller csrf = %q", csrf)
	}
}

func TestFingerprintStableAndNonReversible(t *testing.T) {
	a := linkedin.NewCallerCredential("secret_li_at", "ajax:secret")
	b := linkedin.NewCallerCredential("secret_li_at", "ajax:secret")
	if a.Fingerprint() != b.Fingerprint() {
		t.Error("identical credentials should share a fingerprint")
	}
	if a.Fingerprint() == linkedin.NewCallerCredential("other", "ajax:other").Fingerprint() {
		t.Error("different credentials should not share a fingerprint")
	}
	if !strings.HasPrefix(a.Fingerprint(), "cs_") {
		t.Errorf("fingerprint format = %q", a.Fingerprint())
	}
	if strings.Contains(a.Fingerprint(), "secret") {
		t.Error("fingerprint must not contain the raw credential")
	}
	if !a.IsCaller() || a.Mode() != domain.ModeCaller {
		t.Errorf("caller mode = %v", a.Mode())
	}

	s := linkedin.ServerCredential()
	if s.IsCaller() || s.Fingerprint() != "" || s.Mode() != domain.ModeServer {
		t.Errorf("server mode = %v fp = %q", s.Mode(), s.Fingerprint())
	}
}
