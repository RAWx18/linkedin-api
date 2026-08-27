// SPDX-FileCopyrightText: 2026 Ryan Madhuwala
// SPDX-License-Identifier: MIT

package api_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/garudexlabs/linkedin-api/internal/api"
	"github.com/garudexlabs/linkedin-api/internal/audit"
	"github.com/garudexlabs/linkedin-api/internal/cache"
	"github.com/garudexlabs/linkedin-api/internal/config"
	"github.com/garudexlabs/linkedin-api/internal/observability"
	"github.com/garudexlabs/linkedin-api/internal/service"
)

const profileURL = "/v1/profile?url=https://www.linkedin.com/in/ada-lovelace"

type captureRecorder struct {
	mu   sync.Mutex
	recs []audit.Record
}

func (c *captureRecorder) Record(rec audit.Record) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.recs = append(c.recs, rec)
}

func (c *captureRecorder) snapshot() []audit.Record {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]audit.Record, len(c.recs))
	copy(out, c.recs)
	return out
}

func newAuditServer(t *testing.T, cfg *config.Config, m *mockClient, c service.Cache, usage api.UsageQuerier) (*httptest.Server, *captureRecorder) {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	metrics := observability.NewMetrics()
	if c == nil {
		c = noopCache{}
	}
	svc := service.NewProfileService(service.Deps{Client: m, Cache: c, Metrics: metrics, Logger: logger})
	rec := &captureRecorder{}
	handler := api.NewRouter(api.Deps{
		Config: cfg, Service: svc, Metrics: metrics, Logger: logger,
		Ready: func() bool { return true }, Recorder: rec, Usage: usage,
	})
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv, rec
}

func waitForRecords(t *testing.T, rec *captureRecorder, n int) []audit.Record {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if recs := rec.snapshot(); len(recs) >= n {
			return recs
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("expected %d audit records, got %d", n, len(rec.snapshot()))
	return nil
}

func TestAuditRecordsSuccessfulLookup(t *testing.T) {
	srv, rec := newAuditServer(t, baseConfig(), &mockClient{}, nil, nil)
	resp, _ := get(t, srv, profileURL, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	r := waitForRecords(t, rec, 1)[0]
	if r.ProfileID != "ada-lovelace" {
		t.Errorf("profile_id = %q, want ada-lovelace", r.ProfileID)
	}
	if !r.UpstreamCalled || r.UpstreamOutcome != audit.OutcomeOK {
		t.Errorf("upstream not recorded: called=%v outcome=%q", r.UpstreamCalled, r.UpstreamOutcome)
	}
	if r.Cached {
		t.Error("first lookup marked as cached")
	}
	if r.RateDecision != audit.DecisionAllowed || r.ErrorClass != "" || r.Status != http.StatusOK {
		t.Errorf("unexpected record: %+v", r)
	}
	if r.RequestID == "" {
		t.Error("request id not recorded")
	}
}

func TestAuditRecordsCacheHit(t *testing.T) {
	srv, rec := newAuditServer(t, baseConfig(), &mockClient{}, cache.NewTTL(time.Minute, 100), nil)
	get(t, srv, profileURL, nil)
	get(t, srv, profileURL, nil)
	recs := waitForRecords(t, rec, 2)
	if recs[0].Cached || !recs[0].UpstreamCalled {
		t.Errorf("first record should be an upstream miss: %+v", recs[0])
	}
	if !recs[1].Cached || recs[1].UpstreamCalled {
		t.Errorf("second record should be a cache hit with no upstream call: %+v", recs[1])
	}
}

func TestAuditRecordsRateLimitDecision(t *testing.T) {
	cfg := baseConfig()
	cfg.RateLimit = config.RateLimitConfig{Enabled: true, RPS: 1, Burst: 1}
	srv, rec := newAuditServer(t, cfg, &mockClient{}, nil, nil)

	for i := 0; i < 3; i++ {
		get(t, srv, profileURL, nil)
	}
	recs := waitForRecords(t, rec, 3)

	var limited bool
	for _, r := range recs {
		if r.RateDecision == audit.DecisionIPLimited {
			limited = true
			if r.Status != http.StatusTooManyRequests || r.ErrorClass != "rate_limited" {
				t.Errorf("rate-limited record malformed: %+v", r)
			}
			if r.UpstreamCalled {
				t.Error("rate-limited request should not reach upstream")
			}
		}
	}
	if !limited {
		t.Error("no request was recorded as ip_limited")
	}
}

func TestUsageEndpoint(t *testing.T) {
	store, err := audit.Open(filepath.Join(t.TempDir(), "usage.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	now := time.Now()
	if err := store.Insert(context.Background(), []audit.Record{
		{Time: now, ProfileID: "ada", ClientIP: "1.1.1.1", KeyID: "k", Status: 200, RateDecision: audit.DecisionAllowed, UpstreamCalled: true, UpstreamOutcome: audit.OutcomeOK},
		{Time: now, ProfileID: "ada", ClientIP: "1.1.1.1", KeyID: "k", Status: 200, RateDecision: audit.DecisionAllowed, Cached: true},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	cfg := baseConfig()
	cfg.Audit.AdminKeys = []string{"admin-key"}
	srv, _ := newAuditServer(t, cfg, &mockClient{}, nil, store)

	if resp, _ := get(t, srv, "/admin/usage", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("usage without admin key = %d, want 401", resp.StatusCode)
	}

	resp, body := get(t, srv, "/admin/usage?window=1h&limit=5", map[string]string{"X-API-Key": "admin-key"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("usage with admin key = %d, want 200", resp.StatusCode)
	}
	var out struct {
		Summary struct {
			Total int64 `json:"total_requests"`
		} `json:"summary"`
		TopProfiles []audit.Count `json:"top_profiles"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode usage: %v (%s)", err, body)
	}
	if out.Summary.Total != 2 {
		t.Errorf("total_requests = %d, want 2", out.Summary.Total)
	}
	if len(out.TopProfiles) == 0 || out.TopProfiles[0].Key != "ada" {
		t.Errorf("top profiles = %v, want ada first", out.TopProfiles)
	}
}

func TestUsageEndpointDisabledWithoutAdminKeys(t *testing.T) {
	store, err := audit.Open(filepath.Join(t.TempDir(), "usage.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	srv, _ := newAuditServer(t, baseConfig(), &mockClient{}, nil, store)
	if resp, _ := get(t, srv, "/admin/usage", map[string]string{"X-API-Key": "anything"}); resp.StatusCode != http.StatusNotFound {
		t.Errorf("usage endpoint should be unregistered without admin keys, got %d", resp.StatusCode)
	}
}
