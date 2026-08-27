// SPDX-FileCopyrightText: 2026 Ryan Madhuwala
// SPDX-License-Identifier: MIT

package api

import (
	"io/fs"
	"log/slog"
	"net/http"

	"github.com/garudexlabs/linkedin-api/internal/audit"
	"github.com/garudexlabs/linkedin-api/internal/config"
	"github.com/garudexlabs/linkedin-api/internal/observability"
	"github.com/garudexlabs/linkedin-api/internal/service"
)

// Deps holds everything the router needs to serve requests.
type Deps struct {
	Config   *config.Config
	Service  *service.ProfileService
	Metrics  *observability.Metrics
	Logger   *slog.Logger
	Ready    func() bool
	UI       fs.FS
	Recorder audit.Recorder
	Usage    UsageQuerier
}

// NewRouter builds the fully wired HTTP handler. The API surface (/v1/*) is
// wrapped with rate limiting and API-key auth; health, metrics and the UI are
// intentionally exempt so probes and dashboards work without credentials.
func NewRouter(d Deps) http.Handler {
	mux := http.NewServeMux()

	ph := &profileHandler{svc: d.Service, allowCaller: d.Config.LinkedIn.AllowCallerSession}
	hh := &healthHandler{ready: d.Ready}

	var apiMW []middleware
	if d.Recorder != nil {
		apiMW = append(apiMW, auditMiddleware(d.Recorder))
	}
	if d.Config.RateLimit.Enabled {
		apiMW = append(apiMW, newRateLimiter(d.Config.RateLimit.RPS, d.Config.RateLimit.Burst, clientIP, audit.DecisionIPLimited).middleware)
	}
	apiMW = append(apiMW, newAPIKeyAuth(d.Config.APIKeys).middleware)
	if d.Config.RateLimit.Enabled {
		apiMW = append(apiMW, newRateLimiter(d.Config.RateLimit.KeyRPS, d.Config.RateLimit.KeyBurst, extractAPIKey, audit.DecisionKeyLimited).middleware)
	}

	mux.Handle("GET /v1/profile", chain(http.HandlerFunc(ph.handle), apiMW...))

	mux.HandleFunc("GET /healthz", hh.live)
	mux.HandleFunc("GET /readyz", hh.readyz)

	if d.Usage != nil && len(d.Config.Audit.AdminKeys) > 0 {
		uh := &usageHandler{querier: d.Usage}
		admin := newAPIKeyAuth(d.Config.Audit.AdminKeys)
		mux.Handle("GET /admin/usage", admin.middleware(http.HandlerFunc(uh.handle)))
	}

	if d.Config.Metrics.Enabled {
		mux.Handle("GET /metrics", d.Metrics.Handler())
	}

	if d.UI != nil {
		mux.Handle("GET /", http.FileServer(http.FS(d.UI)))
	}

	return chain(mux, requestID, observe(d.Logger, d.Metrics), recoverer(d.Logger))
}
