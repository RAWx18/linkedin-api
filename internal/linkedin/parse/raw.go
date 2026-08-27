// SPDX-FileCopyrightText: 2026 Ryan Madhuwala
// SPDX-License-Identifier: MIT

package parse

import "github.com/garudexlabs/linkedin-api/internal/domain"

// vectorImage models a LinkedIn image asset: a root URL plus sized artifacts.
type vectorImage struct {
	RootURL   string `json:"rootUrl"`
	Artifacts []struct {
		Width                         int    `json:"width"`
		FileIdentifyingURLPathSegment string `json:"fileIdentifyingUrlPathSegment"`
	} `json:"artifacts"`
}

// largest resolves the highest-resolution image URL, or nil when unavailable. A
// nil receiver is tolerated so callers can pass optional fields directly.
func (v *vectorImage) largest() *domain.Image {
	if v == nil || v.RootURL == "" || len(v.Artifacts) == 0 {
		return nil
	}
	best := v.Artifacts[0]
	for _, a := range v.Artifacts[1:] {
		if a.Width > best.Width {
			best = a
		}
	}
	if best.FileIdentifyingURLPathSegment == "" {
		return nil
	}
	return &domain.Image{URL: v.RootURL + best.FileIdentifyingURLPathSegment}
}

// strPtr returns nil for empty strings so optional fields stay absent.
func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
