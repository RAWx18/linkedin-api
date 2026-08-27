// SPDX-FileCopyrightText: 2026 Ryan Madhuwala
// SPDX-License-Identifier: MIT

package api_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/garudexlabs/linkedin-api/internal/api"
	"github.com/garudexlabs/linkedin-api/internal/audit"
	"github.com/garudexlabs/linkedin-api/internal/config"
	"github.com/garudexlabs/linkedin-api/internal/domain"
	"github.com/garudexlabs/linkedin-api/internal/observability"
	"github.com/garudexlabs/linkedin-api/internal/service"
)

const (
	hdrLiAt = "X-LinkedIn-Li-At"
	hdrJS   = "X-LinkedIn-JSESSIONID"
	hdrUA   = "X-LinkedIn-User-Agent"
)

func callerConfig() *config.Config {
	cfg := baseConfig()
	cfg.LinkedIn.AllowCallerSession = true
	return cfg
}

func TestCallerSessionModeSelected(t *testing.T) {
	m := &mockClient{}
	srv := newServer(callerConfig(), m)
	defer srv.Close()

	resp, body := get(t, srv, "/v1/profile?url=https://www.linkedin.com/in/ada",
		map[string]string{hdrLiAt: "caller_li", hdrJS: "ajax:caller"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d body = %s", resp.StatusCode, body)
	}
	if !m.credential().IsCaller() {
		t.Error("expected caller-session mode")
	}
}

func TestServerModeWhenNoCallerHeaders(t *testing.T) {
	m := &mockClient{}
	srv := newServer(callerConfig(), m)
	defer srv.Close()

	resp, _ := get(t, srv, "/v1/profile?url=https://www.linkedin.com/in/ada", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if m.credential().IsCaller() {
		t.Error("expected server-session mode")
	}
}

func TestPartialCallerCredentialsRejected(t *testing.T) {
	m := &mockClient{}
	srv := newServer(callerConfig(), m)
	defer srv.Close()

	for _, h := range []map[string]string{{hdrLiAt: "only"}, {hdrJS: "ajax:only"}} {
		resp, body := get(t, srv, "/v1/profile?url=https://www.linkedin.com/in/ada", h)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("status = %d", resp.StatusCode)
		}
		if code := decodeError(t, body).Error.Code; code != string(domain.CodeInvalidRequest) {
			t.Errorf("code = %q", code)
		}
	}
	if m.callCount() != 0 {
		t.Errorf("partial credentials must not reach upstream, calls = %d", m.callCount())
	}
}

func TestCallerSessionDisabledRejectsHeaders(t *testing.T) {
	m := &mockClient{}
	srv := newServer(baseConfig(), m) // AllowCallerSession defaults to false
	defer srv.Close()

	resp, body := get(t, srv, "/v1/profile?url=https://www.linkedin.com/in/ada",
		map[string]string{hdrLiAt: "x", hdrJS: "ajax:y"})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if code := decodeError(t, body).Error.Code; code != string(domain.CodeInvalidRequest) {
		t.Errorf("code = %q", code)
	}
	if m.callCount() != 0 {
		t.Error("a disabled caller session must not reach upstream")
	}
}

func TestOversizedCallerCredentialRejected(t *testing.T) {
	m := &mockClient{}
	srv := newServer(callerConfig(), m)
	defer srv.Close()

	resp, _ := get(t, srv, "/v1/profile?url=https://www.linkedin.com/in/ada",
		map[string]string{hdrLiAt: strings.Repeat("a", 5000), hdrJS: "ajax:y"})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if m.callCount() != 0 {
		t.Error("a malformed credential must not reach upstream")
	}
}

func TestCallerUserAgentHeaderAccepted(t *testing.T) {
	m := &mockClient{}
	srv := newServer(callerConfig(), m)
	defer srv.Close()

	resp, body := get(t, srv, "/v1/profile?url=https://www.linkedin.com/in/ada",
		map[string]string{hdrLiAt: "caller_li", hdrJS: "ajax:caller", hdrUA: "caller-agent/1.0"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d body = %s", resp.StatusCode, body)
	}
	if !m.credential().IsCaller() {
		t.Error("expected caller-session mode")
	}
}

func TestOversizedCallerUserAgentRejected(t *testing.T) {
	m := &mockClient{}
	srv := newServer(callerConfig(), m)
	defer srv.Close()

	resp, _ := get(t, srv, "/v1/profile?url=https://www.linkedin.com/in/ada",
		map[string]string{hdrLiAt: "caller_li", hdrJS: "ajax:caller", hdrUA: strings.Repeat("a", 5000)})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if m.callCount() != 0 {
		t.Error("a malformed user-agent must not reach upstream")
	}
}

func TestCallerSessionExpiredReturns401AndDoesNotRetry(t *testing.T) {
	m := &mockClient{profile: func() (json.RawMessage, error) {
		return nil, domain.UpstreamAuth(errors.New("401"))
	}}
	srv := newServer(callerConfig(), m)
	defer srv.Close()

	headers := map[string]string{hdrLiAt: "caller_li", hdrJS: "ajax:caller"}
	resp, body := get(t, srv, "/v1/profile?url=https://www.linkedin.com/in/ada", headers)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d body = %s", resp.StatusCode, body)
	}
	if code := decodeError(t, body).Error.Code; code != string(domain.CodeCallerSessionInvalid) {
		t.Errorf("code = %q", code)
	}

	resp, _ = get(t, srv, "/v1/profile?url=https://www.linkedin.com/in/ada", headers)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("second status = %d", resp.StatusCode)
	}
	if m.callCount() != 1 {
		t.Errorf("an expired caller session must not be retried upstream, calls = %d", m.callCount())
	}
}

