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
	Config      *config.Config
	Service     *service.ProfileService
	ImageClient *http.Client
	Metrics     *observability.Metrics
	Logger      *slog.Logger
	Ready       func() bool
	UI          fs.FS
	Recorder    audit.Recorder
	Usage       UsageQuerier
}

// NewRouter builds the fully wired HTTP handler. The API surface (/v1/*) is
// wrapped with rate limiting and API-key auth; health, metrics and the UI are
// intentionally exempt so probes and dashboards work without credentials. When
// the UI is served, same-origin requests from it are admitted without an API key
// so the server's key never reaches the browser; programmatic clients still need
// the key, and the admin endpoint is never exempt.
func NewRouter(d Deps) http.Handler {
	mux := http.NewServeMux()

	ph := &profileHandler{svc: d.Service, allowCaller: d.Config.LinkedIn.AllowCallerSession}
	imageClient := d.ImageClient
	if imageClient == nil {
		imageClient = defaultImageClient()
	}
	ih := &imageHandler{client: imageClient}
	hh := &healthHandler{ready: d.Ready}

	publicAuth := newAPIKeyAuth(d.Config.APIKeys)
	if d.UI != nil {
		publicAuth.allowFirstParty()
	}

	depth := d.Config.Server.TrustedProxyDepth

	var apiMW []middleware
	if d.Config.Audit.Enabled || d.Recorder != nil {
		apiMW = append(apiMW, auditMiddleware(d.Logger, d.Recorder, depth))
	}
	if d.Config.RateLimit.Enabled {
		apiMW = append(apiMW, newRateLimiter(d.Config.RateLimit.RPS, d.Config.RateLimit.Burst, func(r *http.Request) string { return clientIP(r, depth) }, audit.DecisionIPLimited).middleware)
	}
	apiMW = append(apiMW, publicAuth.middleware)
	if d.Config.RateLimit.Enabled {
		apiMW = append(apiMW, newRateLimiter(d.Config.RateLimit.KeyRPS, d.Config.RateLimit.KeyBurst, extractAPIKey, audit.DecisionKeyLimited).middleware)
	}

	mux.Handle("GET /v1/profile", chain(http.HandlerFunc(ph.handle), apiMW...))
	mux.Handle("GET /v1/image", chain(http.HandlerFunc(ih.handle), apiMW...))

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
		files := http.FileServer(http.FS(d.UI))
		mux.Handle("GET /", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-cache")
			files.ServeHTTP(w, r)
		}))
	}

	return chain(mux, requestID, observe(d.Logger, d.Metrics, depth), recoverer(d.Logger))
}
