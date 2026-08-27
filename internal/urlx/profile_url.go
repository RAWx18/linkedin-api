// SPDX-FileCopyrightText: 2026 Ryan Madhuwala
// SPDX-License-Identifier: MIT

package urlx

import (
	"net/url"
	"regexp"
	"strings"

	"github.com/garudexlabs/linkedin-api/internal/domain"
)

const (
	maxInputLen = 512
	maxSlugLen  = 150
)

// hostPattern matches linkedin.com plus its locale, www and mobile subdomains.
// Anchoring both ends prevents look-alikes such as linkedin.com.evil.com.
var hostPattern = regexp.MustCompile(`^([a-z]{2,3}\.)?(www\.|m\.)?linkedin\.com$`)

// slugPattern bounds the vanity identifier to the character classes LinkedIn
// actually issues, allowing percent-escapes for internationalized handles.
var slugPattern = regexp.MustCompile(`^[\p{L}\p{N}\-_%.]+$`)

// ProfileRef is a validated, normalized reference to a LinkedIn profile.
type ProfileRef struct {
	PublicID     string
	CanonicalURL string
}

// IsLinkedInHost reports whether host belongs to the LinkedIn domain allowlist.
func IsLinkedInHost(host string) bool {
	host = strings.ToLower(host)
	if i := strings.IndexByte(host, ':'); i >= 0 {
		host = host[:i]
	}
	return hostPattern.MatchString(host)
}

// Parse validates a user-supplied LinkedIn profile URL and extracts its public
// identifier. It never triggers a network fetch: the raw URL is used only to
// derive the identifier, which is the primary defense against SSRF because all
// upstream calls are built against a fixed, trusted base URL.
func Parse(raw string) (ProfileRef, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ProfileRef{}, domain.Invalid("profile url is required")
	}
	if len(raw) > maxInputLen {
		return ProfileRef{}, domain.Invalid("profile url is too long")
	}
	// Treat a bare host without a scheme as https so it parses with a host set.
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ProfileRef{}, domain.Invalid("profile url is malformed")
	}
	switch u.Scheme {
	case "http", "https":
	default:
		return ProfileRef{}, domain.Invalid("profile url must use http or https")
	}
	if !IsLinkedInHost(u.Host) {
		return ProfileRef{}, domain.Invalid("profile url host must be a linkedin.com domain")
	}
	// EscapedPath preserves the original percent-encoding so internationalized
	// identifiers survive intact and encoded slashes are not treated as separators.
	slug, err := extractSlug(u.EscapedPath())
	if err != nil {
		return ProfileRef{}, err
	}
	return ProfileRef{
		PublicID:     slug,
		CanonicalURL: "https://www.linkedin.com/in/" + slug,
	}, nil
}

func extractSlug(p string) (string, error) {
	parts := strings.Split(strings.Trim(p, "/"), "/")
	if len(parts) != 2 || parts[0] != "in" || parts[1] == "" {
		return "", domain.Invalid("profile url must be of the form https://www.linkedin.com/in/{identifier}")
	}
	slug := parts[1]
	decoded, err := url.PathUnescape(slug)
	if err != nil {
		return "", domain.Invalid("profile identifier is malformed")
	}
	if len(decoded) > maxSlugLen || !slugPattern.MatchString(slug) {
		return "", domain.Invalid("profile identifier is invalid")
	}
	return slug, nil
}
