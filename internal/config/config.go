// SPDX-FileCopyrightText: 2026 Ryan Madhuwala
// SPDX-License-Identifier: MIT

package config

import (
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/garudexlabs/linkedin-api/internal/urlx"
)

const (
	EnvDevelopment = "development"
	EnvProduction  = "production"
)

// Config aggregates every runtime setting. It is loaded once at startup and
// treated as immutable thereafter.
type Config struct {
	Env       string
	Server    ServerConfig
	LinkedIn  LinkedInConfig
	Cache     CacheConfig
	RateLimit RateLimitConfig
	Upstream  UpstreamConfig
	Audit     AuditConfig
	APIKeys   []string
	Log       LogConfig
	Metrics   MetricsConfig
}

type ServerConfig struct {
	Port            int
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	IdleTimeout     time.Duration
	ShutdownTimeout time.Duration
}

type LinkedInConfig struct {
	LiAt                  string
	JSessionID            string
	BaseURL               string
	UserAgent             string
	AcceptLanguage        string
	Timeout               time.Duration
	MaxRetries            int
	RetryBackoff          time.Duration
	ProfileTimeout        time.Duration
	AllowCallerSession    bool
	Sections              []string
	EnrichmentConcurrency int
}

type CacheConfig struct {
	Enabled    bool
	TTL        time.Duration
	MaxEntries int
}

type RateLimitConfig struct {
	Enabled  bool
	RPS      float64
	Burst    int
	KeyRPS   float64
	KeyBurst int
}

// UpstreamConfig bounds and protects traffic to LinkedIn: an aggregate rate and
// concurrency ceiling, a circuit breaker, and negative caching of missing
// profiles so abusive or failing traffic cannot be amplified upstream.
type UpstreamConfig struct {
	MaxConcurrency   int
	RateRPS          float64
	RateBurst        int
	BreakerThreshold int
	BreakerCooldown  time.Duration
	SessionThreshold int
	SessionCooldown  time.Duration
	CallerSessionTTL time.Duration
	NegativeCacheTTL time.Duration
}

// AuditConfig controls the durable request-audit store: where it lives, how long
// history is kept, and how it buffers writes off the request path. AdminKeys
// guard the usage query endpoint, which is exposed only when they are set.
type AuditConfig struct {
	Enabled       bool
	DBPath        string
	Retention     time.Duration
	BufferSize    int
	BatchSize     int
	FlushInterval time.Duration
	AdminKeys     []string
}

type LogConfig struct {
	Level  string
	Format string
}

type MetricsConfig struct {
	Enabled bool
}

