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
	svc         *service.ProfileService
	allowCaller bool
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
	cred, err := callerCredential(r, h.allowCaller)
	if err != nil {
		writeError(w, r, err)
		return
	}
	audit.SetProfileID(r.Context(), ref.PublicID)
	audit.SetCredential(r.Context(), string(cred.Mode()), cred.Fingerprint())
	result, err := h.svc.GetProfile(r.Context(), ref, cred)
	if err != nil {
		writeError(w, r, err)
		return
	}
	audit.SetSections(r.Context(), audit.SectionsList(result.Meta.Sections))
	writeJSON(w, http.StatusOK, result)
}
