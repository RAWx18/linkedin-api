// SPDX-FileCopyrightText: 2026 Ryan Madhuwala
// SPDX-License-Identifier: MIT

package api

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/garudexlabs/linkedin-api/internal/audit"
)

// auditMiddleware records one privacy-safe entry per API request. It is the
// outermost API-route layer so it also captures requests rejected by rate
// limiting or authentication. Every request is emitted as a structured "audit"
// log event, which the platform log pipeline (Azure Log Analytics) keeps as a
// complete, queryable history regardless of storage. When a durable store is
// present the record is additionally enqueued for it; that enqueue is
// non-blocking, so a slow or failing store never delays the profile lookup.
func auditMiddleware(logger *slog.Logger, rec audit.Recorder, proxyDepth int) middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ev := audit.NewEvent()
			r = r.WithContext(audit.WithEvent(r.Context(), ev))

			sr := &statusRecorder{ResponseWriter: w}
			start := time.Now()
			next.ServeHTTP(sr, r)

			record := ev.Snapshot(audit.Record{
				Time:      start.UTC(),
				RequestID: RequestIDFromContext(r.Context()),
				ClientIP:  clientIP(r, proxyDepth),
				KeyID:     audit.KeyID(extractAPIKey(r)),
				Status:    sr.Status(),
				Latency:   time.Since(start),
			})

			audit.Log(r.Context(), logger, record)
			if rec != nil {
				rec.Record(record)
			}
		})
	}
}
