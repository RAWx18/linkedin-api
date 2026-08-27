// SPDX-FileCopyrightText: 2026 Ryan Madhuwala
// SPDX-License-Identifier: MIT

package api

import (
	"net/http"
	"testing"

	"github.com/garudexlabs/linkedin-api/internal/domain"
)

func TestStatusFor(t *testing.T) {
	cases := map[domain.Code]int{
		domain.CodeInvalidRequest:      http.StatusBadRequest,
		domain.CodeUnauthorized:        http.StatusUnauthorized,
		domain.CodeProfileNotFound:     http.StatusNotFound,
		domain.CodeRateLimited:         http.StatusTooManyRequests,
		domain.CodeUpstreamRateLimited: http.StatusTooManyRequests,
		domain.CodeUpstreamAuthFailed:  http.StatusBadGateway,
		domain.CodeUpstreamParseError:  http.StatusBadGateway,
		domain.CodeUpstreamTimeout:     http.StatusGatewayTimeout,
		domain.CodeUpstreamUnavailable: http.StatusServiceUnavailable,
		domain.CodeInternal:            http.StatusInternalServerError,
	}
	for code, want := range cases {
		if got := statusFor(code); got != want {
			t.Errorf("statusFor(%s) = %d, want %d", code, got, want)
		}
	}
	if got := statusFor(domain.Code("something_unknown")); got != http.StatusInternalServerError {
		t.Errorf("unknown code should map to 500, got %d", got)
	}
}
