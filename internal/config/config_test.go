// SPDX-FileCopyrightText: 2026 Ryan Madhuwala
// SPDX-License-Identifier: MIT

package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	for _, k := range []string{"ENV", "LINKEDIN_LI_AT", "LINKEDIN_JSESSIONID", "API_KEYS", "SERVER_PORT", "LOG_FORMAT", "CACHE_ENABLED"} {
		t.Setenv(k, "")
	}
	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Env != EnvDevelopment {
		t.Errorf("env = %q", cfg.Env)
	}
	if cfg.Server.Port != 8080 {
		t.Errorf("port = %d", cfg.Server.Port)
	}
	if !cfg.Cache.Enabled {
		t.Error("cache should default to enabled")
	}
}

func TestLoadEnvOverride(t *testing.T) {
	t.Setenv("SERVER_PORT", "9090")
	t.Setenv("LOG_FORMAT", "text")
	t.Setenv("CACHE_ENABLED", "false")
	t.Setenv("API_KEYS", "a, b ,c")
	t.Setenv("RATE_LIMIT_RPS", "7.5")
	t.Setenv("HTTP_REQUEST_TIMEOUT", "5s")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Server.Port != 9090 {
		t.Errorf("port = %d", cfg.Server.Port)
	}
	if cfg.Log.Format != "text" {
		t.Errorf("log format = %q", cfg.Log.Format)
	}
	if cfg.Cache.Enabled {
		t.Error("cache should be disabled")
	}
	if len(cfg.APIKeys) != 3 {
		t.Errorf("api keys = %v", cfg.APIKeys)
	}
	if cfg.RateLimit.RPS != 7.5 {
		t.Errorf("rps = %v", cfg.RateLimit.RPS)
	}
	if cfg.LinkedIn.Timeout != 5*time.Second {
		t.Errorf("timeout = %v", cfg.LinkedIn.Timeout)
	}
}

func validProdConfig() *Config {
	return &Config{
		Env:       EnvProduction,
		Server:    ServerConfig{Port: 8080},
		LinkedIn:  LinkedInConfig{BaseURL: "https://www.linkedin.com", UserAgent: "Mozilla/5.0 (test) Browser/1.0", Timeout: time.Second, ProfileTimeout: time.Second},
		Cache:     CacheConfig{Enabled: true, MaxEntries: 10},
		RateLimit: RateLimitConfig{Enabled: true, RPS: 1, Burst: 1, KeyRPS: 1, KeyBurst: 1},
		Upstream: UpstreamConfig{
			MaxConcurrency:   1,
			RateRPS:          1,
			RateBurst:        1,
			BreakerThreshold: 1,
			BreakerCooldown:  time.Second,
			SessionThreshold: 1,
			SessionCooldown:  time.Second,
			CallerSessionTTL: time.Second,
		},
		Log: LogConfig{Format: "json"},
	}
}

func TestValidateProductionRequiresSecrets(t *testing.T) {
	err := validProdConfig().Validate()
	if err == nil {
		t.Fatal("expected a validation error in production without secrets")
	}
	for _, want := range []string{"LINKEDIN_LI_AT", "LINKEDIN_JSESSIONID", "API_KEYS"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should mention %s: %s", want, err.Error())
		}
	}
}

func TestValidateProductionComplete(t *testing.T) {
	cfg := validProdConfig()
	cfg.LinkedIn.LiAt = "cookie"
	cfg.LinkedIn.JSessionID = "ajax:1"
	cfg.APIKeys = []string{"key"}
	if err := cfg.Validate(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateRequiresUserAgentWithSession(t *testing.T) {
	cfg := validProdConfig()
	cfg.LinkedIn.LiAt = "cookie"
	cfg.LinkedIn.JSessionID = "ajax:1"
	cfg.APIKeys = []string{"key"}
	cfg.LinkedIn.UserAgent = ""
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "LINKEDIN_USER_AGENT") {
		t.Errorf("a configured session must require an explicit User-Agent, got %v", err)
	}
}

func TestValidateInvalidBaseURL(t *testing.T) {
	cfg := validProdConfig()
	cfg.LinkedIn.BaseURL = "https://example.com"
	if err := cfg.Validate(); err == nil {
		t.Error("expected an error for a non-LinkedIn base url")
	}
}

func TestValidateRetryBackoff(t *testing.T) {
	cfg := validProdConfig()
	cfg.LinkedIn.LiAt = "cookie"
	cfg.LinkedIn.JSessionID = "ajax:1"
	cfg.APIKeys = []string{"key"}
	cfg.LinkedIn.MaxRetries = 2
	cfg.LinkedIn.RetryBackoff = 0
	if err := cfg.Validate(); err == nil {
		t.Error("expected an error when retries are enabled but backoff is zero")
	}
	cfg.LinkedIn.RetryBackoff = 100 * time.Millisecond
	if err := cfg.Validate(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateAuditSettings(t *testing.T) {
	cfg := validProdConfig()
	cfg.LinkedIn.LiAt = "cookie"
	cfg.LinkedIn.JSessionID = "ajax:1"
	cfg.APIKeys = []string{"key"}

	cfg.Audit = AuditConfig{Enabled: true}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation errors for an incomplete audit config")
	}
	for _, want := range []string{"AUDIT_DB_PATH", "AUDIT_RETENTION", "AUDIT_BUFFER_SIZE", "AUDIT_BATCH_SIZE", "AUDIT_FLUSH_INTERVAL"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should mention %s: %s", want, err.Error())
		}
	}

	cfg.Audit = AuditConfig{Enabled: true, DBPath: "audit.db", Retention: time.Hour, BufferSize: 10, BatchSize: 5, FlushInterval: time.Second}
	if err := cfg.Validate(); err != nil {
		t.Errorf("unexpected error for a valid audit config: %v", err)
	}
}

func TestLogValueRedactsSecrets(t *testing.T) {
	cfg := &Config{
		LinkedIn: LinkedInConfig{LiAt: "supersecretcookie", JSessionID: "ajax:secretsession"},
		APIKeys:  []string{"secretkey"},
	}
	if s := cfg.LogValue().String(); strings.Contains(s, "secret") {
		t.Errorf("log value leaked a secret: %s", s)
	}
}
