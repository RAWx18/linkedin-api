// SPDX-FileCopyrightText: 2026 Ryan Madhuwala
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/garudexlabs/linkedin-api/internal/api"
	"github.com/garudexlabs/linkedin-api/internal/audit"
	"github.com/garudexlabs/linkedin-api/internal/cache"
	"github.com/garudexlabs/linkedin-api/internal/config"
	"github.com/garudexlabs/linkedin-api/internal/linkedin"
	"github.com/garudexlabs/linkedin-api/internal/observability"
	"github.com/garudexlabs/linkedin-api/internal/service"
	"github.com/garudexlabs/linkedin-api/internal/upstream"
	"github.com/garudexlabs/linkedin-api/web"
)

func main() {
	if err := run(); err != nil {
		_, _ = os.Stderr.WriteString("fatal: " + err.Error() + "\n")
		os.Exit(1)
	}
}

// profileSections resolves the configured section names to the client's closed
// allowlist, failing fast on an unknown name so misconfiguration is caught at
// startup rather than silently dropping data.
func profileSections(names []string) ([]linkedin.Section, error) {
	out := make([]linkedin.Section, 0, len(names))
	seen := make(map[linkedin.Section]bool)
	for _, name := range names {
		sec, ok := linkedin.ParseSection(strings.ToLower(strings.TrimSpace(name)))
		if !ok {
			return nil, fmt.Errorf("unknown PROFILE_SECTIONS entry %q", name)
		}
		if !seen[sec] {
			seen[sec] = true
			out = append(out, sec)
		}
	}
	return out, nil
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	logger := observability.NewLogger(cfg.Log)
	logger.Info("starting linkedin-api", "config", cfg)

	metrics := observability.NewMetrics()

	session := linkedin.NewSession(cfg.LinkedIn.LiAt, cfg.LinkedIn.JSessionID)
	client := linkedin.NewClient(cfg.LinkedIn, session, metrics, logger)

	var profileCache service.Cache = cache.Noop{}
	if cfg.Cache.Enabled {
		profileCache = cache.NewTTL(cfg.Cache.TTL, cfg.Cache.MaxEntries)
	}

	guard := upstream.New(upstream.Config{
		MaxConcurrency:   cfg.Upstream.MaxConcurrency,
		RateRPS:          cfg.Upstream.RateRPS,
		RateBurst:        cfg.Upstream.RateBurst,
		FailureThreshold: cfg.Upstream.BreakerThreshold,
		Cooldown:         cfg.Upstream.BreakerCooldown,
		SessionThreshold: cfg.Upstream.SessionThreshold,
		SessionCooldown:  cfg.Upstream.SessionCooldown,
	}, metrics, logger)

	var negative service.NegativeCache
	if cfg.Cache.Enabled && cfg.Upstream.NegativeCacheTTL > 0 {
		negative = cache.NewNegative(cfg.Upstream.NegativeCacheTTL, cfg.Cache.MaxEntries)
	}

	sections, err := profileSections(cfg.LinkedIn.Sections)
	if err != nil {
		return err
	}

	svc := service.NewProfileService(service.Deps{
		Client:                client,
		Cache:                 profileCache,
		Negative:              negative,
		Gate:                  guard,
		Metrics:               metrics,
		Logger:                logger,
		ProfileTimeout:        cfg.LinkedIn.ProfileTimeout,
		CallerSessionTTL:      cfg.Upstream.CallerSessionTTL,
		EnrichmentSections:    sections,
		EnrichmentConcurrency: cfg.LinkedIn.EnrichmentConcurrency,
	})

	// Auditing is best effort: if its store cannot be opened the service still
	// runs, it simply records no history rather than refusing to start.
	var (
		recorder    audit.Recorder
		usage       api.UsageQuerier
		auditWriter *audit.Writer
	)
	if cfg.Audit.Enabled {
		store, err := audit.Open(auditDBPath(cfg.Audit.DBPath))
		if err != nil {
			logger.Error("audit store unavailable, continuing without auditing", "error", err)
		} else {
			auditWriter = audit.NewWriter(store, audit.WriterConfig{
				BufferSize:    cfg.Audit.BufferSize,
				BatchSize:     cfg.Audit.BatchSize,
				FlushInterval: cfg.Audit.FlushInterval,
				Retention:     cfg.Audit.Retention,
				PurgeInterval: time.Hour,
			}, metrics, logger)
			recorder = auditWriter
			usage = store
		}
	}

	ui, err := web.Assets()
	if err != nil {
		logger.Warn("ui assets unavailable, serving api only", "error", err)
		ui = nil
	}

	// readiness flips to false during shutdown so the platform stops routing.
	var ready atomic.Bool
	ready.Store(true)

	router := api.NewRouter(api.Deps{
		Config:   cfg,
		Service:  svc,
		Metrics:  metrics,
		Logger:   logger,
		Ready:    func() bool { return ready.Load() },
		UI:       ui,
		Recorder: recorder,
		Usage:    usage,
	})

	srv := &http.Server{
		Addr:         ":" + strconv.Itoa(cfg.Server.Port),
		Handler:      router,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
		IdleTimeout:  cfg.Server.IdleTimeout,
	}

	serverErr := make(chan error, 1)
	go func() {
		logger.Info("http server listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	select {
	case err := <-serverErr:
		return err
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	}

	ready.Store(false)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.Server.ShutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
		return err
	}

	// Flush buffered audit records once the server has stopped accepting requests.
	if auditWriter != nil {
		flushCtx, flushCancel := context.WithTimeout(context.Background(), cfg.Server.ShutdownTimeout)
		defer flushCancel()
		if err := auditWriter.Close(flushCtx); err != nil {
			logger.Error("audit writer close failed", "error", err)
		}
	}

	logger.Info("shutdown complete")
	return nil
}

// auditDBPath gives each replica its own audit database file when more than one
// instance runs, so concurrent writers never share a single SQLite file.
func auditDBPath(base string) string {
	replica := os.Getenv("CONTAINER_APP_REPLICA_NAME")
	if replica == "" {
		return base
	}
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(filepath.Base(base), ext)
	return filepath.Join(filepath.Dir(base), name+"-"+replica+ext)
}
