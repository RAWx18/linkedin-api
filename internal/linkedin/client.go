// SPDX-FileCopyrightText: 2026 Ryan Madhuwala
// SPDX-License-Identifier: MIT

package linkedin

import (
	"context"
	"encoding/json"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/garudexlabs/linkedin-api/internal/audit"
	"github.com/garudexlabs/linkedin-api/internal/config"
	"github.com/garudexlabs/linkedin-api/internal/domain"
	"github.com/garudexlabs/linkedin-api/internal/observability"
)

// Client performs authenticated requests against the DASH API. It owns a pooled
// HTTP transport for connection reuse and applies bounded, jittered retries only
// to transient failures.
type Client struct {
	http         *http.Client
	baseURL      string
	session      *Session
	userAgent    string
	timeout      time.Duration
	maxRetries   int
	retryBackoff time.Duration
	metrics      *observability.Metrics
	logger       *slog.Logger
}

// NewClient constructs a Client from the LinkedIn configuration and session.
func NewClient(cfg config.LinkedInConfig, session *Session, metrics *observability.Metrics, logger *slog.Logger) *Client {
	transport := &http.Transport{
		Proxy:               http.ProxyFromEnvironment,
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
		ForceAttemptHTTP2:   true,
	}
	return &Client{
		http: &http.Client{
			Transport: transport,
			// Voyager redirects unauthenticated requests to a login wall. Return the
			// redirect instead of chasing it so the session state is read directly.
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		baseURL:      strings.TrimRight(cfg.BaseURL, "/"),
		session:      session,
		userAgent:    cfg.UserAgent,
		timeout:      cfg.Timeout,
		maxRetries:   cfg.MaxRetries,
		retryBackoff: cfg.RetryBackoff,
		metrics:      metrics,
		logger:       logger,
	}
}

// FetchProfile retrieves the member's base profile document by public identifier.
func (c *Client) FetchProfile(ctx context.Context, publicID string) (json.RawMessage, error) {
	path, query := profileRequest(publicID)
	return c.get(ctx, "profile", path, query)
}

// get runs a bounded retry loop over a single GET request. The overall deadline
// spans all attempts so total time is capped regardless of retries.
func (c *Client) get(ctx context.Context, endpoint, path string, query url.Values) (json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	target := c.baseURL + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}

	for attempt := 0; ; attempt++ {
		body, retry, err := c.attempt(ctx, endpoint, target)
		if err == nil {
			return body, nil
		}
		if attempt >= c.maxRetries || !retry {
			return nil, err
		}
		if !c.sleepBackoff(ctx, attempt) {
			return nil, err
		}
		c.metrics.UpstreamRetries.Inc()
		audit.AddRetry(ctx)
	}
}

// attempt performs one request and reports whether a retry is warranted.
func (c *Client) attempt(ctx context.Context, endpoint, target string) (json.RawMessage, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, false, domain.Internal(err)
	}
	c.setHeaders(req)

	start := time.Now()
	resp, err := c.http.Do(req)
	c.metrics.UpstreamDuration.WithLabelValues(endpoint).Observe(time.Since(start).Seconds())

	if err != nil {
		derr := classifyTransport(err)
		c.metrics.UpstreamRequests.WithLabelValues(endpoint, "error").Inc()
		if derr.Code == domain.CodeUpstreamTimeout {
			c.metrics.UpstreamTimeouts.Inc()
		}
		c.logger.DebugContext(ctx, "linkedin upstream transport error", "endpoint", endpoint, "code", derr.Code)
		return nil, retryableCode(derr.Code) && ctx.Err() == nil, derr
	}
	defer func() { _ = resp.Body.Close() }()

	c.metrics.UpstreamRequests.WithLabelValues(endpoint, strconv.Itoa(resp.StatusCode)).Inc()
	c.logger.DebugContext(ctx, "linkedin upstream response", "endpoint", endpoint, "status", resp.StatusCode)

	if resp.StatusCode == http.StatusOK {
		raw, rerr := readBody(resp.Body)
		if rerr != nil {
			return nil, false, domain.UpstreamParse(rerr)
		}
		return raw, false, nil
	}

	retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))
	drain(resp.Body)
	derr := classifyStatus(resp.StatusCode, retryAfter)
	switch derr.Code {
	case domain.CodeUpstreamRateLimited:
		c.metrics.UpstreamRateLimited.Inc()
	case domain.CodeUpstreamAuthFailed:
		c.metrics.UpstreamAuthFailures.Inc()
	}
	return nil, retryableStatus(resp.StatusCode), derr
}

func (c *Client) setHeaders(req *http.Request) {
	h := req.Header
	h.Set("Accept", "application/json")
	h.Set("User-Agent", c.userAgent)
	h.Set("X-RestLi-Protocol-Version", "2.0.0")
	h.Set("X-Li-Lang", "en_US")
	c.session.apply(h)
}

// sleepBackoff waits an exponential, jittered interval or aborts if the context
// is cancelled. It returns false when the wait was cut short by cancellation.
func (c *Client) sleepBackoff(ctx context.Context, attempt int) bool {
	backoff := c.retryBackoff << attempt
	jitter := time.Duration(rand.Int64N(int64(backoff)/2 + 1))
	timer := time.NewTimer(backoff + jitter)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
