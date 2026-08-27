// SPDX-FileCopyrightText: 2026 Ryan Madhuwala
// SPDX-License-Identifier: MIT

package api

import (
	"net/http"

	"github.com/garudexlabs/linkedin-api/internal/domain"
	"github.com/garudexlabs/linkedin-api/internal/linkedin"
)

// Caller sessions are supplied through dedicated headers, never through the URL,
// query string, or request body, so they cannot land in access logs or browser
// history.
const (
	headerLiAt       = "X-LinkedIn-Li-At"
	headerJSessionID = "X-LinkedIn-JSESSIONID"
	headerUserAgent  = "X-LinkedIn-User-Agent"
)

// maxCredentialLen bounds each caller cookie so a hostile header cannot be used
// to exhaust memory or smuggle control characters.
const maxCredentialLen = 4096

// callerCredential resolves which LinkedIn session a request uses. It returns the
// server credential when the caller supplies no session, a validation error when
// only part of the pair is present, a value is malformed, or caller sessions are
// disabled, and a request-scoped caller credential when both cookies are valid.
// The raw values are consumed here and never logged or stored.
func callerCredential(r *http.Request, allow bool) (linkedin.Credential, error) {
	liAt := r.Header.Get(headerLiAt)
	jsession := r.Header.Get(headerJSessionID)

	if liAt == "" && jsession == "" {
		return linkedin.ServerCredential(), nil
	}
	if !allow {
		return linkedin.Credential{}, domain.Invalid("caller-supplied LinkedIn sessions are not enabled")
	}
	if liAt == "" || jsession == "" {
		return linkedin.Credential{}, domain.Invalid("both " + headerLiAt + " and " + headerJSessionID + " are required to use a caller session")
	}
	if !validCredential(liAt) || !validCredential(jsession) {
		return linkedin.Credential{}, domain.Invalid("the supplied LinkedIn session headers are malformed")
	}
	userAgent := r.Header.Get(headerUserAgent)
	if userAgent != "" && !validCredential(userAgent) {
		return linkedin.Credential{}, domain.Invalid("the supplied " + headerUserAgent + " header is malformed")
	}
	return linkedin.NewCallerCredential(liAt, jsession).WithUserAgent(userAgent), nil
}

// validCredential rejects empty, oversized, or control-character-bearing values
// so a caller header can never be used for header injection or resource
// exhaustion before it is turned into a request-scoped session.
func validCredential(v string) bool {
	if v == "" || len(v) > maxCredentialLen {
		return false
	}
	for i := 0; i < len(v); i++ {
		if v[i] < 0x20 || v[i] == 0x7f {
			return false
		}
	}
	return true
}
