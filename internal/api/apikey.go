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
	keys []string
}

func newAPIKeyAuth(keys []string) *apiKeyAuth {
	return &apiKeyAuth{keys: keys}
}

func (a *apiKeyAuth) enabled() bool { return len(a.keys) > 0 }

// middleware protects administrative routes when admin keys are configured.
func (a *apiKeyAuth) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.enabled() {
			next.ServeHTTP(w, r)
			return
		}
		if a.valid(extractAPIKey(r)) {
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
