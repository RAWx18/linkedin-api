// SPDX-FileCopyrightText: 2026 Ryan Madhuwala
// SPDX-License-Identifier: MIT

package api

import (
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/garudexlabs/linkedin-api/internal/domain"
	"github.com/garudexlabs/linkedin-api/internal/observability"
)

type middleware func(http.Handler) http.Handler

// chain composes middleware so the first argument is the outermost layer.
func chain(h http.Handler, mws ...middleware) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}

// recoverer converts a panic into a safe 500 and logs it without exposing detail.
func recoverer(logger *slog.Logger) middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.ErrorContext(r.Context(), "panic recovered",
						"request_id", RequestIDFromContext(r.Context()),
						"route", routeLabel(r),
						"panic", rec,
					)
					writeError(w, r, domain.Internal(nil))
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// observe records latency and outcome for every request. It deliberately logs
// only non-sensitive fields: never the query string, headers, or credentials.
func observe(logger *slog.Logger, metrics *observability.Metrics) middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w}
			next.ServeHTTP(rec, r)

			route := routeLabel(r)
			status := rec.Status()
			dur := time.Since(start)

			metrics.HTTPRequests.WithLabelValues(route, r.Method, strconv.Itoa(status)).Inc()
			metrics.HTTPDuration.WithLabelValues(route).Observe(dur.Seconds())

			logger.InfoContext(r.Context(), "http_request",
				"request_id", RequestIDFromContext(r.Context()),
				"method", r.Method,
				"route", route,
				"status", status,
				"duration_ms", dur.Milliseconds(),
				"client_ip", clientIP(r),
			)
		})
	}
}

// routeLabel returns a low-cardinality route label. Unknown paths (including UI
// assets) collapse to "static" to keep metric and log cardinality bounded.
func routeLabel(r *http.Request) string {
	switch r.URL.Path {
	case "/v1/profile", "/healthz", "/readyz", "/metrics", "/admin/usage":
		return r.URL.Path
	default:
		return "static"
	}
}

// clientIP returns the remote IP without the port. It intentionally trusts only
// the connection's remote address rather than spoofable forwarding headers.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
