// SPDX-FileCopyrightText: 2026 Ryan Madhuwala
// SPDX-License-Identifier: MIT

package domain

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestAsError(t *testing.T) {
	base := UpstreamAuth(errors.New("401"))
	wrapped := fmt.Errorf("service: %w", base)
	got, ok := AsError(wrapped)
	if !ok {
		t.Fatal("expected to extract a *Error from the chain")
	}
	if got.Code != CodeUpstreamAuthFailed {
		t.Errorf("code = %s", got.Code)
	}
	if _, ok := AsError(errors.New("plain")); ok {
		t.Error("a plain error should not extract a *Error")
	}
}

func TestErrorMessageAndUnwrap(t *testing.T) {
	cause := errors.New("root cause")
	e := UpstreamUnavailable(cause)
	if !errors.Is(e, cause) {
		t.Error("Unwrap should expose the cause")
	}
	if !strings.Contains(e.Error(), "root cause") {
		t.Errorf("Error() should include the cause: %s", e.Error())
	}
	if plain := Invalid("bad input"); plain.Unwrap() != nil {
		t.Error("a constructor without a cause should unwrap to nil")
	}
}

func TestRetryAfterCarried(t *testing.T) {
	if e := RateLimited("slow down", 7); e.RetryAfter != 7 || e.Code != CodeRateLimited {
		t.Errorf("rate limited: %+v", e)
	}
	if e := UpstreamRateLimited(30, errors.New("429")); e.RetryAfter != 30 || e.Code != CodeUpstreamRateLimited {
		t.Errorf("upstream rate limited: %+v", e)
	}
}

func TestConstructorCodes(t *testing.T) {
	cases := map[Code]*Error{
		CodeInvalidRequest:      Invalid("x"),
		CodeUnauthorized:        Unauthorized("x"),
		CodeProfileNotFound:     NotFound("x"),
		CodeInternal:            Internal(errors.New("x")),
		CodeUpstreamTimeout:     UpstreamTimeout(errors.New("x")),
		CodeUpstreamParseError:  UpstreamParse(errors.New("x")),
		CodeUpstreamUnavailable: UpstreamUnavailable(errors.New("x")),
	}
	for want, e := range cases {
		if e.Code != want {
			t.Errorf("got %s, want %s", e.Code, want)
		}
	}
}
