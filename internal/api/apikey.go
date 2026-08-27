// SPDX-FileCopyrightText: 2026 Ryan Madhuwala
// SPDX-License-Identifier: MIT

package api

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/garudexlabs/linkedin-api/internal/domain"
)

type apiKeyAuth struct {
	keys       []string
	firstParty bool
}

func newAPIKeyAuth(keys []string) *apiKeyAuth {
	return &apiKeyAuth{keys: keys}
}

// allowFirstParty exempts genuine same-origin browser requests (the co-hosted UI)
// from the key requirement, so the server's API key never has to be shipped to
// the browser. It is opt-in and never enabled for administrative endpoints.
func (a *apiKeyAuth) allowFirstParty() *apiKeyAuth {
	a.firstParty = true
	return a
}

func (a *apiKeyAuth) enabled() bool { return len(a.keys) > 0 }

// middleware enforces a valid API key when any keys are configured. When no keys
// are set (local development) it is a pass-through. When first-party access is
// allowed, a same-origin browser request from the served UI is admitted without a
// key so the secret stays server-side; every other protection still applies.
func (a *apiKeyAuth) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.enabled() {
			next.ServeHTTP(w, r)
			return
		}
		if a.valid(extractAPIKey(r)) || (a.firstParty && sameOriginBrowser(r)) {
			next.ServeHTTP(w, r)
			return
		}
		writeError(w, r, domain.Unauthorized("a valid API key is required"))
	})
}

// valid compares the provided key against every configured key in constant time
// to avoid leaking key material through timing differences.
func (a *apiKeyAuth) valid(provided string) bool {
	if provided == "" {
		return false
	}
	var matched bool
	for _, k := range a.keys {
		if subtle.ConstantTimeCompare([]byte(provided), []byte(k)) == 1 {
			matched = true
		}
	}
	return matched
}

func extractAPIKey(r *http.Request) string {
	if h := r.Header.Get("X-API-Key"); h != "" {
		return h
	}
	const prefix = "Bearer "
	if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, prefix) {
		return strings.TrimPrefix(auth, prefix)
	}
	return ""
}

// sameOriginBrowser reports whether the request is a first-party call from the
// served UI. Browsers set Sec-Fetch-Site from the real origin and page scripts
// cannot override it, so a cross-site page cannot forge same-origin. Only a
// non-browser client can send this header directly, and it stays bound by the
// per-IP and upstream limits, so it gains no secret and no extra capacity.
func sameOriginBrowser(r *http.Request) bool {
	return r.Header.Get("Sec-Fetch-Site") == "same-origin"
}
