// SPDX-FileCopyrightText: 2026 Ryan Madhuwala
// SPDX-License-Identifier: MIT

package api

import (
	"net/http"
	"strconv"

	"github.com/garudexlabs/linkedin-api/internal/audit"
	"github.com/garudexlabs/linkedin-api/internal/domain"
)

type errorEnvelope struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Code      domain.Code `json:"code"`
	Message   string      `json:"message"`
	RequestID string      `json:"request_id,omitempty"`
}

// statusFor maps a machine-readable error code to an HTTP status. Upstream
// authentication and parse failures surface as 502 so callers can distinguish
// them from transient upstream problems (503/504).
func statusFor(code domain.Code) int {
	switch code {
	case domain.CodeInvalidRequest:
		return http.StatusBadRequest
	case domain.CodeUnauthorized:
		return http.StatusUnauthorized
	case domain.CodeProfileNotFound:
		return http.StatusNotFound
	case domain.CodeRateLimited, domain.CodeUpstreamRateLimited:
		return http.StatusTooManyRequests
	case domain.CodeUpstreamAuthFailed, domain.CodeUpstreamParseError:
		return http.StatusBadGateway
	case domain.CodeUpstreamTimeout:
		return http.StatusGatewayTimeout
	case domain.CodeUpstreamUnavailable:
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}

// writeError renders any error as a safe, structured JSON response. Non-domain
// errors collapse to a generic internal error so implementation detail never
// leaks to clients.
func writeError(w http.ResponseWriter, r *http.Request, err error) {
	de, ok := domain.AsError(err)
	if !ok {
		de = domain.Internal(err)
	}
	audit.SetError(r.Context(), string(de.Code))
	if de.RetryAfter > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(de.RetryAfter))
	}
	writeJSON(w, statusFor(de.Code), errorEnvelope{Error: errorBody{
		Code:      de.Code,
		Message:   de.Message,
		RequestID: RequestIDFromContext(r.Context()),
	}})
}
