// SPDX-FileCopyrightText: 2026 Ryan Madhuwala
// SPDX-License-Identifier: MIT

package api

import (
	"net/http"
	"regexp"
)

const requestIDHeader = "X-Request-ID"

// safeRequestID bounds an inbound correlation id to prevent log injection or
// unbounded values; anything else is replaced with a freshly generated id.
var safeRequestID = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

func requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(requestIDHeader)
		if !safeRequestID.MatchString(id) {
			id = newRequestID()
		}
		w.Header().Set(requestIDHeader, id)
		next.ServeHTTP(w, r.WithContext(contextWithRequestID(r.Context(), id)))
	})
}
