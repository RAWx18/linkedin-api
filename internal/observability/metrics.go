// SPDX-FileCopyrightText: 2026 Ryan Madhuwala
// SPDX-License-Identifier: MIT

package observability

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// durationBuckets target typical API and upstream latencies from 10ms to 10s.
var durationBuckets = []float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10}

// Metrics owns a private registry and every collector the service exposes. A
// private registry keeps metrics free of global state and safe to use in tests.
type Metrics struct {
	registry *prometheus.Registry

	HTTPRequests *prometheus.CounterVec
	HTTPDuration *prometheus.HistogramVec

	UpstreamRequests     *prometheus.CounterVec
	UpstreamDuration     *prometheus.HistogramVec
	UpstreamRetries      prometheus.Counter
	UpstreamTimeouts     prometheus.Counter
	UpstreamRateLimited  prometheus.Counter
	UpstreamAuthFailures prometheus.Counter
	ParseFailures        prometheus.Counter

	Cache    *prometheus.CounterVec
	Profiles *prometheus.CounterVec

	UpstreamRejected *prometheus.CounterVec
	CircuitOpen      prometheus.Gauge
	CircuitTrips     prometheus.Counter
	SessionHealthy   prometheus.Gauge
	Coalesced        prometheus.Counter

	CallerSessionInvalid    prometheus.Counter
	CallerSessionsUnhealthy prometheus.Gauge

	AuditWritten     prometheus.Counter
	AuditDropped     prometheus.Counter
	AuditWriteErrors prometheus.Counter
}

// NewMetrics constructs and registers all collectors, including Go runtime and
// process metrics.
func NewMetrics() *Metrics {
	reg := prometheus.NewRegistry()
	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)

	m := &Metrics{
		registry: reg,
		HTTPRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total HTTP requests handled, labeled by route, method and status.",
		}, []string{"route", "method", "status"}),
		HTTPDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "HTTP request latency in seconds, labeled by route.",
			Buckets: durationBuckets,
		}, []string{"route"}),
		UpstreamRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "upstream_requests_total",
			Help: "Total LinkedIn upstream requests, labeled by endpoint and status.",
		}, []string{"endpoint", "status"}),
		UpstreamDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "upstream_request_duration_seconds",
			Help:    "LinkedIn upstream request latency in seconds, labeled by endpoint.",
			Buckets: durationBuckets,
		}, []string{"endpoint"}),
		UpstreamRetries:      newCounter("upstream_retries_total", "Total upstream request retries."),
		UpstreamTimeouts:     newCounter("upstream_timeouts_total", "Total upstream request timeouts."),
		UpstreamRateLimited:  newCounter("upstream_rate_limited_total", "Total upstream rate-limit (429) responses."),
		UpstreamAuthFailures: newCounter("upstream_auth_failures_total", "Total upstream authentication failures."),
		ParseFailures:        newCounter("parse_failures_total", "Total upstream response parse failures."),
		Cache: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "profile_cache_total",
			Help: "Profile cache lookups, labeled by result (hit|miss).",
		}, []string{"result"}),
		Profiles: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "profiles_retrieved_total",
			Help: "Profile retrievals, labeled by result (success|failure).",
		}, []string{"result"}),
		UpstreamRejected: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "upstream_rejected_total",
			Help: "Requests rejected before reaching LinkedIn, labeled by reason (circuit_open|rate|concurrency).",
		}, []string{"reason"}),
		CircuitOpen:             newGauge("upstream_circuit_open", "Whether the upstream circuit breaker is open (1) or closed (0)."),
		CircuitTrips:            newCounter("upstream_circuit_trips_total", "Total times the upstream circuit breaker has opened."),
		SessionHealthy:          newGauge("upstream_session_healthy", "Whether the LinkedIn server session is healthy (1) or unhealthy (0) after auth or challenge responses."),
		Coalesced:               newCounter("upstream_requests_coalesced_total", "Requests served by coalescing onto an in-flight identical retrieval."),
		CallerSessionInvalid:    newCounter("caller_session_invalid_total", "Total caller-supplied sessions rejected by LinkedIn or fast-failed as already expired."),
		CallerSessionsUnhealthy: newGauge("caller_sessions_unhealthy", "Number of caller sessions currently tracked as invalid or expired."),
		AuditWritten:            newCounter("audit_events_written_total", "Total audit records durably written to the store."),
		AuditDropped:            newCounter("audit_events_dropped_total", "Total audit records dropped because the write buffer was full."),
		AuditWriteErrors:        newCounter("audit_write_errors_total", "Total audit batch writes that failed."),
	}

	reg.MustRegister(
		m.HTTPRequests, m.HTTPDuration, m.UpstreamRequests, m.UpstreamDuration,
		m.UpstreamRetries, m.UpstreamTimeouts, m.UpstreamRateLimited,
		m.UpstreamAuthFailures, m.ParseFailures, m.Cache, m.Profiles,
		m.UpstreamRejected, m.CircuitOpen, m.CircuitTrips, m.Coalesced,
		m.SessionHealthy,
		m.CallerSessionInvalid, m.CallerSessionsUnhealthy,
		m.AuditWritten, m.AuditDropped, m.AuditWriteErrors,
	)
	return m
}

// Handler serves the metrics in Prometheus text exposition format.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}

func newCounter(name, help string) prometheus.Counter {
	return prometheus.NewCounter(prometheus.CounterOpts{Name: name, Help: help})
}

func newGauge(name, help string) prometheus.Gauge {
	return prometheus.NewGauge(prometheus.GaugeOpts{Name: name, Help: help})
}
