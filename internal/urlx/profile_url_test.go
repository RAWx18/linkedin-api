// SPDX-FileCopyrightText: 2026 Ryan Madhuwala
// SPDX-License-Identifier: MIT

package urlx

import (
	"strings"
	"testing"
)

func TestParseValid(t *testing.T) {
	cases := []struct {
		name, in, wantID string
	}{
		{"basic", "https://www.linkedin.com/in/ada-lovelace", "ada-lovelace"},
		{"trailing slash", "https://www.linkedin.com/in/ada-lovelace/", "ada-lovelace"},
		{"query params", "https://www.linkedin.com/in/ada-lovelace?originalSubdomain=uk", "ada-lovelace"},
		{"fragment", "https://www.linkedin.com/in/ada-lovelace#about", "ada-lovelace"},
		{"no scheme", "www.linkedin.com/in/ada-lovelace", "ada-lovelace"},
		{"locale subdomain", "https://uk.linkedin.com/in/ada-lovelace", "ada-lovelace"},
		{"mobile subdomain", "https://m.linkedin.com/in/ada-lovelace", "ada-lovelace"},
		{"uppercase host", "https://WWW.LinkedIn.com/in/ada-lovelace", "ada-lovelace"},
		{"http scheme", "http://www.linkedin.com/in/ada-lovelace", "ada-lovelace"},
		{"surrounding spaces", "  https://www.linkedin.com/in/ada-lovelace  ", "ada-lovelace"},
		{"percent encoded", "https://www.linkedin.com/in/ada%2Dlovelace", "ada%2Dlovelace"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ref, err := Parse(tc.in)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ref.PublicID != tc.wantID {
				t.Errorf("public id = %q, want %q", ref.PublicID, tc.wantID)
			}
			if ref.CanonicalURL != "https://www.linkedin.com/in/"+tc.wantID {
				t.Errorf("canonical = %q", ref.CanonicalURL)
			}
		})
	}
}

func TestParseInvalid(t *testing.T) {
	cases := []struct{ name, in string }{
		{"empty", ""},
		{"whitespace", "   "},
		{"wrong host", "https://example.com/in/ada"},
		{"lookalike suffix", "https://linkedin.com.evil.com/in/ada"},
		{"lookalike prefix", "https://evil-linkedin.com/in/ada"},
		{"company path", "https://www.linkedin.com/company/foo"},
		{"root path", "https://www.linkedin.com/"},
		{"missing slug", "https://www.linkedin.com/in/"},
		{"extra segments", "https://www.linkedin.com/in/ada/detail"},
		{"unsupported scheme", "ftp://www.linkedin.com/in/ada"},
		{"javascript scheme", "javascript:alert(1)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Parse(tc.in); err == nil {
				t.Errorf("expected an error for %q", tc.in)
			}
		})
	}
}

func TestParseTooLong(t *testing.T) {
	long := "https://www.linkedin.com/in/" + strings.Repeat("a", 600)
	if _, err := Parse(long); err == nil {
		t.Error("expected an error for an overly long url")
	}
}

func TestIsLinkedInHost(t *testing.T) {
	for _, h := range []string{"linkedin.com", "www.linkedin.com", "uk.linkedin.com", "m.linkedin.com", "www.linkedin.com:443"} {
		if !IsLinkedInHost(h) {
			t.Errorf("%q should be a valid LinkedIn host", h)
		}
	}
	for _, h := range []string{"example.com", "linkedin.com.evil.com", "evil-linkedin.com", "notlinkedin.com", ""} {
		if IsLinkedInHost(h) {
			t.Errorf("%q should not be a valid LinkedIn host", h)
		}
	}
}
