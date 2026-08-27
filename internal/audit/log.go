// SPDX-FileCopyrightText: 2026 Ryan Madhuwala
// SPDX-License-Identifier: MIT

package audit

import (
	"context"
	"log/slog"
	"sort"
	"strings"
)

// Log emits the finished record as a structured "audit" event so the platform
// log pipeline (for example Azure Log Analytics) holds a complete, queryable
// request history that survives restarts, scale-to-zero, and multiple replicas,
// even when the durable SQLite store is unavailable. It contains only
// privacy-safe fields: no secrets, cookies, API keys, or full URLs, only a
// normalized profile identifier and non-reversible fingerprints.
func Log(ctx context.Context, logger *slog.Logger, r Record) {
	if logger == nil {
		return
	}
	logger.LogAttrs(ctx, slog.LevelInfo, "audit",
		slog.String("request_id", r.RequestID),
		slog.String("client_ip", r.ClientIP),
		slog.String("key_id", r.KeyID),
		slog.String("profile_id", r.ProfileID),
		slog.String("credential_mode", r.CredentialMode),
		slog.String("cred_fp", r.CredFP),
		slog.Int("status", r.Status),
		slog.String("rate_decision", r.RateDecision),
		slog.Bool("cached", r.Cached),
		slog.Bool("upstream_called", r.UpstreamCalled),
		slog.String("upstream_outcome", r.UpstreamOutcome),
		slog.Int("retries", r.Retries),
		slog.Int64("latency_ms", r.Latency.Milliseconds()),
		slog.String("error_class", r.ErrorClass),
		slog.String("sections", r.Sections),
	)
}

// SectionsList renders a section-status map as a compact, stable, comma-joined
// list of "name:status" pairs, so the audit record captures which sections were
// requested and whether each was fetched.
func SectionsList(sections map[string]string) string {
	if len(sections) == 0 {
		return ""
	}
	names := make([]string, 0, len(sections))
	for name := range sections {
		names = append(names, name)
	}
	sort.Strings(names)
	var b strings.Builder
	for i, name := range names {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(name)
		b.WriteByte(':')
		b.WriteString(sections[name])
	}
	return b.String()
}