// Load reads configuration from the environment, applies development-friendly
// defaults, and fails fast if the resulting configuration is invalid.
func Load() (*Config, error) {
	c := &Config{
		Env: getEnv("ENV", EnvDevelopment),
		Server: ServerConfig{
			Port:            getEnvInt("SERVER_PORT", 8080),
			ReadTimeout:     getEnvDuration("SERVER_READ_TIMEOUT", 10*time.Second),
			WriteTimeout:    getEnvDuration("SERVER_WRITE_TIMEOUT", 20*time.Second),
			IdleTimeout:     getEnvDuration("SERVER_IDLE_TIMEOUT", 90*time.Second),
			ShutdownTimeout: getEnvDuration("SHUTDOWN_TIMEOUT", 15*time.Second),
		},
		LinkedIn: LinkedInConfig{
			LiAt:                  getEnv("LINKEDIN_LI_AT", ""),
			JSessionID:            getEnv("LINKEDIN_JSESSIONID", ""),
			BaseURL:               getEnv("LINKEDIN_BASE_URL", "https://www.linkedin.com"),
			UserAgent:             getEnv("LINKEDIN_USER_AGENT", ""),
			AcceptLanguage:        getEnv("LINKEDIN_ACCEPT_LANGUAGE", "en-US,en;q=0.9"),
			Timeout:               getEnvDuration("HTTP_REQUEST_TIMEOUT", 10*time.Second),
			MaxRetries:            getEnvInt("HTTP_MAX_RETRIES", 2),
			RetryBackoff:          getEnvDuration("HTTP_RETRY_BACKOFF", 300*time.Millisecond),
			ProfileTimeout:        getEnvDuration("PROFILE_TIMEOUT", 15*time.Second),
			AllowCallerSession:    getEnvBool("LINKEDIN_ALLOW_CALLER_SESSION", true),
			Sections:              getEnvCSVOr("PROFILE_SECTIONS", []string{"experience", "education"}),
			EnrichmentConcurrency: getEnvInt("ENRICHMENT_CONCURRENCY", 4),
		},
		Cache: CacheConfig{
			Enabled:    getEnvBool("CACHE_ENABLED", true),
			TTL:        getEnvDuration("CACHE_TTL", 10*time.Minute),
			MaxEntries: getEnvInt("CACHE_MAX_ENTRIES", 1000),
		},
		RateLimit: RateLimitConfig{
			Enabled:  getEnvBool("RATE_LIMIT_ENABLED", true),
			RPS:      getEnvFloat("RATE_LIMIT_RPS", 5),
			Burst:    getEnvInt("RATE_LIMIT_BURST", 10),
			KeyRPS:   getEnvFloat("RATE_LIMIT_KEY_RPS", 10),
			KeyBurst: getEnvInt("RATE_LIMIT_KEY_BURST", 20),
		},
		Upstream: UpstreamConfig{
			MaxConcurrency:   getEnvInt("UPSTREAM_MAX_CONCURRENCY", 4),
			RateRPS:          getEnvFloat("UPSTREAM_RATE_RPS", 5),
			RateBurst:        getEnvInt("UPSTREAM_RATE_BURST", 10),
			BreakerThreshold: getEnvInt("UPSTREAM_BREAKER_THRESHOLD", 5),
			BreakerCooldown:  getEnvDuration("UPSTREAM_BREAKER_COOLDOWN", 30*time.Second),
			SessionThreshold: getEnvInt("UPSTREAM_SESSION_THRESHOLD", 2),
			SessionCooldown:  getEnvDuration("UPSTREAM_SESSION_COOLDOWN", 5*time.Minute),
			CallerSessionTTL: getEnvDuration("CALLER_SESSION_UNHEALTHY_TTL", 5*time.Minute),
			NegativeCacheTTL: getEnvDuration("UPSTREAM_NEG_CACHE_TTL", time.Minute),
		},
		Audit: AuditConfig{
			Enabled:       getEnvBool("AUDIT_ENABLED", true),
			DBPath:        getEnv("AUDIT_DB_PATH", "audit.db"),
			Retention:     getEnvDuration("AUDIT_RETENTION", 30*24*time.Hour),
			BufferSize:    getEnvInt("AUDIT_BUFFER_SIZE", 4096),
			BatchSize:     getEnvInt("AUDIT_BATCH_SIZE", 128),
			FlushInterval: getEnvDuration("AUDIT_FLUSH_INTERVAL", time.Second),
			AdminKeys:     getEnvCSV("AUDIT_ADMIN_KEYS"),
		},
		APIKeys: getEnvCSV("API_KEYS"),
		Log: LogConfig{
			Level:  getEnv("LOG_LEVEL", "info"),
			Format: getEnv("LOG_FORMAT", "json"),
		},
		Metrics: MetricsConfig{
			Enabled: getEnvBool("METRICS_ENABLED", true),
		},
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return c, nil
}

// IsProduction reports whether the service is configured for production.
func (c *Config) IsProduction() bool {
	return strings.EqualFold(c.Env, EnvProduction)
}

// Validate collects every configuration problem so operators see all issues at
// once rather than fixing them one restart at a time.
func (c *Config) Validate() error {
	var problems []string

	if c.Server.Port < 1 || c.Server.Port > 65535 {
		problems = append(problems, "SERVER_PORT must be between 1 and 65535")
	}
	if u, err := url.Parse(c.LinkedIn.BaseURL); err != nil || u.Scheme != "https" || !urlx.IsLinkedInHost(u.Host) {
		problems = append(problems, "LINKEDIN_BASE_URL must be an https URL on a linkedin.com host")
	}
	if c.LinkedIn.MaxRetries < 0 {
		problems = append(problems, "HTTP_MAX_RETRIES must be >= 0")
	}
	if c.LinkedIn.Timeout <= 0 {
		problems = append(problems, "HTTP_REQUEST_TIMEOUT must be > 0")
	}
	if c.LinkedIn.ProfileTimeout <= 0 {
		problems = append(problems, "PROFILE_TIMEOUT must be > 0")
	}
	if c.LinkedIn.EnrichmentConcurrency <= 0 {
		problems = append(problems, "ENRICHMENT_CONCURRENCY must be > 0")
	}
	if c.LinkedIn.LiAt != "" && c.LinkedIn.UserAgent == "" {
		problems = append(problems, "LINKEDIN_USER_AGENT is required when LINKEDIN_LI_AT is set; set it to the exact User-Agent of the browser where you obtained the li_at and JSESSIONID cookies")
	}
	if c.LinkedIn.MaxRetries > 0 && c.LinkedIn.RetryBackoff <= 0 {
		problems = append(problems, "HTTP_RETRY_BACKOFF must be > 0 when HTTP_MAX_RETRIES > 0")
	}
	if c.Cache.Enabled && c.Cache.MaxEntries < 1 {
		problems = append(problems, "CACHE_MAX_ENTRIES must be >= 1 when the cache is enabled")
	}
	if c.RateLimit.Enabled && (c.RateLimit.RPS <= 0 || c.RateLimit.Burst < 1) {
		problems = append(problems, "RATE_LIMIT_RPS must be > 0 and RATE_LIMIT_BURST >= 1 when rate limiting is enabled")
	}
	if c.RateLimit.Enabled && (c.RateLimit.KeyRPS <= 0 || c.RateLimit.KeyBurst < 1) {
		problems = append(problems, "RATE_LIMIT_KEY_RPS must be > 0 and RATE_LIMIT_KEY_BURST >= 1 when rate limiting is enabled")
	}
	if c.Upstream.MaxConcurrency < 1 {
		problems = append(problems, "UPSTREAM_MAX_CONCURRENCY must be >= 1")
	}
	if c.Upstream.RateRPS <= 0 || c.Upstream.RateBurst < 1 {
		problems = append(problems, "UPSTREAM_RATE_RPS must be > 0 and UPSTREAM_RATE_BURST >= 1")
	}
	if c.Upstream.BreakerThreshold < 1 {
		problems = append(problems, "UPSTREAM_BREAKER_THRESHOLD must be >= 1")
	}
	if c.Upstream.BreakerCooldown <= 0 {
		problems = append(problems, "UPSTREAM_BREAKER_COOLDOWN must be > 0")
	}
	if c.Upstream.SessionThreshold < 1 {
		problems = append(problems, "UPSTREAM_SESSION_THRESHOLD must be >= 1")
	}
	if c.Upstream.SessionCooldown <= 0 {
		problems = append(problems, "UPSTREAM_SESSION_COOLDOWN must be > 0")
	}
	if c.Upstream.CallerSessionTTL <= 0 {
		problems = append(problems, "CALLER_SESSION_UNHEALTHY_TTL must be > 0")
	}
	if c.Audit.Enabled {
		if c.Audit.DBPath == "" {
			problems = append(problems, "AUDIT_DB_PATH is required when auditing is enabled")
		}
		if c.Audit.Retention <= 0 {
			problems = append(problems, "AUDIT_RETENTION must be > 0")
		}
		if c.Audit.BufferSize < 1 {
			problems = append(problems, "AUDIT_BUFFER_SIZE must be >= 1")
		}
		if c.Audit.BatchSize < 1 {
			problems = append(problems, "AUDIT_BATCH_SIZE must be >= 1")
		}
		if c.Audit.FlushInterval <= 0 {
			problems = append(problems, "AUDIT_FLUSH_INTERVAL must be > 0")
		}
	}
	if c.Log.Format != "json" && c.Log.Format != "text" {
		problems = append(problems, "LOG_FORMAT must be 'json' or 'text'")
	}

	if c.IsProduction() {
		if c.LinkedIn.LiAt == "" {
			problems = append(problems, "LINKEDIN_LI_AT is required in production")
		}
		if c.LinkedIn.JSessionID == "" {
			problems = append(problems, "LINKEDIN_JSESSIONID is required in production")
		}
		if len(c.APIKeys) == 0 {
			problems = append(problems, "API_KEYS is required in production")
		}
	}

	if len(problems) > 0 {
		return fmt.Errorf("invalid configuration:\n  - %s", strings.Join(problems, "\n  - "))
	}
	return nil
}

// HasLinkedInSession reports whether upstream credentials are present.
func (c *Config) HasLinkedInSession() bool {
	return c.LinkedIn.LiAt != "" && c.LinkedIn.JSessionID != ""
}

// LogValue implements slog.LogValuer so the config can be logged without ever
// exposing secret material.
func (c *Config) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("env", c.Env),
		slog.Int("server_port", c.Server.Port),
		slog.Bool("cache_enabled", c.Cache.Enabled),
		slog.Bool("rate_limit_enabled", c.RateLimit.Enabled),
		slog.Bool("metrics_enabled", c.Metrics.Enabled),
		slog.Bool("audit_enabled", c.Audit.Enabled),
		slog.Bool("linkedin_session_present", c.HasLinkedInSession()),
		slog.Int("api_keys_count", len(c.APIKeys)),
		slog.String("log_level", c.Log.Level),
	)
}

func getEnv(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func getEnvInt(key string, def int) int {
	if v, ok := os.LookupEnv(key); ok {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return n
		}
	}
	return def
}

func getEnvFloat(key string, def float64) float64 {
	if v, ok := os.LookupEnv(key); ok {
		if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
			return f
		}
	}
	return def
}

func getEnvBool(key string, def bool) bool {
	if v, ok := os.LookupEnv(key); ok {
		if b, err := strconv.ParseBool(strings.TrimSpace(v)); err == nil {
			return b
		}
	}
	return def
}

func getEnvDuration(key string, def time.Duration) time.Duration {
	if v, ok := os.LookupEnv(key); ok {
		if d, err := time.ParseDuration(strings.TrimSpace(v)); err == nil {
			return d
		}
	}
	return def
}

// getEnvCSVOr returns the CSV value for key, or the default when the key is unset.
func getEnvCSVOr(key string, def []string) []string {
	if v := getEnvCSV(key); v != nil {
		return v
	}
	return def
}

func getEnvCSV(key string) []string {
	v, ok := os.LookupEnv(key)
	if !ok {
		return nil
	}
	var out []string
	for _, part := range strings.Split(v, ",") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