// safeBuf is a concurrency-safe sink for captured logs.
type safeBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *safeBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *safeBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func newLeakServer(t *testing.T, cfg *config.Config, m *mockClient) (*httptest.Server, *safeBuf, string) {
	t.Helper()
	logBuf := &safeBuf{}
	logger := slog.New(slog.NewJSONHandler(logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	metrics := observability.NewMetrics()
	dbPath := filepath.Join(t.TempDir(), "audit.db")
	store, err := audit.Open(dbPath)
	if err != nil {
		t.Fatalf("open audit: %v", err)
	}
	writer := audit.NewWriter(store, audit.WriterConfig{
		BufferSize: 64, BatchSize: 8, FlushInterval: 5 * time.Millisecond,
		Retention: time.Hour, PurgeInterval: time.Hour,
	}, metrics, logger)
	svc := service.NewProfileService(service.Deps{
		Client: m, Cache: noopCache{}, Metrics: metrics, Logger: logger,
	})
	handler := api.NewRouter(api.Deps{
		Config: cfg, Service: svc, Metrics: metrics, Logger: logger,
		Ready: func() bool { return true }, Recorder: writer, Usage: store,
	})
	srv := httptest.NewServer(handler)
	t.Cleanup(func() {
		srv.Close()
		_ = writer.Close(context.Background())
	})
	return srv, logBuf, dbPath
}

func assertNoSentinel(t *testing.T, where, haystack string, sentinels []string) {
	t.Helper()
	for _, s := range sentinels {
		if strings.Contains(haystack, s) {
			t.Errorf("a credential sentinel leaked into %s", where)
		}
	}
}

// waitAuditRows waits for at least n rows to flush, then returns every text
// column concatenated so a test can scan the durable store for leaked secrets.
func waitAuditRows(t *testing.T, dbPath string, n int) string {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+dbPath+"?_pragma=busy_timeout(2000)")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() { _ = db.Close() }()

	deadline := time.Now().Add(3 * time.Second)
	for {
		var count int
		if err := db.QueryRow("SELECT COUNT(*) FROM audit_events").Scan(&count); err == nil && count >= n {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("audit rows did not appear in time")
		}
		time.Sleep(10 * time.Millisecond)
	}

	rows, err := db.Query(`SELECT request_id, client_ip, key_id, profile_id,
		credential_mode, cred_fp, upstream_outcome, rate_decision, error_class FROM audit_events`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer func() { _ = rows.Close() }()

	var sb strings.Builder
	for rows.Next() {
		var a, b, c, d, e, f, g, h, i string
		if err := rows.Scan(&a, &b, &c, &d, &e, &f, &g, &h, &i); err != nil {
			t.Fatalf("scan: %v", err)
		}
		sb.WriteString(strings.Join([]string{a, b, c, d, e, f, g, h, i}, "|"))
	}
	return sb.String()
}

func TestCallerCredentialsNeverLeak(t *testing.T) {
	const sentinelLiAt = "SENTINEL5LIAT5zzz111"
	const sentinelJSCore = "SENTINEL5JS5zzz222"
	m := &mockClient{}
	srv, logBuf, dbPath := newLeakServer(t, callerConfig(), m)

	headers := map[string]string{hdrLiAt: sentinelLiAt, hdrJS: "ajax:" + sentinelJSCore}
	resp, body := get(t, srv, "/v1/profile?url=https://www.linkedin.com/in/ada", headers)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d body = %s", resp.StatusCode, body)
	}
	if !m.credential().IsCaller() {
		t.Fatal("expected caller-session mode")
	}

	sentinels := []string{sentinelLiAt, sentinelJSCore}
	assertNoSentinel(t, "response body", string(body), sentinels)

	_, metricsBody := get(t, srv, "/metrics", nil)
	assertNoSentinel(t, "metrics", string(metricsBody), sentinels)

	assertNoSentinel(t, "audit store", waitAuditRows(t, dbPath, 1), sentinels)
	assertNoSentinel(t, "logs", logBuf.String(), sentinels)
}

func TestAuditRecordsCredentialModeNotSecrets(t *testing.T) {
	m := &mockClient{}
	srv, rec := newAuditServer(t, callerConfig(), m, nil, nil)
	const sentinel = "SENTINELCRED9999"
	get(t, srv, "/v1/profile?url=https://www.linkedin.com/in/ada",
		map[string]string{hdrLiAt: sentinel, hdrJS: "ajax:" + sentinel})

	r := waitForRecords(t, rec, 1)[0]
	if r.CredentialMode != string(domain.ModeCaller) {
		t.Errorf("credential_mode = %q", r.CredentialMode)
	}
	if !strings.HasPrefix(r.CredFP, "cs_") {
		t.Errorf("cred_fp = %q", r.CredFP)
	}
	if strings.Contains(r.CredFP, sentinel) || strings.Contains(r.CredentialMode, sentinel) {
		t.Error("audit record must not contain raw credential material")
	}

	srv2, rec2 := newAuditServer(t, callerConfig(), &mockClient{}, nil, nil)
	get(t, srv2, "/v1/profile?url=https://www.linkedin.com/in/ada", nil)
	r2 := waitForRecords(t, rec2, 1)[0]
	if r2.CredentialMode != string(domain.ModeServer) || r2.CredFP != "" {
		t.Errorf("server record mode = %q fp = %q", r2.CredentialMode, r2.CredFP)
	}
}

func TestConcurrentCallersDoNotLeakAcrossRequests(t *testing.T) {
	m := &mockClient{}
	srv := newServer(callerConfig(), m)
	defer srv.Close()

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			headers := map[string]string{
				hdrLiAt: "li_" + string(rune('A'+i)),
				hdrJS:   "ajax:" + string(rune('A'+i)),
			}
			resp, _ := get(t, srv, "/v1/profile?url=https://www.linkedin.com/in/ada", headers)
			if resp.StatusCode != http.StatusOK {
				t.Errorf("caller %d status = %d", i, resp.StatusCode)
			}
		}(i)
	}
	wg.Wait()
}
