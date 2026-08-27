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
	"testing"

	"github.com/garudexlabs/linkedin-api/internal/api"
	"github.com/garudexlabs/linkedin-api/internal/config"
	"github.com/garudexlabs/linkedin-api/internal/domain"
	"github.com/garudexlabs/linkedin-api/internal/observability"
	"github.com/garudexlabs/linkedin-api/internal/service"
)

type mockClient struct {
	profile func() (json.RawMessage, error)
}

func (m *mockClient) FetchProfile(context.Context, string) (json.RawMessage, error) {
	if m.profile != nil {
		return m.profile()
	}
	return json.RawMessage(`{"elements":[{"firstName":"Ada","lastName":"Lovelace","headline":"Math"}]}`), nil
}

type noopCache struct{}

func (noopCache) Get(string) (*domain.ProfileResult, bool) { return nil, false }
func (noopCache) Set(string, *domain.ProfileResult)        {}

func baseConfig() *config.Config {
	return &config.Config{
		Env:       config.EnvDevelopment,
		Server:    config.ServerConfig{Port: 8080},
		Cache:     config.CacheConfig{Enabled: false},
		RateLimit: config.RateLimitConfig{Enabled: false},
		Metrics:   config.MetricsConfig{Enabled: true},
		Log:       config.LogConfig{Format: "json"},
	}
}

func newServer(cfg *config.Config, m *mockClient) *httptest.Server {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	metrics := observability.NewMetrics()
	svc := service.NewProfileService(service.Deps{
		Client: m, Cache: noopCache{}, Metrics: metrics, Logger: logger,
	})
	handler := api.NewRouter(api.Deps{
		Config: cfg, Service: svc, Metrics: metrics, Logger: logger,
		Ready: func() bool { return true },
	})
	return httptest.NewServer(handler)
}

func get(t *testing.T, srv *httptest.Server, path string, headers map[string]string) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, srv.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	return resp, body
}

