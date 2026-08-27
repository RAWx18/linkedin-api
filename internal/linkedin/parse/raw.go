// SPDX-FileCopyrightText: 2026 Ryan Madhuwala
// SPDX-License-Identifier: MIT

package parse

import (
	"sort"

	"github.com/garudexlabs/linkedin-api/internal/domain"
)

// vectorImage models a LinkedIn image asset: a root URL plus sized artifacts.
type vectorImage struct {
	RootURL   string     `json:"rootUrl"`
	Artifacts []artifact `json:"artifacts"`
}

type artifact struct {
	Width                         int    `json:"width"`
	Height                        int    `json:"height"`
	FileIdentifyingURLPathSegment string `json:"fileIdentifyingUrlPathSegment"`
}

// image resolves the domain image: every usable sized variant ordered from
// smallest to largest, with the largest also exposed as the primary URL. A nil
// receiver is tolerated so callers can pass optional fields directly.
func (v *vectorImage) image() *domain.Image {
	if v == nil || v.RootURL == "" || len(v.Artifacts) == 0 {
		return nil
	}
	variants := make([]domain.ImageVariant, 0, len(v.Artifacts))
	for _, a := range v.Artifacts {
		if a.Width <= 0 || a.FileIdentifyingURLPathSegment == "" {
			continue
		}
		variants = append(variants, domain.ImageVariant{
			Width:  a.Width,
			Height: a.Height,
			URL:    v.RootURL + a.FileIdentifyingURLPathSegment,
		})
	}
	if len(variants) == 0 {
		return nil
	}
	sort.Slice(variants, func(i, j int) bool { return variants[i].Width < variants[j].Width })
	return &domain.Image{URL: variants[len(variants)-1].URL, Variants: variants}
}

// strPtr returns nil for empty strings so optional fields stay absent.
func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
