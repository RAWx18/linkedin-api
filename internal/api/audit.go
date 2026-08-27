// SPDX-FileCopyrightText: 2026 Ryan Madhuwala
// SPDX-License-Identifier: MIT

package api

import (
	"net/http"
	"time"

	"github.com/garudexlabs/linkedin-api/internal/audit"
)

// auditMiddleware records one durable, privacy-safe row per API request. It is
// the outermost API-route layer so it also captures requests rejected by rate
// limiting or authentication. Recording is a non-blocking enqueue, so a slow or
// failing audit store never delays or fails the profile lookup itself.
func auditMiddleware(rec audit.Recorder) middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ev := audit.NewEvent()
			r = r.WithContext(audit.WithEvent(r.Context(), ev))

			sr := &statusRecorder{ResponseWriter: w}
			start := time.Now()
			next.ServeHTTP(sr, r)

			rec.Record(ev.Snapshot(audit.Record{
				Time:      start.UTC(),
				RequestID: RequestIDFromContext(r.Context()),
				ClientIP:  clientIP(r),
				KeyID:     audit.KeyID(extractAPIKey(r)),
				Status:    sr.Status(),
				Latency:   time.Since(start),
			}))
		})
	}
}
