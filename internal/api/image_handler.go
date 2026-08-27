// SPDX-FileCopyrightText: 2026 Ryan Madhuwala
// SPDX-License-Identifier: MIT

package api

import (
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/garudexlabs/linkedin-api/internal/domain"
)

const maxImageBytes = 8 << 20

type imageHandler struct {
	client *http.Client
}

func defaultImageClient() *http.Client {
	return &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func (h *imageHandler) handle(w http.ResponseWriter, r *http.Request) {
	target, err := url.Parse(r.URL.Query().Get("url"))
	if err != nil || target.Scheme != "https" || target.Host != "media.licdn.com" || target.User != nil {
		writeError(w, r, domain.Invalid("image URL must use https://media.licdn.com"))
		return
	}

	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, target.String(), nil)
	if err != nil {
		writeError(w, r, domain.Invalid("image URL is malformed"))
		return
	}
	req.Header.Set("Accept", "image/avif,image/webp,image/*")
	resp, err := h.client.Do(req)
	if err != nil {
		writeError(w, r, domain.UpstreamUnavailable(err))
		return
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		writeError(w, r, domain.UpstreamUnavailable(fmt.Errorf("image response status %d", resp.StatusCode)))
		return
	}

	contentType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil || !strings.HasPrefix(contentType, "image/") {
		writeError(w, r, domain.UpstreamUnavailable(fmt.Errorf("invalid image content type %q", contentType)))
		return
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxImageBytes+1))
	if err != nil || len(body) > maxImageBytes {
		writeError(w, r, domain.UpstreamUnavailable(fmt.Errorf("image response exceeds limit")))
		return
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}
