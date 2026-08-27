// SPDX-FileCopyrightText: 2026 Ryan Madhuwala
// SPDX-License-Identifier: MIT

package linkedin

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/garudexlabs/linkedin-api/internal/domain"
)

func TestClassifyStatus(t *testing.T) {
	cases := []struct {
		status   int
		wantCode domain.Code
	}{
		{http.StatusUnauthorized, domain.CodeUpstreamAuthFailed},
		{http.StatusForbidden, domain.CodeUpstreamAuthFailed},
		{http.StatusNotFound, domain.CodeProfileNotFound},
		{http.StatusTooManyRequests, domain.CodeUpstreamRateLimited},
		{http.StatusInternalServerError, domain.CodeUpstreamUnavailable},
		{http.StatusBadGateway, domain.CodeUpstreamUnavailable},
		{http.StatusTeapot, domain.CodeUpstreamUnavailable},
		{http.StatusFound, domain.CodeUpstreamAuthFailed},
		{999, domain.CodeUpstreamUnavailable},
	}
	for _, tc := range cases {
		if e := classifyStatus(tc.status, 0); e.Code != tc.wantCode {
			t.Errorf("status %d: code = %s, want %s", tc.status, e.Code, tc.wantCode)
		}
	}
	if e := classifyStatus(http.StatusTooManyRequests, 12); e.RetryAfter != 12 {
		t.Errorf("retry-after not carried: %d", e.RetryAfter)
	}
}

func TestRetryableStatus(t *testing.T) {
	if !retryableStatus(500) || !retryableStatus(503) {
		t.Error("5xx should be retryable")
	}
	if retryableStatus(429) || retryableStatus(404) || retryableStatus(401) {
		t.Error("4xx should not be retryable")
	}
	if !retryableStatus(599) {
		t.Error("599 should be retryable")
	}
	if retryableStatus(600) || retryableStatus(999) {
		t.Error("non-standard codes at or above 600 should not be retryable")
	}
}

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

func TestClassifyTransport(t *testing.T) {
	if e := classifyTransport(context.DeadlineExceeded); e.Code != domain.CodeUpstreamTimeout {
		t.Errorf("deadline: %s", e.Code)
	}
	if e := classifyTransport(errors.New("connection refused")); e.Code != domain.CodeUpstreamUnavailable {
		t.Errorf("generic: %s", e.Code)
	}
	if e := classifyTransport(fmt.Errorf("dial: %w", timeoutErr{})); e.Code != domain.CodeUpstreamTimeout {
		t.Errorf("net timeout: %s", e.Code)
	}
}

func TestParseRetryAfter(t *testing.T) {
	if got := parseRetryAfter(""); got != 0 {
		t.Errorf("empty = %d", got)
	}
	if got := parseRetryAfter("30"); got != 30 {
		t.Errorf("seconds = %d", got)
	}
	if got := parseRetryAfter("-5"); got != 0 {
		t.Errorf("negative = %d", got)
	}
	if got := parseRetryAfter("garbage"); got != 0 {
		t.Errorf("garbage = %d", got)
	}
	future := time.Now().Add(2 * time.Minute).UTC().Format(http.TimeFormat)
	if got := parseRetryAfter(future); got <= 0 {
		t.Errorf("http-date should yield positive seconds, got %d", got)
	}
	past := time.Now().Add(-time.Hour).UTC().Format(http.TimeFormat)
	if got := parseRetryAfter(past); got != 0 {
		t.Errorf("past date should yield 0, got %d", got)
	}
}

func TestReadBodySizeCap(t *testing.T) {
	if _, err := readBody(strings.NewReader("hello")); err != nil {
		t.Errorf("small body should read cleanly: %v", err)
	}
	oversized := io.LimitReader(zeroReader{}, maxBodyBytes+10)
	if _, err := readBody(oversized); err == nil {
		t.Error("expected an error when the body exceeds the size cap")
	}
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) { return len(p), nil }
