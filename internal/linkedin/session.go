// SPDX-FileCopyrightText: 2026 Ryan Madhuwala
// SPDX-License-Identifier: MIT

package linkedin

import (
	"fmt"
	"net/http"
	"strings"
)

// Session holds the immutable authentication material for the Voyager API. It is
// read-only after construction, so a failed or concurrent request can never
// corrupt shared client state or leak one request's context into another.
type Session struct {
	csrfToken    string
	cookieHeader string
}

// NewSession precomputes the Cookie header and derives the CSRF token. LinkedIn
// uses the JSESSIONID value (without surrounding quotes) as the csrf-token header
// while still sending the quoted form in the Cookie header.
func NewSession(liAt, jsessionID string) *Session {
	csrf := strings.Trim(jsessionID, `"`)
	return &Session{
		csrfToken:    csrf,
		cookieHeader: fmt.Sprintf("li_at=%s; JSESSIONID=%q", liAt, csrf),
	}
}

// apply sets the authentication headers on an outgoing request.
func (s *Session) apply(h http.Header) {
	h.Set("Cookie", s.cookieHeader)
	h.Set("Csrf-Token", s.csrfToken)
}
