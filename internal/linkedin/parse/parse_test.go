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
	if !p.Creator || !p.TopVoice {
		t.Errorf("creator=%v top_voice=%v", p.Creator, p.TopVoice)
	}
	if p.Student || p.Memorialized {
		t.Errorf("student=%v memorialized=%v", p.Student, p.Memorialized)
	}
	if p.ProfileLanguage != "en_US" {
		t.Errorf("profile_language = %q", p.ProfileLanguage)
	}
	if p.CreatorWebsite != "https://gatesnot.es/AI" {
		t.Errorf("creator_website = %q", p.CreatorWebsite)
	}
	if len(p.Topics) != 5 || p.Topics[0] != "books" || p.Topics[1] != "climatechange" {
		t.Errorf("topics = %v", p.Topics)
	}
	if len(p.ProfilePicture.Variants) != 4 {
		t.Errorf("expected 4 profile picture variants, got %d", len(p.ProfilePicture.Variants))
	}
}

func TestProfileImageVariants(t *testing.T) {
	p, err := Profile(loadFixture(t, "dash_profile.json"), testRef())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	pic := p.ProfilePicture
	wantWidths := []int{100, 200, 400, 800}
	if len(pic.Variants) != len(wantWidths) {
		t.Fatalf("variants = %+v", pic.Variants)
	}
	for i, v := range pic.Variants {
		if v.Width != wantWidths[i] {
			t.Errorf("variant %d width = %d, want %d (ascending)", i, v.Width, wantWidths[i])
		}
		if v.Height == 0 || !strings.HasPrefix(v.URL, "https://media.licdn.com/") {
			t.Errorf("variant %d malformed: %+v", i, v)
		}
	}
	if pic.URL != pic.Variants[len(pic.Variants)-1].URL {
		t.Error("primary url should be the largest variant")
	}
	bg := p.BackgroundImage
	if bg == nil || len(bg.Variants) != 2 || bg.Variants[0].Width != 800 || bg.Variants[1].Width != 1400 {
		t.Errorf("background variants = %+v", bg)
	}
}

func TestProfileTopicsDedupAndMalformed(t *testing.T) {
	raw := []byte(`{"elements":[{"firstName":"A","creatorInfo":{"associatedHashtagUrns":[` +
		`"urn:li:fsd_hashtag:(ai,urn:li:activity:-)",` +
		`"urn:li:fsd_hashtag:(ai,urn:li:activity:-)",` +
		`"malformed","urn:li:fsd_hashtag:(cloud)",""]}}]}`)
	p, err := Profile(raw, testRef())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(p.Topics) != 2 || p.Topics[0] != "ai" || p.Topics[1] != "cloud" {
		t.Errorf("topics = %v, want [ai cloud]", p.Topics)
	}
}

func TestProfileDefensiveOptionals(t *testing.T) {
	raw := []byte(`{"elements":[{"firstName":"B","lastName":"C",` +
		`"topVoiceBadge":null,"creator":true,"primaryLocale":{"language":"fr"},` +
		`"profilePicture":{"displayImage":{"vectorImage":{"rootUrl":"https://x/","artifacts":[` +
		`{"width":0,"fileIdentifyingUrlPathSegment":"skip"},` +
		`{"width":200,"height":200,"fileIdentifyingUrlPathSegment":"seg200"},` +
		`{"width":100,"fileIdentifyingUrlPathSegment":""}]}}}}]}`)
	p, err := Profile(raw, testRef())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.TopVoice {
		t.Error("null topVoiceBadge must not set top_voice")
	}
	if p.CreatorWebsite != "" || len(p.Topics) != 0 {
		t.Error("missing creatorInfo must yield no website or topics")
	}
	if p.ProfileLanguage != "fr" {
		t.Errorf("language = %q, want fr", p.ProfileLanguage)
	}
	if p.ProfilePicture == nil || len(p.ProfilePicture.Variants) != 1 || p.ProfilePicture.Variants[0].Width != 200 {
		t.Errorf("only the usable artifact should remain: %+v", p.ProfilePicture)
	}
}

func TestHashtagName(t *testing.T) {
	cases := map[string]string{
		"urn:li:fsd_hashtag:(ai,urn:li:activity:-)": "ai",
		"urn:li:fsd_hashtag:(cloud)":                "cloud",
		"urn:li:fsd_hashtag:( spaced ,x)":           "spaced",
		"nothing":                                   "",
		"urn:li:fsd_hashtag:()":                     "",
	}
	for in, want := range cases {
		if got := hashtagName(in); got != want {
			t.Errorf("hashtagName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestProfileWrongTypeIsParseError(t *testing.T) {
	_, err := Profile([]byte(`{"elements":[{"premium":"yes"}]}`), testRef())
	de, ok := domain.AsError(err)
	if !ok || de.Code != domain.CodeUpstreamParseError {
		t.Fatalf("expected upstream_parse_error, got %v", err)
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
