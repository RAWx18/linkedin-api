// SPDX-FileCopyrightText: 2026 Ryan Madhuwala
// SPDX-License-Identifier: MIT

package parse

import (
	"os"
	"strings"
	"testing"

	"github.com/garudexlabs/linkedin-api/internal/domain"
	"github.com/garudexlabs/linkedin-api/internal/urlx"
)

func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}

func testRef() urlx.ProfileRef {
	return urlx.ProfileRef{PublicID: "williamhgates", CanonicalURL: "https://www.linkedin.com/in/williamhgates"}
}

func TestProfileFull(t *testing.T) {
	p, err := Profile(loadFixture(t, "dash_profile.json"), testRef())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if p.PublicIdentifier != "williamhgates" || p.ProfileURL != "https://www.linkedin.com/in/williamhgates" {
		t.Errorf("identity = %q / %q", p.PublicIdentifier, p.ProfileURL)
	}
	if p.FullName != "Bill Gates" {
		t.Errorf("full name = %q", p.FullName)
	}
	if p.Headline == "" {
		t.Error("headline should be present")
	}
	if p.Summary == nil || *p.Summary == "" {
		t.Error("summary should be present")
	}
	if !p.Premium || !p.Influencer || !p.Verified {
		t.Errorf("badges: premium=%v influencer=%v verified=%v", p.Premium, p.Influencer, p.Verified)
	}
	if p.Location == nil || p.Location.CountryCode != "US" {
		t.Errorf("location = %+v", p.Location)
	}
	if p.ProfilePicture == nil || !strings.Contains(p.ProfilePicture.URL, "800_800") {
		t.Errorf("expected largest profile picture, got %+v", p.ProfilePicture)
	}
	if !strings.HasPrefix(p.ProfilePicture.URL, "https://media.licdn.com/dms/image/v2/") {
		t.Errorf("profile picture url = %q", p.ProfilePicture.URL)
	}
	if p.BackgroundImage == nil {
		t.Error("background image should be present")
	}
	if len(p.Websites) != 1 || !strings.Contains(p.Websites[0].URL, "gatesnot.es") {
		t.Errorf("websites = %+v", p.Websites)
	}
}

func TestProfilePartial(t *testing.T) {
	raw := []byte(`{"elements":[{"firstName":"Grace","lastName":"Hopper"}]}`)
	p, err := Profile(raw, testRef())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.FullName != "Grace Hopper" {
		t.Errorf("full name = %q", p.FullName)
	}
	if p.Summary != nil || p.Location != nil || p.ProfilePicture != nil || p.BackgroundImage != nil {
		t.Errorf("optional fields should be absent: %+v", p)
	}
	if len(p.Websites) != 0 || p.Premium || p.Verified {
		t.Errorf("unexpected populated fields: %+v", p)
	}
}

func TestProfileNotFound(t *testing.T) {
	_, err := Profile([]byte(`{"elements":[]}`), testRef())
	de, ok := domain.AsError(err)
	if !ok || de.Code != domain.CodeProfileNotFound {
		t.Fatalf("expected profile_not_found, got %v", err)
	}
}

func TestProfileMalformed(t *testing.T) {
	_, err := Profile([]byte("not json"), testRef())
	de, ok := domain.AsError(err)
	if !ok || de.Code != domain.CodeUpstreamParseError {
		t.Fatalf("expected upstream_parse_error, got %v", err)
	}
}

func BenchmarkProfile(b *testing.B) {
	raw, err := os.ReadFile("testdata/dash_profile.json")
	if err != nil {
		b.Fatal(err)
	}
	ref := testRef()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Profile(raw, ref); err != nil {
			b.Fatal(err)
		}
	}
}
