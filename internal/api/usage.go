// SPDX-FileCopyrightText: 2026 Ryan Madhuwala
// SPDX-License-Identifier: MIT

package api

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/garudexlabs/linkedin-api/internal/audit"
	"github.com/garudexlabs/linkedin-api/internal/domain"
)

// UsageQuerier is the read side of the audit store the usage endpoint needs.
type UsageQuerier interface {
	Summary(ctx context.Context, since time.Time) (audit.Summary, error)
	TopProfiles(ctx context.Context, since time.Time, limit int) ([]audit.Count, error)
	TopClients(ctx context.Context, since time.Time, limit int) ([]audit.Count, error)
	TopUpstreamClients(ctx context.Context, since time.Time, limit int) ([]audit.Count, error)
}

const (
	defaultUsageWindow = 24 * time.Hour
	maxUsageWindow     = 30 * 24 * time.Hour
	defaultUsageLimit  = 10
	maxUsageLimit      = 100
)

type usageHandler struct {
	querier UsageQuerier
}

type usageResponse struct {
	Window             string        `json:"window"`
	Summary            audit.Summary `json:"summary"`
	TopProfiles        []audit.Count `json:"top_profiles"`
	TopClients         []audit.Count `json:"top_clients"`
	TopUpstreamClients []audit.Count `json:"top_upstream_clients"`
}

// handle answers a windowed usage query. The window and result limit are clamped
// to fixed maximums so the endpoint stays cheap and cannot be turned into an
// expensive scan, and every query is served from an index.
func (h *usageHandler) handle(w http.ResponseWriter, r *http.Request) {
	window := parseWindow(r.URL.Query().Get("window"))
	limit := parseLimit(r.URL.Query().Get("limit"))
	since := time.Now().Add(-window)
	ctx := r.Context()

	summary, err := h.querier.Summary(ctx, since)
	if err != nil {
		writeError(w, r, domain.Internal(err))
		return
	}
	profiles, err := h.querier.TopProfiles(ctx, since, limit)
	if err != nil {
		writeError(w, r, domain.Internal(err))
		return
	}
	clients, err := h.querier.TopClients(ctx, since, limit)
	if err != nil {
		writeError(w, r, domain.Internal(err))
		return
	}
	upstream, err := h.querier.TopUpstreamClients(ctx, since, limit)
	if err != nil {
		writeError(w, r, domain.Internal(err))
		return
	}

	writeJSON(w, http.StatusOK, usageResponse{
		Window:             window.String(),
		Summary:            summary,
		TopProfiles:        profiles,
		TopClients:         clients,
		TopUpstreamClients: upstream,
	})
}

func parseWindow(raw string) time.Duration {
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return defaultUsageWindow
	}
	if d > maxUsageWindow {
		return maxUsageWindow
	}
	return d
}

func parseLimit(raw string) int {
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return defaultUsageLimit
	}
	if n > maxUsageLimit {
		return maxUsageLimit
	}
	return n
}
