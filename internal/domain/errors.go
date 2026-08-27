// SPDX-FileCopyrightText: 2026 Ryan Madhuwala
// SPDX-License-Identifier: MIT

package domain

import (
	"errors"
	"fmt"
)

// Code is a stable, machine-readable error identifier returned to API clients.
type Code string

const (
	CodeInvalidRequest       Code = "invalid_request"
	CodeUnauthorized         Code = "unauthorized"
	CodeCallerSessionInvalid Code = "caller_session_invalid"
	CodeProfileNotFound      Code = "profile_not_found"
	CodeRateLimited          Code = "rate_limited"
	CodeUpstreamAuthFailed   Code = "upstream_auth_failed"
	CodeUpstreamRateLimited  Code = "upstream_rate_limited"
	CodeUpstreamTimeout      Code = "upstream_timeout"
	CodeUpstreamUnavailable  Code = "upstream_unavailable"
	CodeUpstreamParseError   Code = "upstream_parse_error"
	CodeInternal             Code = "internal_error"
)

// Error carries a client-safe message alongside an internal cause. Message may
// be returned to callers; cause holds detail that must never leave the process.
type Error struct {
	Code       Code
	Message    string
	RetryAfter int // seconds; only meaningful for rate-limit codes
	cause      error
}

func (e *Error) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.cause)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *Error) Unwrap() error { return e.cause }

func newError(code Code, msg string, cause error) *Error {
	return &Error{Code: code, Message: msg, cause: cause}
}

// Invalid reports malformed or unsupported client input.
func Invalid(msg string) *Error { return newError(CodeInvalidRequest, msg, nil) }

// Unauthorized reports a missing or invalid API key on the public API.
func Unauthorized(msg string) *Error { return newError(CodeUnauthorized, msg, nil) }

// CallerSessionInvalid reports that a caller-supplied LinkedIn session was
// rejected by LinkedIn or is already known to be expired. It never carries the
// credential values in its cause.
func CallerSessionInvalid(msg string) *Error { return newError(CodeCallerSessionInvalid, msg, nil) }

// NotFound reports that the requested profile could not be located.
func NotFound(msg string) *Error { return newError(CodeProfileNotFound, msg, nil) }

// RateLimited reports that the caller exceeded the public API rate limit.
func RateLimited(msg string, retryAfter int) *Error {
	e := newError(CodeRateLimited, msg, nil)
	e.RetryAfter = retryAfter
	return e
}

// Internal wraps an unexpected failure behind a generic client message.
func Internal(cause error) *Error {
	return newError(CodeInternal, "an unexpected internal error occurred", cause)
}

// UpstreamAuth reports that LinkedIn rejected the configured session.
func UpstreamAuth(cause error) *Error {
	return newError(CodeUpstreamAuthFailed, "authentication with LinkedIn failed", cause)
}

// UpstreamRateLimited reports that LinkedIn rate limited the request.
func UpstreamRateLimited(retryAfter int, cause error) *Error {
	e := newError(CodeUpstreamRateLimited, "LinkedIn rate limit reached, please retry later", cause)
	e.RetryAfter = retryAfter
	return e
}

// UpstreamTimeout reports that the LinkedIn request exceeded its deadline.
func UpstreamTimeout(cause error) *Error {
	return newError(CodeUpstreamTimeout, "the request to LinkedIn timed out", cause)
}

// UpstreamUnavailable reports a transient LinkedIn connectivity or server error.
func UpstreamUnavailable(cause error) *Error {
	return newError(CodeUpstreamUnavailable, "LinkedIn is temporarily unavailable", cause)
}

// UpstreamParse reports that a LinkedIn response could not be decoded.
func UpstreamParse(cause error) *Error {
	return newError(CodeUpstreamParseError, "could not parse the LinkedIn response", cause)
}

// AsError extracts a *Error from anywhere in an error chain.
func AsError(err error) (*Error, bool) {
	var e *Error
	if errors.As(err, &e) {
		return e, true
	}
	return nil, false
}