func decodeError(t *testing.T, body []byte) struct {
	Error struct {
		Code      string `json:"code"`
		Message   string `json:"message"`
		RequestID string `json:"request_id"`
	} `json:"error"`
} {
	t.Helper()
	var env struct {
		Error struct {
			Code      string `json:"code"`
			Message   string `json:"message"`
			RequestID string `json:"request_id"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("decode error envelope: %v (%s)", err, body)
	}
	return env
}

func TestHealthz(t *testing.T) {
	srv := newServer(baseConfig(), &mockClient{})
	defer srv.Close()
	resp, _ := get(t, srv, "/healthz", nil)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d", resp.StatusCode)
	}
}

func TestReadyz(t *testing.T) {
	srv := newServer(baseConfig(), &mockClient{})
	defer srv.Close()
	if resp, _ := get(t, srv, "/readyz", nil); resp.StatusCode != http.StatusOK {
		t.Errorf("ready status = %d", resp.StatusCode)
	}
}

func TestReadyzNotReady(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	metrics := observability.NewMetrics()
	svc := service.NewProfileService(service.Deps{
		Client: &mockClient{}, Cache: noopCache{}, Metrics: metrics, Logger: logger,
	})
	handler := api.NewRouter(api.Deps{
		Config: baseConfig(), Service: svc, Metrics: metrics, Logger: logger,
		Ready: func() bool { return false },
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()
	if resp, _ := get(t, srv, "/readyz", nil); resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("not-ready status = %d", resp.StatusCode)
	}
}

func TestProfileSuccess(t *testing.T) {
	srv := newServer(baseConfig(), &mockClient{})
	defer srv.Close()
	resp, body := get(t, srv, "/v1/profile?url=https://www.linkedin.com/in/ada-lovelace", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d body = %s", resp.StatusCode, body)
	}
	if resp.Header.Get("X-Request-ID") == "" {
		t.Error("expected an X-Request-ID header")
	}
	var env struct {
		Data struct {
			FullName   string `json:"full_name"`
			ProfileURL string `json:"profile_url"`
		} `json:"data"`
		Meta struct {
			Source string `json:"source"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Data.FullName != "Ada Lovelace" {
		t.Errorf("full_name = %q", env.Data.FullName)
	}
	if env.Meta.Source != "linkedin" {
		t.Errorf("source = %q", env.Meta.Source)
	}
}

func TestProfileMissingURL(t *testing.T) {
	srv := newServer(baseConfig(), &mockClient{})
	defer srv.Close()
	resp, body := get(t, srv, "/v1/profile", nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	env := decodeError(t, body)
	if env.Error.Code != string(domain.CodeInvalidRequest) {
		t.Errorf("code = %q", env.Error.Code)
	}
	if env.Error.RequestID == "" {
		t.Error("error should include a request id")
	}
}

func TestProfileBadHost(t *testing.T) {
	srv := newServer(baseConfig(), &mockClient{})
	defer srv.Close()
	resp, _ := get(t, srv, "/v1/profile?url=https://example.com/in/ada", nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d", resp.StatusCode)
	}
}

func TestProfileUpstreamAuthMapsTo502(t *testing.T) {
	m := &mockClient{profile: func() (json.RawMessage, error) {
		return nil, domain.UpstreamAuth(nil)
	}}
	srv := newServer(baseConfig(), m)
	defer srv.Close()
	resp, body := get(t, srv, "/v1/profile?url=https://www.linkedin.com/in/ada", nil)
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if code := decodeError(t, body).Error.Code; code != string(domain.CodeUpstreamAuthFailed) {
		t.Errorf("code = %q", code)
	}
}

func TestProfileNotFoundMapsTo404(t *testing.T) {
	m := &mockClient{profile: func() (json.RawMessage, error) {
		return nil, domain.NotFound("missing")
	}}
	srv := newServer(baseConfig(), m)
	defer srv.Close()
	resp, _ := get(t, srv, "/v1/profile?url=https://www.linkedin.com/in/ada", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d", resp.StatusCode)
	}
}

func TestProfileUpstreamUnavailableMapsTo503(t *testing.T) {
	m := &mockClient{profile: func() (json.RawMessage, error) {
		return nil, domain.UpstreamUnavailable(nil)
	}}
	srv := newServer(baseConfig(), m)
	defer srv.Close()
	resp, _ := get(t, srv, "/v1/profile?url=https://www.linkedin.com/in/ada", nil)
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", resp.StatusCode)
	}
}

func TestAPIKeyEnforced(t *testing.T) {
	cfg := baseConfig()
	cfg.APIKeys = []string{"secret-key"}
	srv := newServer(cfg, &mockClient{})
	defer srv.Close()

	resp, body := get(t, srv, "/v1/profile?url=https://www.linkedin.com/in/ada", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("without a key status = %d", resp.StatusCode)
	}
	if code := decodeError(t, body).Error.Code; code != string(domain.CodeUnauthorized) {
		t.Errorf("code = %q", code)
	}

	resp, _ = get(t, srv, "/v1/profile?url=https://www.linkedin.com/in/ada", map[string]string{"X-API-Key": "secret-key"})
	if resp.StatusCode != http.StatusOK {
		t.Errorf("with a valid key status = %d", resp.StatusCode)
	}
}

func TestRateLimit(t *testing.T) {
	cfg := baseConfig()
	cfg.RateLimit = config.RateLimitConfig{Enabled: true, RPS: 1, Burst: 1}
	srv := newServer(cfg, &mockClient{})
	defer srv.Close()

	path := "/v1/profile?url=https://www.linkedin.com/in/ada"
	if resp, _ := get(t, srv, path, nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("first request status = %d", resp.StatusCode)
	}
	resp, body := get(t, srv, path, nil)
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("second request status = %d", resp.StatusCode)
	}
	if code := decodeError(t, body).Error.Code; code != string(domain.CodeRateLimited) {
		t.Errorf("code = %q", code)
	}
}

func TestPerKeyRateLimit(t *testing.T) {
	cfg := baseConfig()
	cfg.APIKeys = []string{"k1", "k2"}
	// Keep the per-IP limit permissive so the per-key limit is what binds.
	cfg.RateLimit = config.RateLimitConfig{Enabled: true, RPS: 1000, Burst: 1000, KeyRPS: 1, KeyBurst: 1}
	srv := newServer(cfg, &mockClient{})
	defer srv.Close()

	path := "/v1/profile?url=https://www.linkedin.com/in/ada"
	if resp, _ := get(t, srv, path, map[string]string{"X-API-Key": "k1"}); resp.StatusCode != http.StatusOK {
		t.Fatalf("k1 first status = %d", resp.StatusCode)
	}
	if resp, _ := get(t, srv, path, map[string]string{"X-API-Key": "k1"}); resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("k1 second status = %d", resp.StatusCode)
	}
	if resp, _ := get(t, srv, path, map[string]string{"X-API-Key": "k2"}); resp.StatusCode != http.StatusOK {
		t.Errorf("k2 should have its own budget, status = %d", resp.StatusCode)
	}
}
