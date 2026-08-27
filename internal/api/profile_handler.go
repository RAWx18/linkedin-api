// SPDX-FileCopyrightText: 2026 Ryan Madhuwala
// SPDX-License-Identifier: MIT

package api

import (
	"net/http"

	"github.com/garudexlabs/linkedin-api/internal/audit"
	"github.com/garudexlabs/linkedin-api/internal/domain"
	"github.com/garudexlabs/linkedin-api/internal/service"
	"github.com/garudexlabs/linkedin-api/internal/urlx"
)

type profileHandler struct {
	svc *service.ProfileService
}

// handle validates the requested profile URL and returns the normalized profile.
func (h *profileHandler) handle(w http.ResponseWriter, r *http.Request) {
	raw := r.URL.Query().Get("url")
	if raw == "" {
		writeError(w, r, domain.Invalid("query parameter 'url' is required"))
		return
	}
	ref, err := urlx.Parse(raw)
	if err != nil {
		writeError(w, r, err)
		return
	}
	audit.SetProfileID(r.Context(), ref.PublicID)
	result, err := h.svc.GetProfile(r.Context(), ref)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
