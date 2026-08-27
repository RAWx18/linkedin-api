// SPDX-FileCopyrightText: 2026 Ryan Madhuwala
// SPDX-License-Identifier: MIT

package linkedin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/garudexlabs/linkedin-api/internal/domain"
)

// maxBodyBytes bounds how much of an upstream response we buffer, protecting the
// process from a hostile or malfunctioning upstream returning an enormous body.
const maxBodyBytes = 10 << 20 // 10 MiB

// classifyStatus maps a non-2xx upstream status to a structured domain error.
// The internal cause never includes the response body, so nothing sensitive can
// leak through the error chain.
func classifyStatus(status, retryAfter int) *domain.Error {
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return domain.UpstreamAuth(fmt.Errorf("linkedin returned status %d", status))
	case status >= 300 && status < 400:
		// A redirect from Voyager means the session is missing, invalid, or expired.
		return domain.UpstreamAuth(fmt.Errorf("linkedin redirected to login (status %d)", status))
	case status == http.StatusNotFound:
		return domain.NotFound("the requested LinkedIn profile was not found")
	case status == http.StatusTooManyRequests:
		return domain.UpstreamRateLimited(retryAfter, fmt.Errorf("linkedin returned status %d", status))
	default:
		return domain.UpstreamUnavailable(fmt.Errorf("linkedin returned status %d", status))
	}
}

// retryableStatus reports whether an upstream status is worth retrying. Only
// standard server-side failures are retried; auth, redirects, rate-limit,
// not-found, and non-standard codes such as LinkedIn's 999 are not.
func retryableStatus(status int) bool {
	return status >= 500 && status < 600
}

// retryableCode reports whether a transport-level domain error is retryable.
func retryableCode(code domain.Code) bool {
	return code == domain.CodeUpstreamTimeout || code == domain.CodeUpstreamUnavailable
}

// classifyTransport distinguishes timeouts from other connectivity failures.
func classifyTransport(err error) *domain.Error {
	if errors.Is(err, context.DeadlineExceeded) {
		return domain.UpstreamTimeout(err)
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return domain.UpstreamTimeout(err)
	}
	return domain.UpstreamUnavailable(err)
}

// readBody reads an upstream body up to the size cap, rejecting oversized ones.
func readBody(body io.Reader) (json.RawMessage, error) {
	data, err := io.ReadAll(io.LimitReader(body, maxBodyBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxBodyBytes {
		return nil, errors.New("linkedin response exceeded size limit")
	}
	return json.RawMessage(data), nil
}

// drain consumes a small prefix of a discarded body so the connection can be
// reused from the pool instead of being closed.
func drain(body io.Reader) {
	_, _ = io.Copy(io.Discard, io.LimitReader(body, 4096))
}

// parseRetryAfter interprets a Retry-After header given as seconds or HTTP-date.
func parseRetryAfter(v string) int {
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil {
		if secs < 0 {
			return 0
		}
		return secs
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := int(time.Until(t).Seconds()); d > 0 {
			return d
		}
	}
	return 0
}
