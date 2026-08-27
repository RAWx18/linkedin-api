// SPDX-FileCopyrightText: 2026 Ryan Madhuwala
// SPDX-License-Identifier: MIT

package api_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
	"time"

	"github.com/garudexlabs/linkedin-api/internal/api"
	"github.com/garudexlabs/linkedin-api/internal/audit"
	"github.com/garudexlabs/linkedin-api/internal/config"
	"github.com/garudexlabs/linkedin-api/internal/observability"
	"github.com/garudexlabs/linkedin-api/internal/service"
)

// uiFS is a minimal embedded UI for router tests.
var uiFS = fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<html></html>")}}

type stubUsage struct{}

func (stubUsage) Summary(context.Context, time.Time) (audit.Summary, error) {
	return audit.Summary{}, nil
}
func (stubUsage) TopProfiles(context.Context, time.Time, int) ([]audit.Count, error) { return nil, nil }
func (stubUsage) TopClients(context.Context, time.Time, int) ([]audit.Count, error)  { return nil, nil }
func (stubUsage) TopUpstreamClients(context.Context, time.Time, int) ([]audit.Count, error) {
	return nil, nil
}

func newUIServer(t *testing.T, cfg *config.Config, m *mockClient, usage api.UsageQuerier) *httptest.Server {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	metrics := observability.NewMetrics()
	svc := service.NewProfileService(service.Deps{Client: m, Cache: noopCache{}, Metrics: metrics, Logger: logger})
	handler := api.NewRouter(api.Deps{
		Config: cfg, Service: svc, Metrics: metrics, Logger: logger,
		Ready: func() bool { return true }, UI: uiFS, Usage: usage,
	})
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

func TestAdminRequiresKey(t *testing.T) {
	cfg := baseConfig()
	cfg.Audit.AdminKeys = []string{"admin-key"}
	srv := newUIServer(t, cfg, &mockClient{}, stubUsage{})

	if resp, _ := get(t, srv, "/admin/usage", map[string]string{"Sec-Fetch-Site": "same-origin"}); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("admin same-origin without a key: status = %d, want 401", resp.StatusCode)
	}
	if resp, _ := get(t, srv, "/admin/usage", map[string]string{"X-API-Key": "admin-key"}); resp.StatusCode != http.StatusOK {
		t.Errorf("admin with a valid key: status = %d, want 200", resp.StatusCode)
	}
}

func TestHealthAndMetricsPublic(t *testing.T) {
	cfg := baseConfig()
	srv := newUIServer(t, cfg, &mockClient{}, nil)
	for _, p := range []string{"/healthz", "/readyz", "/metrics"} {
		if resp, _ := get(t, srv, p, nil); resp.StatusCode != http.StatusOK {
			t.Errorf("%s must stay public: status = %d, want 200", p, resp.StatusCode)
		}
	}
}
