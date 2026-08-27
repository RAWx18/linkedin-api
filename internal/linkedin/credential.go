// SPDX-FileCopyrightText: 2026 Ryan Madhuwala
// SPDX-License-Identifier: MIT

package linkedin

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"

	"github.com/garudexlabs/linkedin-api/internal/domain"
)

// fpKey is a process-scoped random key for fingerprinting caller credentials. A
// keyed hash keeps fingerprints non-reversible and uncorrelatable across process
// restarts, so a fingerprint can never be turned back into a credential.
var fpKey = randomKey()

func randomKey() []byte {
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		// If the OS RNG is unavailable the fingerprints simply become stable
		// across restarts; they stay non-reversible either way.
		for i := range k {
			k[i] = byte(i*7 + 1)
		}
	}
	return k
}

// Credential is an immutable, request-scoped choice of which LinkedIn session a
// single request uses. For caller mode it holds a session built from
// caller-supplied cookies, an optional caller-supplied User-Agent so the request
// matches the browser that created the cookies, and a non-reversible fingerprint;
// the raw cookie values live only inside the session for the duration of the
// request and are never logged, persisted, or used as a key.
type Credential struct {
	mode        domain.CredentialMode
	session     *Session
	userAgent   string
	fingerprint string
}

// ServerCredential selects the server-configured session.
func ServerCredential() Credential {
	return Credential{mode: domain.ModeServer}
}

// NewCallerCredential builds a request-scoped credential from caller cookies.
func NewCallerCredential(liAt, jsessionID string) Credential {
	return Credential{
		mode:        domain.ModeCaller,
		session:     NewSession(liAt, jsessionID),
		fingerprint: fingerprint(liAt, jsessionID),
	}
}

// WithUserAgent returns a copy of the credential that presents the given
// User-Agent, so a caller session can match the browser that created its cookies.
// An empty value leaves the server-configured User-Agent in place.
func (c Credential) WithUserAgent(ua string) Credential {
	c.userAgent = ua
	return c
}

// Mode reports the credential mode, defaulting to the server session.
func (c Credential) Mode() domain.CredentialMode {
	if c.mode == "" {
		return domain.ModeServer
	}
	return c.mode
}

// IsCaller reports whether the request uses a caller-supplied session.
func (c Credential) IsCaller() bool { return c.mode == domain.ModeCaller }

// Fingerprint returns the non-reversible caller-session identifier, or an empty
// string for the server session.
func (c Credential) Fingerprint() string { return c.fingerprint }

// userAgentOr returns the credential's own User-Agent when set, otherwise the
// given server default, so a caller session can match its originating browser.
func (c Credential) userAgentOr(fallback string) string {
	if c.userAgent != "" {
		return c.userAgent
	}
	return fallback
}

// applyTo writes the authentication headers for this request, using the caller
// session when present and otherwise the server session.
func (c Credential) applyTo(h http.Header, server *Session) {
	s := c.session
	if s == nil {
		s = server
	}
	if s != nil {
		s.apply(h)
	}
}

// fingerprint derives a short, non-reversible identifier for a caller session
// using a keyed hash so a caller's requests can be grouped for health tracking
// and audit without ever exposing or storing the credential values.
func fingerprint(liAt, jsessionID string) string {
	mac := hmac.New(sha256.New, fpKey)
	_, _ = mac.Write([]byte(liAt))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(strings.Trim(jsessionID, `"`)))
	return "cs_" + hex.EncodeToString(mac.Sum(nil))[:16]
}
