// SPDX-FileCopyrightText: 2026 Ryan Madhuwala
// SPDX-License-Identifier: MIT

package api

import "net/http"

type healthHandler struct {
	ready func() bool
}

// live reports process liveness and never depends on upstream availability.
func (h *healthHandler) live(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// readyz reports whether the service is ready to serve, based on internal state
// only (configuration and session presence, shutdown status) and never a live
// profile fetch.
func (h *healthHandler) readyz(w http.ResponseWriter, _ *http.Request) {
	if h.ready == nil || h.ready() {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
		return
	}
	writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not_ready"})
}
