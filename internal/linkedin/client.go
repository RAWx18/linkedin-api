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

// fallbackUserAgent is used only when no User-Agent is configured, which is
// legitimate only in development without a session. Any request that
// authenticates a session must present the exact browser User-Agent that created
// the cookies, so the session context stays explicit and immutable.
const fallbackUserAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 " +
	"(KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"

// defaultAcceptLanguage matches what a browser sends; its absence is a strong
// non-browser signal, so the client always presents it.
const defaultAcceptLanguage = "en-US,en;q=0.9"

// Client performs authenticated requests against the DASH API. It owns a pooled
// HTTP transport for connection reuse and applies bounded, jittered retries only
// to transient failures.
type Client struct {
	http           *http.Client
	baseURL        string
	session        *Session
	userAgent      string
	acceptLanguage string
	timeout        time.Duration
	maxRetries     int
	retryBackoff   time.Duration
	metrics        *observability.Metrics
	logger         *slog.Logger
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
		baseURL:        strings.TrimRight(cfg.BaseURL, "/"),
		session:        session,
		userAgent:      orDefault(cfg.UserAgent, fallbackUserAgent),
		acceptLanguage: orDefault(cfg.AcceptLanguage, defaultAcceptLanguage),
		timeout:        cfg.Timeout,
		maxRetries:     cfg.MaxRetries,
		retryBackoff:   cfg.RetryBackoff,
		metrics:        metrics,
		logger:         logger,
	}
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// FetchProfile retrieves the member's base profile document by public identifier.
// The credential selects which session authenticates the request: the server
// session by default, or a caller-supplied session for that single call.
func (c *Client) FetchProfile(ctx context.Context, publicID string, cred Credential) (json.RawMessage, error) {
	path, query := profileRequest(publicID)
	return c.get(ctx, "profile", path, query, cred)
}

// get runs a bounded retry loop over a single GET request. The overall deadline
// spans all attempts so total time is capped regardless of retries.
func (c *Client) get(ctx context.Context, endpoint, path string, query url.Values, cred Credential) (json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	target := c.baseURL + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}

	for attempt := 0; ; attempt++ {
		body, retry, err := c.attempt(ctx, endpoint, target, cred)
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
func (c *Client) attempt(ctx context.Context, endpoint, target string, cred Credential) (json.RawMessage, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, false, domain.Internal(err)
	}
	c.setHeaders(req, cred)
	c.logRequest(ctx, endpoint, req, cred)

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

func (c *Client) setHeaders(req *http.Request, cred Credential) {
	h := req.Header
	h.Set("Accept", "application/json")
	h.Set("User-Agent", cred.userAgentOr(c.userAgent))
	h.Set("Accept-Language", c.acceptLanguage)
	h.Set("X-RestLi-Protocol-Version", "2.0.0")
	h.Set("X-Li-Lang", "en_US")
	cred.applyTo(h, c.session)
}

// logRequest emits the exact request metadata at debug level for browser-vs-Go
// comparison. It logs only non-sensitive values: cookie and credential values,
// the CSRF token, and authorization material are never included, only the cookie
// names and whether the CSRF token is present.
func (c *Client) logRequest(ctx context.Context, endpoint string, req *http.Request, cred Credential) {
	c.logger.DebugContext(ctx, "linkedin upstream request",
		"endpoint", endpoint,
		"credential_mode", cred.Mode(),
		"user_agent", req.Header.Get("User-Agent"),
		"accept", req.Header.Get("Accept"),
		"accept_language", req.Header.Get("Accept-Language"),
		"x_li_lang", req.Header.Get("X-Li-Lang"),
		"x_restli_protocol_version", req.Header.Get("X-RestLi-Protocol-Version"),
		"cookies", cookieNames(req.Header.Get("Cookie")),
		"has_csrf_token", req.Header.Get("Csrf-Token") != "",
	)
}

// cookieNames extracts only the cookie names from a Cookie header, never the
// values, so the diagnostic can show which cookies are sent without leaking any.
func cookieNames(cookie string) string {
	if cookie == "" {
		return ""
	}
	parts := strings.Split(cookie, ";")
	names := make([]string, 0, len(parts))
	for _, p := range parts {
		if k, _, ok := strings.Cut(strings.TrimSpace(p), "="); ok && k != "" {
			names = append(names, k)
		}
	}
	return strings.Join(names, ",")
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
